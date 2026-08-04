package apps

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
)

// winPSEncoded 将 PowerShell 脚本编码为 -EncodedCommand 的参数（UTF-16LE base64）。
// 兼容 PowerShell 5.1；由于命令体全部 base64 化，无论远端默认 shell 是 cmd / PowerShell /
// Git Bash 都不存在引号转义问题。编码结果为纯 ASCII base64，可安全嵌入其他命令串。
func winPSEncoded(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// winPS 将 PowerShell 脚本编码为完整调用命令。
func winPS(script string) string {
	return "powershell -NoProfile -NonInteractive -EncodedCommand " + winPSEncoded(script)
}

// psLiteral 将字符串包裹为 PowerShell 单引号字面量（内部单引号转义为 ''）。
func psLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
