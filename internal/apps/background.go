package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"ezssh/internal/sshhub"
	"ezssh/internal/store"
)

// ProcLister 进程列表查询接口（*ProcessManager 满足；测试可打桩）。
type ProcLister interface {
	List(hostID string) ([]ProcessInfo, error)
}

// BackgroundView 后台任务对外视图（含实时进程统计）。
type BackgroundView struct {
	ID       string  `json:"id"`
	HostID   string  `json:"hostId"`
	HostName string  `json:"hostName"`
	PID      int     `json:"pid"`
	Command  string  `json:"command"`
	Started  int64   `json:"started"`
	Status   string  `json:"status"` // running | exited | unknown（主机离线/未解锁）
	CPU      float64 `json:"cpu"`
	MEM      float64 `json:"mem"`
	RSS      uint64  `json:"rss"`
	Start    string  `json:"start"`
}

// BackgroundManager 管理在远端服务器上长期后台运行的命令（nohup/setsid 分离式启动）。
// 任务元数据持久化到 SQLite，网关重启后按 host+pid 重新挂接继续监控。
type BackgroundManager struct {
	hub      *sshhub.Hub
	sftp     *SFTPManager
	procs    ProcLister
	st       *store.Store
	platform func(hostID string) string // 平台判定；测试可注入，避免真实拨号
}

func NewBackgroundManager(hub *sshhub.Hub, sftp *SFTPManager, procs ProcLister, st *store.Store) *BackgroundManager {
	m := &BackgroundManager{hub: hub, sftp: sftp, procs: procs, st: st}
	m.platform = m.detectPlatform
	return m
}

func (m *BackgroundManager) detectPlatform(hostID string) string {
	if p, err := m.hub.Platform(hostID); err == nil {
		return p
	}
	return "linux"
}

func (m *BackgroundManager) isWindows(hostID string) bool {
	return m.platform(hostID) == "windows"
}

// Start 在目标主机上分离式启动后台命令并落库，返回任务。
func (m *BackgroundManager) Start(ctx context.Context, hostID, hostName, command string) (*store.BackgroundTask, error) {
	id := "bg_" + randomHex(6)
	if m.isWindows(hostID) {
		return m.startWindows(ctx, hostID, hostName, command, id)
	}
	return m.startLinux(ctx, hostID, hostName, command, id)
}

// startLinux 两步远程操作：① SFTP 写脚本（命令原文，免疫引号/$/反引号）；
// ② 单独 exec 用 setsid/nohup 分离启动并回显 $!（保证 PID 是真正运行脚本的进程）。
func (m *BackgroundManager) startLinux(ctx context.Context, hostID, hostName, command, id string) (*store.BackgroundTask, error) {
	script := "/tmp/ezssh_bg_" + id + ".sh"
	logPath := "/tmp/ezssh_bg_" + id + ".log"

	c, err := m.sftp.Client(hostID)
	if err != nil {
		return nil, fmt.Errorf("sftp connect: %w", err)
	}
	if err := c.MkdirAll("/tmp"); err != nil {
		return nil, fmt.Errorf("mkdir /tmp: %w", err)
	}
	f, err := c.Create(script)
	if err != nil {
		return nil, fmt.Errorf("create script: %w", err)
	}
	content := command
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return nil, fmt.Errorf("write script: %w", err)
	}
	f.Close()

	out, err := m.exec(ctx, hostID, linuxDetachCmd(script, logPath))
	if err != nil {
		return nil, fmt.Errorf("detach: %w (%s)", err, strings.TrimSpace(out))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]))
	if err != nil {
		return nil, fmt.Errorf("parse pid from %q: %w", strings.TrimSpace(out), err)
	}

	task := &store.BackgroundTask{
		ID: id, HostID: hostID, HostName: hostName, PID: pid,
		Command: command, LogPath: logPath, ErrPath: logPath, Started: time.Now().Unix(),
	}
	if err := m.st.CreateTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// startWindows 单条 winPS 脚本：用 Start-Process 分离启动子 powershell（命令经
