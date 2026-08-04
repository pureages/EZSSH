package api

import (
	"net/url"
	"strconv"
	"testing"
	"unicode/utf16"
)

// TestSftpReadGBK 覆盖 GBK / UTF-16LE(BOM) / ASCII 三种编码的读取转码。
func TestSftpReadGBK(t *testing.T) {
	ts, cookie := newAuthedTest(t)
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "gbk-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host: %d", code)
	}
	hostID, _ := hostResp["id"].(string)

	read := func(path string) (int, string) {
		c, resp, _ := doJSON(t, "GET", ts.URL+"/api/hosts/"+hostID+"/sftp/read?path="+url.QueryEscape(path), cookie, nil)
		content, _ := resp["content"].(string)
		return c, content
	}

	// GBK「你好\n」（0xC4E3 你，0xBAC3 好）
	seedRemoteFile(t, host, portStr, "gbk.txt", []byte{0xC4, 0xE3, 0xBA, 0xC3, 0x0A})
	if c, content := read("gbk.txt"); c != 200 || content != "你好\n" {
		t.Fatalf("gbk: %d %q", c, content)
	}

	// UTF-16LE with BOM「世界」
	u := utf16.Encode([]rune("世界"))
	utf16Bytes := []byte{0xFF, 0xFE}
	for _, v := range u {
		utf16Bytes = append(utf16Bytes, byte(v), byte(v>>8))
	}
	seedRemoteFile(t, host, portStr, "u16.txt", utf16Bytes)
	if c, content := read("u16.txt"); c != 200 || content != "世界" {
		t.Fatalf("utf16: %d %q", c, content)
	}

	// ASCII 原样
	seedRemoteFile(t, host, portStr, "ascii.txt", []byte("plain text\n"))
	if c, content := read("ascii.txt"); c != 200 || content != "plain text\n" {
		t.Fatalf("ascii: %d %q", c, content)
	}
}
