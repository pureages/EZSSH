# EZSSH v0.0.7-5

> 发布日期：2026-09-15

本版本新增 **「默认拒绝」**：没有建站的域名（或直接用 IP）指向网关时，**不再显示任何站点内容**。

---

## ✨ 新增功能

### 未匹配站点的访问一律拒绝（部署站点时自动生成）

- **背景**：nginx 对每个监听端口只认一个「默认 server」——显式 `default_server` 优先，没有的话就用**配置里第一个** server 块兜底。此前面板不下发默认块，于是**任何指向该服务器 IP 的域名都会看到「第一个站点」的内容**（常见表现：陌生域名/直接 IP 访问 HTTPS 时出现某个站点的页面，HTTP 下出现 nginx 欢迎页）。
- **现在**：部署站点时会自动生成并校准 `/etc/nginx/conf.d/000-ezssh-deny.conf`，把 80 / 443 都声明为 `default_server` 并 `return 444`（**不返回任何内容，直接关闭连接**）：

```nginx
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    return 444;
}
server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    server_name _;
    ssl_certificate     /etc/nginx/ssl/_ezssh_default/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/_ezssh_default/key.pem;
    return 444;
}
```

- **自签名证书自动生成**：nginx < 1.19.4 没有 `ssl_reject_handshake`，443 的默认块必须指定一张证书；面板会在目标机自动生成 `/etc/nginx/ssl/_ezssh_default/`（10 年有效，私钥 600），**仅用于握完手立即断开**，不需要你申请任何证书。
- **自动让出发行版默认站点**：若存在 `/etc/nginx/sites-enabled/default`（Ubuntu / Debian 自带、占用 80 的 `default_server`），会移除该软链并把 `sites-available/default` 里的 `listen 80 default_server` 降级——**只处理指向发行版自带文件的软链，用户自己的配置不动**。不做这一步，新增默认块会导致 `nginx -t` 报 `duplicate default server`。
- **冲突自动回滚**：若默认块仍与既有配置冲突导致 `nginx -t` 失败，面板会**自动删除该块并重试**，站点本身照常部署，并在部署提示里说明原因。

### 效果

| 访问方式 | 升级前 | 升级后 |
|---|---|---|
| 已配置的站点（`www.xxx` / `vps.xxx`…） | 正常 | 正常（不受影响） |
| 陌生域名（DNS 指过来但没建站） | 显示第一个站点内容 / nginx 欢迎页 | **打不开**（连接直接断开） |
| 直接用 IP 访问 | 同上 | **打不开** |

---

## ⚠️ 升级后需要做一次「部署」

站点配置由面板下发，**已存在的站点需要点一次「部署」**才会生成默认拒绝块（与 v0.0.7-4 的 WebSocket 修复一样，一次部署会把两项都校准）。

---

## 📌 注意事项

- **HTTP-01 校验**：若给「尚未建站」的域名用 HTTP-01 签发证书，`http://域名/.well-known/acme-challenge/...` 会被默认块拒绝；**DNS-01 不受影响**。
- **放行/关闭**：想放行某个域名，建站即可；想整体关闭该行为，删除目标机上的 `/etc/nginx/conf.d/000-ezssh-deny.conf` 后 `nginx -s reload`。
- **浏览器观感**：陌生域名走 HTTPS 会先收到自签名证书的「不受信任」提示，继续后连接立即断开、不显示任何内容。

---

## 🧪 测试

新增单元测试：默认块必须是 80/443 双栈 `default_server` + `return 444` + 引用自签名证书，且**不得包含 `proxy_pass` / `root` / `index` 等任何站点内容指令**；落地脚本必须包含证书生成、发行版默认站点软链处理与 `default_server` 降级。

---

## 📦 安装方式

**一键安装（Linux / macOS / Windows(msys)）**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**Docker**

```bash
docker run -d --name ezssh \
  --network host \
  -v ezssh-data:/app/data \
  pureages/ezssh:0.0.7-5
```

**发布产物**

- `ezssh-0.0.7-5-linux-amd64.tar.gz`
- `ezssh-0.0.7-5-linux-arm64.tar.gz`
- `ezssh-0.0.7-5-darwin-amd64.tar.gz`
- `ezssh-0.0.7-5-darwin-arm64.tar.gz`
- `ezssh-0.0.7-5-windows-amd64.tar.gz`
- `ezssh-0.0.7-5-windows-arm64.tar.gz`
- Docker 镜像：`pureages/ezssh:0.0.7-5`、`pureages/ezssh:latest`

---

## ⬆️ 升级注意

- 数据库结构无变更，直接替换二进制 / 镜像即可。
- 升级后对任一站点点一次「部署」，即可同时获得 WS 反代修复（v0.0.7-4）与默认拒绝块（本版本）。

---
