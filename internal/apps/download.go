package apps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"ezssh/internal/sshhub"
	"ezssh/internal/winpath"
)

// aria2 RPC 只绑定远端 127.0.0.1，通过 SSH direct-tcpip 隧道访问，不对外暴露端口。
const aria2RPCAddr = "127.0.0.1:16800"

// aria2 数据目录：daemon 状态与预下载暂存文件都放在这里。
const aria2Home = "/tmp/ezssh_aria2"

// DownloadManager 提供“把链接下载到服务器磁盘”的能力：
// 直链 / 种子两种来源，经远端 aria2c 下载，支持暂停、继续、取消与实时进度。
type DownloadManager struct {
	hub *sshhub.Hub

	mu       sync.Mutex
	tasks    map[string]*DownloadTask
	clients  map[string]*aria2RPC
	ensureMu sync.Mutex // 串行化 daemon 启动，避免并发重复拉起

	execRPCMu    sync.Mutex
	execRPCHosts map[string]bool // 目标机 sshd 禁 TCP 转发（AllowTcpForwarding no）时，RPC 降级走 exec 通道
}

// Aria2Status 目标机 aria2 安装状态。
type Aria2Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

// DownloadTask 一个下载任务。
type DownloadTask struct {
	ID        string `json:"id"`
	HostID    string `json:"hostId"` // 目标主机（保存磁盘所在）
	URL       string `json:"url"`
	Dir       string `json:"dir"` // 目标机保存目录
	Name      string `json:"name"`
	GID       string `json:"gid"` // aria2 gid
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Speed     int64  `json:"speed"`
	Error     string `json:"error"`
	CreatedAt int64  `json:"createdAt"`
}

var dlTaskSeq int64

func newTaskID() string {
	return fmt.Sprintf("dl_%x_%d", time.Now().UnixNano(), atomic.AddInt64(&dlTaskSeq, 1))
}

func NewDownloadManager(hub *sshhub.Hub) *DownloadManager {
	return &DownloadManager{
		hub:          hub,
		tasks:        make(map[string]*DownloadTask),
		clients:      make(map[string]*aria2RPC),
		execRPCHosts: make(map[string]bool),
	}
}

// isWindows 判断目标机是否为 Windows（探测失败按 Linux 处理）。
func (m *DownloadManager) isWindows(hostID string) bool {
	p, err := m.hub.Platform(hostID)
	if err != nil {
		return false
	}
	return p == "windows"
}

// CheckInstalled 检测目标机是否已安装 aria2。
func (m *DownloadManager) CheckInstalled(hostID string) (Aria2Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out string
	var err error
	if m.isWindows(hostID) {
		out, err = m.exec(ctx, hostID, winPS(winAria2CheckScript))
	} else {
		out, err = m.exec(ctx, hostID,
			`if command -v aria2c >/dev/null 2>&1; then aria2c --version | head -1; else echo "__ARIA2_NOT_FOUND__"; fi`)
	}
	if err != nil {
		return Aria2Status{}, err
	}
	out = strings.TrimSpace(out)
	if strings.Contains(out, "__ARIA2_NOT_FOUND__") {
		return Aria2Status{Installed: false}, nil
	}
	return Aria2Status{Installed: true, Version: out}, nil
}

// winAria2CheckScript 检测 Windows 上的 aria2：PATH 与 %USERPROFILE%\ezssh_aria2\aria2c.exe 都查。
// Install 把 aria2c.exe 装到用户目录（不在 PATH），必须一并检测，否则装完前端仍显示未安装。
const winAria2CheckScript = "$c = Get-Command aria2c -ErrorAction SilentlyContinue\n" +
	"if (-not $c) {\n" +
	"  $p = Join-Path $env:USERPROFILE 'ezssh_aria2\\aria2c.exe'\n" +
	"  if (Test-Path $p) { & $p --version | Select-Object -First 1; exit 0 }\n" +
	"}\n" +
	"if (-not $c) { Write-Output '__ARIA2_NOT_FOUND__'; exit 0 }\n" +
	"& $c.Source --version | Select-Object -First 1"

// winAria2InstallScript 在 Windows 上免管理员安装 aria2：
// 下载 aria2 1.37.0 win64 zip 到用户目录并解压，把 aria2c.exe 拷到 %USERPROFILE%\ezssh_aria2\aria2c.exe。
const winAria2InstallScript = "$ErrorActionPreference = 'Stop'\n" +
	"$c = Get-Command aria2c -ErrorAction SilentlyContinue\n" +
	"if ($c) { Write-Output ('aria2 已安装: ' + (& $c.Source --version | Select-Object -First 1)); exit 0 }\n" +
	"$D = Join-Path $env:USERPROFILE 'ezssh_aria2'\n" +
	"$exe = Join-Path $D 'aria2c.exe'\n" +
	"if (Test-Path $exe) { Write-Output ('aria2 已安装: ' + (& $exe --version | Select-Object -First 1)); exit 0 }\n" +
	"Write-Output '==> 下载 aria2 1.37.0 (win64)'\n" +
	"New-Item -ItemType Directory -Force -Path $D | Out-Null\n" +
	"$zip = Join-Path $D 'aria2-1.37.0.zip'\n" +
	"$url = 'https://github.com/aria2/aria2/releases/download/release-1.37.0/aria2-1.37.0-win-64bit-build1.zip'\n" +
	"[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12\n" +
	"Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing\n" +
	"Write-Output '==> 解压安装'\n" +
	"Expand-Archive -Path $zip -DestinationPath $D -Force\n" +
	"$found = Get-ChildItem -Path $D -Recurse -Filter aria2c.exe | Select-Object -First 1\n" +
	"if (-not $found) { throw '未在压缩包中找到 aria2c.exe' }\n" +
	"Copy-Item $found.FullName $exe -Force\n" +
	"Remove-Item $zip -Force\n" +
	"Write-Output '==> 安装完成'\n" +
	"& $exe --version | Select-Object -First 1"

