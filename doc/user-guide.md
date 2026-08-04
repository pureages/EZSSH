# EZSSH 用户使用手册

干净、高效、可视化的自托管中心化 SSH Web 网关 · 版本 v0.0.2 · 更新日期 2026-08-04

## 目录

- [1. 产品简介](#s1)
- [2. 快速开始](#s2)
  - [2.1 一键安装](#s2-1)
  - [2.2 Docker 部署](#s2-2)
  - [2.3 自行构建](#s2-3)
- [3. Web 桌面使用](#s3)
- [4. 服务器类型](#s4)
  - [4.1 Windows 安装 OpenSSH Server](#s4-1)
- [5. 安全特性](#s5)
- [6. 部署与运维](#s6)
  - [6.1 环境变量](#s6-1)
  - [6.2 数据目录与备份](#s6-2)
  - [6.3 公网部署建议](#s6-3)

<a id="s1"></a>
## 1. 产品简介

**干净、高效、可视化的自托管中心化 SSH Web 网关，GO驱动**：仅依赖目标机的原生 SSH / SFTP 服务，无需在服务器上安装任何 Agent，也不开放额外端口，安全可控。

系统由两部分组成：

- **网关后端**（Go）：提供 REST + WebSocket 服务，负责认证、凭据保险库、SSH 连接池（ConnectionHub）、SFTP 文件操作以及各类管理逻辑，数据持久化在 SQLite。
- **Web 前端**（React + Vite）：浏览器中的桌面外壳与各窗口应用，通过 HTTP / WebSocket 与网关通信。

<a id="s2"></a>
## 2. 快速开始

<a id="s2-1"></a>
### 1. 一键安装（复制粘贴即可）

支持 Linux / macOS / Windows(msys)。

**国际-安装脚本**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**国内加速-安装脚本**：

```bash
bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/master/scripts/install-cn.sh)
```


<a id="s2-2"></a>
### 2.2 Docker 部署

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
```

<a id="s2-3"></a>
### 2.3 自行构建

**环境要求**：Go 1.22+（构建后端）、Node.js 20+ 与 npm（仅构建前端时需要）。

```bash
# 后端
go build -buildvcs=false ./cmd/ezssh
./ezsshd

# 前端（可选，后端也可直接托管 web/dist）
cd web && npm install && npm run build
```

默认监听 `127.0.0.1:49466`（可用环境变量 `EZSSH_LISTEN` / `EZSSH_PORT` 覆盖）。

<a id="s3"></a>
## 3. Web 桌面使用

登录后进入类 Windows 的 Web 桌面，以窗口化应用管理所有服务器：

- 应用以**窗口**形式打开，可拖拽移动、缩放，右上角为最小化 / 最大化 / 关闭按钮；**选中窗口后按 ESC 可关闭**。
- **服务器图标**：双击打开文件管理器，右键弹出菜单（终端 / 文件管理器 / 任务管理器 / Docker / 防火墙 / 直链下载 / 编辑），左键可拖拽自定义位置；图标显示系统 Logo、国家/地区国旗与三行微型监控（CPU|内存|硬盘、上下行速率、总流量）。
- **任务栏与应用中心**：底部任务栏管理已打开窗口，🪟 按钮打开应用中心（世界地图、添加服务器、设置、直链下载、一键命令、网站管理）。
- **后台进度**：长耗时操作（Nginx 安装、网站部署、证书签发、文件复制粘贴等）可最小化到后台继续执行，成功后任务栏角标自动收起，失败自动还原窗口展示错误。

<a id="s4"></a>
## 4. 服务器类型

新增服务器支持 **Linux** 与 **Windows** 两类：

- **Linux**：任意支持 SSH 的发行版（Ubuntu / Debian / CentOS / Alpine 等），支持密码 / 私钥认证，可用全部应用。
- **Windows**：支持 SSH 连接的 Windows 服务器（需启用 OpenSSH Server），监控、进程、文件管理等应用可用。

> **注意**：添加 Windows 服务器并非走 RDP 的 3389 端口，而是走 **SSH 端口**，需要在服务器上安装 OpenSSH Server。

<a id="s4-1"></a>
### 4.1 Windows 安装 OpenSSH Server

在需要添加的 Windows 服务器上，以管理员身份打开 PowerShell 执行以下命令：

1. **检查是否已安装**（若已有 `sshd` 服务则忽略安装步骤）：

   ```powershell
   Get-Service -Name sshd
   ```

2. **安装服务**：

   ```powershell
   Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
   ```

3. **启动并设为自动**：

   ```powershell
   Start-Service sshd
   Set-Service -Name sshd -StartupType 'Automatic'
   ```

4. **检查防火墙规则**（确认 22 端口入站已放行）：

   ```powershell
   Get-NetFirewallRule -DisplayName "*SSH*" | Get-NetFirewallPortFilter | Where-Object {$_.LocalPort -eq 22}
   ```

   如果没有找到相关规则，运行以下命令创建：

   ```powershell
   New-NetFirewallRule -Name "OpenSSH-Server-In-TCP" -DisplayName "OpenSSH Server (SSH)" -Enabled True -Direction Inbound -Protocol TCP -LocalPort 22 -Action Allow
   ```

<a id="s5"></a>
## 5. 安全特性

- **凭据保险库**：主机密码 / 私钥以 AES-256-GCM 加密入库，密钥由登录口令经 Argon2id 派生，重启后需重新登录解锁。
- **修改口令重加密**：改密时用新口令派生新密钥并重加密全部主机凭据，旧口令无法解密。
- **登录验证码**：某 IP 登录失败一次后要求输入 SVG 验证码（一次性、5 分钟过期）；失败 5 次/分钟则锁定 5 分钟。
- **自定义登录路由**：可在「设置 → 安全」修改登录页地址（如 `/secret-admin`），隐藏登录入口。
- **TOFU 主机密钥确认**：首次连接记录远端主机密钥指纹，后续连接校验一致，防止中间人替换。
- **高危操作审计**：登录 / 连接 / kill / 删除容器 / 改密 / 改设置等均写入审计日志。
- **Web 安全**：终端输出仅经 xterm 渲染不入 DOM（防 XSS）；SQL 参数化；验证码 SVG 由后端生成。

<a id="s6"></a>
## 6. 部署与运维

<a id="s6-1"></a>
### 6.1 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `EZSSH_LISTEN` | `127.0.0.1` | 监听地址 |
| `EZSSH_PORT` | `49466` | 监听端口 |
| `EZSSH_DATA` | `data` | 数据目录（数据库等所有持久化数据） |
| `EZSSH_DB` | `data/ezssh.db` | SQLite 文件路径（显式指定则优先） |
| `EZSSH_MASTER_KEY` | 空 | 注入主密钥，跳过「登录时解锁保险库」 |

<a id="s6-2"></a>
### 6.2 数据目录与备份

- 所有持久化数据统一收纳在 `data/` 目录，数据库即 `data/ezssh.db`。
- 旧版本根目录的 `ezssh.db` 会在首次启动时自动迁移到 `data/`（非破坏性复制，旧文件保留，确认无误后可手动删除）。
- **备份 / 迁移**：Docker 部署时把宿主机的 `./data` 映射到容器的 `/app/data` 即可完成备份/迁移。

<a id="s6-3"></a>
### 6.3 公网部署建议

> **警告**：默认监听 127.0.0.1。公网部署强烈建议置于 Caddy / Nginx 之后并启用 HTTPS；不要直接暴露裸 HTTP 到公网。

---

[English](user-guide.en.md) | 简体中文