// -EncodedCommand 传入，纯 ASCII base64 免疫引号/换行/中文），stdout/stderr 分写两个日志文件。
func (m *BackgroundManager) startWindows(ctx context.Context, hostID, hostName, command, id string) (*store.BackgroundTask, error) {
	out, err := m.exec(ctx, hostID, winPS(windowsStartScript(id, winPSEncoded(command))))
	if err != nil {
		return nil, fmt.Errorf("start detached: %w (%s)", err, strings.TrimSpace(out))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return nil, fmt.Errorf("parse pid from %q: %w", strings.TrimSpace(out), err)
	}
	logPath := `%USERPROFILE%\ezssh_bg_` + id + `\out.log`
	errPath := `%USERPROFILE%\ezssh_bg_` + id + `\err.log`

	task := &store.BackgroundTask{
		ID: id, HostID: hostID, HostName: hostName, PID: pid,
		Command: command, LogPath: logPath, ErrPath: errPath, Started: time.Now().Unix(),
	}
	if err := m.st.CreateTask(task); err != nil {
		return nil, err
	}
	return task, nil
}

// List 返回全部后台任务视图：按主机分组、每主机只取一次进程表后按 PID 匹配。
// 主机不可达（离线/保险库未解锁）→ unknown；PID 命中 → running；否则 → exited。
// 顺带清理「已退出且启动超过 7 天」的陈旧任务，防表无限增长。
func (m *BackgroundManager) List(ctx context.Context) ([]BackgroundView, error) {
	tasks, err := m.st.ListTasks()
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return []BackgroundView{}, nil
	}

	byHost := make(map[string][]store.BackgroundTask)
	for _, t := range tasks {
		byHost[t.HostID] = append(byHost[t.HostID], t)
	}

	var views []BackgroundView
	for hostID, htasks := range byHost {
		procs, perr := m.procs.List(hostID)
		isWin := m.isWindows(hostID)
		for _, t := range htasks {
			v := BackgroundView{
				ID: t.ID, HostID: t.HostID, HostName: t.HostName, PID: t.PID,
				Command: t.Command, Started: t.Started,
			}
			switch {
			case perr != nil:
				v.Status = "unknown"
			default:
				if p, ok := matchProc(procs, t, isWin); ok {
					v.Status = "running"
					v.CPU, v.MEM, v.RSS, v.Start = p.CPU, p.MEM, p.RSS, p.Start
				} else {
					v.Status = "exited"
				}
			}
			views = append(views, v)
		}
	}

	// 清理陈旧已退出任务
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	kept := views[:0]
	for _, v := range views {
		if v.Status == "exited" && v.Started < cutoff {
			_ = m.st.DeleteTask(v.ID)
			continue
		}
		kept = append(kept, v)
	}
	return kept, nil
}

// matchProc 按 PID 匹配任务进程。Linux 额外校验命令含脚本路径防 PID 复用。
func matchProc(procs []ProcessInfo, t store.BackgroundTask, isWin bool) (ProcessInfo, bool) {
	for _, p := range procs {
		if p.PID != t.PID {
			continue
		}
		if isWin {
			return p, true
		}
		if strings.Contains(p.Command, "/tmp/ezssh_bg_"+t.ID+".sh") {
			return p, true
		}
	}
	return ProcessInfo{}, false
}

// Kill 结束后台任务进程（不删行，下轮 List 显示 exited）。
func (m *BackgroundManager) Kill(id string) error {
	t, err := m.st.GetTask(id)
	if err != nil {
		return err
	}
	if m.isWindows(t.HostID) {
		return m.killWindows(t)
	}
	return m.killLinux(t)
}

