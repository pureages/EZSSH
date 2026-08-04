package apps

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"ezssh/internal/sshhub"
)

// Snapshot 一次采集结果（对前端暴露）。
type Snapshot struct {
	TS        int64      `json:"ts"`
	Error     string     `json:"error,omitempty"`
	CPU       float64    `json:"cpu"`
	CPUPer    []float64  `json:"cpu_per"`
	Load1     float64    `json:"load1"`
	Load5     float64    `json:"load5"`
	Load15    float64    `json:"load15"`
	MemTotal  uint64     `json:"mem_total"`
	MemUsed   uint64     `json:"mem_used"`
	MemPct    float64    `json:"mem_pct"`
	SwapTotal uint64     `json:"swap_total"`
	SwapUsed  uint64     `json:"swap_used"`
	SwapPct   float64    `json:"swap_pct"`
	Disks     []DiskStat `json:"disks"`
	Net       []NetStat  `json:"net"`
}

type DiskStat struct {
	Mount string  `json:"mount"`
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Pct   float64 `json:"pct"`
}

type NetStat struct {
	Iface string  `json:"iface"`
	RxBps float64 `json:"rx_bps"`
	TxBps float64 `json:"tx_bps"`
	// 接口累计流量字节（/proc/net/dev 原始计数），用于"总上传/总下载"
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// df -Pm 采用 POSIX 输出格式：设备名过长不会换行、字段顺序固定，GNU 与 BusyBox 一致。
const monitorCmd = `cat /proc/stat; echo ===; cat /proc/meminfo; echo ===; cat /proc/loadavg; echo ===; df -Pm; echo ===; cat /proc/net/dev`

// Monitor 按主机维护采集协程与订阅者。
type Monitor struct {
	hub     *sshhub.Hub
	mu      sync.Mutex
	hosts   map[string]*hostMonitor
	onData  func(hostID string, snap Snapshot)
}

type hostMonitor struct {
	cancel context.CancelFunc
}

func NewMonitor(hub *sshhub.Hub) *Monitor {
	return &Monitor{hub: hub, hosts: make(map[string]*hostMonitor)}
}

func (m *Monitor) SetOnData(fn func(hostID string, snap Snapshot)) {
	m.mu.Lock()
	m.onData = fn
	m.mu.Unlock()
}

// Ensure 确保某主机的采集协程在运行。
func (m *Monitor) Ensure(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.hosts[hostID]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.hosts[hostID] = &hostMonitor{cancel: cancel}
	go m.loop(ctx, hostID)
}

// Stop 停止某主机的采集（订阅者清零时调用）。
func (m *Monitor) Stop(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hm, ok := m.hosts[hostID]; ok {
		hm.cancel()
		delete(m.hosts, hostID)
	}
}

func (m *Monitor) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, hm := range m.hosts {
		hm.cancel()
	}
	m.hosts = make(map[string]*hostMonitor)
}

func (m *Monitor) loop(ctx context.Context, hostID string) {
	first := true
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var prev *rawData
	var prevWin *winRaw
	prevTime := time.Now()
	interval := 2.0
	// 连续失败计数：仅在进入失败态时向前端推送一次错误快照，成功后清空
	lastErr := ""

	pushErr := func(t time.Time, err error) {
		// 采集失败，保留上一个快照间隔
		prevTime = t
		if lastErr != "" {
			return
		}
		lastErr = err.Error()
		m.mu.Lock()
		fn := m.onData
		m.mu.Unlock()
		if fn != nil {
			// 错误快照携带空数组，避免前端对 nil net/disks 崩溃
			fn(hostID, Snapshot{
				TS:      time.Now().Unix(),
				Error:   lastErr,
				CPUPer:  []float64{},
				Disks:   []DiskStat{},
				Net:     []NetStat{},
			})
		}
	}
	pushSnap := func(snap Snapshot) {
		m.mu.Lock()
		fn := m.onData
		m.mu.Unlock()
		if fn != nil {
			fn(hostID, snap)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			platform, _ := m.hub.Platform(hostID)
			if platform == "windows" {
				raw, err := m.collectWindows(hostID)
				if err != nil {
					pushErr(t, err)
					continue
				}
				lastErr = ""
				cur := parseWindowsRaw(raw)
				interval = t.Sub(prevTime).Seconds()
				if interval <= 0 {
					interval = 2
				}
				// 网络累计流量：rate*interval 叠加到上一轮累计值上（Win32_Perf* 为瞬时 gauge）
				if prevWin != nil {
					for name, n := range cur.net {
						if p, ok := prevWin.net[name]; ok {
							n.rxBytes = p.rxBytes + uint64(float64(n.rxPerSec)*interval)
							n.txBytes = p.txBytes + uint64(float64(n.txPerSec)*interval)
							cur.net[name] = n
						}
					}
				}
				snap := buildWindowsSnapshot(cur)
				prevWin = cur
				prevTime = t
				first = false
				pushSnap(snap)
				continue
			}

			raw, err := m.collect(hostID)
			if err != nil {
				pushErr(t, err)
				continue
			}
			lastErr = ""
			cur := parseRaw(raw)
			interval = t.Sub(prevTime).Seconds()
			if interval <= 0 {
				interval = 2
			}
			snap := buildSnapshot(prev, cur, first, interval)
			prev = cur
			prevTime = t
			first = false
			pushSnap(snap)
		}
	}
}

