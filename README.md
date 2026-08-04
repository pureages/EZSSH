# EZSSH

> 版本：v0.0.1

**干净、高效、可视化的自托管中心化 SSH Web 网关**：浏览器登录后进入类 Windows 的 Web 桌面，以窗口化应用管理所有 Linux 服务器。仅依赖目标机的原生 SSH 服务，不装 Agent、不开额外端口。

## 里程碑进度

| 里程碑 | 状态 | 说明 |
|---|---|---|
| M1 网关骨架 | ✅ 完成 | 登录/初始化、主机 CRUD、凭据保险库、ConnectionHub 建连 |
| M2 桌面外壳 + 终端 | ✅ 完成 | Web 桌面 + 窗口管理 + xterm.js 终端全链路（真实 VPS 验证通过） |
| M3 文件管理器 | ✅ 完成 | SFTP 可视化文件管理（真实 VPS 验证通过） |
| M4 监控 + 进程 | ✅ 完成 | 实时监控图表 + 进程管理（真实 VPS 验证通过） |
| M5 Docker + 应用中心 | ✅ 完成 | 容器/镜像/日志/stats（真实 VPS 验证通过） |
| M6 加固与发布 | ✅ 完成 | TOFU 主机密钥确认 + 验证码 + Docker 镜像 + 文档 |
| M7 实用应用扩展 | ✅ 完成 | 防火墙管理、直链下载、世界地图、硬件信息、Docker 应用市场 |
| M8 网站管理 + 发布准备 | ✅ 完成 | 一键命令、网站管理（Nginx + Let's Encrypt）、默认英文、README 与用户手册 |

## 功能特性

**Web 桌面（类 Windows）**
- 深色玻璃拟态桌面，窗口可拖拽/缩放/最小化/全屏/关闭（Windows 风格按钮在右上角）；**选中窗口后按 ESC 可关闭**（含各应用的子窗口/弹窗）
- 长耗时操作（Nginx 安装、网站部署、证书签发、文件复制粘贴等）进度窗口可**最小化到后台**继续执行：成功后任务栏角标自动收起，失败则自动还原窗口展示错误
- 服务器图标可**左键拖拽自定义位置**（自动对齐开关，位置持久化到浏览器）；支持**框选**多选
- **双击图标打开文件管理器**；图标右键菜单：打开终端/文件管理器/任务管理器/Docker/防火墙/直链下载，以及编辑服务器
- 图标左侧显示**系统 Logo**（自动识别发行版，如 Ubuntu/Alpine/Debian）与**国家/地区国旗**（按 IP 地理位置）
- 图标下方实时显示**三行微型监控**：CPU|内存|硬盘、上传速率|下载速率、总上传|总下载（分色、悬停显示说明，可在设置中隐藏；**采集失败时显示「已离线」而非全 0**）
- 桌面右键菜单：刷新 / 添加服务器 / 设置 / 自动对齐 / 退出登录

**桌面应用**
- 终端：xterm.js 全功能 SSH 终端（多窗口、自适应、链接可点）
- 文件管理器：目录浏览、上传/下载（实时进度）、在线编辑、右侧常驻预览面板
  （文本可编辑保存；图片可缩放/旋转；视频可播放）、新建/重命名/删除/权限、空白右键菜单
- 文件管理器右键**复制/剪切/粘贴**：同服务器直接粘贴；跨服务器可选**直连传输**（源机 scp 直推，数据不经网关）或**中转传输**（网关双 SFTP 转发），均带实时进度条且可取消
- 任务管理器（系统监控 + 进程融合，参考 Windows 任务管理器双页签）：
  - **硬件**：左侧硬件列表 + 右侧详情（系统/CPU/内存/Swap/硬盘/网络），含发行版、主机名、运行时长、CPU 型号/核心数、**虚拟机识别**（DMI 检测）
  - 性能：CPU/内存/Swap/负载/磁盘/网络实时图表（ECharts），概览含硬盘占用/总大小、下载/上传速率与总下载/总上传，数字精确到一位小数
  - 进程：进程列表、搜索、TERM/KILL
