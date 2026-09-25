package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

var serverStartTime = time.Now()

// ============ 登录信息 / 站点公开配置 / 健康检查 ============

// GET /api/login-info — 返回客户端真实 IP 和地理位置（用于登录安全通知）。
func (h *Handler) loginInfo(w http.ResponseWriter, r *http.Request) {
	ip := h.realIP(r)
	location := getIPLocation(ip)

	log.Printf("[LoginInfo] IP: %s | 地点: %s | UA: %s", ip, location, truncate(r.Header.Get("User-Agent"), 80))

	resp := map[string]any{"ip": ip, "location": location}
	// ★ 安全优化：生产环境不返回调试信息，防止内部架构泄露
	if h.d.Cfg.NodeEnv != "production" {
		resp["_debug"] = map[string]any{
			"cfConnectingIp": headerOrNull(r, "Cf-Connecting-Ip"),
			"xRealIp":        headerOrNull(r, "X-Real-Ip"),
			"xForwardedFor":  headerOrNull(r, "X-Forwarded-For"),
			"remoteAddress":  r.RemoteAddr,
		}
	}
	util.WriteJSON(w, 200, resp)
}

func headerOrNull(r *http.Request, key string) any {
	if v := r.Header.Get(key); v != "" {
		return v
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// getIPLocation 通过 ip-api.com 查询 IP 地理位置（对应 index.ts）。
func getIPLocation(ip string) string {
	// 本地 IP 直接返回
	if ip == "127.0.0.1" || strings.HasPrefix(ip, "192.168.") ||
		strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "172.") {
		return "内网地址"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/" + ip + "?fields=status,country,regionName,city,query&lang=zh-CN")
	if err != nil {
		return "未知地点"
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "未知地点"
	}
	var data struct {
		Status     string `json:"status"`
		Country    string `json:"country"`
		RegionName string `json:"regionName"`
		City       string `json:"city"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "未知地点"
	}
	if data.Status == "success" {
		parts := []string{}
		if data.Country != "" {
			parts = append(parts, data.Country)
		}
		if data.City != "" {
			parts = append(parts, data.City)
		} else if data.RegionName != "" {
			parts = append(parts, data.RegionName)
		}
		if loc := strings.Join(parts, " · "); loc != "" {
			return loc
		}
	}
	return "未知地点"
}

// GET /api/site-config-public — 返回可公开的站点配置子集（不含敏感字段）。
func (h *Handler) siteConfigPublic(w http.ResponseWriter, r *http.Request) {
	site := h.getSystemConfig(r.Context(), "site")
	name := cfgStr(site, "name")
	if name == "" {
		name = "CQIM"
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	util.WriteJSON(w, 200, map[string]any{
		"name":          name,
		"description":   cfgStr(site, "description"),
		"url":           cfgStr(site, "url"),
		"logo":          cfgStr(site, "logo"),
		"icp":           cfgStr(site, "icp"),
		"policeIcp":     cfgStr(site, "policeIcp"),
		"copyright":     cfgStr(site, "copyright"),
		"autoPlayVideo": cfgBool(site, "autoPlayVideo"),
	})
}

func cfgBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

// GET /api/health — 健康检查。
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	databaseAvailable := true
	if err := h.d.DB.Pool.QueryRow(ctx, "SELECT 1").Scan(new(int)); err != nil {
		databaseAvailable = false
		log.Printf("[health] PostgreSQL 检查失败: %v", err)
	}
	redisAvailable := h.checkRedisHealth(ctx)
	env := h.d.Cfg.NodeEnv
	if env == "" {
		env = "development"
	}
	if databaseAvailable && redisAvailable {
		util.WriteJSON(w, 200, map[string]any{
			"ok":        true,
			"service":   "cqim",
			"checks":    map[string]any{"database": true, "redis": true},
			"env":       env,
			"uptime":    int64(time.Since(serverStartTime).Seconds()),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	errCode := "redis_unavailable"
	if !databaseAvailable {
		errCode = "database_unavailable"
	}
	util.WriteJSON(w, 503, map[string]any{
		"ok":        false,
		"service":   "cqim",
		"checks":    map[string]any{"database": databaseAvailable, "redis": redisAvailable},
		"error":     errCode,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// ============ 腾讯地图 ============

// GET /api/txmap-config — 返回腾讯地图 Key 配置（是否启用）。
func (h *Handler) txmapConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.getSystemConfig(r.Context(), "txmap")
	key := cfgStr(cfg, "key")
	util.WriteJSON(w, 200, map[string]any{"key": key, "enabled": key != ""})
}

// GET /api/txmap/geocoder/reverse — 逆地理编码代理（后端代理避免 Key 泄露和跨域）。
func (h *Handler) txmapReverse(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lng := r.URL.Query().Get("lng")
	if lat == "" || lng == "" {
		util.WriteError(w, 400, "缺少 lat/lng 参数")
		return
	}
	key := cfgStr(h.getSystemConfig(r.Context(), "txmap"), "key")
	if key == "" {
		util.WriteError(w, 503, "腾讯地图 Key 未配置")
		return
	}
	url := "https://apis.map.qq.com/ws/geocoder/v1/?location=" + lat + "," + lng + "&key=" + key + "&get_poi=0"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		util.WriteError(w, 500, "服务器错误")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var data struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Result  *struct {
			Address            string `json:"address"`
			FormattedAddresses *struct {
				Recommend string `json:"recommend"`
			} `json:"formatted_addresses"`
			AddressComponent *struct {
				Province string `json:"province"`
				City     string `json:"city"`
				District string `json:"district"`
			} `json:"address_component"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		util.WriteError(w, 500, "服务器错误")
		return
	}
	if data.Status == 0 && data.Result != nil {
		res := data.Result
		formatted := res.Address
		if res.FormattedAddresses != nil && res.FormattedAddresses.Recommend != "" {
			formatted = res.FormattedAddresses.Recommend
		}
		var province, city, district string
		if res.AddressComponent != nil {
			province, city, district = res.AddressComponent.Province, res.AddressComponent.City, res.AddressComponent.District
		}
		util.WriteJSON(w, 200, map[string]any{
			"address":           res.Address,
			"formatted_address": formatted,
			"province":          province,
			"city":              city,
			"district":          district,
		})
		return
	}
	msg := data.Message
	if msg == "" {
		msg = "逆地理编码失败"
	}
	util.WriteJSON(w, 500, map[string]any{"error": msg, "code": data.Status})
}

// GET /api/txmap/staticmap — 静态地图代理（消息气泡缩略图）。
func (h *Handler) txmapStaticmap(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, lng := q.Get("lat"), q.Get("lng")
	zoom, width, height := q.Get("zoom"), q.Get("width"), q.Get("height")
	if zoom == "" {
		zoom = "15"
	}
	if width == "" {
		width = "300"
	}
	if height == "" {
		height = "150"
	}
	if lat == "" || lng == "" {
		writePlain(w, 400, "缺少 lat/lng 参数")
		return
	}
	key := cfgStr(h.getSystemConfig(r.Context(), "txmap"), "key")
	if key == "" {
		writePlain(w, 503, "Key 未配置")
		return
	}
	url := "https://apis.map.qq.com/ws/staticmap/v2/?center=" + lat + "," + lng +
		"&zoom=" + zoom + "&size=" + width + "*" + height +
		"&markers=size:large|color:red|label:A|" + lat + "," + lng + "&key=" + key
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		writePlain(w, 500, "服务器错误")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writePlain(w, resp.StatusCode, "静态地图请求失败")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		writePlain(w, 500, "服务器错误")
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

// writePlain 写纯文本响应（对应 TS res.status().send(string)）。
func writePlain(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}