func (m *Monitor) collect(hostID string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(monitorCmd)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---- 解析 ----

type cpuState struct {
	total uint64
	idle  uint64
	user  uint64
	sys   uint64
}

type rawData struct {
	cpu     cpuState
	cpuPer  map[string]cpuState
	mem     map[string]uint64
	load    [3]float64
	disks   []DiskStat
	net     map[string][2]uint64 // iface -> [rx,tx]
	hasPrev bool
}

func parseRaw(out string) *rawData {
	rd := &rawData{
		cpuPer: make(map[string]cpuState),
		mem:    make(map[string]uint64),
		net:    make(map[string][2]uint64),
	}
	section := 0 // 0=stat 1=meminfo 2=load 3=df 4=net
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "===" {
			section++
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case 0:
			if strings.HasPrefix(line, "cpu") {
				parseCPULine(line, rd)
			}
		case 1:
			if i := strings.Index(line, ":"); i > 0 {
				key := strings.TrimSpace(line[:i])
				val, _ := strconv.ParseUint(strings.Fields(line[i+1:])[0], 10, 64)
				rd.mem[key] = val
			}
		case 2:
			f := strings.Fields(line)
			for i := 0; i < len(f) && i < 3; i++ {
				rd.load[i], _ = strconv.ParseFloat(f[i], 64)
			}
		case 3:
			// df -Pm：设备列可能是 /dev/xxx、overlay、tmpfs 等（Alpine 常见），
			// 用挂载点列（f[5]，必以 / 开头）来过滤数据行，兼容各种发行版。
			if f := strings.Fields(line); len(f) >= 6 && strings.HasPrefix(f[5], "/") {
				// df -Pm 单位为 MB，需乘 1024*1024 转为字节
				total, _ := strconv.ParseUint(f[1], 10, 64)
				used, _ := strconv.ParseUint(f[2], 10, 64)
				pct := 0.0
				if total > 0 {
					pct = float64(used) / float64(total) * 100
				}
				rd.disks = append(rd.disks, DiskStat{Mount: f[5], Total: total * 1024 * 1024, Used: used * 1024 * 1024, Pct: pct})
			}
		case 4:
			if i := strings.Index(line, ":"); i > 0 {
				iface := strings.TrimSpace(line[:i])
				f := strings.Fields(line[i+1:])
				if len(f) >= 10 {
					rx, _ := strconv.ParseUint(f[0], 10, 64)
					tx, _ := strconv.ParseUint(f[8], 10, 64)
					rd.net[iface] = [2]uint64{rx, tx}
				}
			}
		}
	}
	return rd
}

