package main

import (
	"io"
	"os"
	"path/filepath"
)

// dataDir 返回数据目录：EZSSH_DATA 优先，默认工作目录下的 data/。
// 所有持久化数据（数据库等）统一收纳于此，便于 Docker 映射卷与备份迁移。
func dataDir() string {
	return getenv("EZSSH_DATA", "data")
}

// defaultDBPath 返回默认数据库路径（数据目录下的 ezssh.db）。
func defaultDBPath(dir string) string {
	return filepath.Join(dir, "ezssh.db")
}

// migrateLegacyDB 把旧版根目录的 ezssh.db 一次性迁移到 data 目录。
// 仅当 EZSSH_DB 未显式指定、目标 db 尚不存在、且旧库存在时执行（非破坏性复制，旧文件保留）。
// WAL 存在时一并复制，保证未 checkpoint 的事务不丢失。
func migrateLegacyDB(dbPath string) (migrated bool, err error) {
	if os.Getenv("EZSSH_DB") != "" {
		return false, nil
	}
	if _, err := os.Stat(dbPath); err == nil {
		return false, nil // 目标库已存在（含 EZSSH_DB 指向已存在文件）
	}
	legacy := "ezssh.db"
	if _, err := os.Stat(legacy); err != nil {
		return false, nil // 无旧库，无需迁移
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return false, err
	}
	for _, suffix := range []string{"", "-wal"} {
		src, dst := legacy+suffix, dbPath+suffix
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return false, err
		}
	}
	return true, nil
}

// copyFile 以流式方式复制单个文件。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
