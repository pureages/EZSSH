# EZSSH

版本：v0.0.1

**干净、高效、可视化的自托管中心化 SSH Web 网关**：仅依赖目标机的原生 SSH / SFTP 服务，无需在服务器上安装任何 Agent，也不开放额外端口，安全可控。

如果你想要更详细的内容请查看[文档](doc/user-guide.html)

## 快速开始

### 1. 一键安装（复制粘贴即可）

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

脚本会自动下载源码、检测平台/架构、编译服务端与终端 Agent、构建前端，并进入初始化向导。

> 需要 Ubuntu 已安装 Go 1.25+。也可以先克隆仓库再本地安装：`git clone https://github.com/pureages/EZSSH.git && cd EZSSH && bash scripts/install.sh`

### 2. Docker 部署

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
```

### 3. 自行构建

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
