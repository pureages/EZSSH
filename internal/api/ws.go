package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"ezssh/internal/apps"
)

var upgrader = websocket.Upgrader{
	// 开发期放开 Origin 校验；生产部署由反代 + 同源策略兜底
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
}

const maxMessageSize = 1 << 20 // 1MB

// wsMsg 统一消息信封。
type wsMsg struct {
	Type      string          `json:"type"`
	HostID    string          `json:"hostId"`
	AppID     string          `json:"appId"`
	ChannelID string          `json:"channelId"`
	Payload   json.RawMessage `json:"payload"`
}

// wsConn 封装单个 WebSocket 连接及其终端会话。
type wsConn struct {
	srv   *Server
	conn  *websocket.Conn
	hub   *apps.TerminalManager
	mu    sync.Mutex // 串行化写
	muSub sync.Mutex
}

// GET /ws 终端与后续 App 的统一长连接入口。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || !s.am.Validate(c.Value) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	wc := &wsConn{srv: s, conn: conn, hub: apps.NewTerminalManager(s.hub)}
	defer wc.cleanup()

	conn.SetReadLimit(maxMessageSize)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if err := wc.handle(data); err != nil {
			// 错误消息回传请求的 channelId，让发起方 App 能收到并展示
			wc.sendError(wc.channelOf(data), err.Error())
		}
	}
}

// cleanup 断开时清理终端会话与监控订阅。
func (w *wsConn) cleanup() {
	w.hub.CloseAll()
	w.muSub.Lock()
	defer w.muSub.Unlock()
	w.srv.unsubscribeAll(w)
}

