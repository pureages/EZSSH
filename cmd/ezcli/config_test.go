package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	c := DefaultConfig()
	c.Host = "127.0.0.1"
	c.Port = 59466
	c.Username = "admin"
	c.Password = "secret123"
	c.LoginRoute = "/console"
	c.ServerBinary = "/usr/local/bin/ezsshd"
	c.DataDir = filepath.Join(t.TempDir(), "data")
	c.PidFile = filepath.Join(t.TempDir(), "server.pid")
	c.LogFile = filepath.Join(t.TempDir(), "server.log")
	c.Lang = "zh"
	c.path = path

	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Host != c.Host || got.Port != c.Port || got.Username != c.Username ||
		got.Password != c.Password || got.LoginRoute != c.LoginRoute ||
		got.ServerBinary != c.ServerBinary || got.DataDir != c.DataDir ||
		got.PidFile != c.PidFile || got.LogFile != c.LogFile || got.Lang != c.Lang {
		t.Errorf("round-trip mismatch:\n got=%+v\nwant=%+v", got, c)
	}
	if got.BaseURL() != "http://127.0.0.1:59466" {
		t.Errorf("BaseURL = %q", got.BaseURL())
	}
}

func TestLoadDefaultsLang(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	// 旧配置无 lang 字段
	if err := os.WriteFile(path, []byte(`{"port":9000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Lang != "en" {
		t.Errorf("expected default lang en, got %q", got.Lang)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err != ErrNoConfig {
		t.Errorf("expected ErrNoConfig, got %v", err)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "dir", "agent.json")
	c := DefaultConfig()
	c.path = path
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestConfigFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "agent.json")
	c := DefaultConfig()
	c.path = path
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}
