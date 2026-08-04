# EZSSH

> [English](README.en.md) | 简体中文

版本：v0.0.5-2

**原生 SSH / SFTP 驱动的轻量干净、可视化的服务器 / VPS 自托管中心化 Web 网关，GO 驱动**，服务器无需任何 Agent，安全可控。

如果你想要更详细的内容请查看[用户手册](doc/user-guide.md)

## 预览

<p align="center">
  <img src="img/1.png" alt="EZSSH 预览 1" width="48%" />
  <img src="img/3.png" alt="EZSSH 预览 2" width="48%" />
</p>

## 特点

1. **可视化操作**：原生 SSH / SFTP 驱动，服务器无需任何后台 Agent，安全可控。
2. **跨服务器文件传输**：支持在不同服务器之间直接复制粘贴文件。
3. **Docker 管理**：可视化统一管理 Docker。
4. **服务器监控**：实时监控服务器状态，内置任务管理器。
5. **一键命令**：常用命令一键执行，站点统一管理。

## 快速开始

### 1. 一键安装（复制粘贴即可）

支持 Linux / macOS / Windows(msys)。

**安装脚本**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**或**

**国内加速-安装脚本**（Gitee 源）：

```bash
EZSSH_SRC=gitee bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/main/scripts/install.sh)
```

### 2. Docker 部署

```bash
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  pureages/ezssh:latest
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
