package apps

import "testing"

// Alpine/BusyBox 形态的 df -Pm 输出：设备列可能是 overlay/tmpfs 等非 /dev/xxx。
func TestParseRawAlpineDf(t *testing.T) {
	out := "cpu  100 0 50 800 10 0 5 0 0 0\n" +
		"===\n" +
		"MemTotal:       1000000 kB\nMemFree:        500000 kB\nMemAvailable:    900000 kB\nSwapTotal:      0 kB\nSwapFree:       0 kB\n" +
		"===\n" +
		"0.05 0.10 0.15 1/2 345\n" +
		"===\n" +
		"Filesystem     1M-blocks      Used Available Use% Mounted on\n" +
		"overlay               20000      8000      12000  40% /\n" +
		"tmpfs                  1000        10        990   1% /dev/shm\n" +
		"===\n" +
		"eth0: 1000000 0 0 0 0 0 0 0 200000 0 0 0 0 0 0 0\n"

	rd := parseRaw(out)
	if len(rd.disks) != 2 {
		t.Fatalf("expected 2 disks (overlay/tmpfs), got %d: %+v", len(rd.disks), rd.disks)
	}
	root := rd.disks[0]
	if root.Mount != "/" || root.Total != 20000*1024*1024 || root.Used != 8000*1024*1024 {
		t.Fatalf("root disk: %+v", root)
	}
	shm := rd.disks[1]
	if shm.Mount != "/dev/shm" || shm.Total != 1000*1024*1024 {
		t.Fatalf("shm disk: %+v", shm)
	}
	if rd.cpu.total == 0 || rd.mem["MemTotal"] != 1000000 {
		t.Fatalf("cpu/mem: %+v", rd)
	}
	if _, ok := rd.net["eth0"]; !ok {
		t.Fatalf("missing eth0 net: %+v", rd.net)
	}
}
