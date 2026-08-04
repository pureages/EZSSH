package apps

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodePSCmd(cmd string) string {
	b64 := strings.TrimPrefix(cmd, "powershell -NoProfile -NonInteractive -EncodedCommand ")
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(u))
}

// TestBuildExtractCommand 覆盖 Linux/Windows 各格式的命令构造与不支持格式。
func TestBuildExtractCommand(t *testing.T) {
	// Linux zip：带 unzip 存在性检测前缀
	cmd, ok := BuildExtractCommand("linux", "/tmp/a.zip", "/tmp")
	if !ok {
		t.Fatal("zip should be ok")
	}
	for _, want := range []string{"command -v unzip", "unzip -o '/tmp/a.zip' -d '/tmp'"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("missing %q: %s", want, cmd)
		}
	}
	// Linux tar.gz
	cmd, ok = BuildExtractCommand("linux", "/tmp/a.tar.gz", "/tmp")
	if !ok || !strings.Contains(cmd, "tar -xf '/tmp/a.tar.gz' -C '/tmp'") {
		t.Fatalf("tar: %q %v", cmd, ok)
	}
	// Linux tar.xz 也支持
	if _, ok = BuildExtractCommand("linux", "/tmp/a.tar.xz", "/tmp"); !ok {
		t.Fatal("tar.xz should be ok")
	}

	// Windows zip：EncodedCommand 体内为 Expand-Archive（展示路径）
	cmd, ok = BuildExtractCommand("windows", "/C:/Users/me/a.zip", "/C:/Users/me")
	if !ok {
		t.Fatal("win zip should be ok")
	}
	body := decodePSCmd(cmd)
	if !strings.Contains(body, "Expand-Archive -LiteralPath 'C:/Users/me/a.zip' -DestinationPath 'C:/Users/me' -Force") {
		t.Fatalf("win zip body: %s", body)
	}
	// Windows tar：tar.exe
	cmd, ok = BuildExtractCommand("windows", "/C:/Users/me/a.tar.gz", "/C:/Users/me")
	if !ok {
		t.Fatal("win tar should be ok")
	}
	body = decodePSCmd(cmd)
	if !strings.Contains(body, "& tar.exe -xf 'C:/Users/me/a.tar.gz' -C 'C:/Users/me'") {
		t.Fatalf("win tar body: %s", body)
	}

	// 不支持的格式（.rar / 无扩展名）
	if _, ok = BuildExtractCommand("linux", "/tmp/a.rar", "/tmp"); ok {
		t.Fatal("rar should not be ok")
	}
	if _, ok = BuildExtractCommand("linux", "/tmp/noext", "/tmp"); ok {
		t.Fatal("no-extension should not be ok")
	}
}
