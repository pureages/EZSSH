package apps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"ezssh/internal/sshhub"
	"ezssh/internal/store"
	"ezssh/internal/winpath"
)

// CopyManager 提供服务器内与跨服务器（直连/中转）的文件复制/剪切能力。
type CopyManager struct {
	hub  *sshhub.Hub
	sftp *SFTPManager
}

func NewCopyManager(hub *sshhub.Hub, sftp *SFTPManager) *CopyManager {
	return &CopyManager{hub: hub, sftp: sftp}
}

// isWindows 判断目标机是否为 Windows（探测失败按 Linux 处理）。
func (m *CopyManager) isWindows(hostID string) bool {
	p, err := m.hub.Platform(hostID)
	if err != nil {
		return false
	}
	return p == "windows"
}

// ---- 本地（同服务器）复制/剪切 ----

// LocalPaste 在目标主机内复制（copy）或移动（move）一个文件/目录到 dstDir。
// 移动直接用 Rename（原子操作）；复制在远端执行 cp -a，数据不经过 web 服务器。
// progress 回调接收已累计传输字节数（通过轮询目标端大小估算），可为 nil。
func (m *CopyManager) LocalPaste(ctx context.Context, hostID, srcPath, dstDir string, isMove bool, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Windows 目标机：前端传来的是展示路径（C:/Users），先统一转成 SFTP 路径
	// （/C:/Users），使 path.Join / Rename / 进度轮询都工作在 SFTP 路径上。
	if m.isWindows(hostID) {
		srcPath = winpath.ToSFTP(srcPath)
		dstDir = winpath.ToSFTP(dstDir)
	}
	c, err := m.sftp.Client(hostID)
	if err != nil {
		return err
	}
	dstPath := path.Join(dstDir, path.Base(srcPath))
	if path.Clean(dstPath) == path.Clean(srcPath) {
		// 同路径（剪切到自身所在目录）无需操作
		return nil
	}
	if isMove {
		if err := c.Rename(srcPath, dstPath); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
		return nil
	}
	// 复制：同机直接在远端执行 cp -a，数据不经过 web 服务器
	return m.localCopy(ctx, hostID, srcPath, dstPath, progress)
}

// localCopy 在远端主机上执行 cp -a 完成同机复制（数据不经过 web 服务器）。
// 目录采用「mkdir -p 目标 && cp -a 源/. 目标/」的合并语义，
// 与原先 SFTP 复制（目标已存在时合并、不存在时新建）保持一致。
// 通过轮询目标路径大小估算进度，progress 可为 nil。
func (m *CopyManager) localCopy(ctx context.Context, hostID, srcPath, dstPath string, progress func(int64)) error {
	c, err := m.sftp.Client(hostID)
	if err != nil {
		return err
	}
	fi, err := c.Lstat(srcPath)
	if err != nil {
		return fmt.Errorf("lstat source: %w", err)
	}
	if m.isWindows(hostID) {
		// Windows 主机用 PowerShell 的 Copy-Item 等价实现 cp -a 语义。
		// 目录：先建目标目录，再把源目录内容合并复制进去（≈ cp -a src/. dst/）。
		// 命令经 winPS 的 -EncodedCommand 编码，从默认 cmd.exe shell 执行也免疫引号问题。
		srcD := winpath.ToDisplay(srcPath)
		dstD := winpath.ToDisplay(dstPath)
		if fi.IsDir() {
			cmd := winPS("New-Item -ItemType Directory -Force -Path " + psLiteral(dstD) + " | Out-Null; " +
				"Copy-Item -Path (Join-Path " + psLiteral(srcD) + " '*') -Destination " + psLiteral(dstD) + " -Recurse -Force")
			return m.runRemoteCopy(ctx, hostID, cmd, c, dstPath, progress)
		}
		cmd := winPS("Copy-Item -LiteralPath " + psLiteral(srcD) + " -Destination " + psLiteral(dstD) + " -Force")
		return m.runRemoteCopy(ctx, hostID, cmd, c, dstPath, progress)
	}
	if fi.IsDir() {
		cmd := fmt.Sprintf("mkdir -p %s && cp -a %s/. %s/", sshQuote(dstPath), sshQuote(srcPath), sshQuote(dstPath))
		return m.runRemoteCopy(ctx, hostID, cmd, c, dstPath, progress)
	}
	return m.runRemoteCopy(ctx, hostID, "cp -a "+sshQuote(srcPath)+" "+sshQuote(dstPath), c, dstPath, progress)
}

