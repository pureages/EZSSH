# EZSSH

> English | [简体中文](README.md)

Version: v0.0.5-2

**A lightweight, clean, visualized, native SSH / SFTP-driven self-hosted centralized SSH web gateway, powered by Go** — no Agent required on servers, safe and controllable.

For more details, see the [User Guide](doc/user-guide.en.md)

## Preview

<p align="center">
  <img src="img/1.png" alt="EZSSH Preview 1" width="48%" />
  <img src="img/3.png" alt="EZSSH Preview 2" width="48%" />
</p>

## Features

1. **Visual operations**: native SSH / SFTP driven — no background Agent required on servers, safe and controllable.
2. **Cross-server file transfer**: copy and paste files directly between different servers.
3. **Docker management**: manage Docker visually and centrally.
4. **Server monitoring**: monitor server status in real time, with a built-in task manager.
5. **One-click commands**: run frequently used commands with one click, and manage sites centrally.

## Quick Start

### 1. One-Click Install (Copy & Paste)

Supports Linux / macOS / Windows(msys).

**Install script**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**or**

**CN mirror install script** (Gitee source, for users who can't reach GitHub from mainland China):

```bash
EZSSH_SRC=gitee bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/main/scripts/install.sh)
```

### 2. Docker Deployment

```bash
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  pureages/ezssh:latest
```

### 3. Build from Source

**Requirements**: Go 1.22+ (to build the backend), Node.js 20+ and npm (only needed to build the frontend).

```bash
# Backend
go build -buildvcs=false ./cmd/ezssh
./ezsshd

# Frontend (optional; the backend can also serve web/dist directly)
cd web && npm install && npm run build
```

Listens on `127.0.0.1:49466` by default (overridable via the `EZSSH_LISTEN` / `EZSSH_PORT` environment variables).

## License

[MIT](LICENSE)