// winAria2EnsureScript 在 Windows 上保证 aria2 RPC daemon 就绪，并把 RPC 密钥输出到 stdout。
// 与 Linux 版契约一致：stdout 输出密钥；失败输出 __NO_ARIA2__ / __START_FAILED__（附日志尾部）。
const winAria2EnsureScript = "$ErrorActionPreference = 'Stop'\n" +
	"$D = Join-Path $env:USERPROFILE 'ezssh_aria2'\n" +
	"New-Item -ItemType Directory -Force -Path $D | Out-Null\n" +
	"$secretFile = Join-Path $D 'rpc-token'\n" +
	"$pidFile = Join-Path $D 'pid'\n" +
	"$verFile = Join-Path $D 'version'\n" +
	"$logFile = Join-Path $D 'start.log'\n" +
	"$V = 2\n" +
	"# 快速路径：已有存活 daemon 且密钥有效，直接复用\n" +
	"if ((Test-Path $verFile) -and ((Get-Content $verFile -ErrorAction SilentlyContinue) -eq $V) -and (Test-Path $pidFile) -and (Test-Path $secretFile)) {\n" +
	"  $oldPid = (Get-Content $pidFile -ErrorAction SilentlyContinue).Trim()\n" +
	"  if ($oldPid -and (Get-Process -Id $oldPid -ErrorAction SilentlyContinue)) {\n" +
	"    Write-Output (Get-Content $secretFile -Raw).Trim()\n" +
	"    exit 0\n" +
	"  }\n" +
	"}\n" +
	"# 清理占用 16800 的孤儿 daemon（Win7 无 NetTCPConnection 模块时跳过，不影响主流程）\n" +
	"if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) {\n" +
	"  $conns = Get-NetTCPConnection -LocalPort 16800 -State Listen -ErrorAction SilentlyContinue\n" +
	"  foreach ($conn in $conns) { Stop-Process -Id $conn.OwningProcess -Force -ErrorAction SilentlyContinue }\n" +
	"  Start-Sleep -Seconds 1\n" +
	"}\n" +
	"# 定位 aria2c（PATH 或用户目录，Install 只装到用户目录不在 PATH）\n" +
	"$BIN = $null\n" +
	"$c = Get-Command aria2c -ErrorAction SilentlyContinue\n" +
	"if ($c) { $BIN = $c.Source }\n" +
	"if (-not $BIN) {\n" +
	"  $p = Join-Path $D 'aria2c.exe'\n" +
	"  if (Test-Path $p) { $BIN = $p }\n" +
	"}\n" +
	"if (-not $BIN) { Write-Output '__NO_ARIA2__'; exit 1 }\n" +
	"$SECRET = 'ezssh_' + ([guid]::NewGuid().ToString('N'))\n" +
	"Set-Content -Path $secretFile -Value $SECRET -NoNewline\n" +
	"$trackers = 'udp://tracker.opentrackr.org:1337/announce,udp://open.demonii.com:1337/announce,udp://open.stealth.si:80/announce,udp://tracker.torrent.eu.org:451/announce,udp://explodie.org:6969/announce,udp://tracker.cyberia.is:6969/announce,udp://exodus.desync.com:6969/announce,udp://tracker.moeking.me:6969/announce,http://tracker.opentrackr.org:1337/announce,http://tracker.openbittorrent.com:80/announce,https://tracker.gbitt.info/announce'\n" +
	"$args = @('--enable-rpc', '--rpc-listen-all=false', '--rpc-listen-port=16800', ('--rpc-secret=' + $SECRET), ('--dir=\"' + $D + '\"'), '--continue=true', '--max-concurrent-downloads=8', '--split=8', '--max-connection-per-server=8', '--file-allocation=none', '--seed-time=0', '--auto-file-renaming=true', '--allow-overwrite=false', '--log-level=warn', '--enable-dht=true', '--dht-listen-port=6881', '--bt-max-peers=200', '--bt-save-metadata=true', '--bt-load-saved-metadata=true', ('--bt-tracker=' + $trackers))\n" +
	"$proc = Start-Process -FilePath $BIN -ArgumentList $args -WindowStyle Hidden -PassThru -RedirectStandardError $logFile\n" +
	"Set-Content -Path $pidFile -Value $proc.Id -NoNewline\n" +
	"Set-Content -Path $verFile -Value $V -NoNewline\n" +
	"for ($i = 0; $i -lt 10; $i++) {\n" +
	"  Start-Sleep -Seconds 1\n" +
	"  if (Get-Process -Id $proc.Id -ErrorAction SilentlyContinue) {\n" +
	"    Write-Output $SECRET\n" +
	"    exit 0\n" +
	"  }\n" +
	"}\n" +
	"Write-Output '__START_FAILED__'\n" +
	"if (Test-Path $logFile) { Get-Content $logFile -Tail 8 }\n" +
	"exit 1"