// runRemoteCopy 通过 SSH 执行远端 cp 命令，并轮询目标路径大小回报进度。
func (m *CopyManager) runRemoteCopy(ctx context.Context, hostID, cmd string, c *sftp.Client, dstPath string, progress func(int64)) error {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	done := make(chan error, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg != "" {
				done <- fmt.Errorf("cp: %w (%s)", err, msg)
			} else {
				done <- fmt.Errorf("cp: %w", err)
			}
			return
		}
		done <- nil
	}()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = sess.Close()
			return ctx.Err()
		case err := <-done:
			return err
		case <-ticker.C:
			if progress != nil {
				if n, err := pathSize(ctx, c, dstPath); err == nil {
					progress(n)
				}
			}
		}
	}
}

// ---- 中转（数据经 web 服务器）复制/剪切 ----

// RelayPaste 以 web 服务器为中枢，将源主机上的文件/目录流式复制到目标主机。
// 源与目标使用两条独立 SFTP 通道，数据不落盘，直接 io.Copy 转发。
// progress 回调接收已累计传输字节数，可为 nil。
func (m *CopyManager) RelayPaste(ctx context.Context, srcID, dstID, srcPath, dstDir string, isMove bool, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 源/目标为 Windows 时，展示路径（C:/Users）先转成 SFTP 路径（/C:/Users），
	// 双通道 pkg/sftp 与递归删除才在正确的路径空间上工作。
	if m.isWindows(srcID) {
		srcPath = winpath.ToSFTP(srcPath)
	}
	if m.isWindows(dstID) {
		dstDir = winpath.ToSFTP(dstDir)
	}
	srcC, err := m.sftp.Client(srcID)
	if err != nil {
		return err
	}
	dstC, err := m.sftp.Client(dstID)
	if err != nil {
		return err
	}
	dstPath := path.Join(dstDir, path.Base(srcPath))
	var copied int64
	if err := relayCopyPath(ctx, srcC, dstC, srcPath, dstPath, func(delta int64) {
		copied += delta
		if progress != nil {
			progress(copied)
		}
	}); err != nil {
		return err
	}
	if isMove {
		return removePathR(ctx, srcC, srcPath)
	}
	return nil
}

// SourceSize 统计源路径（文件或目录）的递归总字节数，供传输进度计算。
func (m *CopyManager) SourceSize(ctx context.Context, hostID, srcPath string) (int64, error) {
	// Windows 目标机：展示路径转 SFTP 路径后再统计
	if m.isWindows(hostID) {
		srcPath = winpath.ToSFTP(srcPath)
	}
	c, err := m.sftp.Client(hostID)
	if err != nil {
		return 0, err
	}
	return pathSize(ctx, c, srcPath)
}

func pathSize(ctx context.Context, c *sftp.Client, p string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fi, err := c.Lstat(p)
	if err != nil {
		return 0, err
	}
	// 符号链接或普通文件：直接返回大小
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fi.Size(), nil
	}
	var total int64
	entries, err := c.ReadDir(p)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		s, err := pathSize(ctx, c, path.Join(p, e.Name()))
		if err != nil {
			return 0, err
		}
		total += s
	}
	return total, nil
}

// ---- 直连（源机 scp 直推目标机）复制/剪切 ----

