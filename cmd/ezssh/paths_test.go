package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirDefault(t *testing.T) {
	os.Unsetenv("EZSSH_DATA")
	if d := dataDir(); d != "data" {
		t.Fatalf("default dataDir: got %q want %q", d, "data")
	}
	t.Setenv("EZSSH_DATA", "my-data")
	if d := dataDir(); d != "my-data" {
		t.Fatalf("custom dataDir: got %q want %q", d, "my-data")
	}
}

func TestDefaultDBPath(t *testing.T) {
	if p := defaultDBPath("data"); p != filepath.Join("data", "ezssh.db") {
		t.Fatalf("defaultDBPath: got %q", p)
	}
}

func TestMigrateLegacyDB(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // 工作目录切到临时目录，旧库路径基于 CWD

	// 准备旧库 main + wal
	legacy := filepath.Join(dir, "ezssh.db")
	if err := os.WriteFile(legacy, []byte("main-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy+"-wal", []byte("wal-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "data", "ezssh.db")

	// EZSSH_DB 显式设置 → 不迁移
	t.Setenv("EZSSH_DB", filepath.Join(dir, "custom.db"))
	migrated, err := migrateLegacyDB(dst)
	if err != nil || migrated {
		t.Fatalf("EZSSH_DB set should not migrate: migrated=%v err=%v", migrated, err)
	}

	// 正常迁移
	os.Unsetenv("EZSSH_DB")
	migrated, err = migrateLegacyDB(dst)
	if err != nil || !migrated {
		t.Fatalf("expected migration: migrated=%v err=%v", migrated, err)
	}
	// main 与 wal 都已复制
	if b, _ := os.ReadFile(dst); string(b) != "main-data" {
		t.Fatalf("migrated main content: %q", b)
	}
	if b, _ := os.ReadFile(dst + "-wal"); string(b) != "wal-data" {
		t.Fatalf("migrated wal content: %q", b)
	}
	// 旧文件保留
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy db should be kept: %v", err)
	}

	// 目标已存在 → 不再迁移
	migrated, err = migrateLegacyDB(dst)
	if err != nil || migrated {
		t.Fatalf("existing db should not re-migrate: migrated=%v err=%v", migrated, err)
	}
}

func TestMigrateLegacyDBNoLegacy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	os.Unsetenv("EZSSH_DB")

	migrated, err := migrateLegacyDB(filepath.Join(dir, "data", "ezssh.db"))
	if err != nil || migrated {
		t.Fatalf("no legacy should not migrate: migrated=%v err=%v", migrated, err)
	}
}

func TestMigrateLegacyDBWalAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	os.Unsetenv("EZSSH_DB")

	legacy := filepath.Join(dir, "ezssh.db")
	if err := os.WriteFile(legacy, []byte("main-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "data", "ezssh.db")
	migrated, err := migrateLegacyDB(dst)
	if err != nil || !migrated {
		t.Fatalf("expected migration: migrated=%v err=%v", migrated, err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "main-only" {
		t.Fatalf("migrated content: %q", b)
	}
	if _, err := os.Stat(dst + "-wal"); err == nil {
		t.Fatal("wal should not be created when absent")
	}
}