- Docker 管理：容器/镜像/stats/日志/启动停止重启删除、容器详情 inspect、**Docker 一键安装**（流式输出）、**应用市场**（内置常用镜像模板，填端口/环境变量/挂载即可安装，支持自定义 docker run）、支持不支持的环境变量注入配置文件
- **防火墙管理**（基于 ufw，Ubuntu 默认可用）：
  - 查看防火墙状态/版本/启用情况，一键开启/关闭
  - 规则列表（解析自 /etc/ufw/user.rules），支持**禁 IP / 允许 IP / 禁端口 / 允许端口 / 端口范围 / 全部流量**
  - 开启前**自动放行 SSH 端口**（自动探测实际 SSH 端口），避免把自己锁在门外
- **直链下载**（基于 aria2，免登录直链 + 种子）：
  - 一键安装 aria2（自动识别 apt/dnf/yum/apk，流式输出安装过程）
  - 支持 HTTP/HTTPS **直链**与 **BitTorrent 种子**下载（磁力链已移除）
  - **保存目录**可在目标机浏览器上实时浏览选择；**保存文件名**可自定义（默认取链接文件名，种子文件由内容决定）
  - 暂停 / 继续 / 取消，实时进度、速率、剩余时间；daemon 常驻目标机（/tmp/ezssh_aria2）
- **世界地图**（ECharts 世界地图）：按 IP 地理位置聚合展示全部服务器，绿色光点 = 在线（光点大小随 CPU 负载，点击查看详情），灰色 = 离线；tooltip 显示在线率、负载、速率等
- **一键命令**：跨服务器批量执行常用命令（保存的命令可一键分发到多台服务器）
- **网站管理**（Nginx + Let's Encrypt，跨服务器）：
  - 按域名建站：**静态网站 / 反向代理 / 301 重定向**；站点分组、启停、编辑；删除需**输入域名确认**
  - 静态站点根目录不存在时**自动创建目录并写入默认首页**（index.html，跟随界面语言中/英）
  - 未安装 Nginx 的服务器支持**一键安装**（自动识别 apt/dnf/yum/apk，流式输出）
  - **Let's Encrypt 证书**：HTTP-01（webroot）与 **Cloudflare DNS-01**（可管理多个 DNS 账户）两种验证方式，acme.sh 安装 + 自动续签 + 到期监控（「同步」刷新到期时间）
  - 勾选 SSL 时实时**检测证书是否已安装**，证书就绪即可一键部署 HTTPS；未就绪则自动降级 HTTP 并提示
  - 站点行内「**查看文件**」直接打开该服务器文件管理器并进入网站根目录
- **设置**：个性化 / 桌面 / 安全 / 语言 / 关于EZSSH

**安全与设置**
- **国际化**：默认界面语言为 **English**，全站支持**简体中文 / English** 一键切换（设置 → 语言），语言偏好保存在服务器端，任何设备打开都一致
- 设置应用左侧菜单：**个性化 / 桌面 / 安全 / 语言 / 关于EZSSH**
  - 桌面：**隐藏图标监控**开关（默认显示）
  - 安全：**修改密码**（改密后自动重加密全部主机凭据）、**安全路由**（自定义登录页地址，隐藏入口）
  - 语言：**界面语言**下拉（简体中文 / English，偏好存服务器端）
  - 个性化：**上传桌面背景**（PNG/JPG/GIF 动图/WebP，可恢复默认渐变）、**桌面图标大小**（60%–150%）、**图标监控文字大小**（9–16px）、**图标监控文字颜色**（点击示例区域取色，逐项自定义 CPU/内存/硬盘/速率/总量及分隔符颜色）
  - 关于EZSSH：**版本信息**（`v0.0.1`，随 `-ldflags "-X main.version=x.y.z"` 构建注入）、**作者**（pureages）、**GitHub 链接**
