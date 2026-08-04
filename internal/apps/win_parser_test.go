package apps

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestParseWindowsRaw 覆盖 winMonitorScript 的 4 段输出解析。
func TestParseWindowsRaw(t *testing.T) {
	out := "M\t16760832\t4194304\t19259392\t8388608\n" +
		"===\n" +
		"C\t_Total\t12.5\n" +
		"C\t0\t2.1\n" +
		"C\t1\t22.9\n" +
		"===\n" +
		"D\tC:\t107374182400\t42949672960\n" +
		"D\tD:\t214748364800\t107374182400\n" +
		"===\n" +
		"N\t以太网\t123456\t65432\n" +
		"N\tLoopback Pseudo-Interface 1\t0\t0\n"

	wr := parseWindowsRaw(out)
	if wr.memTotalKB != 16760832 || wr.memFreeKB != 4194304 {
		t.Fatalf("mem: %+v", wr)
	}
	if wr.vmTotalKB != 19259392 || wr.vmFreeKB != 8388608 {
		t.Fatalf("vm: %+v", wr)
	}
	if wr.cpuTotal != 12.5 {
		t.Fatalf("cpu total: %v", wr.cpuTotal)
	}
	if len(wr.disks) != 2 {
		t.Fatalf("disks: %+v", wr.disks)
	}
	if wr.disks[0].Mount != "C:" || wr.disks[0].Total != 107374182400 || wr.disks[0].Used != 64424509440 {
		t.Fatalf("disk0: %+v", wr.disks[0])
	}
	if p := wr.disks[0].Pct; p < 59.99 || p > 60.01 {
		t.Fatalf("disk0 pct: %v", p)
	}
	// loopback 由 Where-Object 过滤，脚本不会输出；解析器遇非 4 段也应跳过
	if n, ok := wr.net["以太网"]; !ok || n.rxPerSec != 123456 || n.txPerSec != 65432 {
		t.Fatalf("net: %+v", wr.net)
	}
	if len(wr.net) != 1 {
		t.Fatalf("expected 1 net iface, got %+v", wr.net)
	}
}

// TestBuildWindowsSnapshot 验证内存/swap 换算。
func TestBuildWindowsSnapshot(t *testing.T) {
	cur := &winRaw{
		memTotalKB: 1000, memFreeKB: 300,
		vmTotalKB: 2000, vmFreeKB: 700,
		cpuTotal: 25.5,
		disks:    []DiskStat{{Mount: "C:", Total: 100, Used: 50, Pct: 50}},
	}
	snap := buildWindowsSnapshot(cur)
	if snap.MemTotal != 1000*1024 || snap.MemUsed != 700*1024 {
		t.Fatalf("mem: %+v", snap)
	}
	if snap.SwapTotal != 1000*1024 || snap.SwapUsed != 600*1024 {
		// swap_total = 2000-1000=1000KB；swap_used = 1000-(700-300)=600KB
		t.Fatalf("swap: %+v", snap)
	}
	if snap.CPU != 25.5 {
		t.Fatalf("cpu: %v", snap.CPU)
	}
}

// TestParseWindowsHardwareInfo 覆盖 winHwScript 解析：UPTIME 秒数、HypervisorPresent 虚拟化探测。
func TestParseWindowsHardwareInfo(t *testing.T) {
	out := "CS\tTencent\tStandard PC (Q35 + ICH9, 2009)\tTrue\n" +
		"===\n" +
		"CPU\tIntel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz\t8\n" +
		"===\n" +
		"OS\tMicrosoft Windows Server 2022\t10.0.20348\n" +
		"===\n" +
		"WIN-VM01\n" +
		"===\n" +
		"UPTIME\t123456.789\n"

	hi := parseWindowsHardwareInfo(out)
	if hi.OS != "Microsoft Windows Server 2022 (10.0.20348)" {
		t.Fatalf("os: %q", hi.OS)
	}
	if hi.Distro != "windows" || hi.DistroName != "Microsoft Windows Server 2022" {
		t.Fatalf("distro: %+v", hi)
	}
	if hi.CPUModel != "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz" || hi.CPUCores != 8 {
		t.Fatalf("cpu: %+v", hi)
	}
	if hi.Hostname != "WIN-VM01" {
		t.Fatalf("hostname: %q", hi.Hostname)
	}
	if hi.Uptime != 123456 {
		t.Fatalf("uptime: got %d, want 123456", hi.Uptime)
	}
	if hi.VM != true || hi.Hypervisor != "qemu/kvm" {
		t.Fatalf("vm: %+v", hi)
	}
}

// TestParseWindowsHardwareInfo_Hyperv 微软虚拟机：DMI 匹配 hyperv。
func TestParseWindowsHardwareInfo_Hyperv(t *testing.T) {
	out := "CS\tMicrosoft Corporation\tVirtual Machine\tTrue\n" +
		"===\n" +
		"CPU\tIntel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz\t8\n" +
		"===\n" +
		"OS\tMicrosoft Windows 11 Pro\t10.0.22631\n" +
		"===\n" +
		"WIN-VM02\n" +
		"===\n" +
		"UPTIME\t999.5\n"

	hi := parseWindowsHardwareInfo(out)
	if !hi.VM || hi.Hypervisor != "hyperv" {
		t.Fatalf("vm: %+v", hi)
	}
}