func (w *wsConn) handle(data []byte) error {
	var m wsMsg
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("bad json: %v", err)
	}
	switch m.Type {
	// ---- 终端 ----
	case "terminal.open":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var p struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if p.Cols <= 0 {
			p.Cols = 80
		}
		if p.Rows <= 0 {
			p.Rows = 24
		}
		return w.hub.Open(hostID, m.ChannelID, p.Cols, p.Rows, w.pushOutput, w.pushExit)

	case "terminal.input":
		var p struct {
			Data string `json:"data"` // base64
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		b, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return fmt.Errorf("bad input data: %v", err)
		}
		return w.hub.Write(m.ChannelID, b)

	case "terminal.resize":
		var p struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		return w.hub.Resize(m.ChannelID, p.Cols, p.Rows)

	case "terminal.close":
		w.hub.Close(m.ChannelID)
		return nil

	// ---- 监控 ----
	case "monitor.subscribe":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		subID := w.subIDOf(m)
		w.srv.subscribe(w, hostID, subID)
		w.send("monitor.subscribed", m.ChannelID, map[string]string{"hostId": hostID, "subId": subID})
		return nil

	case "monitor.unsubscribe":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		w.srv.unsubscribe(w, hostID, w.subIDOf(m))
		return nil

	case "monitor.hwinfo":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		// 异步执行：桌面图标需批量探测多个主机的系统 logo，
		// 同步执行会阻塞本条 WS 读循环、延迟 monitor.data 等其它消息。
		go func() {
			hi, err := w.srv.monitor.HardwareInfo(hostID)
			if err != nil {
				w.sendError(m.ChannelID, err.Error())
				return
			}
			w.send("monitor.hwinfo", m.ChannelID, map[string]any{"info": hi})
		}()
		return nil

	// ---- 进程 ----
	case "ps.list":
		hostID := w.hostIDOf(m)
		procs, err := w.srv.procs.List(hostID)
		if err != nil {
			return err
		}
		w.send("ps.list", m.ChannelID, map[string]any{"processes": procs})
		return nil

	case "ps.kill":
		var p struct {
			PID    int `json:"pid"`
			Signal int `json:"signal"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if p.Signal != 9 {
			p.Signal = 15
		}
		if err := w.srv.procs.Kill(w.hostIDOf(m), p.PID, p.Signal); err != nil {
			return err
		}
		w.send("ps.killed", m.ChannelID, map[string]any{"pid": p.PID})
		return nil

	// ---- Docker ----
	case "docker.list":
		hostID := w.hostIDOf(m)
		containers, err := w.srv.docker.ListContainers(hostID)
		if err != nil {
			return err
		}
		w.send("docker.list", m.ChannelID, map[string]any{"containers": containers})
		return nil

	case "docker.images":
		hostID := w.hostIDOf(m)
		images, err := w.srv.docker.ListImages(hostID)
		if err != nil {
			return err
		}
		w.send("docker.images", m.ChannelID, map[string]any{"images": images})
		return nil

	case "docker.stats":
		hostID := w.hostIDOf(m)
		stats, err := w.srv.docker.ListStats(hostID)
		if err != nil {
			return err
		}
		w.send("docker.stats", m.ChannelID, map[string]any{"stats": stats})
		return nil

	case "docker.action":
		var p struct {
			Action string `json:"action"` // start|stop|restart|rm|rmi
			ID     string `json:"id"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		var err error
		switch p.Action {
		case "start":
			err = w.srv.docker.Start(w.hostIDOf(m), p.ID)
		case "stop":
			err = w.srv.docker.Stop(w.hostIDOf(m), p.ID)
		case "restart":
			err = w.srv.docker.Restart(w.hostIDOf(m), p.ID)
		case "rm":
			err = w.srv.docker.Remove(w.hostIDOf(m), p.ID)
		case "rmi":
			err = w.srv.docker.RemoveImage(w.hostIDOf(m), p.ID)
		default:
			return fmt.Errorf("unknown docker action %q", p.Action)
		}
		if err != nil {
			return err
		}
		w.send("docker.action", m.ChannelID, map[string]any{"ok": true, "action": p.Action, "id": p.ID})
		return nil

	case "docker.logs":
		var p struct {
			ID   string `json:"id"`
			Tail int    `json:"tail"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		logs, err := w.srv.docker.Logs(w.hostIDOf(m), p.ID, p.Tail)
		if err != nil {
			return err
		}
		w.send("docker.logs", m.ChannelID, map[string]any{"id": p.ID, "logs": logs})
		return nil

	case "docker.check":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		st, err := w.srv.docker.CheckInstalled(hostID)
		if err != nil {
			return err
		}
		w.send("docker.check", m.ChannelID, map[string]any{"installed": st.Installed, "version": st.Version})
		return nil

	case "docker.install":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		// 安装耗时长，异步执行并流式回传输出，避免阻塞连接上的其他消息
		go func() {
			err := w.srv.docker.Install(hostID, func(line string) {
				w.send("docker.install.output", m.ChannelID, map[string]string{"line": line})
			})
			if err != nil {
				w.send("docker.install.done", m.ChannelID, map[string]any{"ok": false, "message": err.Error()})
			} else {
				w.send("docker.install.done", m.ChannelID, map[string]any{"ok": true})
			}
		}()
		w.send("docker.install", m.ChannelID, map[string]any{"ok": true, "started": true})
		return nil

	case "docker.create":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var spec apps.CreateSpec
		if err := json.Unmarshal(m.Payload, &spec); err != nil {
			return err
		}
		id, err := w.srv.docker.CreateContainer(hostID, spec)
		if err != nil {
			return err
		}
		w.send("docker.create", m.ChannelID, map[string]any{"ok": true, "containerId": id})
		return nil

	case "docker.inspect":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		details, err := w.srv.docker.Inspect(w.hostIDOf(m), p.ID)
		if err != nil {
			return err
		}
		w.send("docker.inspect", m.ChannelID, map[string]any{"container": details})
		return nil

	case "docker.create.stream":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var spec apps.CreateSpec
		if err := json.Unmarshal(m.Payload, &spec); err != nil {
			return err
		}
		// 异步执行并流式回传 docker 输出（拉取镜像进度等）
		go func() {
			id, err := w.srv.docker.CreateContainerStream(hostID, spec, func(line string) {
				w.send("docker.create.output", m.ChannelID, map[string]string{"line": line})
			})
			if err != nil {
				w.send("docker.create.done", m.ChannelID, map[string]any{"ok": false, "message": err.Error()})
			} else {
				w.send("docker.create.done", m.ChannelID, map[string]any{"ok": true, "containerId": id})
			}
		}()
		w.send("docker.create.stream", m.ChannelID, map[string]any{"ok": true, "started": true})
		return nil

	// ---- 防火墙 ----
	case "firewall.status":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		st, err := w.srv.firewall.Status(hostID)
		if err != nil {
			return err
		}
		w.send("firewall.status", m.ChannelID, map[string]any{"status": st})
		return nil

	case "firewall.set":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if err := w.srv.firewall.SetEnabled(hostID, p.Enabled); err != nil {
			return err
		}
		w.send("firewall.set", m.ChannelID, map[string]any{"ok": true, "enabled": p.Enabled})
		return nil

	case "firewall.list":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		rules, err := w.srv.firewall.ListRules(hostID)
		if err != nil {
			return err
		}
		w.send("firewall.list", m.ChannelID, map[string]any{"rules": rules})
		return nil

	case "firewall.rule.add":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var spec apps.RuleSpec
		if err := json.Unmarshal(m.Payload, &spec); err != nil {
			return err
		}
		msg, err := w.srv.firewall.AddRule(hostID, spec)
		if err != nil {
			return err
		}
		w.send("firewall.rule.add", m.ChannelID, map[string]any{"ok": true, "message": msg})
		return nil

	case "firewall.rule.remove":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var spec apps.RuleSpec
		if err := json.Unmarshal(m.Payload, &spec); err != nil {
			return err
		}
		msg, err := w.srv.firewall.RemoveRule(hostID, spec)
		if err != nil {
			return err
		}
		w.send("firewall.rule.remove", m.ChannelID, map[string]any{"ok": true, "message": msg})
		return nil

	// ---- 下载 ----
	case "download.check":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		st, err := w.srv.download.CheckInstalled(hostID)
		if err != nil {
			return err
		}
		w.send("download.check", m.ChannelID, map[string]any{"status": st})
		return nil

	case "download.install":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		// 安装耗时长，异步执行并流式回传输出
		go func() {
			err := w.srv.download.Install(hostID, func(line string) {
				w.send("download.install.output", m.ChannelID, map[string]string{"line": line})
			})
			if err != nil {
				w.send("download.install.done", m.ChannelID, map[string]any{"ok": false, "message": err.Error()})
			} else {
				w.send("download.install.done", m.ChannelID, map[string]any{"ok": true})
			}
		}()
		return nil

	case "download.listdir":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		// 同步执行：目录浏览需快速响应；加超时防 SSH 假死拖住处理循环
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		listing, err := w.srv.download.ListDir(ctx, hostID, p.Path)
		if err != nil {
			return err
		}
		w.send("download.listdir", m.ChannelID, listing)
		return nil

	case "download.add":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		var p struct {
			URL           string `json:"url"`
			Dir           string `json:"dir"`
			Name          string `json:"name"`
			TorrentBase64 string `json:"torrentBase64"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		task, err := w.srv.download.Add(hostID, p.URL, p.TorrentBase64, p.Dir, p.Name)
		if err != nil {
			return err
		}
		w.send("download.add", m.ChannelID, map[string]any{"task": task})
		return nil

	case "download.list":
		hostID := w.hostIDOf(m)
		if hostID == "" {
			return fmt.Errorf("hostId required")
		}
		// 列表由前端每 2s 轮询触发，必须加超时，防止 SSH 假死拖住整个 WS 处理循环
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tasks := w.srv.download.List(ctx, hostID)
		w.send("download.list", m.ChannelID, map[string]any{"tasks": tasks})
		return nil

	case "download.pause":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if err := w.srv.download.Pause(p.ID); err != nil {
			return err
		}
		w.send("download.pause", m.ChannelID, map[string]any{"ok": true})
		return nil

	case "download.resume":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if err := w.srv.download.Resume(p.ID); err != nil {
			return err
		}
		w.send("download.resume", m.ChannelID, map[string]any{"ok": true})
		return nil

	case "download.cancel":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return err
		}
		if err := w.srv.download.Cancel(p.ID); err != nil {
			return err
		}
		w.send("download.cancel", m.ChannelID, map[string]any{"ok": true})
		return nil

	case "ping":
		w.send("pong", m.ChannelID, nil)
		return nil

	default:
		return fmt.Errorf("unknown message type %q", m.Type)
	}
}