- 登录验证码：失败一次后显示 SVG 验证码（防暴力破解）
- 凭据保险库：AES-256-GCM + Argon2id
- TOFU 主机密钥确认、登录限流、操作审计

## 后端

### 环境要求

- Go 1.22+（`go.mod` 依赖最新 toolchain，会自动切换）

### 运行

```bash
# 首次构建并运行
go build -buildvcs=false ./cmd/ezssh
.\ezssh.exe

# 或直接运行
go run ./cmd/ezssh
```

默认监听 `127.0.0.1:49466`，可通过环境变量覆盖：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `EZSSH_LISTEN` | `127.0.0.1` | 监听地址 |
| `EZSSH_PORT` | `49466` | 监听端口 |
| `EZSSH_DATA` | `data` | 数据目录（数据库等所有持久化数据） |
| `EZSSH_DB` | `data/ezssh.db` | SQLite 文件路径（显式指定则优先） |
| `EZSSH_MASTER_KEY` | 空 | 注入主密钥，跳过"登录时解锁保险库" |

### 终端 Agent（ezssh 命令）

EZSSH 附带一个纯终端的后台管理 Agent（`cmd/ezcli`，产物为 `ezssh`），无需打开浏览器即可完成**安装、初始化与日常运维**：运行安装向导后服务在后台运行，之后随时输入 `ezssh` 即可调出交互式管理菜单。

**构建（也可直接用下方一键脚本）**

```bash
go build -buildvcs=false ./cmd/ezcli     # 产物 ezssh（Agent）
go build -buildvcs=false ./cmd/ezssh     # 产物 ezsshd（服务端守护进程）
```

**一键安装脚本**（自动检测平台/架构、编译服务端+Agent、构建前端，并进入初始化向导）：

```bash
bash scripts/install.sh
```

产物安装到 `/usr/local/bin`（root）或 `~/.local/bin`；`~/.local/bin` 不在 PATH 时脚本会提示添加。Windows（Git Bash/msys）同样适用，产物为 `ezsshd.exe` / `ezssh.exe`。

**初始化向导（`ezssh setup`）**——逐项回车使用默认值即可：

| 项目 | 默认值 |
|---|---|
| 管理员账号 | `admin` |
| 管理员密码 | `admin123456` |
| 登录路由 | `/login` |
| 监听端口 | `49466` |
| 服务端程序 | 自动探测 `ezsshd`（可用 `EZSSHD` 环境变量指定） |
| 数据目录 | `~/.ezssh/data` |

向导会：后台启动服务 → 轮询健康 → 首次使用自动创建管理员 → 设置登录路由，最后打印访问地址。

**交互式管理菜单（`ezssh`）**：

```
EZSSH 管理终端 v0.0.1
服务: http://127.0.0.1:49466    状态: 运行中 (PID 1234)

  1) 运行状态      2) 查看账号信息
  3) 修改账号密码  4) 修改登录路由
  5) 停止服务      6) 启动服务
  0) 退出
```

- **1 运行状态**：PID、HTTP 健康、版本、初始化状态、保险库解锁状态、登录路由、界面语言
- **2 查看账号信息**：账号、密码、登录路由、访问地址
- **3 修改账号密码**：校验旧密码（回车默认本地已存值），改密后自动同步本地配置
- **4 修改登录路由**：以 `/` 开头、不含空格/`#`/`?`，改后同步服务端
- **5/6 停止/启动服务**：后台进程管理（PID 文件 + 健康探测判活，跨平台）

**一次性子命令**（便于脚本调用）：`ezssh status|account|passwd|route|start|stop`；`passwd`/`route` 仍以交互方式接收输入。