// Install 一键安装 aria2，输出逐行流式回调。Linux 走包管理器，Windows 走用户目录解压。
func (m *DownloadManager) Install(hostID string, onLine func(string)) error {
	if m.isWindows(hostID) {
		return m.runScript(hostID, winAria2InstallScript, onLine)
	}
	script := `set -e
if command -v aria2c >/dev/null 2>&1; then
  echo "aria2 已安装: $(aria2c --version | head -1)"
  exit 0
fi
echo "==> 检测系统包管理器"
PM=""
if command -v apt-get >/dev/null 2>&1; then PM=apt
elif command -v dnf >/dev/null 2>&1; then PM=dnf
elif command -v yum >/dev/null 2>&1; then PM=yum
elif command -v apk >/dev/null 2>&1; then PM=apk
fi
if [ -z "$PM" ]; then
  echo "错误：不支持的系统（未检测到 apt/dnf/yum/apk）"
  exit 1
fi
case "$PM" in
  apt) apt-get update -y && apt-get install -y aria2 ;;
  dnf) dnf install -y aria2 ;;
  yum) yum install -y aria2 ;;
  apk) apk add --no-cache aria2 ;;
esac
echo "==> 安装完成"
aria2c --version | head -1`
	return m.runScript(hostID, script, onLine)
}

// Add 新建下载任务。hostID 为目标主机（直连下载）。
// url 为直链，torrentB64 为 base64 编码的种子内容（两者取其一，种子优先）。
// name 为自定义保存文件名（仅直链生效，空则自动取链接文件名）。
func (m *DownloadManager) Add(hostID, url, torrentB64, dir, name string) (*DownloadTask, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if strings.TrimSpace(url) == "" && torrentB64 == "" {
		return nil, fmt.Errorf("请填写下载链接或选择种子文件")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("请填写保存目录")
	}
	if torrentB64 != "" {
		if _, err := base64.StdEncoding.DecodeString(torrentB64); err != nil {
			return nil, fmt.Errorf("种子文件内容无效")
		}
	}

	task := &DownloadTask{
		ID:        newTaskID(),
		HostID:    hostID,
		URL:       url,
		Dir:       strings.TrimRight(strings.TrimSpace(dir), "/"),
		Name:      displayName(url),
		Status:    "waiting",
		CreatedAt: time.Now().Unix(),
	}

	// 确保保存目录存在（aria2 对不存在的目录可能直接失败）
	if m.isWindows(hostID) {
		if _, err := m.exec(ctx, hostID, winPS("New-Item -ItemType Directory -Force -Path "+psLiteral(task.Dir)+" | Out-Null")); err != nil {
			return nil, fmt.Errorf("创建保存目录失败: %w", err)
		}
	} else if _, err := m.exec(ctx, hostID, "mkdir -p -- "+sshQuote(task.Dir)); err != nil {
		return nil, fmt.Errorf("创建保存目录失败: %w", err)
	}

	gid, err := m.addToAria2(ctx, hostID, url, torrentB64, task.Dir, name)
	if err != nil {
		return nil, err
	}
	task.GID = gid

	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	return task, nil
}

// List 返回目标主机的全部任务，并刷新实时进度。
func (m *DownloadManager) List(ctx context.Context, hostID string) []*DownloadTask {
	m.mu.Lock()
	var tasks []*DownloadTask
	for _, t := range m.tasks {
		if t.HostID == hostID {
			tasks = append(tasks, t)
		}
	}
	m.mu.Unlock()

	for _, t := range tasks {
		m.refresh(ctx, t)
	}
	return tasks
}

// Pause 暂停下载。
func (m *DownloadManager) Pause(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t, err := m.get(id)
	if err != nil {
		return err
	}
	if _, err := m.rpc(ctx, t.HostID, "pause", []any{t.GID}); err != nil {
		if _, err2 := m.rpc(ctx, t.HostID, "forcePause", []any{t.GID}); err2 != nil {
			return err2
		}
	}
	return nil
}

// Resume 继续下载。
func (m *DownloadManager) Resume(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t, err := m.get(id)
	if err != nil {
		return err
	}
	_, err = m.rpc(ctx, t.HostID, "unpause", []any{t.GID})
	return err
}

// Cancel 取消下载并清理残留文件，任务从列表移除。
func (m *DownloadManager) Cancel(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t, err := m.get(id)
	if err != nil {
		return err
	}
	// 从 aria2 移除
	if _, err := m.rpc(ctx, t.HostID, "remove", []any{t.GID}); err != nil {
		_, _ = m.rpc(ctx, t.HostID, "forceRemove", []any{t.GID})
	}
	m.cleanupFiles(ctx, t)

	m.mu.Lock()
	delete(m.tasks, id)
	m.mu.Unlock()
	return nil
}

// DirEntry 目标机目录条目。
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// DirListing 目录浏览结果：当前路径 + 父路径 + 条目（仅列目录）。
type DirListing struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent"`
	Entries []DirEntry `json:"entries"`
}

