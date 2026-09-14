# EZSSH v0.0.7-3

> 发布日期：2026-09-15

本版本修复 **泛域名证书无法用于其下一级域名** 的问题，并优化 **Docker 多架构构建**（不再依赖 QEMU 模拟编译）。

---

## 🐞 问题修复

### 泛域名证书（`*.wyj.me`）无法用于 `www.wyj.me`

- **现象**：已签发 `*.wyj.me` 泛域名证书，但新建站点 `www.wyj.me` 并勾选 SSL 时提示「该域名证书未安装到 `/etc/nginx/ssl/域名/`…」，部署时被降级为 HTTP。
- **原因**：站点部署与证书检测都按「站点主域名」精确查找 `/etc/nginx/ssl/<域名>/`，没有做泛域名匹配，而泛域名证书实际安装在 `/etc/nginx/ssl/*.wyj.me/`。
- **修复**：证书目录解析改为「**精确域名优先，其次上一级泛域名**」：
  - 新建/编辑站点并部署时，自动选用覆盖该站点的证书目录，泛域名证书可直接被 nginx 引用；
  - 建站表单的 SSL 可用性检测（`GET /api/certificates/check`）同步支持泛域名，并回显实际命中的证书目录名（如「证书已安装（\*.wyj.me），到期时间 …」）；
  - 站点配置多个域名时，任一域名命中可用证书即可（**精确目录整体优先于泛域名目录**）。
- **边界**：TLS 泛域名只覆盖**一级**子域——`*.wyj.me` 可用于 `www.wyj.me`，但**不能**用于 `wyj.me`（裸域）或 `a.b.wyj.me`。

---

## ⚡ 构建优化

### Docker 多架构构建不再用 QEMU 模拟编译

- `Dockerfile` 的前端（npm/vite）与后端（Go）构建阶段固定在**构建平台**执行，Go 通过 `GOARCH=$TARGETARCH` 交叉编译出目标架构的静态二进制（`CGO_ENABLED=0`）。
- 效果：arm64 不再需要在 QEMU 下重跑 `npm install` / `vite build` / `go build`，**多架构构建耗时接近单架构**（实测 amd64+arm64 全量构建并推送约 3 分钟，此前 CI 单次要 9~11 分钟）。
- 运行阶段仍按目标架构执行（`apk add`、创建用户等），耗时极短，行为不变。

### Docker 工作流加固

- `docker.yml` 新增 `workflow_dispatch`：可手动补发镜像并指定版本号，**不需要移动 tag**。
- 新增 `timeout-minutes: 30`：镜像推送/缓存导出异常时快速失败，不再干等默认 360 分钟。
- `cache-to` 由 `type=gha,mode=max` 调整为 `mode=min`。

---

## 🐳 Docker 部署

`README` 与用户手册的一键命令已更新为 **host 网络**（容器直接使用宿主机网络，端口由 `EZSSH_PORT` 控制，默认 `49466`）：

```bash
docker run -d --name ezssh \
  --network host \
  -v ezssh-data:/app/data \
  pureages/ezssh:0.0.7-3
```

---

## 📦 安装方式

**一键安装（Linux / macOS / Windows(msys)）**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**发布产物**

- `ezssh-0.0.7-3-linux-amd64.tar.gz`
- `ezssh-0.0.7-3-linux-arm64.tar.gz`
- `ezssh-0.0.7-3-darwin-amd64.tar.gz`
- `ezssh-0.0.7-3-darwin-arm64.tar.gz`
- `ezssh-0.0.7-3-windows-amd64.tar.gz`
- `ezssh-0.0.7-3-windows-arm64.tar.gz`
- Docker 镜像：`pureages/ezssh:0.0.7-3`、`pureages/ezssh:latest`

---

## ⬆️ 升级注意

- 数据库结构无变更，直接替换二进制 / 镜像即可。
- **已签发泛域名证书的用户**：升级后对 `www.xxx.com` 这类站点**重新部署一次**，nginx 配置即会改为引用泛域名证书目录（`/etc/nginx/ssl/*.xxx.com/`），随后即可正常启用 HTTPS。
- 使用 Docker 时建议改用 `--network host`；沿用旧命令（`-p` 映射）也可以，但需保证映射端口与 `EZSSH_PORT` 一致。

---
