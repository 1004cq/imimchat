package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ GET /api/admin/dashboard 仪表盘 ============

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWindow := startOfToday.AddDate(0, 0, -6)

	count := func(sql string, args ...any) int64 {
		var n int64
		if err := h.db().Pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			log.Printf("[Admin] 仪表盘计数查询失败: %v", err)
		}
		return n
	}

	totalUsers := count(`SELECT COUNT(*) FROM "User"`)
	bannedUsers := count(`SELECT COUNT(*) FROM "User" WHERE "isBanned"=true`)
	totalMoments := count(`SELECT COUNT(*) FROM "Moment"`)
	totalComments := count(`SELECT COUNT(*) FROM "MomentComment"`)
	totalMedia := count(`SELECT COUNT(*) FROM "MediaFile"`)
	totalIpBlacklist := count(`SELECT COUNT(*) FROM "IpBlacklist"`)
	totalAnnouncements := count(`SELECT COUNT(*) FROM "Announcement"`)
	pendingReports := count(`SELECT COUNT(*) FROM "Report" WHERE "status"='pending'`)
	totalSensitiveWords := count(`SELECT COUNT(*) FROM "SensitiveWord" WHERE "isActive"=true`)
	totalLoginLogs := count(`SELECT COUNT(*) FROM "LoginLog"`)
	totalIllegalRequests := count(`SELECT COUNT(*) FROM "IllegalRequest"`)

	// pgx 严格模式：专用行 struct（6 列严格对应）。
	type recentUserRow struct {
		Id        string    `db:"id"`
		Username  string    `db:"username"`
		Nickname  *string   `db:"nickname"`
		Email     *string   `db:"email"`
		IsBanned  bool      `db:"isBanned"`
		CreatedAt time.Time `db:"createdAt"`
	}
	recentUsers, err := db.QueryToStructs[recentUserRow](ctx, h.db(),
		`SELECT "id","username","nickname","email","isBanned","createdAt" FROM "User" ORDER BY "createdAt" DESC LIMIT 5`)
	if err != nil {
		log.Printf("[Admin] 仪表盘查询失败: %v", err)
	}

	type recentMomentRow struct {
		db.Moment
		Username *string `db:"username"`
		Nickname *string `db:"nickname"`
	}
	recentMomentRows, err := db.QueryToStructs[recentMomentRow](ctx, h.db(),
		`SELECT m."id",m."userId",m."content",m."visibility",m."location",m."topics",m."isPinned",m."pinnedAt",m."viewCount",m."sortOrder",m."createdAt",m."updatedAt",
		        u."username",u."nickname"
		 FROM "Moment" m JOIN "User" u ON u."id"=m."userId"
		 ORDER BY m."createdAt" DESC LIMIT 5`)
	if err != nil {
		log.Printf("[Admin] 仪表盘查询失败: %v", err)
	}
	recentMoments := make([]map[string]any, 0, len(recentMomentRows))
	for i := range recentMomentRows {
		m := &recentMomentRows[i]
		recentMoments = append(recentMoments, map[string]any{
			"id": m.Id, "userId": m.UserId, "content": m.Content, "visibility": m.Visibility,
			"location": m.Location, "topics": m.Topics, "isPinned": m.IsPinned, "pinnedAt": m.PinnedAt,
			"viewCount": m.ViewCount, "sortOrder": m.SortOrder, "createdAt": m.CreatedAt, "updatedAt": m.UpdatedAt,
			"user": map[string]any{"username": m.Username, "nickname": m.Nickname},
		})
	}

	recentLoginLogs, err := db.QueryToStructs[db.LoginLog](ctx, h.db(),
		`SELECT "id","userId","username","email","phone","ip","userAgent","country","city","success","failReason","loginType","createdAt"
		 FROM "LoginLog" ORDER BY "createdAt" DESC LIMIT 5`)
	if err != nil {
		log.Printf("[Admin] 仪表盘查询失败: %v", err)
	}

	onlineCount := len(h.getOnlineUsers(ctx))

	// 最近 7 天每日统计
	dailyStats := make([]map[string]any, 0, 7)
	for i := 0; i < 7; i++ {
		dayStart := startOfWindow.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		newUsers := count(`SELECT COUNT(*) FROM "User" WHERE "createdAt">=$1 AND "createdAt"<$2`, dayStart, dayEnd)
		privateMessages := count(`SELECT COUNT(*) FROM "PrivateMessage" WHERE "createdAt">=$1 AND "createdAt"<$2`, dayStart, dayEnd)
		groupMessages := count(`SELECT COUNT(*) FROM "GroupMessage" WHERE "createdAt">=$1 AND "createdAt"<$2`, dayStart, dayEnd)
		activeUsers := count(`SELECT COUNT(*) FROM (
			SELECT "senderId" FROM "PrivateMessage" WHERE "createdAt">=$1 AND "createdAt"<$2
			UNION
			SELECT "senderId" FROM "GroupMessage" WHERE "createdAt">=$1 AND "createdAt"<$2
		) t`, dayStart, dayEnd)
		dailyStats = append(dailyStats, map[string]any{
			"date":        fmt.Sprintf("%d/%d", int(dayStart.Month()), dayStart.Day()),
			"newUsers":    newUsers,
			"activeUsers": activeUsers,
			"messages":    privateMessages + groupMessages,
		})
	}

	// 消息类型分布
	type typeRow struct {
		MsgType string `db:"msgType"`
		Count   int64  `db:"count"`
	}
	typeCounts := map[string]int64{}
	for _, tbl := range []string{`"PrivateMessage"`, `"GroupMessage"`} {
		rows, err := db.QueryToStructs[typeRow](ctx, h.db(),
			`SELECT "msgType", COUNT(*) AS "count" FROM `+tbl+` GROUP BY "msgType"`)
		if err != nil {
			log.Printf("[Admin] 仪表盘查询失败: %v", err)
			continue
		}
		for _, row := range rows {
			typeCounts[row.MsgType] += row.Count
		}
	}
	messageTypes := make([]map[string]any, 0, len(typeCounts))
	var totalMessages int64
	for typ, c := range typeCounts {
		messageTypes = append(messageTypes, map[string]any{"type": typ, "count": c})
		totalMessages += c
	}
	sort.Slice(messageTypes, func(i, j int) bool {
		return messageTypes[i]["count"].(int64) > messageTypes[j]["count"].(int64)
	})

	recentUserItems := make([]map[string]any, 0, len(recentUsers))
	for i := range recentUsers {
		u := &recentUsers[i]
		recentUserItems = append(recentUserItems, map[string]any{
			"id": u.Id, "username": u.Username, "nickname": u.Nickname, "email": u.Email,
			"isBanned": u.IsBanned, "createdAt": u.CreatedAt,
		})
	}

	if recentLoginLogs == nil {
		recentLoginLogs = []db.LoginLog{}
	}

	util.WriteJSON(w, 200, map[string]any{
		"overview": map[string]any{
			"totalUsers": totalUsers, "onlineUsers": onlineCount, "bannedUsers": bannedUsers,
			"totalMessages": totalMessages, "pendingReports": pendingReports,
			"totalSensitiveWords": totalSensitiveWords, "totalAnnouncements": totalAnnouncements,
		},
		"dailyStats":   dailyStats,
		"messageTypes": messageTypes,
		"stats": map[string]any{
			"totalUsers": totalUsers, "bannedUsers": bannedUsers, "totalMoments": totalMoments,
			"totalComments": totalComments, "totalMedia": totalMedia, "totalLoginLogs": totalLoginLogs,
			"totalIpBlacklist": totalIpBlacklist, "totalIllegalRequests": totalIllegalRequests,
		},
		"recentUsers":   recentUserItems,
		"recentMoments": recentMoments,
		"recentLogins":  recentLoginLogs,
	})
}

// getOnlineUsers 在线用户 ID 列表（对应 redis.ts getOnlineUsers：SCAN online:*，异常降级为空）。
func (h *Handler) getOnlineUsers(ctx context.Context) []string {
	out := []string{}
	if h.deps.Redis == nil {
		return out
	}
	iter := h.deps.Redis.RDB.Scan(ctx, 0, "online:*", 200).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if id := key[len("online:"):]; id != "" {
			out = append(out, id)
		}
	}
	return out
}