// ListDir 列出目标主机 dir 目录下的子目录，供“保存目录”浏览选择。
func (m *DownloadManager) ListDir(ctx context.Context, hostID, dir string) (*DirListing, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "/"
	}
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		dir = "/"
	}
	if m.isWindows(hostID) {
		return m.listDirWindows(ctx, hostID, dir)
	}
	// 目录名/路径可能包含空格与特殊字符，全部 hex 编码后传输，避免解析歧义
	script := `exec 2>/dev/null
D=` + sshQuote(dir) + `
[ -d "$D" ] || { echo "__NO_DIR__"; exit 1; }
cd "$D" || { echo "__NO_DIR__"; exit 1; }
echo "DIR	$(printf '%s' "$PWD" | od -A n -t x1 | tr -d ' \n')"
P="$(dirname "$PWD")"
echo "PARENT	$(printf '%s' "$P" | od -A n -t x1 | tr -d ' \n')"
for f in * .[!.]* ..?*; do
  [ -e "$f" ] || continue
  if [ -d "$f" ]; then t=d; else t=f; fi
  echo "$t	$(printf '%s' "$f" | od -A n -t x1 | tr -d ' \n')"
done`
	out, err := m.exec(ctx, hostID, script)
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "__NO_DIR__") {
		return nil, fmt.Errorf("目录不存在或无权访问: %s", dir)
	}
	listing := &DirListing{Path: dir}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, '\t')
		if i < 0 {
			continue
		}
		typ, hexName := line[:i], line[i+1:]
		name, derr := hexToStr(hexName)
		if derr != nil {
			continue
		}
		switch typ {
		case "DIR":
			listing.Path = name
		case "PARENT":
			listing.Parent = name
		case "d":
			listing.Entries = append(listing.Entries, DirEntry{Name: name, IsDir: true})
		}
	}
	if listing.Path == "" {
		listing.Path = dir
	}
	return listing, nil
}

// winDirListScript 以 PowerShell 列出目录（仅子目录），输出契约与 Linux 版一致：
// DIR/PARENT/d 行 + hex 编码的名称；路径统一为前向斜杠展示路径（C:/Users）。
// 脚本以 `$D = ` 开头，目标目录由调用方用 psLiteral 追加。
const winDirListScript = "$D = " +
	"$D = $D.TrimEnd('/','\\')\n" +
	"if ($D -eq '') { $D = '/' }\n" +
	"function Hx($s) { $b = [Text.Encoding]::UTF8.GetBytes($s); ($b | ForEach-Object { $_.ToString('x2') }) -join '' }\n" +
	"# 盘符列表\n" +
	"if ($D -eq '/') {\n" +
	"  Write-Output ('DIR\t' + (Hx '/'))\n" +
	"  Write-Output ('PARENT\t' + (Hx '/'))\n" +
	"  Get-PSDrive -PSProvider FileSystem -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '^[a-zA-Z]$' } | ForEach-Object {\n" +
	"    $n = $_.Name + ':'\n" +
	"    if (Test-Path -LiteralPath ($n + '\\')) { Write-Output ('d\t' + (Hx $n)) }\n" +
	"  }\n" +
	"  exit 0\n" +
	"}\n" +
	"# 盘根（C:/ 或 C:）\n" +
	"if ($D -match '^[a-zA-Z]:[/\\\\]?$') {\n" +
	"  Write-Output ('DIR\t' + (Hx $D))\n" +
	"  Write-Output ('PARENT\t' + (Hx '/'))\n" +
	"  Get-ChildItem -LiteralPath $D -Force -ErrorAction SilentlyContinue | Where-Object { $_.PSIsContainer } | ForEach-Object {\n" +
	"    Write-Output ('d\t' + (Hx $_.Name))\n" +
	"  }\n" +
	"  exit 0\n" +
	"}\n" +
	"if (-not (Test-Path -LiteralPath $D)) { Write-Output '__NO_DIR__'; exit 1 }\n" +
	"Write-Output ('DIR\t' + (Hx $D.Replace('\\','/')))\n" +
	"$par = Split-Path -Parent $D\n" +
	"if ($par -eq '') { $par = '/' } else { $par = $par.Replace('\\','/') }\n" +
	"Write-Output ('PARENT\t' + (Hx $par))\n" +
	"Get-ChildItem -LiteralPath $D -Force -ErrorAction SilentlyContinue | Where-Object { $_.PSIsContainer } | ForEach-Object {\n" +
	"  Write-Output ('d\t' + (Hx $_.Name))\n" +
	"}"

// listDirWindows 列出 Windows 目标机目录（仅子目录）。入参为展示路径（可能带 /C: 形式），
// 先经 winpath.ToDisplay 归一为 C:/ 形式再交给 PowerShell。
func (m *DownloadManager) listDirWindows(ctx context.Context, hostID, dir string) (*DirListing, error) {
	dir = winpath.ToDisplay(dir)
	out, err := m.exec(ctx, hostID, winPS(winDirListScript+psLiteral(dir)))
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "__NO_DIR__") {
		return nil, fmt.Errorf("目录不存在或无权访问: %s", dir)
	}
	listing := &DirListing{Path: dir}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, '\t')
		if i < 0 {
			continue
		}
		typ, hexName := line[:i], line[i+1:]
		name, derr := hexToStr(hexName)
		if derr != nil {
			continue
		}
		switch typ {
		case "DIR":
			listing.Path = name
		case "PARENT":
			listing.Parent = name
		case "d":
			listing.Entries = append(listing.Entries, DirEntry{Name: name, IsDir: true})
		}
	}
	if listing.Path == "" {
		listing.Path = dir
	}
	return listing, nil
}

