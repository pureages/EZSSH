package api

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GitHub 仓库与版本检查配置。
const (
	updateRepoOwner = "pureages"
	updateRepoName  = "EZSSH"
	updateRepoURL   = "https://github.com/" + updateRepoOwner + "/" + updateRepoName
	updateAPIURL    = "https://api.github.com/repos/" + updateRepoOwner + "/" + updateRepoName + "/releases/latest"
	// updateUserAgent 用于 GitHub API / 下载请求的 UA。
	updateUserAgent = "EZSSH-updater/1.0"
)

// releaseInfo 是 GitHub releases/latest 响应中关心的字段。
type releaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// fetchLatestRelease 查询 GitHub 最新 Release（默认分支，无需认证）。
func fetchLatestRelease() (*releaseInfo, error) {
	req, err := http.NewRequest(http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %s", resp.Status)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("empty tag_name from github api")
	}
	return &rel, nil
}

// stripV 去掉版本号前缀的 v / V。
func stripV(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
}

// versionTuple 将 "x.y.z" 解析为数字元组；非数字段视为 0。
func versionTuple(v string) []int {
	parts := strings.Split(stripV(v), ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// compareVersions 比较两个语义化版本号：a<b 返回 -1，a==b 返回 0，a>b 返回 1。
func compareVersions(a, b string) int {
	at, bt := versionTuple(a), versionTuple(b)
	for i := 0; i < max(len(at), len(bt)); i++ {
		var av, bv int
		if i < len(at) {
			av = at[i]
		}
		if i < len(bt) {
			bv = bt[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// GET /api/update-check → { current, latest, update_available, release_url }
// 检查 GitHub 最新 Release 版本号，与本地版本对比。
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	rel, err := fetchLatestRelease()
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("check update failed: %v", err))
		return
	}
	latest := stripV(rel.TagName)
	current := stripV(s.Version)
	available := compareVersions(latest, current) > 0
	writeJSON(w, http.StatusOK, map[string]any{
		"current":          current,
		"latest":           latest,
		"update_available": available,
		"release_url":      rel.HTMLURL,
	})
}

// POST /api/update → { ok, message }
// 一键更新：下载最新 Release 的预编译包，替换服务端二进制与前端，然后自动重启。
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	rel, err := fetchLatestRelease()
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("check update failed: %v", err))
		return
	}
	latest := stripV(rel.TagName)
	if compareVersions(latest, stripV(s.Version)) <= 0 {
		writeErr(w, http.StatusBadRequest, "already up to date")
		return
	}
	if err := applyUpdate(latest); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("update failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      "updated",
		"message": "updated to " + latest + ", restarting",
		"version": latest,
	})
	// 响应写完后自动重启
	go func() {
		time.Sleep(800 * time.Millisecond)
		restartSelf()
	}()
}

// applyUpdate 下载并解压指定版本的预编译包，替换当前二进制与前端。
func applyUpdate(version string) error {
	asset := fmt.Sprintf("ezssh-%s-%s-%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("%s/releases/download/v%s/%s", updateRepoURL, version, asset)

	tmp, err := os.MkdirTemp("", "ezssh-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// 下载
	tarball := filepath.Join(tmp, asset)
	if err := downloadFile(url, tarball); err != nil {
		return fmt.Errorf("download %s: %v", asset, err)
	}

	// 解压
	if err := extractTarGz(tarball, tmp); err != nil {
		return fmt.Errorf("extract: %v", err)
	}

	// 当前二进制
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %v", err)
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return fmt.Errorf("absolute self: %v", err)
	}

	// 替换二进制：先备份，再原子替换。
	binName := "ezsshd"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	newBin := filepath.Join(tmp, binName)
	if _, err := os.Stat(newBin); err != nil {
		return fmt.Errorf("missing %s in package", binName)
	}
	backup := self + ".bak"
	_ = os.Remove(backup)
	if err := copyFile(self, backup); err != nil {
		return fmt.Errorf("backup binary: %v", err)
	}
	if err := os.Rename(newBin, self); err != nil {
		// Windows 下运行中的 exe 无法覆盖，回滚备份
		_ = os.Rename(backup, self)
		return fmt.Errorf("replace binary: %v", err)
	}
	_ = os.Chmod(self, 0o755)

	// 替换前端到所有候选目录中实际存在的目录
	webSrc := filepath.Join(tmp, "web", "dist")
	if fi, err := os.Stat(webSrc); err == nil && fi.IsDir() {
		installed := false
		for _, cand := range webDistCandidates() {
			if cand == "" {
				continue
			}
			if _, err := os.Stat(cand); err != nil {
				continue
			}
			if err := copyDir(webSrc, cand); err != nil {
				return fmt.Errorf("update frontend %s: %v", cand, err)
			}
			installed = true
		}
		if !installed {
			// 无现成前端目录：安装到默认位置 ~/.ezssh/web/dist
			home, err := os.UserHomeDir()
			if err == nil {
				dst := filepath.Join(home, ".ezssh", "web", "dist")
				if err := copyDir(webSrc, dst); err != nil {
					return fmt.Errorf("install frontend: %v", err)
				}
			}
		}
	}

	return nil
}

// downloadFile 下载 URL 到本地文件（跟随重定向）。
func downloadFile(url, dst string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// extractTarGz 解压 tar.gz 到 dst 目录。
func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// 防路径穿越
		name := filepath.Join(dst, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(name, filepath.Clean(dst)+string(os.PathSeparator)) && name != filepath.Clean(dst) {
			return fmt.Errorf("illegal path in archive: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			out, err := os.Create(name)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			_ = os.Chmod(name, os.FileMode(hdr.Mode))
		}
	}
	return nil
}

// copyFile 复制单个文件。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}

// copyDir 递归复制目录内容（含子目录）到 dst。
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// restartSelf 重启当前进程：Windows 用特殊处理，Unix 直接替换镜像。
func restartSelf() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Windows 无法 exec 替换自身镜像，通过 cmd 启动新进程后退出。
		cmd := exec.Command(self)
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Start()
		os.Exit(0)
	}
	_ = exec.Command(self, os.Args[1:]...).Start()
	os.Exit(0)
}

// max 返回两个整数中的较大值。
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
