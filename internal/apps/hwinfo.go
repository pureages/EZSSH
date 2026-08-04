package apps

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// HardwareInfo 目标机硬件与系统静态信息（一次性采集，用于硬件页展示）。
type HardwareInfo struct {
	OS          string `json:"os"`          // uname -srm，如 "Linux 6.1.0-18-amd64 x86_64"
	Distro      string `json:"distro"`      // /etc/os-release 的 ID，如 ubuntu / alpine / debian
	DistroName  string `json:"distroName"`  // /etc/os-release 的 PRETTY_NAME，如 "Ubuntu 22.04.3 LTS"
	Hostname    string `json:"hostname"`    // 主机名
	Uptime      int64  `json:"uptime"`      // 系统已运行秒数（/proc/uptime）
	CPUModel    string `json:"cpuModel"`    // CPU 型号，如 "Intel(R) Xeon(R) Platinum 8269CY CPU @ 2.50GHz"
	CPUCores    int    `json:"cpuCores"`    // 逻辑核心数（/proc/cpuinfo processor 行数）
	VM          bool   `json:"vm"`          // 是否虚拟机
	Hypervisor  string `json:"hypervisor"`  // 虚拟化平台名（vmware/virtualbox/qemu/kvm/xen/hyperv…），物理机为空
	ProductName string `json:"productName"` // DMI 产品名，如 "VMware Virtual Platform"；容器内可能为空
	Vendor      string `json:"vendor"`      // DMI 厂商，如 "VMware, Inc."
}

// hwCmd 一次性采集硬件/系统静态信息。
// uname -srm 给出内核与架构；/proc/cpuinfo 给出 CPU 型号/核心数/hypervisor 标志；
// /etc/os-release 给出发行版标识与名称；DMI（/sys/class/dmi/id，容器内可能不可读）
// 给出厂商与产品名用于判断虚拟化平台。
// 注意：新采集项一律追加在命令尾部（新增 === 分隔 section），
// 避免打乱既有解析顺序与测试夹具。
const hwCmd = `uname -srm; echo ===; cat /proc/cpuinfo; echo ===; cat /sys/class/dmi/id/product_name 2>/dev/null; echo ===; cat /sys/class/dmi/id/sys_vendor 2>/dev/null; echo ===; cat /etc/os-release 2>/dev/null; echo ===; hostname; echo ===; cat /proc/uptime`

// HardwareInfo 采集目标机硬件/系统静态信息。
func (m *Monitor) HardwareInfo(hostID string) (HardwareInfo, error) {
	platform, err := m.hub.Platform(hostID)
	if err != nil {
		platform = "linux"
	}
	if platform == "windows" {
		return m.hardwareInfoWindows(hostID)
	}
	return m.hardwareInfoLinux(hostID)
}

func (m *Monitor) hardwareInfoLinux(hostID string) (HardwareInfo, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return HardwareInfo{}, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return HardwareInfo{}, err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(hwCmd)
	if err != nil {
		// 部分容器缺少 DMI/无法读 /sys，uname 与 cpuinfo 通常仍可用；
		// 命令整体失败（如 SSH 中断）才报错。
		return HardwareInfo{}, err
	}
	return parseHardwareInfo(string(out)), nil
}

