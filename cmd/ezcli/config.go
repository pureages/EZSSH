package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config 是 ezssh 终端 Agent 的本地配置文件（~/.ezssh/agent.json）。
// 账号密码以明文存储（文件权限 0600），用于「查看账号信息」与自动登录。
type Config struct {
	Host         string `json:"host"`          // 服务监听地址，默认 127.0.0.1
	Port         int    `json:"port"`          // 服务监听端口，默认 49466
	Username     string `json:"username"`      // 管理员账号，默认 admin
	Password     string `json:"password"`      // 管理员密码，默认 admin123456
	LoginRoute   string `json:"login_route"`   // 登录路由，默认 /login
	ServerBinary string `json:"server_binary"` // ezsshd 服务端二进制绝对路径
	DataDir      string `json:"data_dir"`      // 服务数据目录（数据库等）
	PidFile      string `json:"pid_file"`      // 服务进程 PID 文件
	LogFile      string `json:"log_file"`      // 服务日志文件
	Lang         string `json:"lang"`          // 界面语言 en|zh（默认 en，与产品默认语言一致）

	path string // 配置文件路径（内存字段，不序列化）
}

// ErrNoConfig 表示配置文件不存在，需要先运行 `ezssh setup`。
var ErrNoConfig = errors.New("config file not found")

// DefaultConfig 返回一份默认配置。
func DefaultConfig() *Config {
	return &Config{
		Host:       "127.0.0.1",
		Port:       49466,
		Username:   "admin",
		Password:   "admin123456",
		LoginRoute: "/login",
		Lang:       "en",
	}
}

// BaseURL 返回服务端 HTTP 地址。
func (c *Config) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", c.Host, c.Port)
}

// DefaultConfigPath 返回默认配置文件路径 ~/.ezssh/agent.json。
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ezssh", "agent.json"), nil
}

// defaultDir 返回 ~/.ezssh 目录（不存在则创建，权限 0700）。
func defaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".ezssh")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// Load 读取配置文件；文件不存在返回 ErrNoConfig。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoConfig
		}
		return nil, err
	}
	c := DefaultConfig()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 49466
	}
	if c.Lang == "" {
		c.Lang = "en"
	}
	c.path = path
	return c, nil
}

// Save 将配置写入磁盘（权限 0600），并确保父目录存在。
// 需在 Load 之后调用，或直接设置 cfg.path。
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config path is empty")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}