// killLinux 进程组杀（setsid 启动时整棵进程树一起死）→ 失败回退单 PID → 再升级 KILL。
// 远端已无该进程时忽略退出码错误。
func (m *BackgroundManager) killLinux(t *store.BackgroundTask) error {
	cmds := []string{
		fmt.Sprintf("kill -TERM -- -%d 2>/dev/null || kill -TERM -- %d", t.PID, t.PID),
		fmt.Sprintf("kill -KILL -- -%d 2>/dev/null || kill -KILL -- %d", t.PID, t.PID),
	}
	return m.killRemote(context.Background(), t.HostID, cmds)
}

// killWindows taskkill 杀进程树；必要时 /T /F 升级。
func (m *BackgroundManager) killWindows(t *store.BackgroundTask) error {
	ctx := context.Background()
	cmds := []string{
		"taskkill /PID " + strconv.Itoa(t.PID) + " /T",
		"taskkill /PID " + strconv.Itoa(t.PID) + " /T /F",
	}
	return m.killRemote(ctx, t.HostID, cmds)
}

func (m *BackgroundManager) killRemote(ctx context.Context, hostID string, cmds []string) error {
	for _, cmd := range cmds {
		_, err := m.exec(ctx, hostID, cmd)
		if err == nil {
			return nil
		}
		// 连接层错误（离线/会话失败）直接上报；远端 exit-status 忽略（进程可能已消失）
		var ee *ssh.ExitError
		if !errors.As(err, &ee) {
			return err
		}
	}
	return nil
}

// Logs 读取远端日志文件尾部。
func (m *BackgroundManager) Logs(ctx context.Context, id string, lines int) (string, error) {
	t, err := m.st.GetTask(id)
	if err != nil {
		return "", err
	}
	if m.isWindows(t.HostID) {
		ps := "$c = Get-Content -Path " + psLiteral(t.LogPath) + " -Tail " + strconv.Itoa(lines) + " -ErrorAction SilentlyContinue; " +
			"$e = Get-Content -Path " + psLiteral(t.ErrPath) + " -Tail " + strconv.Itoa(lines) + " -ErrorAction SilentlyContinue; " +
			"($c + $e) -join [Environment]::NewLine"
		return m.exec(ctx, t.HostID, winPS(ps))
	}
	return m.exec(ctx, t.HostID, "tail -n "+strconv.Itoa(lines)+" "+sshQuote(t.LogPath))
}

// exec 在目标主机执行命令，返回合并输出与错误；ctx 取消时关闭会话中止远端进程。
func (m *BackgroundManager) exec(ctx context.Context, hostID, cmd string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	type res struct {
		out []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := sess.CombinedOutput(cmd)
		ch <- res{b, err}
	}()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return "", ctx.Err()
	case r := <-ch:
		return string(r.out), r.err
	}
}

// randomHex 生成 n 字节的随机十六进制串（用于任务 ID 与远端文件名）。
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// linuxDetachCmd 构造 Linux 分离式启动命令：优先 setsid（整棵进程树一个进程组，
// $! 即真正运行脚本的进程），无 setsid 时回退 nohup。
func linuxDetachCmd(script, logPath string) string {
	return fmt.Sprintf(
		`if command -v setsid >/dev/null 2>&1; then setsid sh '%s' </dev/null >>'%s' 2>&1 & echo $!; else nohup sh '%s' </dev/null >>'%s' 2>&1 & echo $!; fi`,
		script, logPath, script, logPath)
}

// windowsStartScript 构造 Windows 后台启动脚本（命令经 -EncodedCommand 传入，避免写 .ps1）。
func windowsStartScript(id, enc string) string {
	return "$D = Join-Path $env:USERPROFILE 'ezssh_bg_" + id + "'\n" +
		"New-Item -ItemType Directory -Force -Path $D | Out-Null\n" +
		"$out = Join-Path $D 'out.log'\n" +
		"$err = Join-Path $D 'err.log'\n" +
		"$enc = '" + enc + "'\n" +
		"$p = Start-Process -FilePath powershell -ArgumentList @('-NoProfile','-NonInteractive','-EncodedCommand',$enc) -WindowStyle Hidden -RedirectStandardOutput $out -RedirectStandardError $err -PassThru\n" +
		"$p.Id"
}
