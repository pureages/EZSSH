# EZSSH

> [English](README.en.md) | 简体中文

版本：v0.0.2

**干净、高效、可视化的自托管中心化 SSH Web 网关，GO驱动**：仅依赖目标机的原生 SSH / SFTP 服务，无需在服务器上安装任何 Agent，也不开放额外端口，安全可控。

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
bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/main/scripts/install-cn.sh)
```


### 2. Docker 部署

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
```

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
