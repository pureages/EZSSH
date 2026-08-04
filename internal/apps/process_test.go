package apps

import (
	"strings"
	"testing"
)

func TestParseProcessOutput(t *testing.T) {
	out := "MemTotal:       2000000 kB\n" +
		"    PID  PPID USER       RSS     VSZ COMMAND\n" +
		"    1     0 root      1000    2000 /sbin/init\n" +
		"   42     1 root      3000    4000 /usr/sbin/sshd\n" +
		"   99     1 testuser  2000    3000 nginx: master process\n" +
		"===T\n" +
		"T\t1\t1000\t500\t600000\n" +
		"T\t42\t2000\t1000\t610000\n" +
		"T\t99\t3000\t1500\t620000\n" +
		"===U\n" +
		"UPTIME\t123456.50\n"

	procs, memTotal, uptime := parseProcessOutput(out)
	if len(procs) != 3 {
		t.Fatalf("procs: %d", len(procs))
	}
	if memTotal != 2000000*1024 {
		t.Fatalf("memTotal: %d", memTotal)
	}
	if uptime != 123456.50 {
		t.Fatalf("uptime: %v", uptime)
	}
	byPid := map[int]rawProc{}
	for _, p := range procs {
		byPid[p.pid] = p
	}
	sshd := byPid[42]
	if sshd.user != "root" || sshd.ppid != 1 || sshd.rss != 3000 || sshd.vsz != 4000 {
		t.Fatalf("sshd: %+v", sshd)
	}
	if sshd.command != "/usr/sbin/sshd" {
		t.Fatalf("sshd cmd: %+v", sshd)
	}
	if sshd.ticks != 3000 || sshd.startTicks != 610000 {
		t.Fatalf("sshd ticks: %+v", sshd)
	}
	nginx := byPid[99]
	if nginx.user != "testuser" || nginx.command != "nginx: master process" {
		t.Fatalf("nginx: %+v", nginx)
	}
}

func TestParseProcessOutputNoTimes(t *testing.T) {
	// awk 段失败时（无 ===T/===U），基础列表仍可用，仅 CPU/start 为 0
	out := "MemTotal:       2000000 kB\n" +
		"  1   0 root  1000  2000 /sbin/init\n" +
		" 42   1 root  3000  4000 /usr/sbin/sshd\n"
	procs, memTotal, uptime := parseProcessOutput(out)
	if len(procs) != 2 {
		t.Fatalf("procs: %d", len(procs))
	}
	if memTotal == 0 {
		t.Fatal("memTotal should be set")
	}
	if uptime != 0 {
		t.Fatalf("uptime should be 0: %v", uptime)
	}
	for _, p := range procs {
		if p.ticks != 0 || p.startTicks != 0 {
			t.Fatalf("expected zero ticks: %+v", p)
		}
	}
}

func TestParseProcessOutputCommWithParens(t *testing.T) {
	// stat 中 comm 含括号/空格时，awk 取最后一个 ) 之后的字段，Go 侧直接按 \t 切分
	out := "MemTotal:       2000000 kB\n" +
		"  7   2 root  1234  2097152 /usr/lib/foo)bar\n" +
		"===T\n" +
		"T\t7\t100\t50\t1000\n" +
		"===U\n" +
		"UPTIME\t3600.00\n"
	procs, _, uptime := parseProcessOutput(out)
	if len(procs) != 1 {
		t.Fatalf("procs: %d", len(procs))
	}
	p := procs[0]
	if p.pid != 7 || p.ppid != 2 || p.command != "/usr/lib/foo)bar" {
		t.Fatalf("proc: %+v", p)
	}
	if p.ticks != 150 || p.startTicks != 1000 {
		t.Fatalf("ticks: %+v", p)
	}
	if uptime != 3600 {
		t.Fatalf("uptime: %v", uptime)
	}
}

func TestProcessCmdUsesPortableConstructs(t *testing.T) {
	// 不得依赖 GNU 扩展（-e 已有 || 回退；pcpu/pmem/lstart/--no-headers 不用）
	for _, bad := range []string{"pcpu", "pmem", "lstart", "--no-headers"} {
		if strings.Contains(processCmd, bad) {
			t.Fatalf("processCmd must not use %q", bad)
		}
	}
	if !strings.Contains(processCmd, "-e") || !strings.Contains(processCmd, "||") {
		t.Fatal("processCmd should fall back from -e to plain -o")
	}
	// CPU 时间取自 /proc（awk），兼容 BusyBox
	if !strings.Contains(processCmd, "/proc/[0-9]*/stat") {
		t.Fatal("processCmd should read /proc stat files")
	}
}