// parseHardwareInfo 解析 hwCmd 的输出（section 分隔）。
// section 0=uname 1=cpuinfo 2=product_name 3=sys_vendor 4=os-release 5=hostname 6=uptime
func parseHardwareInfo(out string) HardwareInfo {
	hi := HardwareInfo{}
	flagVM := false
	section := 0
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
			if hi.OS == "" {
				hi.OS = line
			}
		case 1:
			// 兼容 Intel/AMD 的 "model name" 与部分 ARM 的 "Processor"
			if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Processor") {
				if hi.CPUModel == "" {
					if i := strings.Index(line, ":"); i >= 0 {
						hi.CPUModel = strings.TrimSpace(line[i+1:])
					}
				}
			} else if strings.HasPrefix(line, "processor") {
				hi.CPUCores++
			} else if strings.HasPrefix(line, "flags") && strings.Contains(line, "hypervisor") {
				flagVM = true
			}
		case 2:
			if hi.ProductName == "" {
				hi.ProductName = line
			}
		case 3:
			if hi.Vendor == "" {
				hi.Vendor = line
			}
		case 4:
			// /etc/os-release：ID=ubuntu / PRETTY_NAME="Ubuntu 22.04.3 LTS"（值可能带引号）
			if strings.HasPrefix(line, "ID=") {
				hi.Distro = unquoteShell(strings.TrimPrefix(line, "ID="))
			} else if strings.HasPrefix(line, "PRETTY_NAME=") {
				hi.DistroName = unquoteShell(strings.TrimPrefix(line, "PRETTY_NAME="))
			}
		case 5:
			if hi.Hostname == "" {
				hi.Hostname = line
			}
		case 6:
			// /proc/uptime 首字段为已运行秒数（浮点）
			if f := strings.Fields(line); len(f) > 0 {
				if sec, err := strconv.ParseFloat(f[0], 64); err == nil {
					hi.Uptime = int64(sec)
				}
			}
		}
	}
	hi.VM = flagVM
	if name := vmNameFromDMI(hi.ProductName, hi.Vendor); name != "" {
		hi.VM = true
		hi.Hypervisor = name
	} else if flagVM {
		// 有 hypervisor 标志但 DMI 无特征串（容器内常见）
		hi.Hypervisor = "hypervisor"
	}
	return hi
}

// unquoteShell 去掉 shell 变量赋值中可能存在的成对引号。
func unquoteShell(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// ---- Windows 硬件信息 ----

// winHwScript 采集 Windows 静态信息，=== 分隔 5 段：
// 0=CS（Manufacturer\tModel\tHypervisorPresent，后者为 CPUID 虚拟化探测位）
// 1=CPU（Name\tNumberOfLogicalProcessors）
// 2=OS（Caption\tVersion）
// 3=主机名
// 4=UPTIME（已运行秒数，由远端 PowerShell 直接换算，避免 CIM/区域化时间格式解析歧义）。
const winHwScript = "$cs = Get-CimInstance Win32_ComputerSystem; Write-Output (\"CS`t\" + $cs.Manufacturer + \"`t\" + $cs.Model + \"`t\" + $cs.HypervisorPresent)\n" +
	"Write-Output '==='\n" +
	"$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1; Write-Output (\"CPU`t\" + $cpu.Name + \"`t\" + $cpu.NumberOfLogicalProcessors)\n" +
	"Write-Output '==='\n" +
	"$os = Get-CimInstance Win32_OperatingSystem; Write-Output (\"OS`t\" + $os.Caption + \"`t\" + $os.Version)\n" +
	"Write-Output '==='\n" +
	"Write-Output $env:COMPUTERNAME\n" +
	"Write-Output '==='\n" +
	"$boot = (Get-Date) - $os.LastBootUpTime; Write-Output (\"UPTIME`t\" + $boot.TotalSeconds)"

func (m *Monitor) hardwareInfoWindows(hostID string) (HardwareInfo, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return HardwareInfo{}, err
	}
	sess, err := client.NewSession()
	if err != nil {
		return HardwareInfo{}, err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(winPS(winHwScript))
	if err != nil {
		return HardwareInfo{}, err
	}
	return parseWindowsHardwareInfo(string(out)), nil
}

// parseWindowsHardwareInfo 解析 winHwScript 输出。
func parseWindowsHardwareInfo(out string) HardwareInfo {
	hi := HardwareInfo{Distro: "windows"}
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
			if len(f) >= 3 && f[0] == "CS" {
				hi.Vendor = f[1]
				hi.ProductName = f[2]
				// HypervisorPresent：Windows 8/Server 2012+ 暴露的 CPUID 虚拟化探测位，
				// 云厂商（腾讯/阿里/KVM…）报的 DMI 厂商五花八门，用它兜底判虚拟机。
				if len(f) >= 4 && strings.EqualFold(f[3], "True") {
					hi.VM = true
				}
			}
		case 1:
			if len(f) >= 3 && f[0] == "CPU" {
				hi.CPUModel = f[1]
				hi.CPUCores, _ = strconv.Atoi(f[2])
			}
		case 2:
			if len(f) >= 3 && f[0] == "OS" {
				hi.DistroName = f[1] // Caption，如 "Microsoft Windows 11 Pro"
				hi.OS = f[1] + " (" + f[2] + ")"
			}
		case 3:
			if hi.Hostname == "" {
				hi.Hostname = line
			}
		case 4:
			// 已运行秒数直接由远端 PowerShell 换算（UPTIME\t<秒>）
			if len(f) >= 2 && f[0] == "UPTIME" {
				if sec, err := strconv.ParseFloat(f[1], 64); err == nil {
					hi.Uptime = int64(sec)
				}
			}
		}
	}
	if name := vmNameFromDMI(hi.ProductName, hi.Vendor); name != "" {
		hi.VM = true
		hi.Hypervisor = name
	} else if hi.VM {
		// CPUID 探测到 hypervisor，但 DMI 无特征串（云厂商常见），给通用名
		hi.Hypervisor = "hypervisor"
	}
	return hi
}