// ---- 内部实现 ----

func (m *DownloadManager) get(id string) (*DownloadTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("任务不存在或已移除")
	}
	return t, nil
}

func (m *DownloadManager) addToAria2(ctx context.Context, hostID, url, torrentB64, dir, name string) (string, error) {
	opts := map[string]any{"dir": dir, "continue": "true"}
	// 直链支持自定义保存文件名；种子的文件名由内容决定，out 对它们无效
	if torrentB64 == "" {
		if n := sanitizeFileName(name); n != "" {
			opts["out"] = n
		}
	}
	var res json.RawMessage
	var err error
	if torrentB64 != "" {
		res, err = m.rpc(ctx, hostID, "addTorrent", []any{torrentB64, []any{}, opts})
	} else {
		res, err = m.rpc(ctx, hostID, "addUri", []any{[]any{url}, opts})
	}
	if err != nil {
		return "", err
	}
	var gid string
	if err := json.Unmarshal(res, &gid); err != nil || gid == "" {
		return "", fmt.Errorf("aria2 未返回任务编号")
	}
	return gid, nil
}

// refresh 用 aria2 tellStatus 刷新任务进度。
func (m *DownloadManager) refresh(ctx context.Context, t *DownloadTask) {
	m.mu.Lock()
	if t.Status == "complete" || t.Status == "removed" {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	res, err := m.rpc(ctx, t.HostID, "tellStatus", []any{t.GID, []any{
		"status", "completedLength", "totalLength", "downloadSpeed",
		"errorMessage", "files", "bittorrent",
	}})
	if err != nil {
		// 查询失败（如 daemon 重启导致 gid 丢失）：标记错误，防止假活跃
		m.mu.Lock()
		if t.Status == "active" || t.Status == "waiting" || t.Status == "paused" {
			t.Status = "error"
			t.Error = "无法获取任务状态: " + err.Error()
		}
		m.mu.Unlock()
		return
	}
	var st struct {
		Status          string `json:"status"`
		CompletedLength string `json:"completedLength"`
		TotalLength     string `json:"totalLength"`
		DownloadSpeed   string `json:"downloadSpeed"`
		ErrorMessage    string `json:"errorMessage"`
		Files           []struct {
			Path string `json:"path"`
		} `json:"files"`
		Bittorrent *struct {
			Info struct {
				Name string `json:"name"`
			} `json:"info"`
		} `json:"bittorrent"`
	}
	if err := json.Unmarshal(res, &st); err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	t.Completed = parseInt(st.CompletedLength)
	t.Total = parseInt(st.TotalLength)
	t.Speed = parseInt(st.DownloadSpeed)

	if st.Bittorrent != nil && st.Bittorrent.Info.Name != "" {
		t.Name = st.Bittorrent.Info.Name
	} else if len(st.Files) > 0 && st.Files[0].Path != "" {
		t.Name = path.Base(st.Files[0].Path)
	}

	switch st.Status {
	case "active":
		t.Status = "active"
		t.Error = ""
	case "waiting":
		t.Status = "waiting"
	case "paused":
		t.Status = "paused"
	case "error":
		t.Status = "error"
		t.Error = st.ErrorMessage
	case "removed":
		t.Status = "removed"
	case "complete":
		t.Status = "complete"
		t.Error = ""
	}
}

// cleanupFiles 取消时清理残留：aria2 已下载的部分文件及 .aria2 断点文件。
func (m *DownloadManager) cleanupFiles(ctx context.Context, t *DownloadTask) {
	m.rmTaskFiles(ctx, t.HostID, t.GID)
}

// rmTaskFiles 删除某 gid 已产生的磁盘文件（含 .aria2 断点文件）。
func (m *DownloadManager) rmTaskFiles(ctx context.Context, hostID, gid string) {
	res, err := m.rpc(ctx, hostID, "tellStatus", []any{gid, []any{"files"}})
	if err != nil {
		return
	}
	var st struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if json.Unmarshal(res, &st) != nil || len(st.Files) == 0 {
		return
	}
	paths := make([]string, 0, len(st.Files)*2)
	for _, f := range st.Files {
		if f.Path == "" {
			continue
		}
		paths = append(paths, f.Path, f.Path+".aria2")
	}
	if len(paths) == 0 {
		return
	}
	if m.isWindows(hostID) {
		var parts []string
		for _, p := range paths {
			parts = append(parts, psLiteral(p))
		}
		// 文件 + .aria2 断点文件一并删除；-Recurse 兜底目录场景
		_, _ = m.exec(ctx, hostID, winPS("Remove-Item -LiteralPath "+strings.Join(parts, ",")+" -Recurse -Force -ErrorAction SilentlyContinue"))
		return
	}
	qs := make([]string, 0, len(paths))
	for _, p := range paths {
		qs = append(qs, sshQuote(p))
	}
	_, _ = m.exec(ctx, hostID, "rm -f -- "+strings.Join(qs, " "))
}

// hexToStr 解码 ListDir 中 hex 编码的路径/目录名。
func hexToStr(h string) (string, error) {
	b, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// sanitizeFileName 清洗自定义文件名：去掉路径分隔符，只保留 basename。
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = path.Base(name)
	if name == "." || name == "" || name == "/" {
		return ""
	}
	return name
}

// ---- aria2 RPC ----

// rpc 发送一条 aria2 JSON-RPC 调用。连接失败时重建 daemon 并重试一次。
func (m *DownloadManager) rpc(ctx context.Context, hostID, method string, params []any) (json.RawMessage, error) {
	r, err := m.ensureAria2(ctx, hostID)
	if err != nil {
		return nil, err
	}
	res, err := m.callRPC(ctx, hostID, r, method, params)
	if err != nil {
		// 传输层失败（如 daemon 重启、隧道被服务器拒绝）：重建 daemon 后重试一次
		m.dropRPC(hostID)
		r2, err2 := m.ensureAria2(ctx, hostID)
		if err2 != nil {
			return nil, err2
		}
		res, err = m.callRPC(ctx, hostID, r2, method, params)
	}
	return res, err
}

// callRPC 实际执行一次 RPC。优先走 SSH direct-tcpip 隧道；
// 若目标机 sshd 禁止 TCP 转发（AllowTcpForwarding no，报 administratively prohibited），
// 自动降级为 exec 通道：在 SSH 会话内用远端 curl/wget/python3 把请求 POST 给本地 aria2。
func (m *DownloadManager) callRPC(ctx context.Context, hostID string, r *aria2RPC, method string, params []any) (json.RawMessage, error) {
	if m.isExecRPC(hostID) {
		return m.rpcViaExec(ctx, hostID, r.token, method, params)
	}
	res, err := r.call(ctx, method, params)
	if err != nil && isTunnelUnusable(err) {
		m.setExecRPC(hostID)
		return m.rpcViaExec(ctx, hostID, r.token, method, params)
	}
	return res, err
}

// rpcExecHelper 在目标机本地把 HTTP JSON-RPC 请求转发给 127.0.0.1:16800。
// 请求体经 stdin 喂给脚本（cat > 临时文件），用 curl / wget / python3 三种工具按可用性选择。
func (m *DownloadManager) rpcViaExec(ctx context.Context, hostID, token, method string, params []any) (json.RawMessage, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      "ezssh",
		"method":  "aria2." + method,
		"params":  append([]any{"token:" + token}, params...),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	helper := `exec 2>/dev/null
D=/tmp/ezssh_aria2
mkdir -p "$D"
REQ="$D/rpc_req_$$"
cat > "$REQ"
PORT=16800
RESP=""
RC=1
if command -v curl >/dev/null 2>&1; then
  RESP=$(curl -sS --max-time 300 -H 'Content-Type: application/json' --data-binary "@$REQ" "http://127.0.0.1:$PORT/jsonrpc"); RC=$?
elif command -v wget >/dev/null 2>&1; then
  RESP=$(wget -qO- --timeout=300 --post-file="$REQ" --header='Content-Type: application/json' "http://127.0.0.1:$PORT/jsonrpc"); RC=$?
elif command -v python3 >/dev/null 2>&1; then
  RESP=$(python3 -c "
import sys
try:
    import urllib.request
except Exception:
    sys.stdout.write('__RPC_ERR__ no urllib')
    sys.exit(1)
try:
    data = open('$REQ','rb').read()
    req = urllib.request.Request('http://127.0.0.1:16800/jsonrpc', data=data, headers={'Content-Type': 'application/json'})
    sys.stdout.buffer.write(urllib.request.urlopen(req, timeout=300).read())
except Exception as e:
    sys.stdout.write('__RPC_ERR__ %s' % e)
    sys.exit(1)
"); RC=$?
else
  echo "__NO_HTTP_CLIENT__"
  rm -f "$REQ"
  exit 1
fi
rm -f "$REQ"
if [ "$RC" -ne 0 ] || [ -z "$RESP" ]; then
  echo "__RPC_ERR__ 远端 HTTP 客户端 rc=$RC 响应为空"
  exit 1
fi
printf '%s' "$RESP"`

	out, err := m.execStdin(ctx, hostID, helper, body)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	switch {
	case strings.Contains(out, "__NO_HTTP_CLIENT__"):
		return nil, fmt.Errorf("目标机无 curl/wget/python3，无法访问 aria2 RPC")
	case strings.Contains(out, "__RPC_ERR__"):
		detail := strings.TrimSpace(strings.Replace(out, "__RPC_ERR__", "", 1))
		return nil, fmt.Errorf("aria2 rpc(exec): %s", detail)
	}
	var rr struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &rr); err != nil {
		snippet := out
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("aria2 rpc 响应解析失败: %s", snippet)
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("aria2 %s: %s", method, rr.Error.Message)
	}
	return rr.Result, nil
}

