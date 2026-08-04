# EZSSH

> [English](README.en.md) | 简体中文

版本：v0.0.5

**原生 SSH / SFTP驱动的轻量干净、可视化的自托管中心化 SSH Web 网关，GO驱动**，服务器无需任何 Agent，安全可控。

如果你想要更详细的内容请查看[用户手册](doc/user-guide.md)

## 预览

<p align="center">
  <img src="img/1.png" alt="EZSSH 预览 1" width="48%" />
  <img src="img/3.png" alt="EZSSH 预览 2" width="48%" />
</p>

## 快速开始

### 1. 一键安装（复制粘贴即可）

支持 Linux / macOS / Windows(msys)。

**国际-安装脚本**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**国内加速-安装脚本**：

```bash
EZSSH_SRC=gitee bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/main/scripts/install.sh)
```


### 2. Docker 部署

```bash
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  pureages/ezssh:latest
```

> 镜像仅包含编译后的服务端与前端产物，不含源码。支持 `linux/amd64`、`linux/arm64`。也可从源码自构建：`docker build -t ezssh . && docker run -d --name ezssh -p 49466:49466 -v ezssh-data:/app/data ezssh`

### 3. 自行构建

**环境要求**:Go 1.22+（构建后端）、Node.js 20+ 与 npm（仅构建前端时需要）。

```bash
# 后端
go build -buildvcs=false ./cmd/ezssh
./ezsshd

# 前端（可选，后端也可直接托管 web/dist）
cd web && npm install && npm run build
```

默认监听 `127.0.0.1:49466`（可用环境变量 `EZSSH_LISTEN` / `EZSSH_PORT` 覆盖）。

## License

[MIT](LICENSE)
