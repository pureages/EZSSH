# EZSSH User Guide

A clean, efficient, visualized self-hosted centralized SSH web gateway · Version v0.0.3 · Updated 2026-08-04

## Table of Contents

- [1. Introduction](#s1)
- [2. Quick Start](#s2)
  - [2.1 One-Click Install](#s2-1)
  - [2.2 Docker Deployment](#s2-2)
  - [2.3 Build from Source](#s2-3)
- [3. Web Desktop Usage](#s3)
- [4. Server Types](#s4)
  - [4.1 Install OpenSSH Server on Windows](#s4-1)
- [5. Security Features](#s5)
- [6. Deployment & Operations](#s6)
  - [6.1 Environment Variables](#s6-1)
  - [6.2 Data Directory & Backup](#s6-2)
  - [6.3 Public Deployment Recommendations](#s6-3)

<a id="s1"></a>
## 1. Introduction

**A clean, efficient, visualized self-hosted centralized SSH web gateway, powered by Go**: relies only on the target machines' native SSH / SFTP services — no Agent installation required on servers, no extra ports opened, safe and controllable.

The system consists of two parts:

- **Gateway backend** (Go): provides REST + WebSocket services, handling authentication, the credential vault, the SSH connection pool (ConnectionHub), SFTP file operations and all management logic. Data is persisted in SQLite.
- **Web frontend** (React + Vite): the desktop shell and windowed apps in the browser, communicating with the gateway over HTTP / WebSocket.

<a id="s2"></a>
## 2. Quick Start

<a id="s2-1"></a>
### 1. One-Click Install (Copy & Paste)

Supports Linux / macOS / Windows(msys).

**International install script**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
```

**CN mirror install script** (for users who can't reach GitHub from mainland China):

```bash
EZSSH_SRC=gitee bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/main/scripts/install.sh)
```

<a id="s2-2"></a>
### 2.2 Docker Deployment

```bash
docker build -t ezssh .
docker run -d --name ezssh -p 49466:49466 \
  -v ezssh-data:/app/data \
  ezssh
```

<a id="s2-3"></a>
### 2.3 Build from Source

**Requirements**: Go 1.22+ (to build the backend), Node.js 20+ and npm (only needed to build the frontend).

```bash
# Backend
go build -buildvcs=false ./cmd/ezssh
./ezsshd

# Frontend (optional; the backend can also serve web/dist directly)
cd web && npm install && npm run build
```

Listens on `127.0.0.1:49466` by default (overridable via the `EZSSH_LISTEN` / `EZSSH_PORT` environment variables).

<a id="s3"></a>
## 3. Web Desktop Usage

After login you enter a Windows-like web desktop to manage all servers with windowed apps:

- Apps open as **windows** that can be dragged, resized, minimized / maximized / closed via the top-right buttons; **press ESC to close the active window**.
- **Server icons**: double-click to open the file manager; right-click for a context menu (Terminal / Files / Task Manager / Docker / Firewall / Downloads / Edit); drag with the left button to reposition. Icons show the system logo, country flag and a three-line mini monitor (CPU|Memory|Disk, upload/download rates, total traffic).
- **Taskbar & App Center**: the bottom taskbar manages open windows; the 🪟 button opens the App Center (World Map, Add Server, Settings, Downloads, Quick Commands, Websites).
- **Background progress**: long-running operations (Nginx install, site deployment, certificate issuance, file copy/paste, etc.) can be minimized to run in the background — the taskbar badge auto-collapses on success, and the window restores automatically to show an error on failure.

<a id="s4"></a>
## 4. Server Types

New servers support two categories: **Linux** and **Windows**.

- **Linux**: any SSH-enabled distribution (Ubuntu / Debian / CentOS / Alpine, etc.), with password / private-key authentication and access to all apps.
- **Windows**: Windows servers that support SSH connections (OpenSSH Server must be enabled); monitoring, process and file management apps are available.

> **Note**: Adding a Windows server uses the **SSH port**, not the RDP 3389 port. You must install OpenSSH Server on the server.

<a id="s4-1"></a>
### 4.1 Install OpenSSH Server on Windows

On the Windows server to be added, run the following commands in PowerShell as Administrator:

1. **Check if it is already installed** (skip the installation step if the `sshd` service exists):

   ```powershell
   Get-Service -Name sshd
   ```

2. **Install the service**:

   ```powershell
   Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
   ```

3. **Start the service and set it to auto-start**:

   ```powershell
   Start-Service sshd
   Set-Service -Name sshd -StartupType 'Automatic'
   ```

4. **Check the firewall rules** (make sure inbound port 22 is allowed):

   ```powershell
   Get-NetFirewallRule -DisplayName "*SSH*" | Get-NetFirewallPortFilter | Where-Object {$_.LocalPort -eq 22}
   ```

   If no matching rule is found, create one with:

   ```powershell
   New-NetFirewallRule -Name "OpenSSH-Server-In-TCP" -DisplayName "OpenSSH Server (SSH)" -Enabled True -Direction Inbound -Protocol TCP -LocalPort 22 -Action Allow
   ```

<a id="s5"></a>
## 5. Security Features

- **Credential vault**: host passwords / private keys are encrypted with AES-256-GCM; the key is derived from the login passphrase via Argon2id. After a restart you must log in again to unlock.
- **Re-encryption on password change**: changing the passphrase derives a new key and re-encrypts all host credentials; the old passphrase can no longer decrypt them.
- **Login captcha**: after a failed login from an IP, an SVG captcha is required (single-use, 5-minute expiry); 5 failures per minute lock out the IP for 5 minutes.
- **Custom login route**: the login page address can be changed (e.g. `/secret-admin`) under Settings → Security to hide the entry point.
- **TOFU host key confirmation**: the remote host key fingerprint is recorded on first connection and verified afterwards, preventing man-in-the-middle replacement.
- **Audit logging of risky operations**: login / connect / kill / container delete / password change / settings change are all written to the audit log.
- **Web security**: terminal output is rendered only through xterm and never touches the DOM (XSS protection); SQL is parameterized; the captcha SVG is generated server-side.

<a id="s6"></a>
## 6. Deployment & Operations

<a id="s6-1"></a>
### 6.1 Environment Variables

| Variable | Default | Description |
|---|---|---|
| `EZSSH_LISTEN` | `127.0.0.1` | Listen address |
| `EZSSH_PORT` | `49466` | Listen port |
| `EZSSH_DATA` | `data` | Data directory (all persistent data including the database) |
| `EZSSH_DB` | `data/ezssh.db` | SQLite database path (takes priority if explicitly set) |
| `EZSSH_MASTER_KEY` | (empty) | Inject a master key to skip "unlock the vault at login" |

<a id="s6-2"></a>
### 6.2 Data Directory & Backup

- All persistent data is stored under the `data/` directory; the database is `data/ezssh.db`.
- A legacy `ezssh.db` at the repo root is automatically migrated to `data/` on first start (non-destructive copy; the old file is kept and can be deleted manually once verified).
- **Backup / migration**: with Docker, mount the host's `./data` to the container's `/app/data` to back up or migrate.

<a id="s6-3"></a>
### 6.3 Public Deployment Recommendations

> **Warning**: the default listen address is 127.0.0.1. For public deployments it is strongly recommended to place EZSSH behind Caddy / Nginx and enable HTTPS; do not expose raw HTTP to the public internet.

---

English | [简体中文](user-guide.md)