// isTunnelUnusable 判断错误是否为"SSH 服务器拒绝 TCP 转发"这一类结构性失败。
// 这类失败重试隧道没有意义，应降级为 exec 通道。
func isTunnelUnusable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "administratively prohibited") ||
		strings.Contains(s, "open failed") ||
		strings.Contains(s, "no forward entries")
}

func (m *DownloadManager) isExecRPC(hostID string) bool {
	m.execRPCMu.Lock()
	defer m.execRPCMu.Unlock()
	return m.execRPCHosts[hostID]
}

func (m *DownloadManager) setExecRPC(hostID string) {
	m.execRPCMu.Lock()
	defer m.execRPCMu.Unlock()
	m.execRPCHosts[hostID] = true
}

// execStdin 在目标主机上执行命令并把 stdin 内容喂给远端进程。
// 与 exec 同样受 ctx 约束，超时/取消时主动关闭 SSH 会话。
func (m *DownloadManager) execStdin(ctx context.Context, hostID, cmd string, stdin []byte) (string, error) {
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	var closeOnce sync.Once
	var sess *ssh.Session
	closeSess := func() { closeOnce.Do(func() { if sess != nil { sess.Close() } }) }
	defer closeSess()

	go func() {
		client, err := m.hub.GetClient(hostID)
		if err != nil {
			done <- result{"", err}
			return
		}
		s, err := client.NewSession()
		if err != nil {
			done <- result{"", err}
			return
		}
		sess = s
		defer s.Close()
		s.Stdin = bytes.NewReader(stdin)
		out, err := s.CombinedOutput(cmd)
		if err != nil && len(out) == 0 {
			done <- result{"", fmt.Errorf("remote exec: %w", err)}
			return
		}
		done <- result{string(out), nil}
	}()

	select {
	case <-ctx.Done():
		closeSess()
		return "", fmt.Errorf("remote exec 超时: %w", ctx.Err())
	case r := <-done:
		return r.out, r.err
	}
}

