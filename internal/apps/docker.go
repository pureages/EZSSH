package apps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"ezssh/internal/sshhub"
)

// ContainerInfo docker 容器信息。
// 注意：docker ps --format '{{json .}}' 输出的 Names 是逗号分隔字符串（非数组）。
type ContainerInfo struct {
	ID      string `json:"id"`
	Names   string `json:"names"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Ports   string `json:"ports"`
	Created int64  `json:"created"`
}

// NameList 返回容器名列表。
func (c ContainerInfo) NameList() []string {
	var names []string
	for _, n := range strings.Split(c.Names, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// ImageInfo docker 镜像信息（字段对应 docker images --format '{{json .}}'，
// 字段名精确匹配输出："Repository"、"Tag" 等）。
type ImageInfo struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
	Containers string `json:"Containers"`
}

// RepoTag 返回 repository:tag。
func (i ImageInfo) RepoTag() string {
	if i.Repository == "" {
		return "<none>"
	}
	if i.Tag == "" {
		return i.Repository
	}
	return i.Repository + ":" + i.Tag
}

// ContainerStats 容器资源占用（字段对应 docker stats --format '{{json .}}'，
// 注意字段名是 "CPUPerc" 等混合大小写，需精确匹配）。
type ContainerStats struct {
	Container string `json:"Container"`
	ID        string `json:"ID"`
	Name      string `json:"Name"`
	CPUPerc   string `json:"CPUPerc"`
	MemUsage  string `json:"MemUsage"`
	MemPerc   string `json:"MemPerc"`
	NetIO     string `json:"NetIO"`
	BlockIO   string `json:"BlockIO"`
	PIDs      string `json:"PIDs"`
}

// CPUPct 解析 "0.37%" 为 0.37。
func (s ContainerStats) CPUPct() float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(s.CPUPerc, "%"), 64)
	return v
}

// MemPctVal 解析 "13.36%" 为 13.36。
func (s ContainerStats) MemPctVal() float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(s.MemPerc, "%"), 64)
	return v
}

// PIDCount 解析 PIDs 字符串为整数。
func (s ContainerStats) PIDCount() int {
	n, _ := strconv.Atoi(s.PIDs)
	return n
}

// DockerManager 通过目标机 docker CLI 管理容器与镜像。
type DockerManager struct {
	hub *sshhub.Hub
}

func NewDockerManager(hub *sshhub.Hub) *DockerManager {
	return &DockerManager{hub: hub}
}

// run 在目标机执行命令并返回 stdout。
func (m *DockerManager) run(hostID, cmd string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = ctx

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		// 容器不存在等业务错误也返回输出供解析
		if len(out) == 0 {
			return "", fmt.Errorf("docker: %w", err)
		}
	}
	return string(out), nil
}

// ListContainers 列出全部容器。
func (m *DockerManager) ListContainers(hostID string) ([]ContainerInfo, error) {
	raw, err := m.run(hostID, `docker ps -a --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}
	var containers []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c ContainerInfo
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// ListImages 列出镜像。
func (m *DockerManager) ListImages(hostID string) ([]ImageInfo, error) {
	raw, err := m.run(hostID, `docker images --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}
	var images []ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var img ImageInfo
		if err := json.Unmarshal([]byte(line), &img); err != nil {
			continue
		}
		images = append(images, img)
	}
	return images, nil
}

// ListStats 获取容器资源占用（单次采样）。
func (m *DockerManager) ListStats(hostID string) ([]ContainerStats, error) {
	raw, err := m.run(hostID, `docker stats --no-stream --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}
	var stats []ContainerStats
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s ContainerStats
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (m *DockerManager) Start(hostID, id string) error {
	_, err := m.run(hostID, `docker start `+shellQuote(id))
	return err
}

func (m *DockerManager) Stop(hostID, id string) error {
	_, err := m.run(hostID, `docker stop `+shellQuote(id))
	return err
}

func (m *DockerManager) Restart(hostID, id string) error {
	_, err := m.run(hostID, `docker restart `+shellQuote(id))
	return err
}

func (m *DockerManager) Remove(hostID, id string) error {
	_, err := m.run(hostID, `docker rm -f `+shellQuote(id))
	return err
}

func (m *DockerManager) RemoveImage(hostID, id string) error {
	_, err := m.run(hostID, `docker rmi -f `+shellQuote(id))
	return err
}

// Logs 返回容器最近日志。
func (m *DockerManager) Logs(hostID, id string, tail int) (string, error) {
	if tail <= 0 {
		tail = 200
	}
	return m.run(hostID, fmt.Sprintf(`docker logs --tail %d %s 2>&1`, tail, shellQuote(id)))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- 安装检测 / 一键安装 / 创建容器 ----

// DockerStatus docker 安装状态。
type DockerStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

// CreateSpec 创建容器所需的完整参数（对应 docker run）。
type CreateSpec struct {
	Name       string   `json:"name"`
	Image      string   `json:"image"`
	Ports      []string `json:"ports"`     // 形如 "8080:80/tcp"
	Env        []string `json:"env"`       // 形如 "KEY=VALUE"
	Volumes    []string `json:"volumes"`   // 形如 "/host:/container"
	ExtraArgs  []string `json:"extraArgs"` // 额外 docker run 参数（整段）
	Network    string   `json:"network"`   // bridge / host / none
	Privileged bool     `json:"privileged"`
	Restart    string   `json:"restart"` // no / always / unless-stopped / on-failure

	// ConfigFile 为配置文件内容（TOML 等）。非空时创建容器前会写入目标机
	// /opt/<name>/ 下，并以只读卷挂载到 ConfigPath。用于绕过那些无视环境变量的镜像。
	ConfigFile string `json:"configFile"`
	ConfigPath string `json:"configPath"` // 配置在容器内的挂载路径，如 /etc/frp/frps.toml
}

// runStrict 在目标机执行命令，命令失败（非零退出码）时返回错误并带上输出。
func (m *DockerManager) runStrict(hostID, cmd string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker: %s", msg)
	}
	return string(out), nil
}

// CheckInstalled 检测目标机是否已安装 docker，返回安装状态与版本。
// 通过 command -v 判断可区分“未安装”与“连接失败”。
func (m *DockerManager) CheckInstalled(hostID string) (DockerStatus, error) {
	out, err := m.runStrict(hostID, `if command -v docker >/dev/null 2>&1; then docker --version; else echo "__DOCKER_NOT_FOUND__"; fi`)
	if err != nil {
		return DockerStatus{Installed: false}, err
	}
	out = strings.TrimSpace(out)
	if strings.Contains(out, "__DOCKER_NOT_FOUND__") {
		return DockerStatus{Installed: false}, nil
	}
	return DockerStatus{Installed: true, Version: out}, nil
}

// Install 一键安装 Docker：检测包管理器 → 安装 curl → 执行官方 get.docker.com 脚本。
// 安装过程中的每一行输出通过 onLine 回调流式返回。
func (m *DockerManager) Install(hostID string, onLine func(string)) error {
	script := `set -e
if command -v docker >/dev/null 2>&1; then
  echo "Docker 已安装: $(docker --version)"
  exit 0
fi
echo "==> 检测系统包管理器"
PM=""
if command -v apt-get >/dev/null 2>&1; then PM=apt
elif command -v dnf >/dev/null 2>&1; then PM=dnf
elif command -v yum >/dev/null 2>&1; then PM=yum
elif command -v apk >/dev/null 2>&1; then PM=apk
fi
if [ -z "$PM" ]; then
  echo "错误：不支持的系统（未检测到 apt/dnf/yum/apk）"
  exit 1
fi
echo "==> 确保 curl 可用"
if ! command -v curl >/dev/null 2>&1; then
  case "$PM" in
    apt) apt-get update -y && apt-get install -y curl ;;
    dnf) dnf install -y curl ;;
    yum) yum install -y curl ;;
    apk) apk add --no-cache curl ;;
  esac
fi
echo "==> 下载并执行 Docker 官方安装脚本"
curl -fsSL https://get.docker.com | sh
echo "==> 启动 Docker 服务"
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now docker || systemctl start docker || true
else
  service docker start || true
fi
echo "==> 安装完成"
docker --version`
	return m.runScript(hostID, script, onLine)
}

// runScript 通过 `sh -s` 在目标机执行多行脚本，并逐行流式回调 stdout/stderr。
func (m *DockerManager) runScript(hostID, script string, onLine func(string)) error {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	sess.Stdin = strings.NewReader(script)
	if err := sess.Start("sh -s"); err != nil {
		return err
	}

	var wg sync.WaitGroup
	stream := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		// docker pull 等进度用 \r 原地刷新，这里同时按 \n 和 \r 切分输出行
		sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if i := bytes.IndexAny(data, "\n\r"); i >= 0 {
				return i + 1, data[:i], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for sc.Scan() {
			if onLine != nil {
				onLine(sc.Text())
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)
	wg.Wait()

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("docker 安装失败: %w", err)
	}
	return nil
}

// writeHostFile 通过 SFTP 将内容写入目标机的指定路径（自动创建父目录）。
// 相比 shell heredoc，SFTP 对任意内容（含引号、$、反引号）都安全。
func (m *DockerManager) writeHostFile(hostID, filePath string, content []byte) error {
	sshc, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	client, err := sftp.NewClient(sshc)
	if err != nil {
		return fmt.Errorf("sftp 连接失败: %w", err)
	}
	defer client.Close()

	dir := path.Dir(filePath)
	if err := client.MkdirAll(dir); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}
	f, err := client.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", filePath, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", filePath, err)
	}
	return nil
}

