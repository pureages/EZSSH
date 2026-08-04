package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestEnsureBuiltinHost 验证内置网关主机播种：幂等、字段正确、凭据为空。
func TestEnsureBuiltinHost(t *testing.T) {
	st := openTestStore(t)
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("seed twice: %v", err)
	}
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.ID != BuiltinHostID || !h.Builtin || h.Hidden {
		t.Fatalf("builtin flags: %+v", h)
	}
	if h.Host != "127.0.0.1" || h.Port != 22 || h.Username != "" || h.AuthType != "password" {
		t.Fatalf("builtin fields: %+v", h)
	}
	if len(h.Credential) != 0 {
		t.Fatalf("credential should be empty, got %d bytes", len(h.Credential))
	}
}

// TestEnsureBuiltinHostDeleted 用户删除内置主机后（标记置 1），不再重新播种。
func TestEnsureBuiltinHostDeleted(t *testing.T) {
	st := openTestStore(t)
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.SetSetting(SettingBuiltinDeleted, "1"); err != nil {
		t.Fatalf("set deleted flag: %v", err)
	}
	if err := st.DeleteHost(BuiltinHostID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// 模拟重启后的再次播种：应被跳过
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	hosts, err := st.ListHosts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("expected 0 hosts after delete flag, got %d", len(hosts))
	}
}

// TestHostHiddenRoundTrip 验证隐藏标记与「显示全部」。
func TestHostHiddenRoundTrip(t *testing.T) {
	st := openTestStore(t)
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateHostHidden(BuiltinHostID, true); err != nil {
		t.Fatalf("hide: %v", err)
	}
	h, err := st.GetHost(BuiltinHostID)
	if err != nil || !h.Hidden {
		t.Fatalf("expected hidden: %+v %v", h, err)
	}
	if err := st.ShowAllHosts(); err != nil {
		t.Fatalf("show all: %v", err)
	}
	h, _ = st.GetHost(BuiltinHostID)
	if h.Hidden {
		t.Fatal("expected not hidden after show-all")
	}
}
