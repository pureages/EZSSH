package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"ezssh/internal/apps"
	"ezssh/internal/winpath"
)

const maxReadSize = 1 << 20 // 在线编辑限制 1MB

type sftpEntry struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModeNum    uint32 `json:"mode_num"`
	IsDir      bool   `json:"is_dir"`
	IsLink     bool   `json:"is_link"`
	LinkTarget string `json:"link_target,omitempty"`
	Uid        uint32 `json:"uid"`
	Gid        uint32 `json:"gid"`
	Mtime      int64  `json:"mtime"`
}

func (s *Server) sftpClient(hostID string, w http.ResponseWriter) *sftp.Client {
	c, err := s.sftp.Client(hostID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "sftp connect failed: "+err.Error())
		return nil
	}
	return c
}

// sftpReqPath 将前端传来的展示路径按目标机平台转换为 SFTP 路径。
// Windows 目标机走 winpath.ToSFTP（"C:/Users" → "/C:/Users"）；Linux 逐字节透传。
// 平台探测失败时按 Linux 处理（路径不变），与各 app 模块的 isWindows 默认一致。
func (s *Server) sftpReqPath(hostID, display string) string {
	p, err := s.hub.Platform(hostID)
	if err != nil {
		return display
	}
	if p == "windows" {
		return winpath.ToSFTP(display)
	}
	return display
}

// sftpDisplayLinkTarget 把符号链接目标按目标机平台转回展示路径（仅 Windows 需要）。
func (s *Server) sftpDisplayLinkTarget(hostID, target string) string {
	p, err := s.hub.Platform(hostID)
	if err != nil {
		return target
	}
	if p == "windows" {
		return winpath.ToDisplay(target)
	}
	return target
}

// GET /api/hosts/{id}/sftp/list?path=/xx
func (s *Server) handleSftpList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "."
	}
	dir = s.sftpReqPath(id, dir)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	entries, err := c.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "list failed: "+err.Error())
		return
	}
	// 目录优先、按名称排序已在 ReadDir 中完成；转换为 DTO
	out := make([]sftpEntry, 0, len(entries))
	for _, e := range entries {
		ent := sftpEntry{
			Name:    e.Name(),
			Size:    e.Size(),
			Mode:    e.Mode().String(),
			ModeNum: uint32(e.Mode().Perm()),
			IsDir:   e.IsDir(),
			IsLink:  e.Mode()&os.ModeSymlink != 0,
			Mtime:   e.ModTime().Unix(),
		}
		if fi, ok := e.(interface{ Uid() uint32; Gid() uint32 }); ok {
			ent.Uid = fi.Uid()
			ent.Gid = fi.Gid()
		}
		if ent.IsLink {
			if t, err := c.ReadLink(path.Join(dir, e.Name())); err == nil {
				ent.LinkTarget = s.sftpDisplayLinkTarget(id, t)
			}
		}
		out = append(out, ent)
	}
	writeJSON(w, http.StatusOK, out)
}

type pathReq struct {
	Path string `json:"path"`
}