// prepareConfig 若 spec 携带配置文件，则先将内容写入目标机并追加只读挂载。
// 返回处理后的 spec；无配置时原样返回。
func (m *DockerManager) prepareConfig(hostID string, spec CreateSpec) (CreateSpec, error) {
	if strings.TrimSpace(spec.ConfigFile) == "" {
		return spec, nil
	}
	if spec.Name == "" || strings.TrimSpace(spec.ConfigPath) == "" {
		return spec, fmt.Errorf("配置了 configFile 时必须同时提供容器名称与 configPath")
	}
	hostPath := "/opt/" + spec.Name + "/" + path.Base(spec.ConfigPath)
	if err := m.writeHostFile(hostID, hostPath, []byte(spec.ConfigFile)); err != nil {
		return spec, err
	}
	spec.Volumes = append(spec.Volumes, hostPath+":"+spec.ConfigPath+":ro")
	return spec, nil
}

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// buildRunCommand 按 spec 构建 docker run 命令。所有用户输入均做 shell 转义，防止注入。
func (m *DockerManager) buildRunCommand(spec CreateSpec) (string, error) {
	if strings.TrimSpace(spec.Image) == "" {
		return "", fmt.Errorf("镜像不能为空")
	}
	if spec.Name != "" && !containerNameRe.MatchString(spec.Name) {
		return "", fmt.Errorf("容器名称不合法（仅允许字母数字、下划线、点、横线）")
	}
	args := []string{"run", "-d"}
	if spec.Name != "" {
		args = append(args, "--name", shellQuote(spec.Name))
	}
	restart := strings.TrimSpace(spec.Restart)
	if restart == "" {
		restart = "always"
	}
	if restart != "no" {
		args = append(args, "--restart", shellQuote(restart))
	}
	for _, p := range spec.Ports {
		if p = strings.TrimSpace(p); p != "" {
			args = append(args, "-p", shellQuote(p))
		}
	}
	for _, e := range spec.Env {
		if e = strings.TrimSpace(e); e != "" {
			args = append(args, "-e", shellQuote(e))
		}
	}
	for _, v := range spec.Volumes {
		if v = strings.TrimSpace(v); v != "" {
			args = append(args, "-v", shellQuote(v))
		}
	}
	for _, a := range spec.ExtraArgs {
		if a = strings.TrimSpace(a); a != "" {
			args = append(args, shellQuote(a))
		}
	}
	if spec.Privileged {
		args = append(args, "--privileged")
	}
	switch strings.ToLower(spec.Network) {
	case "host", "none":
		args = append(args, "--network", strings.ToLower(spec.Network))
	case "", "default", "bridge":
		// 默认桥接模式，无需显式指定
	default:
		args = append(args, "--network", shellQuote(spec.Network))
	}
	args = append(args, shellQuote(spec.Image))

	return "docker " + strings.Join(args, " "), nil
}

