package media

// preheat.go 移植自 server/cdn-preheat.ts。
//
// 腾讯云 CDN 自动预热模块：调用 PushUrlsCache API 将资源 URL 推送至 CDN 边缘节点。
// 签名使用 TC3-HMAC-SHA256（腾讯云 API 3.0 标准），完全按 TS 版算法用标准库实现。
//
// 环境变量：
//   TENCENT_SECRET_ID  — 腾讯云 API 密钥 ID
//   TENCENT_SECRET_KEY — 腾讯云 API 密钥 Key
//   CDN_DOMAIN         — CDN 加速域名
//   CDN_PREHEAT_AREA   — 预热区域：mainland / overseas / global（默认 mainland）

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============ 配置 ============

const (
	tencentAPIHost    = "cdn.tencentcloudapi.com"
	tcAPIVersion      = "2018-06-06"
	tcAPIAction       = "PushUrlsCache"
	maxURLsPerRequest = 500
	maxDailyQuota     = 1000
	requestInterval   = 200 * time.Millisecond
	dedupTTL          = 24 * time.Hour
	maxPreheatHistory = 100
	signedHeadersTC3  = "content-type;host;x-tc-action"
)

// ============ 类型 ============

// PreheatRecord 单条预热历史记录（对应 TS PreheatRecord）。
type PreheatRecord struct {
	TaskID    string   `json:"taskId"`
	URLs      []string `json:"urls"`
	Area      string   `json:"area"`
	Timestamp int64    `json:"timestamp"`
	Success   bool     `json:"success"`
	Error     string   `json:"error,omitempty"`
}

// PreheatOptions 预热选项（对应 TS PreheatOptions）。
type PreheatOptions struct {
	Area   string
	Force  bool
	Source string
}

// PreheatResult 预热结果（对应 TS PreheatResult）。
type PreheatResult struct {
	Success        bool     `json:"success"`
	PreheatedCount int      `json:"preheatedCount"`
	SkippedCount   int      `json:"skippedCount"`
	TaskIDs        []string `json:"taskIds"`
	DailyUsed      int      `json:"dailyUsed"`
	DailyRemaining int      `json:"dailyRemaining"`
	Errors         []string `json:"errors"`
}

// PreheatStatus 预热模块状态（对应 TS PreheatStatus）。
type PreheatStatus struct {
	Enabled        bool            `json:"enabled"`
	CdnDomain      string          `json:"cdnDomain"`
	Area           string          `json:"area"`
	DailyQuota     int             `json:"dailyQuota"`
	DailyUsed      int             `json:"dailyUsed"`
	DailyRemaining int             `json:"dailyRemaining"`
	CachedURLCount int             `json:"cachedUrlCount"`
	HistoryCount   int             `json:"historyCount"`
	RecentHistory  []PreheatRecord `json:"recentHistory"`
}

// ============ 状态追踪（加锁：Go 为并发服务器，TS 为单线程） ============

var preheatState = struct {
	sync.Mutex
	dailyCount  int
	resetDate   string
	dedup       map[string]int64 // url -> 预热时间戳（毫秒）
	history     []PreheatRecord
	lastRequest time.Time
}{
	dedup: map[string]int64{},
}

// ============ TC3-HMAC-SHA256 签名 ============

func tcSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func tcHMACSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// signTC3Request 生成 TC3-HMAC-SHA256 签名（与 TS signRequest 同算法）。
func signTC3Request(secretID, secretKey, payload string, timestamp int64) string {
	service := "cdn"
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	algorithm := "TC3-HMAC-SHA256"

	// Step 1: 拼接规范请求串
	canonicalHeaders := "content-type:application/json; charset=utf-8\n" +
		"host:" + tencentAPIHost + "\n" +
		"x-tc-action:" + strings.ToLower(tcAPIAction) + "\n"
	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders,
		signedHeadersTC3,
		tcSHA256Hex([]byte(payload)),
	}, "\n")

	// Step 2: 拼接待签名字符串
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{
		algorithm,
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		tcSHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Step 3: 计算签名
	secretDate := tcHMACSHA256([]byte("TC3"+secretKey), date)
	secretService := tcHMACSHA256(secretDate, service)
	secretSigning := tcHMACSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(tcHMACSHA256(secretSigning, stringToSign))

	// Step 4: 拼接 Authorization
	return algorithm + " Credential=" + secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeadersTC3 + ", Signature=" + signature
}