**配置文件**：默认 `~/.ezssh/agent.json`（可用 `--config <path>` 指定）。密码以**明文**存储在该文件（权限 0600），仅本用户可读；若不想明文落盘，可删除配置中的密码字段，但「查看账号信息」将不再显示密码、自动登录类操作需手动输入。

> Agent 完全走现有 REST API（`/api/init`、`/api/login`、`/api/init-status`、`/api/settings`、`/api/change-password`），API 路径固定，不受自定义登录路由影响。

1. 浏览器访问 `http://127.0.0.1:49466`，进入初始化向导创建管理员口令（≥8 位）。界面默认英文，可在「设置 → 语言」切换简体中文。
2. 登录后进入"主机管理"页，新增 SSH 主机（密码或私钥认证），凭据经 AES-256-GCM 加密入库。表单内置「🛠 测试连通性」按钮，可先验证配置再保存。
3. 点击"进入桌面"进入 Web 桌面：
   - **双击**服务器图标 → 打开文件管理器
   - **右键**服务器图标 → 选择打开 终端/文件管理器/任务管理器/Docker/防火墙/直链下载，或编辑服务器
   - **右键桌面空白处** → 刷新 / 添加服务器 / 设置 / 自动对齐 / 退出登录
   - **左键拖动**服务器图标可调整位置（可开启自动对齐）
   - 任务栏 🪟 打开应用中心（世界地图、添加服务器、设置、直链下载、一键命令、网站管理）
4. 登录失败一次后需输入**验证码**；可在「设置」中修改密码、设置安全路由、更换桌面背景、隐藏图标监控。

### 前端构建

后端可直接托管前端产物（`web/dist`）：

```bash
cd web
npm install
npm run build
```

开发态可分别启动：

```bash
# 终端 A：后端
go run ./cmd/ezssh
# 终端 B：前端热更新（默认 5173 端口）
cd web && npm run dev
```

### REST API

**认证与设置**
```
POST   /api/init                 # 首次初始化
GET    /api/init-status          # 初始化/保险库状态/登录路由/界面语言/版本号
POST   /api/login                # 登录（失败需验证码，顺带解锁保险库）
POST   /api/logout
GET    /api/me                   # 当前用户与会话状态
GET    /api/captcha              # 获取验证码（id + SVG）
GET    /api/settings             # 读取设置（登录路由、界面语言）
PUT    /api/settings             # 修改设置（自定义登录路由 / 界面语言）
POST   /api/change-password      # 修改口令（自动重加密全部主机凭据）
```

**主机与 SSH**
```
GET    /api/hosts                # 主机列表（含连接状态/指纹）
POST   /api/hosts                # 新增主机
PUT    /api/hosts/{id}           # 编辑主机
DELETE /api/hosts/{id}           # 删除主机
POST   /api/hosts/{id}/connect   # 建立 SSH 连接（预热）
GET    /api/hosts/{id}/status    # 连接状态
POST   /api/hosts/{id}/exec      # 执行命令
POST   /api/test-connect         # 用表单参数测试 SSH 连通性（不持久化）
```

**SFTP 文件操作**
```
GET    /api/hosts/{id}/sftp/list        # 目录列表
POST   /api/hosts/{id}/sftp/mkdir       # 新建目录
POST   /api/hosts/{id}/sftp/rename      # 重命名
POST   /api/hosts/{id}/sftp/remove      # 删除（递归）
POST   /api/hosts/{id}/sftp/chmod       # 修改权限
GET    /api/hosts/{id}/sftp/read        # 读取文本内容
POST   /api/hosts/{id}/sftp/write       # 写入文本内容
GET    /api/hosts/{id}/sftp/download    # 下载（支持 Range；?inline=1 内联预览）
POST   /api/hosts/{id}/sftp/upload      # 上传（流式 multipart，实时进度）
POST   /api/hosts/{id}/sftp/paste       # 复制/剪切粘贴（同机/直连/中转，NDJSON 流式进度）
```

