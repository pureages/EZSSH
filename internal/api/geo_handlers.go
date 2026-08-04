package api

import (
	"net/http"
	"strings"
	"sync"

	"ezssh/internal/geo"
)

// GET /api/geo?hosts=a,b,c
// 批量查询多个主机地址（IP 或域名）的地理位置，结果以地址为键返回。
func (s *Server) handleGeo(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("hosts")
	parts := strings.Split(raw, ",")
	addrs := make([]string, 0, len(parts))
	for _, p := range parts {
		if addr := strings.TrimSpace(p); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	// 并行查询，互不阻塞
	type res struct {
		addr string
		info geo.Info
	}
	ch := make(chan res, len(addrs))
	var wg sync.WaitGroup
	for _, a := range addrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			ch <- res{addr, geo.Lookup(addr)}
		}(a)
	}
	wg.Wait()
	close(ch)

	out := make(map[string]geo.Info, len(addrs))
	for r := range ch {
		out[r.addr] = r.info
	}
	writeJSON(w, http.StatusOK, out)
}
