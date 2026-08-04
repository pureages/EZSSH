package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// Client 封装与 EZSSH 网关的 REST API 交互，自动持久化会话 Cookie（ezssh_session）。
type Client struct {
	base   string
	http   *http.Client
	config *Config
}

// NewClient 创建 Client（带 CookieJar，登录成功后会话自动保留）。
func NewClient(c *Config) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		base: c.BaseURL(),
		http: &http.Client{Timeout: 15 * time.Second, Jar: jar},
		config: c,
	}
}

// do 发送请求并解析 JSON 响应；非 2xx 返回错误（优先取 error 字段）。
func (c *Client) do(method, path string, body any) (map[string]any, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &m)
	}
	if resp.StatusCode >= 400 {
		msg := ""
		if m != nil {
			if s, ok := m["error"].(string); ok && s != "" {
				msg = s
			}
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", translateServer(msg))
	}
	return m, nil
}

// translateServer 把服务端硬编码的中文错误文案翻译为当前 Agent 语言。
func translateServer(s string) string {
	if lang == "zh" {
		return s
	}
	if v, ok := serverErrs[s]; ok {
		return v
	}
	return s
}

// serverErrs 常见服务端错误（中文原文 → English）。
var serverErrs = map[string]string{
	"账号或口令错误":                             "Invalid account or password",
	"验证码错误或已过期":                          "Invalid captcha or expired",
	"新口令至少 8 位":                           "New password must be at least 8 characters",
	"旧口令不正确":                              "Old password is incorrect",
	"新口令不能与旧口令相同":                       "New password cannot be the same as the old one",
	"保险库未解锁，无法修改口令":                    "Vault is locked; cannot change password",
	"账号不存在":                                "Account not found",
	"读取账号失败":                              "Failed to read account",
	"更新口令失败":                              "Failed to update password",
	"login_route 必须以 / 开头，且不含空格/#/?": "login_route must start with / and contain no spaces, # or ?",
	"login_route 过长":                        "login_route is too long",
	"lang 仅支持 zh/en":                       "lang only supports zh/en",
	"already initialized":                    "already initialized",
	"unauthorized":                           "unauthorized",
	"invalid request body":                   "invalid request body",
	"username must be 1-64 chars":            "username must be 1-64 chars",
	"password must be at least 8 chars":      "password must be at least 8 chars",
	"too many attempts, locked for 5 minutes": "too many attempts, locked for 5 minutes",
}

// InitStatus 获取初始化状态（免登录）。
func (c *Client) InitStatus() (map[string]any, error) {
	return c.do(http.MethodGet, "/api/init-status", nil)
}

// Init 首次初始化：创建管理员账号并解锁保险库。
func (c *Client) Init(username, password string) error {
	_, err := c.do(http.MethodPost, "/api/init", map[string]string{
		"username": username,
		"password": password,
	})
	return err
}

// login 使用指定口令登录并保存会话 Cookie。
func (c *Client) login(user, pwd string) error {
	_, err := c.do(http.MethodPost, "/api/login", map[string]string{
		"username": user,
		"password": pwd,
	})
	return err
}

// Login 使用配置中的账号密码登录。
func (c *Client) Login() error {
	return c.login(c.config.Username, c.config.Password)
}

// SetLoginRoute 设置登录路由（需登录；API 路径固定，不受路由影响）。
func (c *Client) SetLoginRoute(route string) error {
	if err := c.Login(); err != nil {
		return err
	}
	_, err := c.do(http.MethodPut, "/api/settings", map[string]string{
		"login_route": route,
	})
	return err
}

// ChangePassword 修改登录口令（需登录且保险库解锁），并用旧口令完成登录。
func (c *Client) ChangePassword(oldPwd, newPwd string) error {
	if err := c.login(c.config.Username, oldPwd); err != nil {
		return err
	}
	_, err := c.do(http.MethodPost, "/api/change-password", map[string]string{
		"old_password": oldPwd,
		"new_password": newPwd,
	})
	return err
}

// Health 探测服务是否可达（短超时，供状态轮询与判活）。
func (c *Client) Health() bool {
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Get(c.base + "/api/init-status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