**地理信息**
```
GET    /api/geo?hosts=a,b,c       # 批量 IP 地理定位（并行查询，结果以地址为键）
```
- 依次尝试 ip-api.com → ipapi.co，6s 超时，结果 7 天缓存
- 用于桌面图标国旗与世界地图

**网站管理（Nginx + Let's Encrypt）**
```
GET    /api/websites?host_id=&group=          # 站点列表（按服务器/分组过滤）
GET    /api/websites/groups?host_id=          # 站点分组列表
POST   /api/websites                          # 建站（落库）
PUT    /api/websites/{id}                     # 编辑站点
DELETE /api/websites/{id}                     # 删除站点（远端删 conf + reload + 清理证书）
POST   /api/websites/{id}/deploy?lang=        # 部署（写配置 + nginx -t + reload）
POST   /api/websites/{id}/enable              # 启用/停用（重新部署）
GET    /api/nginx/status?host_id=             # Nginx 安装/运行状态
POST   /api/nginx/install                     # 一键安装 Nginx（NDJSON 流式）
GET    /api/certificates?host_id=             # 证书列表
POST   /api/certificates/issue                # 签发证书（NDJSON 流式；http/dns 两种方式）
POST   /api/certificates/{id}/renew           # 强制续签（NDJSON 流式）
POST   /api/certificates/{id}/sync            # 同步到期时间
DELETE /api/certificates/{id}                 # 删除证书记录
GET    /api/certificates/check?host_id=&domain=  # 检测证书是否已安装 + 到期时间
GET/POST   /api/dns-accounts                  # DNS 账户列表 / 新增
PUT/DELETE /api/dns-accounts/{id}             # 编辑 / 删除 DNS 账户
```

**WebSocket `/ws`**：统一消息信封 `{type, hostId, channelId, payload}`。

| type | 说明 |
|---|---|
| `terminal.open/input/output/resize/close/exit` | 终端全链路 |
| `monitor.subscribe/unsubscribe/data` | 实时监控订阅（每 2s 采样推送） |
| `monitor.hwinfo` | 一次性采集系统/硬件静态信息（发行版、CPU 型号、虚拟机识别） |
| `ps.list` / `ps.kill` | 进程列表 / 发信号 |
| `docker.list/images/stats/action/logs/check` | Docker 容器/镜像/统计/日志/操作/检测 |
| `docker.install` (+ `.output/.done`) | 一键安装 Docker（流式输出） |
| `docker.create/inspect` (+ `create.stream/.output/.done`) | 应用市场一键创建容器 / 容器详情 |
| `firewall.status/set/list` | 防火墙状态 / 开关 / 规则列表 |
| `firewall.rule.add/remove` | 新增 / 删除规则 |
| `download.check` | 检测 aria2 是否已安装 |
| `download.install` (+ `.output/.done`) | 一键安装 aria2（流式输出） |
| `download.add` | 添加直链 / 种子下载任务 |
| `download.list` / `download.listdir` | 任务列表（前端轮询）/ 目标机目录浏览 |
| `download.pause/resume/cancel` | 暂停 / 继续 / 取消 |
| `ping` → `pong` | 心跳 |

