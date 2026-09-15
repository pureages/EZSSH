package apps

import (
	"strings"
	"testing"

	"ezssh/internal/store"
)

// 反向代理站点必须生成 WebSocket 升级配置：
// 前端只维护一条 /ws 连接承载终端/监控/任务管理器数据，反代不转发 Upgrade 时握手会 400，
// 表现为「桌面统计不显示、任务管理器显示未知系统」。
func TestGenerateConfigProxyWebSocket(t *testing.T) {
	site := &store.Website{
		ID:        "ws_61401b685e23",
		Domains:   "vps.wyj.me",
		SiteType:  "proxy",
		ProxyPass: "http://127.0.0.1:49466",
		SSL:       true,
	}
	conf := GenerateConfig(site, true, "*.wyj.me")
	for _, want := range []string{
		"map $http_upgrade $ezssh_conn_ws_61401b685e23 {",
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header Connection $ezssh_conn_ws_61401b685e23;",
		"proxy_read_timeout 3600s;",
		"proxy_pass http://127.0.0.1:49466;",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("缺少 %q，生成结果：\n%s", want, conf)
		}
	}
	// map 必须先于 server 段出现，否则 nginx 会报 unknown variable
	if strings.Index(conf, "map $http_upgrade") > strings.Index(conf, "server {") {
		t.Fatalf("map 必须出现在 server 段之前：\n%s", conf)
	}
	// 非反代站点不应生成 WS 配置
	static := &store.Website{ID: "ws_1", Domains: "a.com", SiteType: "static"}
	if conf2 := GenerateConfig(static, false, ""); strings.Contains(conf2, "map $http_upgrade") {
		t.Fatalf("静态站点不应生成 WS map：\n%s", conf2)
	}
}

// 默认拒绝站点：改为 default_server 并 return 444，未建站的域名/直接 IP 访问不会看到任何内容。
func TestGenerateDefaultDenyConfig(t *testing.T) {
	conf := generateDefaultDenyConfig()
	for _, want := range []string{
		"listen 80 default_server;",
		"listen [::]:80 default_server;",
		"listen 443 ssl default_server;",
		"listen [::]:443 ssl default_server;",
		"server_name _;",
		"return 444;",
		defaultDenyCertDir + "/fullchain.pem",
		defaultDenyCertDir + "/key.pem",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("缺少 %q：\n%s", want, conf)
		}
	}
	// 默认拒绝块不得代理或返回任何站点内容
	for _, bad := range []string{"proxy_pass", "root ", "index "} {
		if strings.Contains(conf, bad) {
			t.Fatalf("默认拒绝块不应包含 %q：\n%s", bad, conf)
		}
	}
}

// 落地脚本必须：生成自签名证书 + 让出发行版默认站点占用的 default_server。
func TestDefaultDenyScript(t *testing.T) {
	s := defaultDenyScript()
	for _, want := range []string{
		"mkdir -p " + sshQuote(defaultDenyCertDir),
		"openssl req -x509",
		"/etc/nginx/sites-enabled/default",
		"readlink -f",
		"listen 80 default_server;/listen 80;/",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("脚本缺少 %q：\n%s", want, s)
		}
	}
}