func parseCPULine(line string, rd *rawData) {
	f := strings.Fields(line)
	if len(f) < 8 {
		return
	}
	vals := make([]uint64, len(f)-1)
	for i, v := range f[1:] {
		vals[i], _ = strconv.ParseUint(v, 10, 64)
	}
	// user nice system idle iowait irq softirq steal
	var total, idle uint64
	for _, v := range vals {
		total += v
	}
	idle = vals[3] + vals[4] // idle + iowait
	st := cpuState{total: total, idle: idle, user: vals[0], sys: vals[2]}
	if f[0] == "cpu" {
		rd.cpu = st
	} else {
		rd.cpuPer[f[0]] = st
	}
}

func buildSnapshot(prev, cur *rawData, first bool, interval float64) Snapshot {
	snap := Snapshot{TS: time.Now().Unix(), Load1: cur.load[0], Load5: cur.load[1], Load15: cur.load[2]}

	// 内存
	snap.MemTotal = cur.mem["MemTotal"] * 1024
	memAvail := cur.mem["MemAvailable"]
	if memAvail == 0 {
		memAvail = cur.mem["MemFree"] + cur.mem["Buffers"] + cur.mem["Cached"]
	}
	if snap.MemTotal > 0 {
		snap.MemUsed = snap.MemTotal - memAvail*1024
		snap.MemPct = float64(snap.MemUsed) / float64(snap.MemTotal) * 100
	}
	snap.SwapTotal = cur.mem["SwapTotal"] * 1024
	if snap.SwapTotal > 0 {
		snap.SwapUsed = (cur.mem["SwapTotal"] - cur.mem["SwapFree"]) * 1024
		snap.SwapPct = float64(snap.SwapUsed) / float64(snap.SwapTotal) * 100
	}

	snap.Disks = cur.disks

	// CPU 使用率（两次采样差值）
	calcCPU := func(p, c cpuState) float64 {
		if c.total <= p.total {
			return 0
		}
		dt := c.total - p.total
		di := c.idle - p.idle
		return float64(dt-di) / float64(dt) * 100
	}
	if !first && prev != nil {
		snap.CPU = calcCPU(prev.cpu, cur.cpu)
		for k, cv := range cur.cpuPer {
			if pv, ok := prev.cpuPer[k]; ok {
				snap.CPUPer = append(snap.CPUPer, calcCPU(pv, cv))
			}
		}
	}

	// 网络速率与累计流量
	if !first && prev != nil {
		for k, cv := range cur.net {
			if pv, ok := prev.net[k]; ok {
				snap.Net = append(snap.Net, NetStat{
					Iface:   k,
					RxBps:   float64(cv[0]-pv[0]) / interval,
					TxBps:   float64(cv[1]-pv[1]) / interval,
					RxBytes: cv[0],
					TxBytes: cv[1],
				})
			}
		}
	}
	return snap
}

// ---- Windows 采集 ----

// winMonitorScript 采集 Windows 监控数据，=== 分隔 4 段：
// 0=M（内存，KB）1=C（CPU，Name\tPercentProcessorTime）2=D（磁盘，DeviceID\tSize\tFreeSpace）
// 3=N（网络，Name\tBytesReceivedPerSec\tBytesSentPerSec）。
// Win32_PerfFormattedData_* 为瞬时 gauge，CPU/网络无需差分。
const winMonitorScript = "$mem = Get-CimInstance Win32_OperatingSystem; Write-Output (\"M`t\" + $mem.TotalVisibleMemorySize + \"`t\" + $mem.FreePhysicalMemory + \"`t\" + $mem.TotalVirtualMemorySize + \"`t\" + $mem.FreeVirtualMemory)\n" +
	"Write-Output '==='\n" +
	"Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor | ForEach-Object { Write-Output (\"C`t\" + $_.Name + \"`t\" + $_.PercentProcessorTime) }\n" +
	"Write-Output '==='\n" +
	"Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object { Write-Output (\"D`t\" + $_.DeviceID + \"`t\" + $_.Size + \"`t\" + $_.FreeSpace) }\n" +
	"Write-Output '==='\n" +
	"Get-CimInstance Win32_PerfFormattedData_Tcpip_NetworkInterface | Where-Object { $_.Name -notlike '*loopback*' } | ForEach-Object { Write-Output (\"N`t\" + $_.Name + \"`t\" + $_.BytesReceivedPerSec + \"`t\" + $_.BytesSentPerSec) }"

