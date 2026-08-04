package apps

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ezssh/internal/sshhub"
)

// ProcessInfo 单个进程信息。
type ProcessInfo struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	MEM     float64 `json:"mem"`
	RSS     uint64  `json:"rss"` // KB
	VSZ     uint64  `json:"vsz"` // KB
	Start   string  `json:"start"`
	Command string  `json:"command"`
}

// 进程采集命令（单行，所有工具 GNU 与 BusyBox 均具备）：
//   - ps -o pid,ppid,user,rss,vsz,args：基础信息，全部为 BusyBox 支持的关键字
//     （Alpine busybox 关闭了 FEATURE_PS_LONG，故 -o time/etime 与 pcpu 均不可用，
//     -e 为 GNU 扩展，用 2>/dev/null || 回退到无 -e 版本）
//   - awk 解析 /proc/[0-9]*/stat：取出 utime/stime/starttime，计算 CPU% 与启动时间
//   - 即使 awk 段失败，基础列表仍可用（CPU%=0）
const processCmd = `grep MemTotal /proc/meminfo; ps -eo pid,ppid,user,rss,vsz,args 2>/dev/null || ps -o pid,ppid,user,rss,vsz,args; echo ===T; awk 'BEGIN{FS=" "} FILENAME ~ /^\/proc\/[0-9]+\/stat$/ { s=$0; pos=0; while(1){ t=substr(s,pos+1); i=index(t,")"); if(i==0) break; pos=pos+i }; if(pos==0) next; rest=substr(s,pos+1); n=split(rest,f); print "T\t" $1 "\t" f[12] "\t" f[13] "\t" f[20] }' /proc/[0-9]*/stat /dev/null 2>/dev/null; echo ===U; awk '{print "UPTIME\t" $1}' /proc/uptime`

// cpuSample 某进程两次采样间的 CPU 累计 ticks。
type cpuSample struct {
	ticks uint64
	at    time.Time
}

// ProcessManager 进程列表查询与 kill。
type ProcessManager struct {
	hub  *sshhub.Hub
	mu   sync.Mutex
	prev map[string]map[int]cpuSample
}

func NewProcessManager(hub *sshhub.Hub) *ProcessManager {
	return &ProcessManager{hub: hub, prev: make(map[string]map[int]cpuSample)}
}

// List 返回进程列表（按 CPU 降序）。
func (m *ProcessManager) List(hostID string) ([]ProcessInfo, error) {
	platform, err := m.hub.Platform(hostID)
	if err != nil {
		platform = "linux"
	}
	if platform == "windows" {
		return m.listWindows(hostID)
	}
	return m.listLinux(hostID)
}

func (m *ProcessManager) listLinux(hostID string) ([]ProcessInfo, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	var stderrBuf bytes.Buffer
	sess.Stderr = &stderrBuf
	out, err := sess.Output(processCmd)
	if err != nil && len(out) == 0 {
		if msg := strings.TrimSpace(stderrBuf.String()); msg != "" {
			return nil, fmt.Errorf("%w (stderr: %s)", err, msg)
		}
		return nil, err
	}

	raws, memTotal, uptime := parseProcessOutput(string(out))
	now := time.Now()
	boot := 0.0
	if uptime > 0 {
		boot = float64(now.Unix()) - uptime
	}

	hostPrev := m.prevFor(hostID)
	curSet := make(map[int]bool, len(raws))
	procs := make([]ProcessInfo, 0, len(raws))
	for _, r := range raws {
		p := ProcessInfo{
			PID:     r.pid,
			PPID:    r.ppid,
			User:    r.user,
			RSS:     r.rss,
			VSZ:     r.vsz,
			Command: r.command,
		}
		curSet[p.PID] = true

		// CPU：两次采样 utime+stime 差值 / 采样间隔（ticks 为 1/100 秒）
		if prev, ok := hostPrev[p.PID]; ok && prev.ticks <= r.ticks {
			if dt := now.Sub(prev.at).Seconds(); dt > 0 {
				p.CPU = float64(r.ticks-prev.ticks) / dt
			}
		}
		hostPrev[p.PID] = cpuSample{ticks: r.ticks, at: now}

		if memTotal > 0 {
			p.MEM = float64(p.RSS) * 1024 / float64(memTotal) * 100
		}
		if boot > 0 && r.startTicks > 0 {
			start := time.Unix(int64(boot+float64(r.startTicks)/100), 0)
			p.Start = start.Format("Mon Jan _2 15:04:05 2006")
		}
		procs = append(procs, p)
	}
	// 清理已退出进程的样本
	for pid := range hostPrev {
		if !curSet[pid] {
			delete(hostPrev, pid)
		}
	}

	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPU != procs[j].CPU {
			return procs[i].CPU > procs[j].CPU
		}
		return procs[i].PID < procs[j].PID
	})
	return procs, nil
}

