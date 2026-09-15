# EZSSH v0.0.7-4

> 发布日期：2026-09-15

本版本修复 **用域名反向代理网页版后，实时功能全部失效** 的问题（桌面无监控统计、任务管理器显示「未知系统」）。

---

## 🐞 问题修复

### 反向代理站点未转发 WebSocket 升级，导致实时数据全挂

- **现象**：用「网站管理」把域名反向代理到网关（如 `vps.wyj.me` → `http://127.0.0.1:49466`）后登录进去：
  - 桌面上服务器图标的 CPU / 内存 / 硬盘 / 流量统计**完全不显示**；
  - 任务管理器显示「**未知系统**」，硬件信息与进程列表为空；
  - 终端、防火墙、Docker、文件管理器等实时面板同样没有数据。
- **原因**：EZSSH 的实时数据（终端、监控、任务管理器、防火墙、Docker…）**全部复用同一条 WebSocket 连接** `/ws`；而生成的 nginx 反代配置只设置了 `proxy_http_version 1.1` 与常规转发头，**没有转发 `Upgrade` / `Connection`**，浏览器握手被上游拒绝。网关侧访问日志可见 `GET /ws → 400`（成功升级应为 `101`），前端因此拿不到任何实时数据。
- **修复**：反代站点配置自动生成 WebSocket 升级所需的 `map` 与转发头，并把 `proxy_read_timeout` 提升到 1 小时（避免空闲 60 秒被 nginx 切断长连接）：

```nginx
# WebSocket 升级（EZSSH 的终端/监控/任务管理器等依赖 /ws）
map $http_upgrade $ezssh_conn_<站点ID> {
    default upgrade;
    ''      close;
}

server {
    ...
    location / {
        proxy_pass http://127.0.0.1:49466;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $ezssh_conn_<站点ID>;
        proxy_read_timeout 3600s;
    }
}
```

- `Connection` 变量按站点 ID 独立命名，**多个反代站点并存不会出现 `duplicate map`** 导致 `nginx -t` 失败。
- 新增单元测试：反代站点必须生成 WS 升级配置且 `map` 位于 `server` 之前；静态/重定向站点不生成。

---

## ⚠️ 升级后需要做一次「部署」

**已存在的反向代理站点，需要点一次「部署」**才会重新生成带 WebSocket 支持的配置（配置由面板生成，升级二进制本身不会改写已下发的文件）。

不方便升级时，也可以手动在站点配置的 `location /` 中加入以下两行并 `nginx -s reload` 临时解决：

```nginx
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

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
  pureages/ezssh:0.0.7-4
```

**发布产物**

- `ezssh-0.0.7-4-linux-amd64.tar.gz`
- `ezssh-0.0.7-4-linux-arm64.tar.gz`
- `ezssh-0.0.7-4-darwin-amd64.tar.gz`
- `ezssh-0.0.7-4-darwin-arm64.tar.gz`
- `ezssh-0.0.7-4-windows-amd64.tar.gz`
- `ezssh-0.0.7-4-windows-arm64.tar.gz`
- Docker 镜像：`pureages/ezssh:0.0.7-4`、`pureages/ezssh:latest`

---

## ⬆️ 升级注意

- 数据库结构无变更，直接替换二进制 / 镜像即可。
- 反向代理站点请按上面的「升级后需要做一次『部署』」处理。

---
