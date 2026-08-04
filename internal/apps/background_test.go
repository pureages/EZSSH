package apps

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"ezssh/internal/store"
)

type fakeProcLister struct {
	procs []ProcessInfo
	err   error
}

func (f *fakeProcLister) List(string) ([]ProcessInfo, error) { return f.procs, f.err }

func newBgManagerForTest(t *testing.T, f *fakeProcLister) (*BackgroundManager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	m := NewBackgroundManager(nil, nil, f, st)
	// 测试注入平台，避免真实拨号
	m.platform = func(string) string { return "linux" }
	return m, st
}

func insertTaskAt(t *testing.T, st *store.Store, id, hostID string, pid int, started int64) {
	t.Helper()
	if err := st.CreateTask(&store.BackgroundTask{
		ID: id, HostID: hostID, HostName: "h-" + hostID, PID: pid,
		Command: "echo hi", LogPath: "/tmp/ezssh_bg_" + id + ".log",
		ErrPath: "", Started: started,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
}

func insertTask(t *testing.T, st *store.Store, id, hostID string, pid int) {
	t.Helper()
	insertTaskAt(t, st, id, hostID, pid, time.Now().Unix())
}

// TestLinuxDetachCmd 验证分离式启动命令：优先 setsid、回退 nohup、回显 $!。
func TestLinuxDetachCmd(t *testing.T) {
	cmd := linuxDetachCmd("/tmp/ezssh_bg_x.sh", "/tmp/ezssh_bg_x.log")
	for _, want := range []string{"setsid sh '/tmp/ezssh_bg_x.sh'", "nohup sh '/tmp/ezssh_bg_x.sh'", "echo $!", ">>'/tmp/ezssh_bg_x.log' 2>&1"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("detach cmd missing %q: %s", want, cmd)
		}
	}
}

// TestWindowsStartScript 验证 Windows 启动脚本：含 -EncodedCommand 与分文件重定向。
func TestWindowsStartScript(t *testing.T) {
	enc := winPSEncoded("Write-Output 1")
	script := windowsStartScript("bg_x", enc)
	if !strings.Contains(script, enc) {
		t.Fatalf("script missing encoded command")
	}
	for _, want := range []string{"-RedirectStandardOutput $out", "-RedirectStandardError $err", "-PassThru", "$p.Id", "Start-Process -FilePath powershell"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q: %s", want, script)
		}
	}
}

// TestWinPSEncoded 验证编码可逆且无 powershell 前缀。
func TestWinPSEncoded(t *testing.T) {
	script := "Write-Output 'hi'; $x = \"a`tb\""
	enc := winPSEncoded(script)
	if strings.Contains(enc, " ") || strings.HasPrefix(enc, "powershell") {
		t.Fatalf("bad enc: %q", enc)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	if got := string(utf16.Decode(u)); got != script {
		t.Fatalf("round-trip mismatch: %q != %q", got, script)
	}
}

// TestMatchProc_Linux 校验 Linux 的 PID 复用防护：命令必须含脚本路径。
func TestMatchProc_Linux(t *testing.T) {
	task := store.BackgroundTask{ID: "bg_x", PID: 100}
	procs := []ProcessInfo{
		{PID: 99, Command: "/bin/sh /tmp/ezssh_bg_other.sh"},
		{PID: 100, Command: "sh /tmp/ezssh_bg_bg_x.sh"},
	}
	if _, ok := matchProc(procs, task, false); !ok {
		t.Fatal("expected match with script path")
	}
	// PID 被复用但命令不含本任务脚本 → 判为不存在
	procs[1].Command = "sh /etc/cron.d/task"
	if _, ok := matchProc(procs, task, false); ok {
		t.Fatal("expected NO match on pid reuse")
	}
}

// TestMatchProc_Windows Windows 仅按 PID 匹配。
func TestMatchProc_Windows(t *testing.T) {
	task := store.BackgroundTask{ID: "bg_x", PID: 100}
	procs := []ProcessInfo{{PID: 100, Command: "powershell -EncodedCommand AAAA"}}
	if _, ok := matchProc(procs, task, true); !ok {
		t.Fatal("expected match by pid only")
	}
}

// TestBackgroundList 覆盖 running / exited / unknown 三态。
func TestBackgroundList(t *testing.T) {
	m, st := newBgManagerForTest(t, nil)
	insertTask(t, st, "bg_run", "h1", 100)
	insertTask(t, st, "bg_gone", "h1", 999)
	insertTask(t, st, "bg_offline", "h2", 100)
	// bg_old 是 2023 年启动的陈旧任务（远超 7 天）→ exited 时应被清理
	insertTaskAt(t, st, "bg_old", "h1", 555, 1700000000)

	// 伪造进程表：h1 上有 bg_run 的进程，没有 bg_gone；h2 查询报错（离线）
	byHostProcs := map[string]*fakeProcLister{
		"h1": {procs: []ProcessInfo{{PID: 100, Command: "sh /tmp/ezssh_bg_bg_run.sh", CPU: 3.5, MEM: 1.2, RSS: 4096, Start: "Aug  3 12:00:00 2026"}}},
		"h2": {err: errFakeOffline},
	}
	// 让 List 每次按 hostID 取不同的 fake
	origProcs := m.procs
	m.procs = &hostRouterLister{byHost: byHostProcs, fallback: origProcs}

	// bg_old 的 started=1700000000（2023 年，远超 7 天）→ exited 时会被清理
	views, err := m.List(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]BackgroundView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	if v := byID["bg_run"]; v.Status != "running" || v.CPU != 3.5 || v.MEM != 1.2 || v.RSS != 4096 {
		t.Fatalf("bg_run: %+v", v)
	}
	if v := byID["bg_gone"]; v.Status != "exited" {
		t.Fatalf("bg_gone: %+v", v)
	}
	if v := byID["bg_offline"]; v.Status != "unknown" {
		t.Fatalf("bg_offline: %+v", v)
	}
	// bg_old 为 exited 且超 7 天 → 已清理（从 DB 删除）
	if _, err := st.GetTask("bg_old"); err != store.ErrNotFound {
		t.Fatalf("bg_old should be purged, got %v", err)
	}
}

type errType struct{}

func (errType) Error() string { return "offline" }

var errFakeOffline error = errType{}

// hostRouterLister 按 hostID 返回不同进程表（测试用）。
type hostRouterLister struct {
	byHost   map[string]*fakeProcLister
	fallback ProcLister
}

func (h *hostRouterLister) List(hostID string) ([]ProcessInfo, error) {
	if f, ok := h.byHost[hostID]; ok {
		return f.procs, f.err
	}
	return h.fallback.List(hostID)
}
