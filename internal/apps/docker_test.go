package apps

import (
	"strings"
	"testing"
)

// 空白归一化，便于对「续行符合并后多余空格」的断言保持稳健。
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestNormalizeDockerRun(t *testing.T) {
	ok := []struct {
		name string
		in   string
		want string
	}{
		{"single line", "docker run -d --name nginx -p 80:80 nginx:latest", "docker run -d --name nginx -p 80:80 nginx:latest"},
		{"sudo prefix", "sudo docker run -d nginx:latest", "sudo docker run -d nginx:latest"},
		{"trim spaces", "  docker run -d nginx  ", "docker run -d nginx"},
		{"line continuation", "docker run -d \\\n  -p 80:80 \\\n  nginx:latest", "docker run -d -p 80:80 nginx:latest"},
		{"crlf continuation", "docker run -d \\\r\n  nginx:latest", "docker run -d nginx:latest"},
	}
	for _, c := range ok {
		got, err := normalizeDockerRun(c.in)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if squash(got) != c.want {
			t.Errorf("%s: got %q want %q", c.name, squash(got), c.want)
		}
	}

	bad := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"not docker run", "docker ps"},
		{"docker runx", "docker runx"},
		{"multi command", "docker run -d nginx\nrm -rf /"},
		{"two lines", "docker run -d nginx\ndocker ps"},
	}
	for _, c := range bad {
		if _, err := normalizeDockerRun(c.in); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestBuildRunCommandSpec(t *testing.T) {
	m := &DockerManager{}
	cmd, err := m.buildRunCommand(CreateSpec{Image: "nginx:latest", Name: "web", Ports: []string{"80:80"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `docker run -d --name 'web' --restart 'always' -p '80:80' 'nginx:latest'`
	if cmd != want {
		t.Errorf("got %q want %q", cmd, want)
	}
}

func TestBuildRunCommandRaw(t *testing.T) {
	m := &DockerManager{}
	raw := "docker run -d --name x -p 8080:80 nginx:latest"
	cmd, err := m.buildRunCommand(CreateSpec{RawCommand: raw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != raw {
		t.Errorf("raw command should be used verbatim, got %q", cmd)
	}
	// 非 docker run 命令应被拒绝
	if _, err := m.buildRunCommand(CreateSpec{RawCommand: "rm -rf /"}); err == nil {
		t.Fatal("expected error for non docker run command")
	}
}