// POST /api/hosts/{id}/sftp/mkdir
func (s *Server) handleSftpMkdir(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req pathReq
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	req.Path = s.sftpReqPath(id, req.Path)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	if err := c.Mkdir(req.Path); err != nil {
		writeErr(w, http.StatusBadRequest, "mkdir failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

type renameReq struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// POST /api/hosts/{id}/sftp/rename
func (s *Server) handleSftpRename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req renameReq
	if err := readJSON(r, &req); err != nil || req.OldPath == "" || req.NewPath == "" {
		writeErr(w, http.StatusBadRequest, "old_path/new_path required")
		return
	}
	req.OldPath = s.sftpReqPath(id, req.OldPath)
	req.NewPath = s.sftpReqPath(id, req.NewPath)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	if err := c.Rename(req.OldPath, req.NewPath); err != nil {
		writeErr(w, http.StatusBadRequest, "rename failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// POST /api/hosts/{id}/sftp/remove 支持文件与目录（目录递归删除）。
func (s *Server) handleSftpRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req pathReq
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	req.Path = s.sftpReqPath(id, req.Path)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	if err := removePath(c, req.Path); err != nil {
		writeErr(w, http.StatusBadRequest, "remove failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func removePath(c *sftp.Client, p string) error {
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
			if err := removePath(c, path.Join(p, e.Name())); err != nil {
				return err
			}
		}
		return c.RemoveDirectory(p)
	}
	return c.Remove(p)
}

type chmodReq struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

// POST /api/hosts/{id}/sftp/chmod
func (s *Server) handleSftpChmod(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req chmodReq
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path/mode required")
		return
	}
	req.Path = s.sftpReqPath(id, req.Path)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	if err := c.Chmod(req.Path, os.FileMode(req.Mode)); err != nil {
		writeErr(w, http.StatusBadRequest, "chmod failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// GET /api/hosts/{id}/sftp/read?path=xx 读取文本文件（≤1MB）。
func (s *Server) handleSftpRead(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	p = s.sftpReqPath(id, p)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	fi, err := c.Lstat(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "stat failed: "+err.Error())
		return
	}
	if fi.Size() > maxReadSize {
		writeErr(w, http.StatusBadRequest, "file too large for online editing (limit 1MB)")
		return
	}
	f, err := c.Open(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "open failed: "+err.Error())
		return
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxReadSize+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    path.Base(p),
		"content": decodeText(buf),
		"mode":    uint32(fi.Mode().Perm()),
		"size":    fi.Size(),
	})
}

// decodeText 将读取到的字节转码为 UTF-8，修复 GBK/GB18030/UTF-16 文本乱码：
// UTF-8 BOM → UTF-16 LE/BE BOM → 合法 UTF-8 → GB18030（GBK/GB2312/GB18030 通用）。
// GB18030 对 0x00–0x7F 与 ASCII 一致，控制字符/NUL 原样保留，前端 isBinaryContent
// 对二进制文件的判断不受影响；非法字节映射为 U+FFFD（与转码前 JSON 的表现一致，不会更差）。
func decodeText(buf []byte) string {
	if bytes.HasPrefix(buf, []byte{0xEF, 0xBB, 0xBF}) {
		return string(buf[3:])
	}
	if len(buf) >= 2 {
		switch {
		case buf[0] == 0xFF && buf[1] == 0xFE:
			return decodeBytes(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder(), buf[2:])
		case buf[0] == 0xFE && buf[1] == 0xFF:
			return decodeBytes(unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder(), buf[2:])
		}
	}
	if utf8.Valid(buf) {
		return string(buf)
	}
	return decodeBytes(simplifiedchinese.GB18030.NewDecoder(), buf)
}

func decodeBytes(dec transform.Transformer, buf []byte) string {
	if out, _, err := transform.Bytes(dec, buf); err == nil {
		return string(out)
	}
	return string(buf)
}

type extractReq struct {
	Path string `json:"path"`
}

// POST /api/hosts/{id}/sftp/extract 就地解压归档（解压到归档所在目录）。
// Linux：unzip / tar；Windows：Expand-Archive / tar.exe。格式不支持或执行失败返回错误。
func (s *Server) handleSftpExtract(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req extractReq
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	audit := req.Path
	req.Path = s.sftpReqPath(id, req.Path)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	fi, err := c.Lstat(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "stat failed: "+err.Error())
		return
	}
	if fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "不能解压目录")
		return
	}
	p, _ := s.hub.Platform(id)
	cmd, ok := apps.BuildExtractCommand(p, req.Path, path.Dir(req.Path))
	if !ok {
		writeErr(w, http.StatusBadRequest, "不支持的归档格式（支持 zip / tar / tar.gz / tar.bz2 / tar.xz）")
		return
	}

	client, err := s.hub.GetClient(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "connect failed: "+err.Error())
		return
	}
	sess, err := client.NewSession()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "open session failed: "+err.Error())
		return
	}
	defer sess.Close()
	out, runErr := sess.CombinedOutput(cmd)
	if runErr != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			writeErr(w, http.StatusBadRequest, "解压失败: "+msg)
		} else {
			writeErr(w, http.StatusBadRequest, "解压失败: "+runErr.Error())
		}
		return
	}
	_ = s.st.AddAudit("sftp.extract", id, audit)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

type writeReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// POST /api/hosts/{id}/sftp/write 保存文本文件。
func (s *Server) handleSftpWrite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req writeReq
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path/content required")
		return
	}
	req.Path = s.sftpReqPath(id, req.Path)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	f, err := c.Create(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "create failed: "+err.Error())
		return
	}
	defer f.Close()
	if _, err := f.Write([]byte(req.Content)); err != nil {
		writeErr(w, http.StatusBadRequest, "write failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// GET /api/hosts/{id}/sftp/download?path=xx 大文件下载直传浏览器。
func (s *Server) handleSftpDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	p = s.sftpReqPath(id, p)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}
	fi, err := c.Lstat(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "stat failed: "+err.Error())
		return
	}

	// 目录（或指向目录的符号链接）→ 递归打包为 tar.gz 流式下载
	if fi.IsDir() {
		s.streamDirAsTarGz(w, c, p)
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if si, err := c.Stat(p); err == nil && si.IsDir() {
			s.streamDirAsTarGz(w, c, p)
			return
		}
	}

	f, err := c.Open(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "open failed: "+err.Error())
		return
	}
	defer f.Close()

	name := path.Base(p)

	// inline=1：内联预览（图片/视频），走 http.ServeContent 支持 Range 流式播放
	if r.URL.Query().Get("inline") == "1" {
		w.Header().Set("Content-Disposition", "inline")
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		http.ServeContent(w, r, name, fi.ModTime(), f)
		return
	}

	disposition := fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`,
		escapeHeader(name),
		url.PathEscape(name),
	)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, f)
}

// streamDirAsTarGz 将远程目录递归打包为 tar.gz 流式下载（浏览器端作为单个压缩包保存）。
func (s *Server) streamDirAsTarGz(w http.ResponseWriter, c *sftp.Client, p string) {
	// 先验证可读，避免首包才失败导致用户拿到半截文件
	if _, err := c.ReadDir(p); err != nil {
		writeErr(w, http.StatusBadRequest, "read dir failed: "+err.Error())
		return
	}
	name := path.Base(p)
	if name == "/" || name == "." || name == "" {
		name = "root"
	}
	outName := name + ".tar.gz"
	disposition := fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`,
		escapeHeader(outName),
		url.PathEscape(outName),
	)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := s.walkSFTP(c, p, name, tw); err != nil {
		// 客户端可能已断开，尽力收尾
		_ = tw.Close()
		_ = gz.Close()
		return
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return
	}
	_ = gz.Close()
}

