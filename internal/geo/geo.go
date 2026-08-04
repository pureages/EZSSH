package geo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Info 描述一个 IP/主机地址的地理位置信息。
type Info struct {
	IP          string  `json:"ip"`
	Country     string  `json:"country"`      // 国家名（中文优先）
	CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2
	Region      string  `json:"region"`       // 省份 / 地区
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

var (
	mu      sync.Mutex
	cache   = map[string]Info{}
	fetched = map[string]time.Time{}
	ttl     = 7 * 24 * time.Hour
	client  = &http.Client{Timeout: 6 * time.Second}
)

// Lookup 查询 addr（IP 或域名）的地理位置，结果缓存 7 天。
// 依次尝试 ip-api.com 与 ipapi.co，全部失败时返回仅含 IP 的空信息。
func Lookup(addr string) Info {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return Info{}
	}
	mu.Lock()
	if t, ok := fetched[addr]; ok && time.Since(t) < ttl {
		info := cache[addr]
		mu.Unlock()
		return info
	}
	mu.Unlock()

	info := Info{IP: addr}
	if v := lookupIPAPI(addr); v.CountryCode != "" {
		info = v
	} else if v := lookupIPAPICo(addr); v.CountryCode != "" {
		info = v
	}

	mu.Lock()
	cache[addr] = info
	fetched[addr] = time.Now()
	mu.Unlock()
	return info
}

// ---- 提供方 1：ip-api.com（HTTP 免费，支持中文国家名）----

type ipapiResp struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	RegionName  string  `json:"regionName"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

func lookupIPAPI(addr string) Info {
	resp, err := client.Get("http://ip-api.com/json/" + url.PathEscape(addr) +
		"?fields=status,country,countryCode,regionName,lat,lon&lang=zh-CN")
	if err != nil {
		return Info{}
	}
	defer resp.Body.Close()
	var v ipapiResp
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.Status != "success" {
		return Info{}
	}
	return Info{
		IP:          addr,
		Country:     v.Country,
		CountryCode: v.CountryCode,
		Region:      v.RegionName,
		Lat:         v.Lat,
		Lon:         v.Lon,
	}
}

// ---- 提供方 2：ipapi.co（HTTPS 免费，无 key）----

type ipapicoResp struct {
	CountryName string  `json:"country_name"`
	CountryCode string  `json:"country_code"`
	Region      string  `json:"region"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

func lookupIPAPICo(addr string) Info {
	resp, err := client.Get("https://ipapi.co/" + url.PathEscape(addr) + "/json/")
	if err != nil {
		return Info{}
	}
	defer resp.Body.Close()
	var v ipapicoResp
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.CountryCode == "" {
		return Info{}
	}
	return Info{
		IP:          addr,
		Country:     v.CountryName,
		CountryCode: v.CountryCode,
		Region:      v.Region,
		Lat:         v.Latitude,
		Lon:         v.Longitude,
	}
}