// CreateContainer 按 spec 构建 docker run 命令并创建/启动容器，返回容器 ID。
func (m *DockerManager) CreateContainer(hostID string, spec CreateSpec) (string, error) {
	spec, err := m.prepareConfig(hostID, spec)
	if err != nil {
		return "", err
	}
	cmd, err := m.buildRunCommand(spec)
	if err != nil {
		return "", err
	}
	out, err := m.runStrict(hostID, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ContainerDetails docker 容器详细信息（docker inspect 摘要）。
type ContainerDetails struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Created string   `json:"created"`
	Ports   []string `json:"ports"`
	Env     []string `json:"env"`
	Volumes []string `json:"volumes"`
	Network string   `json:"network"`
	Restart string   `json:"restart"`
	Command string   `json:"command"`
}

// Inspect 返回容器的详细配置信息（docker inspect 摘要）。
func (m *DockerManager) Inspect(hostID, id string) (ContainerDetails, error) {
	raw, err := m.runStrict(hostID, `docker inspect `+shellQuote(id))
	if err != nil {
		return ContainerDetails{}, err
	}
	var arr []struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		Created string `json:"Created"`
		Config  struct {
			Image string   `json:"Image"`
			Env   []string `json:"Env"`
			Cmd   []string `json:"Cmd"`
		} `json:"Config"`
		HostConfig struct {
			RestartPolicy struct {
				Name string `json:"Name"`
			} `json:"RestartPolicy"`
			NetworkMode string `json:"NetworkMode"`
		} `json:"HostConfig"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) == 0 {
		return ContainerDetails{}, fmt.Errorf("解析 docker inspect 输出失败")
	}
	r0 := arr[0]
	d := ContainerDetails{
		ID:      r0.ID,
		Name:    strings.TrimPrefix(r0.Name, "/"),
		Image:   r0.Config.Image,
		State:   r0.State.Status,
		// 初始化为空切片而非 nil，避免 JSON 序列化输出 null 导致前端读 length 崩溃
		Env:     []string{},
		Ports:   []string{},
		Volumes: []string{},
		Network: r0.HostConfig.NetworkMode,
		Restart: r0.HostConfig.RestartPolicy.Name,
		Command: strings.Join(r0.Config.Cmd, " "),
	}
	if r0.Config.Env != nil {
		d.Env = r0.Config.Env
	}
	if t, err := time.Parse(time.RFC3339Nano, r0.Created); err == nil {
		d.Created = t.Local().Format("2006-01-02 15:04:05")
	} else {
		d.Created = r0.Created
	}
	for _, mnt := range r0.Mounts {
		d.Volumes = append(d.Volumes, mnt.Source+":"+mnt.Destination)
	}
	// 端口映射格式化为 "0.0.0.0:7000->7000/tcp"
	for cport, binds := range r0.NetworkSettings.Ports {
		for _, b := range binds {
			d.Ports = append(d.Ports, b.HostIP+":"+b.HostPort+"->"+cport)
		}
	}
	return d, nil
}

// CreateContainerStream 按 spec 创建容器并流式回传 docker 输出（如拉取镜像进度）。
// 成功时返回 64 位容器 ID。
func (m *DockerManager) CreateContainerStream(hostID string, spec CreateSpec, onLine func(string)) (string, error) {
	if spec.ConfigFile != "" && onLine != nil {
		onLine("==> 写入配置文件")
	}
	var err error
	spec, err = m.prepareConfig(hostID, spec)
	if err != nil {
		return "", err
	}
	cmd, err := m.buildRunCommand(spec)
	if err != nil {
		return "", err
	}
	var lastID string
	err = m.runScript(hostID, cmd, func(line string) {
		if onLine != nil {
			onLine(line)
		}
		if t := strings.TrimSpace(line); t != "" {
			lastID = t
		}
	})
	if err != nil {
		return "", err
	}
	// docker run -d 成功时最后一行输出为 64 位容器 ID
	if len(lastID) == 64 {
		return lastID, nil
	}
	return "", fmt.Errorf("安装失败：未识别到容器 ID（%s）", lastID)
}