// walkSFTP 递归遍历远程目录并写入 tar 流。
// 目录条目名称以 / 结尾（含根目录本身）；不可读的子目录 / 无法打开的文件直接跳过，
// 保证整包下载不因个别条目失败而中断。
func (s *Server) walkSFTP(c *sftp.Client, dir, prefix string, tw *tar.Writer) error {
	entries, err := c.ReadDir(dir)
	if err != nil {
		return err
	}
	// 根目录/子目录头：TypeDir 且名称带尾部斜杠，方便解压工具识别目录
	if fi, err := c.Lstat(dir); err == nil {
		dirName := prefix
		if !strings.HasSuffix(dirName, "/") {
			dirName += "/"
		}
		if err := tw.WriteHeader(&tar.Header{Name: dirName, Mode: int64(fi.Mode().Perm()), Typeflag: tar.TypeDir, ModTime: fi.ModTime()}); err != nil {
			return err
		}
	}
	for _, ent := range entries {
		full := path.Join(dir, ent.Name())
		name := ent.Name()
		if prefix != "" {
			name = path.Join(prefix, name)
		}
		mode := int64(ent.Mode().Perm())
		modTime := ent.ModTime()
		switch {
		case ent.IsDir():
			dirName := name
			if !strings.HasSuffix(dirName, "/") {
				dirName += "/"
			}
			if err := tw.WriteHeader(&tar.Header{Name: dirName, Mode: mode, Typeflag: tar.TypeDir, ModTime: modTime}); err != nil {
				return err
			}
			if err := s.walkSFTP(c, full, name, tw); err != nil {
				// 子目录不可读：跳过
			}
		case ent.Mode()&os.ModeSymlink != 0:
			target, err := c.ReadLink(full)
			if err != nil {
				continue
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Typeflag: tar.TypeSymlink, Linkname: target, ModTime: modTime}); err != nil {
				return err
			}
		default:
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: ent.Size(), ModTime: modTime}); err != nil {
				return err
			}
			f, err := c.Open(full)
			if err != nil {
				continue
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// POST /api/hosts/{id}/sftp/upload?path=/xx 上传文件（multipart 或裸流）。
// 使用流式 multipart 解析：边接收边写 SFTP，让浏览器上传进度真实反映传输过程。
func (s *Server) handleSftpUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := r.URL.Query().Get("path")
	if p == "" {
		writeErr(w, http.StatusBadRequest, "path required")
		return
	}
	p = s.sftpReqPath(id, p)
	c := s.sftpClient(id, w)
	if c == nil {
		return
	}

	// 支持 multipart（字段 file）与裸 body 两种方式
	var src io.Reader
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		_, params, err := mime.ParseMediaType(ct)
		if err != nil || params["boundary"] == "" {
			writeErr(w, http.StatusBadRequest, "bad multipart boundary")
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		found := false
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				writeErr(w, http.StatusBadRequest, "parse multipart failed: "+err.Error())
				return
			}
			if part.FormName() == "file" {
				src = part
				found = true
				break
			}
			// 跳过其它字段
			_, _ = io.Copy(io.Discard, part)
		}
		if !found {
			writeErr(w, http.StatusBadRequest, "file field required")
			return
		}
	} else {
		src = r.Body
	}

	f, err := c.Create(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "create failed: "+err.Error())
		return
	}
	defer f.Close()
	if _, err := io.Copy(f, src); err != nil {
		writeErr(w, http.StatusBadRequest, "write failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// pasteReq 粘贴请求：把源主机的文件/目录复制或移动到当前主机的 dst_dir。
// transport: local（同服务器）/ relay（web 中转）/ direct（两服务器直连）。
type pasteReq struct {
	SrcHostID string `json:"src_host_id"`
	SrcPath   string `json:"src_path"`
	DstDir    string `json:"dst_dir"`
	Mode      string `json:"mode"`      // copy | move
	Transport string `json:"transport"` // local | relay | direct
}

// POST /api/hosts/{id}/sftp/paste 文件/目录复制粘贴（支持跨服务器直连/中转传输）。
// 响应为 NDJSON 流：进度事件 {"loaded":N,"total":M}，结束事件 {"ok":"true"}，错误事件 {"error":"..."}。
// 直连传输通过轮询目标端已写入大小上报进度。
func (s *Server) handleSftpPaste(w http.ResponseWriter, r *http.Request) {
	dstID := r.PathValue("id")
	var req pasteReq
	if err := readJSON(r, &req); err != nil || req.SrcHostID == "" || req.SrcPath == "" || req.DstDir == "" {
		writeErr(w, http.StatusBadRequest, "src_host_id/src_path/dst_dir required")
		return
	}
	isMove := req.Mode == "move"

	// 路径按各自主机平台转换：源路径按源机、目标目录按目标机（Windows 展示路径 → SFTP 路径）。
	// 审计日志保留原始展示路径便于阅读。
	auditSrc, auditDst := req.SrcPath, req.DstDir
	req.SrcPath = s.sftpReqPath(req.SrcHostID, req.SrcPath)
	req.DstDir = s.sftpReqPath(dstID, req.DstDir)

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	// 直连传输的进度轮询与主流程可能并发发送，需加锁；
	// 且客户端断开后写响应可能 panic，绝不能让 panic 崩溃整个服务。
	var sendMu sync.Mutex
	send := func(v any) {
		sendMu.Lock()
		defer sendMu.Unlock()
		defer func() { _ = recover() }()
		_ = json.NewEncoder(w).Encode(v)
		flusher.Flush()
	}

	ctx := r.Context()
	var run func() error
	switch req.Transport {
	case "direct":
		// 直连传输：源主机主动 scp 推送到目标主机（数据不经过 web 服务器）。
		// DirectPaste 在后台执行，主 goroutine 每 500ms 轮询目标端已写入大小并发送进度，
		// 避免 handler 返回后仍有 goroutine 写已关闭的响应。
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		total, err := s.copyMgr.SourceSize(ctx, req.SrcHostID, req.SrcPath)
		if err != nil {
			send(map[string]string{"error": "stat source failed: " + err.Error()})
			return
		}
		dstPath := path.Join(req.DstDir, path.Base(req.SrcPath))
		done := make(chan error, 1)
		go func() {
			done <- s.copyMgr.DirectPaste(ctx, req.SrcHostID, dstID, req.SrcPath, req.DstDir, isMove)
		}()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// 客户端已断开（取消/页面关闭）：直接返回，不再写响应
				return
			case err := <-done:
				if err != nil {
					send(map[string]string{"error": err.Error()})
					return
				}
				_ = s.st.AddAudit("sftp.paste", dstID, auditSrc+" -> "+auditDst)
				send(map[string]string{"ok": "true"})
				return
			case <-ticker.C:
				if n, err := s.copyMgr.SourceSize(ctx, dstID, dstPath); err == nil {
					send(map[string]int64{"loaded": n, "total": total})
				}
			}
		}
	case "relay":
		// 中转传输：以 web 服务器为中枢，双 SFTP 通道流式转发
		total, err := s.copyMgr.SourceSize(ctx, req.SrcHostID, req.SrcPath)
		if err != nil {
			send(map[string]string{"error": "stat source failed: " + err.Error()})
			return
		}
		run = func() error {
			return s.copyMgr.RelayPaste(ctx, req.SrcHostID, dstID, req.SrcPath, req.DstDir, isMove,
				func(loaded int64) { send(map[string]int64{"loaded": loaded, "total": total}) })
		}
	default:
		// 同服务器直接复制/移动
		if req.SrcHostID != dstID {
			send(map[string]string{"error": "local paste requires same source and target host"})
			return
		}
		if isMove {
			run = func() error {
				return s.copyMgr.LocalPaste(ctx, dstID, req.SrcPath, req.DstDir, true, nil)
			}
		} else {
			total, err := s.copyMgr.SourceSize(ctx, dstID, req.SrcPath)
			if err != nil {
				send(map[string]string{"error": "stat source failed: " + err.Error()})
				return
			}
			run = func() error {
				return s.copyMgr.LocalPaste(ctx, dstID, req.SrcPath, req.DstDir, false,
					func(loaded int64) { send(map[string]int64{"loaded": loaded, "total": total}) })
			}
		}
	}
	if err := run(); err != nil {
		// 客户端已断开（用户取消）：无需再发送错误
		if ctx.Err() != nil {
			return
		}
		send(map[string]string{"error": err.Error()})
		return
	}
	_ = s.st.AddAudit("sftp.paste", dstID, auditSrc+" -> "+auditDst)
	send(map[string]string{"ok": "true"})
}

// escapeHeader 去除文件名中的 CR/LF 防止响应头注入。
func escapeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}