// DirectPaste 由源主机直接向目标主机发起 scp 推送（数据不经过 web 服务器）。
// 密码认证依赖源机安装 sshpass；私钥认证通过临时密钥文件完成。
// ctx 取消时关闭 SSH session，使远端 scp 进程终止。
func (m *CopyManager) DirectPaste(ctx context.Context, srcID, dstID, srcPath, dstDir string, isMove bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 直连粘贴依赖源主机上的 scp/sshpass 走 POSIX 路径语义，Windows 目标机不支持，
	// 引导用户改用中转模式（经 web 服务器双 SFTP 通道转发）。
	if m.isWindows(srcID) || m.isWindows(dstID) {
		return fmt.Errorf("Windows 主机不支持直连粘贴，请使用中转模式")
	}
	srcSSH, err := m.hub.GetClient(srcID)
	if err != nil {
		return fmt.Errorf("connect source host: %w", err)
	}
	dstHost, dstCred, err := m.hub.DialInfo(dstID)
	if err != nil {
		return fmt.Errorf("resolve target host: %w", err)
	}
	srcC, err := m.sftp.Client(srcID)
	if err != nil {
		return err
	}
	// 判断源路径是否为目录（在源主机远端判断），决定 scp 是否加 -r
	fi, err := srcC.Lstat(srcPath)
	if err != nil {
		return fmt.Errorf("lstat source: %w", err)
	}
	// 确保目标目录存在（scp 推送需要）
	dstC, err := m.sftp.Client(dstID)
	if err != nil {
		return err
	}
	if err := dstC.MkdirAll(dstDir); err != nil {
		return fmt.Errorf("ensure target dir: %w", err)
	}

	// 私钥认证：将目标主机私钥临时写入源主机
	keyPath := ""
	if dstHost.AuthType == "privatekey" {
		keyPath = remoteTempKeyPath(dstHost.ID)
		kf, err := srcC.Create(keyPath)
		if err != nil {
			return fmt.Errorf("stage private key on source host: %w", err)
		}
		if _, err := kf.Write(dstCred); err != nil {
			kf.Close()
			_ = srcC.Remove(keyPath)
			return fmt.Errorf("write private key: %w", err)
		}
		if err := kf.Chmod(0o600); err != nil {
			kf.Close()
			_ = srcC.Remove(keyPath)
			return fmt.Errorf("chmod private key: %w", err)
		}
		kf.Close()
		defer func() { _ = srcC.Remove(keyPath) }()
	}

	cmd := buildDirectScpCommand(srcPath, dstDir, dstHost, dstCred, keyPath, fi.IsDir())
	sess, err := srcSSH.NewSession()
	if err != nil {
		return fmt.Errorf("open session on source host: %w", err)
	}
	defer sess.Close()

	// ctx 取消时关闭 session，远端 scp 进程随即退出
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = sess.Close()
		case <-done:
		}
	}()

	// 捕获远端输出，失败时给出可读的错误原因（如 sshpass 缺失、连接被拒等）
	out, runErr := sess.CombinedOutput(cmd)
	if runErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("direct scp failed: %w (%s)", runErr, msg)
		}
		return fmt.Errorf("direct scp failed: %w", runErr)
	}
	if isMove {
		return removePathR(ctx, srcC, srcPath)
	}
	return nil
}

// buildDirectScpCommand 在源主机上构造 scp 推送命令。
// keyPath 非空时使用私钥认证（scp -i），否则使用 sshpass 密码认证。
// 目标路径固定为 <dstDir>/（带尾部斜杠），scp 会保留源文件/目录名放入该目录。
func buildDirectScpCommand(srcPath, dstDir string, dstHost *store.Host, dstCred []byte, keyPath string, isDir bool) string {
	var sb strings.Builder
	sb.WriteString("sh -c 'set -e; ")
	sb.WriteString(`command -v scp >/dev/null 2>&1 || { echo "scp not found on source host"; exit 127; }; `)
	auth := "scp"
	if keyPath == "" {
		// 密码认证需要 sshpass
		sb.WriteString(`command -v sshpass >/dev/null 2>&1 || { echo "sshpass not found on source host, please install it (apt/yum install sshpass) or use 中转传输"; exit 127; }; `)
		auth = "sshpass -p " + sshQuote(string(dstCred)) + " scp"
	} else {
		auth = "scp -i " + sshQuote(keyPath)
	}
	sb.WriteString(auth)
	if isDir {
		sb.WriteString(" -r")
	}
	sb.WriteString(" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=15")
	sb.WriteString(fmt.Sprintf(" -P %d ", dstHost.Port))
	sb.WriteString(sshQuote(srcPath) + " ")
	sb.WriteString(sshQuote(dstHost.Username) + "@" + sshQuote(dstHost.Host) + ":" + sshQuote(strings.TrimRight(dstDir, "/")+"/"))
	sb.WriteString("'")
	return sb.String()
}

