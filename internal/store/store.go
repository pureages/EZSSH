package store

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

// Store 是 SQLite 访问层（纯 Go 驱动，无 CGO）。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）数据库并执行迁移。
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者，串行化连接避免锁竞争
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE,
  password_hash BLOB NOT NULL,          -- Argon2id 前 32 字节
  salt          BLOB NOT NULL,          -- Argon2id salt，兼作密钥派生盐
  created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS hosts (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  host        TEXT NOT NULL,
  port        INTEGER NOT NULL DEFAULT 22,
  username    TEXT NOT NULL,
  auth_type   TEXT NOT NULL,            -- 'password' | 'privatekey'
  credential  BLOB NOT NULL,            -- AES-256-GCM 加密后的密码或私钥
  group_name  TEXT NOT NULL DEFAULT '',
  remark      TEXT NOT NULL DEFAULT '',
  fingerprint TEXT NOT NULL DEFAULT '',  -- TOFU 主机密钥指纹 SHA256
  hidden      INTEGER NOT NULL DEFAULT 0, -- 1=桌面图标被隐藏
  builtin     INTEGER NOT NULL DEFAULT 0, -- 1=内置主机（网关本机，默认播种、可删除）
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  action     TEXT NOT NULL,
  host_id    TEXT,
  detail     TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS saved_commands (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  command    TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS background_tasks (
  id        TEXT PRIMARY KEY,
  host_id   TEXT NOT NULL,
  host_name TEXT NOT NULL DEFAULT '',
  pid       INTEGER NOT NULL DEFAULT 0,
  command   TEXT NOT NULL,
  log_path  TEXT NOT NULL DEFAULT '',
  err_path  TEXT NOT NULL DEFAULT '',
  started   INTEGER NOT NULL          -- unix 秒
);

CREATE TABLE IF NOT EXISTS websites (
  id           TEXT PRIMARY KEY,
  host_id      TEXT NOT NULL,            -- 部署服务器（hosts.id）
  name         TEXT NOT NULL,            -- 站点名/备注
  group_name   TEXT NOT NULL DEFAULT '',
  domains      TEXT NOT NULL,            -- 逗号分隔，第 1 个为主域名
  site_type    TEXT NOT NULL,            -- static | proxy | redirect
  root_dir     TEXT NOT NULL DEFAULT '', -- static: 网站根目录（webroot）
  proxy_pass   TEXT NOT NULL DEFAULT '', -- proxy: 后端地址 http://127.0.0.1:8080
  redirect_url TEXT NOT NULL DEFAULT '', -- redirect: 目标 URL
  ssl          INTEGER NOT NULL DEFAULT 0,
  cert_id      TEXT NOT NULL DEFAULT '', -- 关联 certificates.id
  enabled      INTEGER NOT NULL DEFAULT 1,
  created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS certificates (
  id             TEXT PRIMARY KEY,
  host_id        TEXT NOT NULL,
  domain         TEXT NOT NULL,
  method         TEXT NOT NULL,              -- http | dns
  dns_account_id TEXT NOT NULL DEFAULT '',   -- method=dns 时对应 dns_accounts.id
  email          TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT '',   -- issuing | active | renewing | error
  expires_at     DATETIME,
  last_renew     DATETIME,
  error          TEXT NOT NULL DEFAULT '',
  created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS dns_accounts (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  provider   TEXT NOT NULL,              -- cloudflare
  token_enc  BLOB NOT NULL,              -- vault(AES-256-GCM) 加密的 API Token
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		return err
	}
	// 兼容旧库：为 hosts 添加 fingerprint 列（若缺失）
	_, err = s.db.Exec(`ALTER TABLE hosts ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		// "duplicate column" 说明已存在，忽略
		if !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	// 兼容旧库：为 hosts 添加 platform 列（'' = 未知/自动检测；'linux' | 'windows'）
	_, err = s.db.Exec(`ALTER TABLE hosts ADD COLUMN platform TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	// 兼容旧库：桌面隐藏标记与内置主机标记
	_, err = s.db.Exec(`ALTER TABLE hosts ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	_, err = s.db.Exec(`ALTER TABLE hosts ADD COLUMN builtin INTEGER NOT NULL DEFAULT 0`)
	if err != nil {
		if !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	return nil
}
