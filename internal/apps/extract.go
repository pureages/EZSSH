package apps

import (
	"strings"

	"ezssh/internal/winpath"
)

// BuildExtractCommand 构造在远端主机「就地解压」归档的命令（解压到归档所在目录）。
// archiveSFTP: 归档文件 SFTP 路径；dirSFTP: 目标目录（归档所在目录）。
// platform: 'windows' | 'linux'（其它按 linux 处理）。
// 返回 (cmd, ok)；ok=false 表示归档格式不支持。
// 命令构建留在 apps 包内，复用未导出的 winPS/psLiteral/sshQuote，api 包只做调用。
func BuildExtractCommand(platform, archiveSFTP, dirSFTP string) (string, bool) {
	lower := strings.ToLower(archiveSFTP)
	isZip := strings.HasSuffix(lower, ".zip")
	if !isZip && !isTarArchive(lower) {
		return "", false
	}
	if platform == "windows" {
		a := winpath.ToDisplay(archiveSFTP)
		d := winpath.ToDisplay(dirSFTP)
		if isZip {
			return winPS("Expand-Archive -LiteralPath " + psLiteral(a) + " -DestinationPath " + psLiteral(d) + " -Force"), true
		}
		return winPS("& tar.exe -xf " + psLiteral(a) + " -C " + psLiteral(d)), true
	}
	if isZip {
		return "command -v unzip >/dev/null 2>&1 || { echo 'unzip not installed on host'; exit 127; }; " +
			"unzip -o " + sshQuote(archiveSFTP) + " -d " + sshQuote(dirSFTP), true
	}
	return "tar -xf " + sshQuote(archiveSFTP) + " -C " + sshQuote(dirSFTP), true
}

func isTarArchive(lower string) bool {
	return strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz")
}