// hostIDOf 返回消息中的 hostId（顶层或 payload）。
func (w *wsConn) hostIDOf(m wsMsg) string {
	if m.HostID != "" {
		return m.HostID
	}
	var p struct {
		HostID string `json:"hostId"`
	}
	if err := json.Unmarshal(m.Payload, &p); err == nil {
		return p.HostID
	}
	return ""
}

// subIDOf 返回消息中的 subId（payload），区分同一连接上的不同订阅者。
func (w *wsConn) subIDOf(m wsMsg) string {
	var p struct {
		SubID string `json:"subId"`
	}
	if err := json.Unmarshal(m.Payload, &p); err == nil && p.SubID != "" {
		return p.SubID
	}
	return "default"
}

// ---- 监控订阅管理 ----

func (s *Server) subscribe(w *wsConn, hostID, subID string) {
	s.muSub.Lock()
	defer s.muSub.Unlock()
	if s.monitorSubs == nil {
		s.monitorSubs = make(map[string]map[string]*wsConn)
	}
	m := s.monitorSubs[hostID]
	if m == nil {
		m = make(map[string]*wsConn)
		s.monitorSubs[hostID] = m
	}
	m[subID] = w
	s.monitor.Ensure(hostID)
}

func (s *Server) unsubscribe(w *wsConn, hostID, subID string) {
	s.muSub.Lock()
	defer s.muSub.Unlock()
	m := s.monitorSubs[hostID]
	if m == nil {
		return
	}
	if m[subID] == w {
		delete(m, subID)
	}
	if len(m) == 0 {
		delete(s.monitorSubs, hostID)
		s.monitor.Stop(hostID)
	}
}