type aria2RPC struct {
	token  string
	client *http.Client
}

func (r *aria2RPC) call(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      "ezssh",
		"method":  "aria2." + method,
		"params":  append([]any{"token:" + r.token}, params...),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+aria2RPCAddr+"/jsonrpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aria2 rpc http %d", resp.StatusCode)
	}
	var rr struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &rr); err != nil {
		return nil, fmt.Errorf("aria2 rpc 响应解析失败")
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("aria2 %s: %s", method, rr.Error.Message)
	}
	return rr.Result, nil
}

// ensureAria2 保证目标机 aria2 RPC daemon 已就绪，返回对应 RPC 客户端。
func (m *DownloadManager) ensureAria2(ctx context.Context, hostID string) (*aria2RPC, error) {
	if r, ok := m.rpcGet(hostID); ok {
		return r, nil
	}

	m.ensureMu.Lock()
	defer m.ensureMu.Unlock()
	if r, ok := m.rpcGet(hostID); ok {
		return r, nil
	}

	script := ""
	if m.isWindows(hostID) {
		script = winPS(winAria2EnsureScript)
	} else {
		script = `set -e
D=/tmp/ezssh_aria2
# 用拼接避免 sh -c 执行时命令行内整段脚本文本被 pkill 模式自匹配误杀
BIN="aria""2c"
PORT=16800
# 配置版本号：启动参数有变化时 bump，使旧 daemon 自动重启以应用新参数
V=2
mkdir -p "$D"
# 快速路径：已有存活 daemon、密钥有效且配置版本一致，直接复用，避免重复拉起
if [ -f "$D/version" ] && [ "$(cat "$D/version")" = "$V" ] && [ -f "$D/pid" ] && kill -0 "$(cat "$D/pid")" 2>/dev/null && [ -s "$D/secret" ]; then
  cat "$D/secret"
  exit 0
fi
# 清理遗留的孤儿 daemon（若仍占 16800 端口会让新实例绑定失败）。
# 先按 pid 文件强杀，再按命令行特征兜底；模式不含脚本自身文本，不会误杀当前 sh -c 进程。
if [ -f "$D/pid" ]; then
  kill -9 "$(cat "$D/pid")" 2>/dev/null || true
  rm -f "$D/pid"
fi
if command -v pkill >/dev/null 2>&1; then
  pkill -9 -f "$BIN.*rpc-listen-port=$PORT" 2>/dev/null || true
fi
sleep 1
if ! command -v "$BIN" >/dev/null 2>&1; then
  echo "__NO_ARIA2__"
  exit 1
fi
SECRET="ezssh_$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
printf '%s' "$SECRET" > "$D/secret"
chmod 600 "$D/secret"
: > "$D/start.log"
set +e
# 不用 --daemon / --pid-file（部分 aria2c 构建不支持 --pid-file 会直接报 unrecognized option）。
# nohup 后台运行并自己记 pid；stdin/stdout/stderr 全部脱离 SSH 通道，nohup 屏蔽 SIGHUP，会话结束后 daemon 存活。
nohup "$BIN" --enable-rpc --rpc-listen-all=false --rpc-listen-port="$PORT" \
  --rpc-secret="$SECRET" --dir="$D" --continue=true \
  --max-concurrent-downloads=8 --split=8 --max-connection-per-server=8 \
  --file-allocation=none --seed-time=0 \
  --auto-file-renaming=true --allow-overwrite=false --log-level=warn \
  --enable-dht=true --dht-listen-port=6881 --bt-max-peers=200 \
  --bt-save-metadata=true --bt-load-saved-metadata=true \
  --bt-tracker="udp://tracker.opentrackr.org:1337/announce,udp://open.demonii.com:1337/announce,udp://open.stealth.si:80/announce,udp://tracker.torrent.eu.org:451/announce,udp://explodie.org:6969/announce,udp://tracker.cyberia.is:6969/announce,udp://exodus.desync.com:6969/announce,udp://tracker.moeking.me:6969/announce,http://tracker.opentrackr.org:1337/announce,http://tracker.openbittorrent.com:80/announce,https://tracker.gbitt.info/announce" \
  < /dev/null > /dev/null 2>>"$D/start.log" &
RC=$?
set -e
if [ "$RC" -ne 0 ]; then
  echo "__START_FAILED__ rc=$RC"
  if [ -s "$D/start.log" ]; then tail -n 8 "$D/start.log"; fi
  exit 1
fi
# nohup 会 exec 成 aria2c，$! 就是它的真实 pid
echo "$!" > "$D/pid"
echo "$V" > "$D/version"
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 1
  if [ -f "$D/pid" ] && kill -0 "$(cat "$D/pid")" 2>/dev/null; then
    cat "$D/secret"
    exit 0
  fi
done
echo "__START_FAILED__"
if [ -s "$D/start.log" ]; then tail -n 8 "$D/start.log"; fi
exit 1`
	}
	out, err := m.exec(ctx, hostID, script)
	if err != nil {
		return nil, fmt.Errorf("启动 aria2 失败: %w", err)
	}
	out = strings.TrimSpace(out)
	switch {
	case strings.Contains(out, "__NO_ARIA2__"):
		return nil, fmt.Errorf("目标机未安装 aria2，请先安装")
	case strings.Contains(out, "__START_FAILED__"):
		// 附带 aria2c 的实际退出码与 stderr 日志尾部，便于定位真实原因
		detail := strings.TrimSpace(strings.Replace(out, "__START_FAILED__", "", 1))
		if detail != "" {
			return nil, fmt.Errorf("aria2 启动失败: %s", detail)
		}
		return nil, fmt.Errorf("aria2 启动失败，请检查目标机系统日志")
	}
	token := strings.TrimSpace(out)
	if token == "" {
		return nil, fmt.Errorf("aria2 启动异常（未获取到 RPC 密钥）")
	}
	r := &aria2RPC{token: token, client: m.newRPCHTTP(hostID)}
	m.rpcSet(hostID, r)
	return r, nil
}