// Kill 结束进程。signal=15 TERM，signal=9 KILL。
func (m *ProcessManager) Kill(hostID string, pid int, signal int) error {
	platform, err := m.hub.Platform(hostID)
	if err != nil {
		platform = "linux"
	}
	if platform == "windows" {
		return m.killWindows(hostID, pid, signal)
	}

	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sig := "TERM"
	if signal == 9 {
		sig = "KILL"
	}
	return sess.Run("kill -" + sig + " " + strconv.Itoa(pid))
}

func (m *ProcessManager) killWindows(hostID string, pid int, signal int) error {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	force := ""
	if signal == 9 {
		force = " -Force"
	}
	return sess.Run(winPS("Stop-Process -Id " + strconv.Itoa(pid) + force + " -ErrorAction SilentlyContinue"))
}

// ---- Windows 进程 ----

// winProcScript 采集 Windows 进程，=== 分隔 2 段：
// 0=M（物理内存 KB）+ P（Win32_Process：PID\tPPID\tName\tWorkingSetSize\tPrivatePageCount\tCommandLine\tCreationDate(MM-dd HH:mm)）
// 1=C（Win32_PerfFormattedData_PerfProc_Process：IDProcess\tPercentProcessorTime）。
const winProcScript = "$os = Get-CimInstance Win32_OperatingSystem; Write-Output (\"M`t\" + $os.TotalVisibleMemorySize)\n" +
	"Get-CimInstance Win32_Process | ForEach-Object { $cmd = $_.CommandLine; if (-not $cmd) { $cmd = $_.Name }; Write-Output (\"P`t\" + $_.ProcessId + \"`t\" + $_.ParentProcessId + \"`t\" + ($_.Name -replace '[|]', ' ') + \"`t\" + $_.WorkingSetSize + \"`t\" + $_.PrivatePageCount + \"`t\" + ($cmd -replace '[|]', ' ') + \"`t\" + $_.CreationDate.ToString('MM-dd HH:mm')) }\n" +
	"Write-Output '==='\n" +
	"Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | ForEach-Object { Write-Output (\"C`t\" + $_.IDProcess + \"`t\" + $_.PercentProcessorTime) }"

// rawProcWin Windows 进程解析中间结构。
type rawProcWin struct {
	pid      int
	ppid     int
	name     string
	rss      uint64 // KB
	vsz      uint64 // KB
	cpuPct   float64
	start    string // MM-dd HH:mm
	command  string
}

// parseWindowsProcesses 解析 winProcScript 输出，返回进程列表与物理内存 KB。
func parseWindowsProcesses(out string) ([]rawProcWin, uint64) {
	var memTotalKB uint64
	cpuByPid := make(map[int]float64)
	var procs []rawProcWin
	section := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "===" {
			section = 1
			continue
		}
		f := strings.Split(line, "\t")
		switch section {
		case 0:
			if len(f) >= 2 && f[0] == "M" {
				memTotalKB, _ = strconv.ParseUint(f[1], 10, 64)
				continue
			}
			if len(f) >= 8 && f[0] == "P" {
				pid, _ := strconv.Atoi(f[1])
				ppid, _ := strconv.Atoi(f[2])
				rss, _ := strconv.ParseUint(f[4], 10, 64)
				vsz, _ := strconv.ParseUint(f[5], 10, 64)
				// winProcScript 已把 CreationDate 格式化为 "MM-dd HH:mm"；
				// 兼容旧的 CIM datetime 夹具：解析成功则重排为 "MM-dd HH:mm"。
				start := f[7]
				if boot, err := parseWmiTime(f[7]); err == nil {
					start = boot.Format("01-02 15:04")
				}
				procs = append(procs, rawProcWin{
					pid: pid, ppid: ppid, name: f[3],
					rss: rss / 1024, vsz: vsz / 1024,
					start: start, command: f[6],
				})
			}
		case 1:
			if len(f) >= 3 && f[0] == "C" {
				if pid, err := strconv.Atoi(f[1]); err == nil {
					cpuByPid[pid], _ = strconv.ParseFloat(f[2], 64)
				}
			}
		}
	}
	for i := range procs {
		procs[i].cpuPct = cpuByPid[procs[i].pid]
	}
	return procs, memTotalKB
}