// parseWmiTime 解析 CIM datetime（如 "20260803091234.500000+480"）为 time.Time。
// 尾缀为 UTC 偏移分钟数（+480 = UTC+8）；无尾缀时按本地时区。
func parseWmiTime(s string) (time.Time, error) {
	if len(s) < 14 {
		return time.Time{}, errors.New("short wmi time")
	}
	year, err1 := strconv.Atoi(s[0:4])
	month, err2 := strconv.Atoi(s[4:6])
	day, err3 := strconv.Atoi(s[6:8])
	hour, err4 := strconv.Atoi(s[8:10])
	min, err5 := strconv.Atoi(s[10:12])
	sec, err6 := strconv.Atoi(s[12:14])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
		return time.Time{}, errors.New("bad wmi time")
	}
	loc := time.Local
	if len(s) >= 22 {
		off, err := strconv.Atoi(s[21:])
		if err == nil {
			loc = time.FixedZone("wmi", off*60)
		}
	}
	return time.Date(year, time.Month(month), day, hour, min, sec, 0, loc), nil
}

// vmNameFromDMI 根据 DMI 厂商/产品名判断虚拟化平台，非虚拟机返回空串。
// 覆盖常见云厂商/虚拟化平台的 SMBIOS 特征串；识别不出时返回空，交由
// CPUID 标志（Linux）或 Win32_ComputerSystem.HypervisorPresent（Windows）兜底。
func vmNameFromDMI(productName, vendor string) string {
	s := strings.ToLower(productName + " " + vendor)
	switch {
	case strings.Contains(s, "vmware"):
		return "vmware"
	case strings.Contains(s, "virtualbox"):
		return "virtualbox"
	case strings.Contains(s, "parallels"):
		return "parallels"
	case strings.Contains(s, "xen"), strings.Contains(s, "domu"):
		return "xen"
	case strings.Contains(s, "hyper-v"), strings.Contains(s, "hyperv"), strings.Contains(s, "microsoft"), strings.Contains(s, "azure"):
		return "hyperv"
	case strings.Contains(s, "oracle"), strings.Contains(s, "virtualmachine"):
		return "oracle/virtualbox"
	case strings.Contains(s, "qemu"), strings.Contains(s, "kvm"), strings.Contains(s, "bochs"),
		strings.Contains(s, "google"), strings.Contains(s, "alibaba"), strings.Contains(s, "aliyun"),
		strings.Contains(s, "tencent"), strings.Contains(s, "tcloud"), strings.Contains(s, "openstack"),
		strings.Contains(s, "nova"), strings.Contains(s, "huawei"), strings.Contains(s, "inspur"),
		strings.Contains(s, "standard pc"), strings.Contains(s, "virtual platform"),
		strings.Contains(s, "aws"), strings.Contains(s, "ec2"), strings.Contains(s, "amazon"):
		return "qemu/kvm"
	default:
		return ""
	}
}