// newRPCHTTP 构建一个经由 SSH direct-tcpip 隧道访问远端 aria2 RPC 的 HTTP 客户端。
// 每次拨号都会走当前 SSH 连接开隧道，SSH 断线重连后自动跟随。
// 拨号受 ctx 约束：SSH 连接假死时不会无限阻塞，而是在 ctx 到期时返回错误。
func (m *DownloadManager) newRPCHTTP(hostID string) *http.Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			type dialResult struct {
				conn net.Conn
				err  error
			}
			ch := make(chan dialResult, 1)
			go func() {
				sshc, err := m.hub.GetClient(hostID)
				if err != nil {
					ch <- dialResult{nil, err}
					return
				}
				c, e := sshc.Dial("tcp", addr)
				ch <- dialResult{c, e}
			}()
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("aria2 隧道拨号超时: %w", ctx.Err())
			case r := <-ch:
				return r.conn, r.err
			}
		},
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: 60 * time.Second}
}

func (m *DownloadManager) rpcGet(hostID string) (*aria2RPC, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.clients[hostID]
	return r, ok
}

func (m *DownloadManager) rpcSet(hostID string, r *aria2RPC) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[hostID] = r
}

func (m *DownloadManager) dropRPC(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, hostID)
}

// ---- SSH 执行辅助（与 DockerManager 同款） ----

// exec 在目标主机上执行命令。受 ctx 约束：超时或取消时会主动关闭 SSH 会话，
// 避免远端命令挂死导致 WebSocket 处理器（以及前端 30s 请求超时）被拖死。
func (m *DownloadManager) exec(ctx context.Context, hostID, cmd string) (string, error) {
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	var closeOnce sync.Once
	var sess *ssh.Session
	closeSess := func() { closeOnce.Do(func() { if sess != nil { sess.Close() } }) }
	defer closeSess()

	go func() {
		client, err := m.hub.GetClient(hostID)
		if err != nil {
			done <- result{"", err}
			return
		}
		s, err := client.NewSession()
		if err != nil {
			done <- result{"", err}
			return
		}
		sess = s
		defer s.Close()
		out, err := s.CombinedOutput(cmd)
		if err != nil && len(out) == 0 {
			done <- result{"", fmt.Errorf("remote exec: %w", err)}
			return
		}
		done <- result{string(out), nil}
	}()

	select {
	case <-ctx.Done():
		closeSess()
		return "", fmt.Errorf("remote exec 超时: %w", ctx.Err())
	case r := <-done:
		return r.out, r.err
	}
}

func (m *DownloadManager) runScript(hostID, script string, onLine func(string)) error {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	sess.Stdin = strings.NewReader(script)
	// Windows 无 sh，脚本经 stdin 喂给 PowerShell（-Command - 从 stdin 读）。
	cmd := "sh -s"
	if m.isWindows(hostID) {
		cmd = "powershell -NoProfile -NonInteractive -Command -"
	}
	if err := sess.Start(cmd); err != nil {
		return err
	}

	var wg sync.WaitGroup
	stream := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if i := bytes.IndexAny(data, "\n\r"); i >= 0 {
				return i + 1, data[:i], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for sc.Scan() {
			if onLine != nil {
				onLine(sc.Text())
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)
	wg.Wait()

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("aria2 安装失败: %w", err)
	}
	return nil
}

// ---- 工具 ----

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// displayName 从链接推导显示名：直链取文件名。
func displayName(link string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(link), "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 && i+1 < len(trimmed) {
		return trimmed[i+1:]
	}
	return trimmed
}
