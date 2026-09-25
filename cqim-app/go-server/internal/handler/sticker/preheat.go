package sticker

import (
	"log"
	"os"
	"strings"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/media"
)

// CDN 预热触发（对应 sticker.ts 中对 cdn-preheat.ts 的调用点）。
//
// 语义与 Node 版完全一致：
//   - isPreheatEnabled：配置了 TENCENT_SECRET_ID + TENCENT_SECRET_KEY 即启用；
//   - install/reload 中 fire-and-forget 异步触发，不阻塞响应，失败仅记日志；
//   - preheatStickerPack 只收集 CDN_DOMAIN 下的 URL（未配置域名则无 URL）；
//   - preheatCdnResources 按域名过滤（未配置则取全部 http(s) URL）。
//
// 注意：腾讯云 PushUrlsCache 的 TC3-HMAC-SHA256 签名与队列/限流/去重等完整实现
// 属于 cdn-preheat.ts 的独立移植范围（本仓库暂未移植），此处保留触发点与过滤语义，
// 实际远端预热调用待 cdn-preheat 模块移植后接入（见 TODO）。
func isPreheatEnabled() bool {
	return os.Getenv("TENCENT_SECRET_ID") != "" && os.Getenv("TENCENT_SECRET_KEY") != ""
}

// extractStickerURLs 提取贴纸包中需预热的 CDN URL（对应 cdn-preheat.ts extractStickerUrls）。
func extractStickerURLs(stickers []StickerItem) []string {
	domain := strings.TrimSpace(os.Getenv("CDN_DOMAIN"))
	if domain == "" {
		return []string{}
	}
	seen := make(map[string]bool)
	var urls []string
	for _, s := range stickers {
		for _, u := range []string{s.URL, strVal(s.ThumbURL)} {
			if u == "" || seen[u] {
				continue
			}
			if strings.Contains(u, domain) {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}
	return urls
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// preheatStickerPack 触发单个贴纸包的 CDN 预热（对应 preheatStickerPack）。
func preheatStickerPack(pack StickerPack) {
	urls := extractStickerURLs(pack.Stickers)
	if len(urls) == 0 {
		return
	}
	preheatCdnResources(urls)
}

// preheatCdnResources 触发一批 URL 的 CDN 预热（对应 preheatCdnResources）。
func preheatCdnResources(rawURLs []string) {
	domain := strings.TrimSpace(os.Getenv("CDN_DOMAIN"))
	var urls []string
	for _, u := range rawURLs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if domain != "" {
			if strings.Contains(u, domain) {
				urls = append(urls, u)
			}
		} else if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return
	}
	// 接入 media 包的真实腾讯云 PushUrlsCache 预热实现（后台异步，不阻塞响应）
	go func() {
		result := media.PreheatURLs(urls, media.PreheatOptions{Source: "sticker"})
		if !result.Success {
			log.Printf("[Sticker] CDN 预热失败: %v", result.Errors)
		} else {
			log.Printf("[Sticker] CDN 预热已提交: %d 个 URL, tasks=%v", result.PreheatedCount, result.TaskIDs)
		}
	}()
}