func (m *ProcessManager) listWindows(hostID string) ([]ProcessInfo, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(winPS(winProcScript))
	if err != nil {
		return nil, err
	}
	raws, memTotalKB := parseWindowsProcesses(string(out))
	procs := make([]ProcessInfo, 0, len(raws))
	for _, r := range raws {
		p := ProcessInfo{
			PID:     r.pid,
			PPID:    r.ppid,
			RSS:     r.rss,
			VSZ:     r.vsz,
			Start:   r.start,
			Command: r.command,
			CPU:     r.cpuPct, // PerfProc 瞬时值，可超 100（等同任务管理器语义）
		}
		if memTotalKB > 0 {
			p.MEM = float64(r.rss) * 1024 / float64(memTotalKB*1024) * 100
		}
		procs = append(procs, p)
	}
	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPU != procs[j].CPU {
			return procs[i].CPU > procs[j].CPU
		}
		return procs[i].PID < procs[j].PID
	})
	return procs, nil
}

func (m *ProcessManager) prevFor(hostID string) map[int]cpuSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	mp, ok := m.prev[hostID]
	if !ok {
		mp = make(map[int]cpuSample)
		m.prev[hostID] = mp
	}
	return mp
}

// rawProc 解析后的中间结构。
type rawProc struct {
	pid        int
	ppid       int
	user       string
	rss        uint64 // KB
	vsz        uint64 // KB
	ticks      uint64 // utime + stime
	startTicks uint64
	command    string
}

// parseProcessOutput 解析采集命令输出。
// 三段：基础信息（meminfo + ps）→ ===T（awk 的 utime/stime/starttime）→ ===U（uptime）。
func parseProcessOutput(out string) ([]rawProc, uint64, float64) {
	var memTotal uint64
	var uptime float64
	section := 0
	byPid := make(map[int]*rawProc)

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "===T" {
			section = 1
			continue
		}
		if line == "===U" {
			section = 2
			continue
		}
		switch section {
		case 0:
			if strings.HasPrefix(line, "MemTotal:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					if v, err := strconv.ParseUint(f[1], 10, 64); err == nil {
						memTotal = v * 1024
					}
				}
				continue
			}
			// ps 基础行：PID PPID USER RSS VSZ ARGS
			f := strings.Fields(line)
			if len(f) < 6 {
				continue
			}
			pid, err := strconv.Atoi(f[0])
			if err != nil {
				continue // 跳过表头
			}
			ppid, _ := strconv.Atoi(f[1])
			rss, _ := strconv.ParseUint(f[3], 10, 64)
			vsz, _ := strconv.ParseUint(f[4], 10, 64)
			byPid[pid] = &rawProc{
				pid: pid, ppid: ppid, user: f[2],
				rss: rss, vsz: vsz,
				command: strings.Join(f[5:], " "),
			}
		case 1:
			// awk 时间行：T\t<pid>\t<utime>\t<stime>\t<starttime>
			f := strings.Split(line, "\t")
			if len(f) < 5 || f[0] != "T" {
				continue
			}
			pid, err := strconv.Atoi(f[1])
			if err != nil {
				continue
			}
			if rp, ok := byPid[pid]; ok {
				utime, _ := strconv.ParseUint(f[2], 10, 64)
				stime, _ := strconv.ParseUint(f[3], 10, 64)
				rp.ticks = utime + stime
				rp.startTicks, _ = strconv.ParseUint(f[4], 10, 64)
			}
		case 2:
			// UPTIME\t<秒>
			if strings.HasPrefix(line, "UPTIME\t") {
				uptime, _ = strconv.ParseFloat(line[len("UPTIME\t"):], 64)
			}
		}
	}

	procs := make([]rawProc, 0, len(byPid))
	for _, rp := range byPid {
		procs = append(procs, *rp)
	}
	return procs, memTotal, uptime
}