// TestParseWindowsHardwareInfo_HPTrue_NoDMI CPUID 探测到虚拟化但 DMI 无特征串 → 通用 hypervisor 名。
func TestParseWindowsHardwareInfo_HPTrue_NoDMI(t *testing.T) {
	out := "CS\tUnknown Vendor\tCustom Model\tTrue\n" +
		"===\n" +
		"CPU\tIntel(R) Xeon(R) Platinum 8269CY CPU @ 2.50GHz\t2\n" +
		"===\n" +
		"OS\tMicrosoft Windows Server 2019\t10.0.17763\n" +
		"===\n" +
		"WIN-VM03\n" +
		"===\n" +
		"UPTIME\t3600\n"

	hi := parseWindowsHardwareInfo(out)
	if !hi.VM || hi.Hypervisor != "hypervisor" {
		t.Fatalf("vm: %+v", hi)
	}
}

// TestParseWindowsHardwareInfo_Physical HypervisorPresent=False 且 DMI 无特征串 → 物理机。
func TestParseWindowsHardwareInfo_Physical(t *testing.T) {
	out := "CS\tDell Inc.\tPowerEdge R740\tFalse\n" +
		"===\n" +
		"CPU\tIntel(R) Xeon(R) Gold 6230R CPU @ 2.10GHz\t32\n" +
		"===\n" +
		"OS\tMicrosoft Windows Server 2022\t10.0.20348\n" +
		"===\n" +
		"WIN-PM01\n" +
		"===\n" +
		"UPTIME\t86400\n"

	hi := parseWindowsHardwareInfo(out)
	if hi.VM {
		t.Fatalf("expected VM=false, got %+v", hi)
	}
	if hi.Hypervisor != "" {
		t.Fatalf("expected empty hypervisor, got %q", hi.Hypervisor)
	}
	if hi.Uptime != 86400 {
		t.Fatalf("uptime: got %d", hi.Uptime)
	}
}

// TestParseWmiTime 验证 CIM datetime 解析（进程启动时间仍走该函数）。
func TestParseWmiTime(t *testing.T) {
	if boot, err := parseWmiTime("20260803091234.500000+480"); err != nil {
		t.Fatalf("parseWmiTime: %v", err)
	} else if boot.Hour() != 9 || boot.Minute() != 12 || boot.Second() != 34 {
		t.Fatalf("wmi time: %v", boot)
	}
}

// TestParseWindowsProcesses 覆盖 winProcScript 的 P/C 段与内存换算。
func TestParseWindowsProcesses(t *testing.T) {
	out := "M\t16760832\n" +
		"P\t4\t0\tSystem\t2097152\t32768\t\t20260801010000.000000+480\n" +
		"P\t1234\t4\texplorer.exe\t209715200\t157286400\tC:\\Windows\\explorer.exe\t20260803080000.000000+480\n" +
		"P\t5678\t1234\ttest|service\t1048576\t8388608\t\"C:\\svc\\test.service.exe\"\t20260803090000.000000+480\n" +
		"===\n" +
		"C\t4\t0.0\n" +
		"C\t1234\t2.5\n" +
		"C\t5678\t1.25\n"

	procs, memTotalKB := parseWindowsProcesses(out)
	if memTotalKB != 16760832 {
		t.Fatalf("memTotalKB: %v", memTotalKB)
	}
	if len(procs) != 3 {
		t.Fatalf("procs: %d %+v", len(procs), procs)
	}
	explorer := procs[1]
	if explorer.pid != 1234 || explorer.ppid != 4 || explorer.name != "explorer.exe" {
		t.Fatalf("explorer: %+v", explorer)
	}
	// WorkingSetSize/PrivatePageCount 字节 → KB
	if explorer.rss != 209715200/1024 || explorer.vsz != 157286400/1024 {
		t.Fatalf("rss/vsz: %+v", explorer)
	}
	if explorer.cpuPct != 2.5 {
		t.Fatalf("cpu: %+v", explorer)
	}
	if !strings.HasPrefix(explorer.start, "08-03") {
		t.Fatalf("start: %q", explorer.start)
	}
	if procs[0].name != "System" || procs[0].command != "" {
		t.Fatalf("system: %+v", procs[0])
	}
	// 管道符被替换为空格，CommandLine 引号保留
	if procs[2].command != "\"C:\\svc\\test.service.exe\"" {
		t.Fatalf("cmdline: %q", procs[2].command)
	}
}

// TestWinPSEncodedCommand 验证 winPS 输出可逆（base64 解码为 UTF-16LE 原文）。
func TestWinPSEncodedCommand(t *testing.T) {
	script := "Get-CimInstance Win32_OperatingSystem; Write-Output \"hi 'quoted'\""
	cmd := winPS(script)
	if !strings.HasPrefix(cmd, "powershell -NoProfile -NonInteractive -EncodedCommand ") {
		t.Fatalf("bad prefix: %q", cmd)
	}
	b64 := strings.TrimPrefix(cmd, "powershell -NoProfile -NonInteractive -EncodedCommand ")
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	if got := string(utf16.Decode(u)); got != script {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}