// removePathR 递归删除远端路径（文件/目录）。
func removePathR(ctx context.Context, c *sftp.Client, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := c.Lstat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		entries, err := c.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := removePathR(ctx, c, path.Join(p, e.Name())); err != nil {
				return err
			}
		}
		return c.RemoveDirectory(p)
	}
	return c.Remove(p)
}

// relayCopyPath 将 srcC 的 srcPath 递归复制到 dstC 的 dstPath（保留权限）。
// srcC 与 dstC 可为同一客户端（同机复制）或不同客户端（跨机中转）。
// progress 回调按增量报告已传输字节数，可为 nil。
func relayCopyPath(ctx context.Context, srcC, dstC *sftp.Client, srcPath, dstPath string, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := srcC.Lstat(srcPath)
	if err != nil {
		return fmt.Errorf("lstat %s: %w", srcPath, err)
	}
	if fi.IsDir() {
		if _, err := dstC.Lstat(dstPath); err != nil {
			if err := dstC.Mkdir(dstPath); err != nil {
				return fmt.Errorf("mkdir %s: %w", dstPath, err)
			}
		}
		entries, err := srcC.ReadDir(srcPath)
		if err != nil {
			return fmt.Errorf("readdir %s: %w", srcPath, err)
		}
		for _, e := range entries {
			if err := relayCopyPath(ctx, srcC, dstC, path.Join(srcPath, e.Name()), path.Join(dstPath, e.Name()), progress); err != nil {
				return err
			}
		}
		return nil
	}
	// 符号链接：保持链接指向而非复制目标内容
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := srcC.ReadLink(srcPath)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", srcPath, err)
		}
		if _, err := dstC.Lstat(dstPath); err == nil {
			_ = dstC.Remove(dstPath)
		}
		if err := dstC.Symlink(target, dstPath); err != nil {
			return fmt.Errorf("symlink %s: %w", dstPath, err)
		}
		return nil
	}
	srcF, err := srcC.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer srcF.Close()
	dstF, err := dstC.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", dstPath, err)
	}
	defer dstF.Close()
	var src io.Reader = srcF
	if progress != nil {
		src = &countingReader{r: srcF, ctx: ctx, progress: progress}
	}
	if _, err := io.Copy(dstF, src); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("copy %s -> %s: %w", srcPath, dstPath, err)
	}
	if err := dstC.Chmod(dstPath, fi.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %s: %w", dstPath, err)
	}
	return nil
}

// countingReader 包装 Reader，每读一块就报告一次增量进度，并在 ctx 取消时中止读取。
type countingReader struct {
	r        io.Reader
	ctx      context.Context
	progress func(int64)
}

func (cr *countingReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
	}
	n, err := cr.r.Read(p)
	if n > 0 && cr.progress != nil {
		cr.progress(int64(n))
	}
	return n, err
}

// remoteTempKeyPath 返回源主机上的临时私钥文件路径（含主机 ID 与进程号避免冲突）。
func remoteTempKeyPath(hostID string) string {
	return fmt.Sprintf("/tmp/ezssh_direct_%s_%d.pem", hostID, os.Getpid())
}

// sshQuote 用单引号包裹 shell 参数并转义内部单引号，避免命令注入。
func sshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
