// Package winpath 提供 Windows 展示路径与 Win32-OpenSSH SFTP 路径之间的纯函数转换。
//
// Win32-OpenSSH 的 sftp-server 在根 / 下列出盘符（C:、D:…），盘根为 /C:，
// 子路径为 /C:/Users 等（与 Go path.Join("/","C:")="/C:"、path.Join("/C:","Users")="/C:/Users" 天然一致）。
// 前端始终只收发展示路径（C:/Users），后端 API 边界对 Windows 目标机调用 ToSFTP 转换，
// 对 Linux 目标机则逐字节透传。
package winpath

import "strings"

// ToSFTP 将 Windows 展示路径转换为 SFTP 路径。仅对 Windows 目标机调用。
//
//	"C:/a/b"  → "/C:/a/b"
//	"C:\a\b"  → "/C:/a/b"
//	"C:"      → "/C:"
//	"C:/"     → "/C:"
//	"/c:/a"   → "/C:/a"（已是 SFTP 形式，盘符大写归一）
//	"/"       → "/"（盘符列表）
func ToSFTP(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "/"
	}
	if p == "/" {
		return "/"
	}
	if len(p) >= 3 && p[0] == '/' && isLetter(p[1]) && p[2] == ':' {
		return "/" + strings.ToUpper(p[1:2]) + p[2:]
	}
	if len(p) >= 2 && isLetter(p[0]) && p[1] == ':' {
		rest := strings.TrimLeft(p[2:], "/")
		if rest == "" {
			return "/" + strings.ToUpper(p[0:1]) + ":"
		}
		return "/" + strings.ToUpper(p[0:1]) + ":/" + rest
	}
	return p
}

// ToDisplay 将 SFTP 路径转换为 Windows 展示路径。仅对 Windows 目标机的展示层调用。
//
//	"/C:/a/b" → "C:/a/b"
//	"/C:"     → "C:/"
//	"/"       → "/"（盘符列表）
func ToDisplay(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "/" {
		return "/"
	}
	if len(p) >= 3 && p[0] == '/' && isLetter(p[1]) && p[2] == ':' {
		rest := strings.TrimLeft(p[3:], "/")
		if rest == "" {
			return strings.ToUpper(p[1:2]) + ":/"
		}
		return strings.ToUpper(p[1:2]) + ":/" + rest
	}
	return p
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
