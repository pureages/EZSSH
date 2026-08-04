# EZSSH

> English | [简体中文](README.md)

Version: v0.0.1

**A clean, efficient, visualized self-hosted centralized SSH web gateway, powered by Go**: relies only on the target machines' native SSH / SFTP services — no Agent installation required on servers, no extra ports opened, safe and controllable.

For more details, see the [User Guide](doc/user-guide.en.md)

## Preview

<p align="center">
  <img src="img/1.png" alt="EZSSH Preview 1" width="48%" />
  <img src="img/2.png" alt="EZSSH Preview 2" width="48%" />
  <br/>
  <img src="img/3.png" alt="EZSSH Preview 3" width="48%" />
</p>

## Quick Start

### 1. One-Click Install (Copy & Paste)

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

The script downloads prebuilt artifacts directly from GitHub Releases (server + terminal Agent + web UI). **No Go / Node.js required, and it never clones the source code.** The initialization wizard starts automatically after installation.

> Supports Linux / macOS / Windows(msys). You can also build from source: `git clone https://github.com/pureages/EZSSH.git && cd EZSSH && bash scripts/install.sh`

### 2. Docker Deployment

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
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
