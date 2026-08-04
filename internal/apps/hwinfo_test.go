package apps

import "testing"

// TestParseHardwareInfo_KVM 解析 KVM 虚拟机采集结果（x86_64，含 DMI）。
func TestParseHardwareInfo_KVM(t *testing.T) {
	content := `Linux 5.15.0-94-generic x86_64
===
processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model	: 85
model name	: Intel(R) Xeon(R) Platinum 8269CY CPU @ 2.50GHz
stepping	: 4
cpu MHz		: 2499.998
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch cpuid_fault invpcid_single pti ssbd ibrs ibpb stibp fsgsbase bmi1 avx2 smep bmi2 erms invpcid avx512f avx512dq rdseed adx smap avx512ifma clflushopt clwb intel_pt avx512cd sha_ni avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves arat avx512_vnni md_clear arch_capabilities
processor	: 1
model name	: Intel(R) Xeon(R) Platinum 8269CY CPU @ 2.50GHz
flags		: fpu ... hypervisor lahf_lm
===
KVM
===
QEMU
`
	hi := parseHardwareInfo(content)
	if hi.OS != "Linux 5.15.0-94-generic x86_64" {
		t.Errorf("OS: got %q", hi.OS)
	}
	if hi.CPUModel != "Intel(R) Xeon(R) Platinum 8269CY CPU @ 2.50GHz" {
		t.Errorf("CPUModel: got %q", hi.CPUModel)
	}
	if hi.CPUCores != 2 {
		t.Errorf("CPUCores: got %d, want 2", hi.CPUCores)
	}
	if !hi.VM {
		t.Error("expected VM=true")
	}
	if hi.Hypervisor != "qemu/kvm" {
		t.Errorf("Hypervisor: got %q, want qemu/kvm", hi.Hypervisor)
	}
	if hi.ProductName != "KVM" || hi.Vendor != "QEMU" {
		t.Errorf("DMI: got %q / %q", hi.ProductName, hi.Vendor)
	}
}

// TestParseHardwareInfo_BareMetal 解析物理机结果（无 hypervisor 标志、DMI 无特征串）。
func TestParseHardwareInfo_BareMetal(t *testing.T) {
	content := `Linux 4.18.0-513.el8.x86_64 x86_64
===
processor	: 0
model name	: Intel(R) Xeon(R) Gold 6230R CPU @ 2.10GHz
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc cpuid aperfmperf pni pclmulqdq dtes64 monitor ds_cpl smx est tm2 ssse3 sdbg fma cx16 xtpr pdcm pcid dca sse4_1 sse4_2 x2apic movbe popcnt aes xsave osxsave avx f16c rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb cat_l3 cdp_l3 invpcid_single intel_ppin tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 hle avx2 smep bmi2 erms invpcid rtm cqm mpx rdt_a avx512f avx512dq rdseed adx smap clflushopt clwb intel_pt avx512cd avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves cqm_llc cqm_occup_llc cqm_mbm_total cqm_mbm_local dtherm ida arat pln pts hwp hwp_act_window hwp_epp pku ospke avx512_vnni md_clear flush_l1d arch_capabilities
===
PowerEdge R740
===
Dell Inc.
`
	hi := parseHardwareInfo(content)
	if hi.OS != "Linux 4.18.0-513.el8.x86_64 x86_64" {
		t.Errorf("OS: got %q", hi.OS)
	}
	if hi.VM {
		t.Error("expected VM=false")
	}
	if hi.Hypervisor != "" {
		t.Errorf("Hypervisor: got %q, want empty", hi.Hypervisor)
	}
	if hi.CPUCores != 1 {
		t.Errorf("CPUCores: got %d, want 1", hi.CPUCores)
	}
}

// TestParseHardwareInfo_NoDMI 容器内 DMI 不可读：靠 hypervisor 标志兜底。
func TestParseHardwareInfo_NoDMI(t *testing.T) {
	content := `Linux 6.1.0-18-amd64 x86_64
===
processor	: 0
model name	: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz
flags		: fpu ... hypervisor lahf_lm
===
===
`
	hi := parseHardwareInfo(content)
	if !hi.VM {
		t.Error("expected VM=true")
	}
	if hi.Hypervisor != "hypervisor" {
		t.Errorf("Hypervisor: got %q, want hypervisor", hi.Hypervisor)
	}
}

// TestParseHardwareInfo_Distro 解析发行版信息：ID / PRETTY_NAME / hostname / uptime。
func TestParseHardwareInfo_Distro(t *testing.T) {
	content := `Linux 5.15.0-91-generic x86_64
===
processor	: 0
model name	: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx
===
===
===
PRETTY_NAME="Ubuntu 22.04.3 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
VERSION_CODENAME=jammy
ID=ubuntu
ID_LIKE=debian
===
my-host
===
12345.67 23456.78
`
	hi := parseHardwareInfo(content)
	if hi.Distro != "ubuntu" {
		t.Errorf("Distro: got %q, want ubuntu", hi.Distro)
	}
	if hi.DistroName != "Ubuntu 22.04.3 LTS" {
		t.Errorf("DistroName: got %q", hi.DistroName)
	}
	if hi.Hostname != "my-host" {
		t.Errorf("Hostname: got %q", hi.Hostname)
	}
	if hi.Uptime != 12345 {
		t.Errorf("Uptime: got %d, want 12345", hi.Uptime)
	}
	if hi.CPUModel != "Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz" {
		t.Errorf("CPUModel: got %q", hi.CPUModel)
	}
}

// TestParseHardwareInfo_Alpine Alpine 的 os-release 不带引号、没有 DMI。
func TestParseHardwareInfo_Alpine(t *testing.T) {
	content := `Linux 6.6.49-0-virt x86_64
===
processor	: 0
model name	: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss syscall nx
===
===
===
NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.19.1
PRETTY_NAME="Alpine Linux v3.19"
HOME_URL="https://alpinelinux.org/"
BUG_REPORT_URL="https://gitlab.alpinelinux.org/alpine/aports/-/issues"
===
alpine-vm
===
9999.00 11111.00
`
	hi := parseHardwareInfo(content)
	if hi.Distro != "alpine" {
		t.Errorf("Distro: got %q, want alpine", hi.Distro)
	}
	if hi.DistroName != "Alpine Linux v3.19" {
		t.Errorf("DistroName: got %q", hi.DistroName)
	}
	if hi.Hostname != "alpine-vm" {
		t.Errorf("Hostname: got %q", hi.Hostname)
	}
	if hi.Uptime != 9999 {
		t.Errorf("Uptime: got %d, want 9999", hi.Uptime)
	}
}