func (s *Server) unsubscribeAll(w *wsConn) {
	for hostID, m := range s.monitorSubs {
		for subID, conn := range m {
			if conn == w {
				delete(m, subID)
			}
		}
		if len(m) == 0 {
			delete(s.monitorSubs, hostID)
			s.monitor.Stop(hostID)
		}
	}
}

func (s *Server) broadcastMonitor(hostID string, snap apps.Snapshot) {
	s.muSub.Lock()
	m := s.monitorSubs[hostID]
	if len(m) == 0 {
		s.muSub.Unlock()
		return
	}
	// 同一连接可能有多条订阅（subId 不同），去重只发一次
	seen := make(map[*wsConn]bool)
	conns := make([]*wsConn, 0, len(m))
	for _, w := range m {
		if !seen[w] {
			seen[w] = true
			conns = append(conns, w)
		}
	}
	s.muSub.Unlock()

	for _, w := range conns {
		w.send("monitor.data", "", map[string]any{"hostId": hostID, "snapshot": snap})
	}
}

// ---- 发送辅助 ----

func (w *wsConn) pushOutput(channelID string, data []byte) {
	w.send("terminal.output", channelID, map[string]string{
		"data": base64.StdEncoding.EncodeToString(data),
	})
}

func (w *wsConn) pushExit(channelID string) {
	w.send("terminal.exit", channelID, nil)
}

// channelOf 从原始消息里提取 channelId，用于把后端错误回传给发起请求的 channel。
func (w *wsConn) channelOf(data []byte) string {
	var m struct {
		ChannelID string `json:"channelId"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.ChannelID
}

func (w *wsConn) sendError(channelID, msg string) {
	w.send("error", channelID, map[string]string{"message": msg})
}

func (w *wsConn) send(msgType, channelID string, payload any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.WriteJSON(map[string]any{
		"type":      msgType,
		"channelId": channelID,
		"payload":   payload,
	})
}
