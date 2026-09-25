package moments

import (
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
)

// ============ 朋友圈专用限流器（分布式，Redis Lua 原子计数） ============
//
// 对应 moments.ts 的 MomentsRateLimiter。原先是进程内 map，多节点下
// 每个节点各自放行 max 次；现改用 middleware 的分布式限流器，
// Redis 不可用时自动降级为进程内计数。

var (
	// 发布限流：每用户每分钟最多 5 条动态
	publishLimiter = middleware.NewRateLimiter("moments-publish", time.Minute, 5)
	// 点赞限流：每用户每分钟最多 30 次
	likeLimiter = middleware.NewRateLimiter("moments-like", time.Minute, 30)
	// 评论限流：每用户每分钟最多 20 条
	commentLimiter = middleware.NewRateLimiter("moments-comment", time.Minute, 20)
)

// checkLimit 计数+1，返回是否在限额内。
func checkLimit(l *middleware.RateLimiter, key string) bool {
	return l.Allow(key)
}