// ============ 核心预热函数 ============

// checkDailyReset 检查并重置每日计数（调用方需持有 preheatState 锁）。
func checkDailyResetLocked(now time.Time) {
	today := now.UTC().Format("2006-01-02")
	if preheatState.resetDate != today {
		preheatState.dailyCount = 0
		preheatState.resetDate = today
		nowMs := now.UnixMilli()
		for u, ts := range preheatState.dedup {
			if nowMs-ts > dedupTTL.Milliseconds() {
				delete(preheatState.dedup, u)
			}
		}
	}
}

// filterNewURLs 过滤已预热的 URL（调用方需持有锁）。
func filterNewURLsLocked(urls []string, nowMs int64) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if last, ok := preheatState.dedup[u]; !ok || nowMs-last > dedupTTL.Milliseconds() {
			out = append(out, u)
		}
	}
	return out
}

// addHistoryLocked 添加预热历史（调用方需持有锁）。
func addHistoryLocked(rec PreheatRecord) {
	preheatState.history = append([]PreheatRecord{rec}, preheatState.history...)
	if len(preheatState.history) > maxPreheatHistory {
		preheatState.history = preheatState.history[:maxPreheatHistory]
	}
}

// callPushUrlsCache 调用腾讯云 CDN PushUrlsCache API。
func callPushUrlsCache(urls []string, area string) (taskID, requestID string, err error) {
	secretID := os.Getenv("TENCENT_SECRET_ID")
	secretKey := os.Getenv("TENCENT_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		return "", "", fmt.Errorf("缺少腾讯云 API 密钥配置（TENCENT_SECRET_ID / TENCENT_SECRET_KEY）")
	}

	payloadBytes, _ := json.Marshal(map[string]any{"Urls": urls, "Area": area})
	payload := string(payloadBytes)
	timestamp := time.Now().Unix()
	authorization := signTC3Request(secretID, secretKey, payload, timestamp)

	req, err := http.NewRequest(http.MethodPost, "https://"+tencentAPIHost, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Host = tencentAPIHost
	req.Header.Set("X-TC-Action", tcAPIAction)
	req.Header.Set("X-TC-Version", tcAPIVersion)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("Authorization", authorization)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	var result struct {
		Response struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
			TaskID    string `json:"TaskId"`
			RequestID string `json:"RequestId"`
		} `json:"Response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}
	if result.Response.Error != nil {
		return "", "", fmt.Errorf("[%s] %s", result.Response.Error.Code, result.Response.Error.Message)
	}
	return result.Response.TaskID, result.Response.RequestID, nil
}

// PreheatURLs 预热指定 URL 列表到 CDN 节点（对应 TS preheatUrls）。
// 自动处理去重（24 小时）、分批（每批 500）、限流（≥200ms 间隔）、配额（每日 1000）。
func PreheatURLs(urls []string, options PreheatOptions) PreheatResult {
	area := options.Area
	if area == "" {
		area = os.Getenv("CDN_PREHEAT_AREA")
	}
	if area == "" {
		area = "mainland"
	}
	source := options.Source
	if source == "" {
		source = "manual"
	}

	preheatState.Lock()
	now := time.Now()
	checkDailyResetLocked(now)
	var newURLs []string
	if options.Force {
		newURLs = append([]string{}, urls...)
	} else {
		newURLs = filterNewURLsLocked(urls, now.UnixMilli())
	}
	skippedCount := len(urls) - len(newURLs)
	preheatState.Unlock()

	emptyResult := func(success bool, errs []string) PreheatResult {
		preheatState.Lock()
		defer preheatState.Unlock()
		return PreheatResult{
			Success:        success,
			PreheatedCount: 0,
			SkippedCount:   skippedCount,
			TaskIDs:        []string{},
			DailyUsed:      preheatState.dailyCount,
			DailyRemaining: max(0, maxDailyQuota-preheatState.dailyCount),
			Errors:         errs,
		}
	}

	if len(newURLs) == 0 {
		log.Printf("[CDN-Preheat] 所有 %d 条 URL 已在 24 小时内预热过，跳过", len(urls))
		return emptyResult(true, []string{})
	}

	preheatState.Lock()
	remaining := maxDailyQuota - preheatState.dailyCount
	preheatState.Unlock()
	if remaining <= 0 {
		msg := fmt.Sprintf("今日预热配额已用完（%d/%d）", preheatState.dailyCount, maxDailyQuota)
		log.Printf("[CDN-Preheat] %s", msg)
		return emptyResult(false, []string{msg})
	}

	urlsToProcess := newURLs
	if len(urlsToProcess) > remaining {
		urlsToProcess = urlsToProcess[:remaining]
	}
	taskIDs := []string{}
	errs := []string{}
	totalPreheated := 0

	for i := 0; i < len(urlsToProcess); i += maxURLsPerRequest {
		end := i + maxURLsPerRequest
		if end > len(urlsToProcess) {
			end = len(urlsToProcess)
		}
		batch := urlsToProcess[i:end]
		batchNo := i/maxURLsPerRequest + 1

		// 限流：请求间隔 ≥200ms
		preheatState.Lock()
		elapsed := time.Since(preheatState.lastRequest)
		preheatState.Unlock()
		if elapsed < requestInterval {
			time.Sleep(requestInterval - elapsed)
		}
		preheatState.Lock()
		preheatState.lastRequest = time.Now()
		preheatState.Unlock()

		taskID, _, err := callPushUrlsCache(batch, area)
		nowMs := time.Now().UnixMilli()
		preheatState.Lock()
		if err == nil {
			taskIDs = append(taskIDs, taskID)
			totalPreheated += len(batch)
			preheatState.dailyCount += len(batch)
			for _, u := range batch {
				preheatState.dedup[u] = nowMs
			}
			addHistoryLocked(PreheatRecord{TaskID: taskID, URLs: batch, Area: area, Timestamp: nowMs, Success: true})
			preheatState.Unlock()
			log.Printf("[CDN-Preheat] 批次 %d 成功：%d 条 URL，TaskId=%s，来源=%s，区域=%s",
				batchNo, len(batch), taskID, source, area)
		} else {
			errMsg := err.Error()
			errs = append(errs, fmt.Sprintf("批次 %d 失败: %s", batchNo, errMsg))
			addHistoryLocked(PreheatRecord{TaskID: "", URLs: batch, Area: area, Timestamp: nowMs, Success: false, Error: errMsg})
			preheatState.Unlock()
			log.Printf("[CDN-Preheat] 批次失败: %s", errMsg)
		}
	}

	preheatState.Lock()
	defer preheatState.Unlock()
	return PreheatResult{
		Success:        len(errs) == 0,
		PreheatedCount: totalPreheated,
		SkippedCount:   skippedCount + (len(newURLs) - len(urlsToProcess)),
		TaskIDs:        taskIDs,
		DailyUsed:      preheatState.dailyCount,
		DailyRemaining: max(0, maxDailyQuota-preheatState.dailyCount),
		Errors:         errs,
	}
}

// ExtractStickerUrls 从贴纸包数据中提取 CDN 域名下的 URL（对应 TS extractStickerUrls）。
func ExtractStickerUrls(stickers []map[string]string, cdnDomain string) []string {
	domain := cdnDomain
	if domain == "" {
		domain = os.Getenv("CDN_DOMAIN")
	}
	if domain == "" {
		return []string{}
	}
	seen := map[string]struct{}{}
	var urls []string
	for _, st := range stickers {
		for _, key := range []string{"url", "thumbUrl"} {
			u := st[key]
			if u != "" && strings.Contains(u, domain) {
				if _, ok := seen[u]; !ok {
					seen[u] = struct{}{}
					urls = append(urls, u)
				}
			}
		}
	}
	sort.Strings(urls)
	return urls
}

// PreheatStickerPack 预热贴纸包中的所有 CDN 资源（对应 TS preheatStickerPack）。
func PreheatStickerPack(packID, packName string, stickers []map[string]string, options PreheatOptions) PreheatResult {
	urls := ExtractStickerUrls(stickers, "")
	if len(urls) == 0 {
		log.Printf("[CDN-Preheat] 贴纸包 %q (%s) 无需预热的 CDN URL", packName, packID)
		preheatState.Lock()
		defer preheatState.Unlock()
		checkDailyResetLocked(time.Now())
		return PreheatResult{
			Success:        true,
			PreheatedCount: 0,
			SkippedCount:   0,
			TaskIDs:        []string{},
			DailyUsed:      preheatState.dailyCount,
			DailyRemaining: max(0, maxDailyQuota-preheatState.dailyCount),
			Errors:         []string{},
		}
	}
	log.Printf("[CDN-Preheat] 开始预热贴纸包 %q (%s)，共 %d 条 URL", packName, packID, len(urls))
	if options.Source == "" {
		options.Source = "sticker-pack:" + packID
	}
	return PreheatURLs(urls, options)
}

// PreheatCdnResources 预热任意资源 URL 列表（对应 TS preheatCdnResources）。
func PreheatCdnResources(rawURLs []string, options PreheatOptions) PreheatResult {
	domain := os.Getenv("CDN_DOMAIN")
	var urls []string
	for _, u := range rawURLs {
		if domain != "" {
			if strings.Contains(u, domain) {
				urls = append(urls, u)
			}
		} else if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		preheatState.Lock()
		defer preheatState.Unlock()
		checkDailyResetLocked(time.Now())
		return PreheatResult{
			Success:        true,
			PreheatedCount: 0,
			SkippedCount:   len(rawURLs),
			TaskIDs:        []string{},
			DailyUsed:      preheatState.dailyCount,
			DailyRemaining: max(0, maxDailyQuota-preheatState.dailyCount),
			Errors:         []string{},
		}
	}
	return PreheatURLs(urls, options)
}

// ============ 状态查询 ============

// GetPreheatStatus 获取预热模块状态（对应 TS getPreheatStatus）。
func GetPreheatStatus() PreheatStatus {
	preheatState.Lock()
	defer preheatState.Unlock()
	checkDailyResetLocked(time.Now())
	recent := make([]PreheatRecord, 0, 20)
	for i, r := range preheatState.history {
		if i >= 20 {
			break
		}
		recent = append(recent, r)
	}
	return PreheatStatus{
		Enabled:        IsPreheatEnabled(),
		CdnDomain:      os.Getenv("CDN_DOMAIN"),
		Area:           firstNonEmptyEnv("CDN_PREHEAT_AREA", "mainland"),
		DailyQuota:     maxDailyQuota,
		DailyUsed:      preheatState.dailyCount,
		DailyRemaining: max(0, maxDailyQuota-preheatState.dailyCount),
		CachedURLCount: len(preheatState.dedup),
		HistoryCount:   len(preheatState.history),
		RecentHistory:  recent,
	}
}

// GetPreheatHistory 获取完整预热历史（对应 TS getPreheatHistory）。
func GetPreheatHistory(limit int) []PreheatRecord {
	if limit <= 0 || limit > maxPreheatHistory {
		limit = 50
	}
	preheatState.Lock()
	defer preheatState.Unlock()
	if limit > len(preheatState.history) {
		limit = len(preheatState.history)
	}
	out := make([]PreheatRecord, limit)
	copy(out, preheatState.history[:limit])
	return out
}

// IsPreheatEnabled 检查预热功能是否可用（对应 TS isPreheatEnabled）。
func IsPreheatEnabled() bool {
	return os.Getenv("TENCENT_SECRET_ID") != "" && os.Getenv("TENCENT_SECRET_KEY") != ""
}

func firstNonEmptyEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