// winRaw Windows 一次采集的中间结构。
type winRaw struct {
	memTotalKB uint64
	memFreeKB  uint64
	vmTotalKB  uint64
	vmFreeKB   uint64
	cpuTotal   float64 // _Total 的 PercentProcessorTime
	disks      []DiskStat
	net        map[string]netWin
}

type netWin struct {
	rxPerSec uint64
	txPerSec uint64
	rxBytes  uint64 // 累计下载字节（由 loop 叠加 rate*interval）
	txBytes  uint64 // 累计上传字节
}

func (m *Monitor) collectWindows(hostID string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(winPS(winMonitorScript))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseWindowsRaw 解析 winMonitorScript 输出。EZSSH Power By pureages
func parseWindowsRaw(out string) *winRaw {
	wr := &winRaw{net: make(map[string]netWin)}
	section := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "===" {
			section++
			continue
		}
		f := strings.Split(line, "\t")
		switch section {
		case 0:
			if len(f) >= 5 && f[0] == "M" {
				wr.memTotalKB, _ = strconv.ParseUint(f[1], 10, 64)
				wr.memFreeKB, _ = strconv.ParseUint(f[2], 10, 64)
				wr.vmTotalKB, _ = strconv.ParseUint(f[3], 10, 64)
				wr.vmFreeKB, _ = strconv.ParseUint(f[4], 10, 64)
			}
		case 1:
			if len(f) >= 3 && f[0] == "C" && f[1] == "_Total" {
				wr.cpuTotal, _ = strconv.ParseFloat(f[2], 64)
			}
		case 2:
			if len(f) >= 4 && f[0] == "D" {
				total, _ := strconv.ParseUint(f[2], 10, 64)
				free, _ := strconv.ParseUint(f[3], 10, 64)
				pct := 0.0
				if total > 0 {
					pct = float64(total-free) / float64(total) * 100
				}
				wr.disks = append(wr.disks, DiskStat{Mount: f[1], Total: total, Used: total - free, Pct: pct})
			}
		case 3:
			// 脚本已用 Where-Object 过滤 loopback；此处再防御一次（名称含 loopback 一律跳过）
			if len(f) >= 4 && f[0] == "N" && !strings.Contains(strings.ToLower(f[1]), "loopback") {
				rx, _ := strconv.ParseUint(f[2], 10, 64)
				tx, _ := strconv.ParseUint(f[3], 10, 64)
				wr.net[f[1]] = netWin{rxPerSec: rx, txPerSec: tx}
			}
		}
	}
	return wr
}

// buildWindowsSnapshot 由 winRaw 构建快照。
// swap_total = 虚拟内存总量 − 物理内存总量；≤0 时视为无 swap（前端隐藏 0 swap）。
func buildWindowsSnapshot(cur *winRaw) Snapshot {
	snap := Snapshot{TS: time.Now().Unix(), CPU: cur.cpuTotal}

	snap.MemTotal = cur.memTotalKB * 1024
	if cur.memTotalKB > 0 {
		snap.MemUsed = (cur.memTotalKB - cur.memFreeKB) * 1024
		snap.MemPct = float64(snap.MemUsed) / float64(snap.MemTotal) * 100
	}
	swapTotal := int64(cur.vmTotalKB) - int64(cur.memTotalKB)
	if swapTotal <= 0 {
		swapTotal = 0
	} else {
		snap.SwapTotal = uint64(swapTotal) * 1024
		swapUsed := swapTotal - (int64(cur.vmFreeKB) - int64(cur.memFreeKB))
		if swapUsed < 0 {
			swapUsed = 0
		}
		snap.SwapUsed = uint64(swapUsed) * 1024
		if snap.SwapTotal > 0 {
			snap.SwapPct = float64(snap.SwapUsed) / float64(snap.SwapTotal) * 100
		}
	}

	snap.Disks = cur.disks
	for name, n := range cur.net {
		snap.Net = append(snap.Net, NetStat{
			Iface:   name,
			RxBps:   float64(n.rxPerSec),
			TxBps:   float64(n.txPerSec),
			RxBytes: n.rxBytes,
			TxBytes: n.txBytes,
		})
	}
	return snap
}