### Docker 部署

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
```

- 数据（数据库 + 加密凭据）持久化在 `/app/data` 卷
- 默认监听 `0.0.0.0:49466`，可用 `-e EZSSH_PORT=xxx` 覆盖

### 数据目录

所有持久化数据统一收纳在 `data/` 目录（默认位于工作目录下），数据库即 `data/ezssh.db`：

- 环境变量 `EZSSH_DATA` 可覆盖数据目录位置；`EZSSH_DB` 可显式指定数据库文件路径
- 旧版本根目录的 `ezssh.db` 会在首次启动时自动迁移到 `data/`（非破坏性复制，旧文件保留，确认无误后可手动删除）
- Docker 部署时把宿主机的 `./data` 映射到容器的 `/app/data` 即可完成备份/迁移：

```bash
docker run -d --name ezssh -p 49466:49466 -v "$(pwd)/data:/app/data" ezssh
```

### 测试

```bash
go test -buildvcs=false -timeout 60s ./...
```

- `internal/vault`：凭据保险库加密往返、口令校验、篡改检测
- `internal/captcha`：验证码生成与校验
- `internal/apps`：监控、进程、硬件信息、防火墙规则解析
- `internal/api`：内置本地 SSH 服务器，全链路覆盖初始化/登录/主机 CRUD/建连/终端/文件（含复制粘贴）/监控/进程/Docker/SFTP/地理定位/设置语言

## 安全特性

- **凭据保险库**：主机密码/私钥以 AES-256-GCM 加密入库，密钥由登录口令经 Argon2id 派生，重启后需重新解锁
- **修改口令重加密**：改密时用新口令派生新密钥并重加密全部主机凭据，旧口令无法解密
- **登录验证码**：某 IP 失败一次后要求输入 SVG 验证码（一次性、5 分钟过期），失败 5 次/分钟锁定 5 分钟
- **自定义登录路由**：可修改登录页地址（如 `/secret-admin`），隐藏登录入口
- **TOFU 主机密钥确认**：首次连接记录远端主机密钥指纹，后续连接校验一致，防止中间人替换
- **高危操作审计**：登录/连接/kill/删除容器/改密/改设置均写入审计日志
- **Web 安全**：终端输出仅经 xterm 渲染不入 DOM（防 XSS）；参数化 SQL；验证码 SVG 由后端生成不入第三方
- **传输安全**：默认监听 127.0.0.1，公网部署需 HTTPS 反代（见下文）

> 公网部署强烈建议置于 Caddy/Nginx 之后并启用 HTTPS；不要直接暴露裸 HTTP 到公网。

## 目录结构

```
cmd/ezssh/          # Go 入口（env：EZSSH_LISTEN/PORT/DATA/DB/MASTER_KEY），安装脚本中命名为 ezsshd
cmd/ezcli/          # 终端 Agent（产物 ezssh：安装向导 + 交互管理菜单）
scripts/install.sh  # 一键安装脚本（编译服务端+Agent、构建前端、进入初始化向导）
internal/
  auth/             # 会话、限流
  captcha/          # SVG 验证码生成与校验
  vault/            # 凭据保险库（AES-256-GCM + Argon2id）
  sshhub/           # ConnectionHub：连接池、保活、惰性重连、TOFU
  geo/              # IP 地理定位（ip-api.com/ipapi.co，7 天缓存）
  apps/             # terminal / sftp / monitor / process / docker /
                    #   firewall（ufw 防火墙）/ download（aria2 直链下载）/ hwinfo（硬件信息）/
                    #   nginx（网站管理）/ cert（Let's Encrypt 证书）/ winutil（Windows 解析）
  api/              # REST + WebSocket 路由与处理器
  store/            # SQLite 访问层
data/               # 运行时数据目录（ezssh.db，默认，可用 EZSSH_DATA 覆盖）
web/                # React SPA（桌面 + 十大 App + 设置：个性化/桌面/安全/语言/关于）
  public/           # 静态资源（含世界地图 geo JSON）
  src/
    desktop/        # 窗口管理器、任务栏、应用中心、应用注册表、openApp（跨应用开窗）
    apps/           # 终端/文件/监控/进程/Docker/防火墙/直链下载/世界地图/一键命令/网站管理/设置
                    #   + dockerMarket 模板
    components/     # HostFormModal / OsLogo（系统 Logo）/ FlagBadge（国旗）等
    lib/            # api/ws/session/monitorStore/osStore/geoStore/monitorBridge/
                    #   clipboardStore/desktopSettingsStore/desktopPresets/
                    #   i18n+i18nDict（国际化）/ escClose（ESC 关闭）/ fmLaunch+terminalLaunch（打开目录）
Dockerfile          # 多阶段构建
```
