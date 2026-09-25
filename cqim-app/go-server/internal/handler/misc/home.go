// home.go — 首页聚合接口，移植自 server/home.ts。
//
// GET /api/home/sync（需要登录）
// 一次性返回当前用户基本信息、总未读数、置顶公告、会话列表及用户群列表，减少首屏 RTT。
package misc

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// convPeer 会话列表项中的对方信息。
type convPeer struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
}

// convItem 会话列表项（与 TS home.ts 构建的 chats 元素字段一致）。
type convItem struct {
	ID            string   `json:"id"`
	ParticipantA  string   `json:"participantA"`
	ParticipantB  string   `json:"participantB"`
	LastMessage   *string  `json:"lastMessage"`
	LastMessageAt *int64   `json:"lastMessageAt"`
	CreatedAt     int64    `json:"createdAt"`
	UnreadCount   int      `json:"unreadCount"`
	Peer          convPeer `json:"peer"`
}

func (h *Handler) homeSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.UserFrom(r).Id

	user, err := db.QueryRowToStruct[db.User](ctx, h.deps.DB,
		`SELECT "id","username","nickname","avatar","bio","role","phone","createdAt" FROM "User" WHERE "id"=$1`, userID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
			return
		}
		log.Printf("[HomeSync] 查询用户失败: %v", err)
		util.WriteError(w, 500, "首页聚合同步失败")
		return
	}

	ann, err := db.QueryRowToStruct[db.Announcement](ctx, h.deps.DB,
		`SELECT "id","title","content","type","createdAt" FROM "Announcement" WHERE "isActive"=true ORDER BY "createdAt" DESC LIMIT 1`)
	if err != nil && !db.IsNotFound(err) {
		log.Printf("[HomeSync] 查询公告失败: %v", err)
		util.WriteError(w, 500, "首页聚合同步失败")
		return
	}

	totalUnread := getUnreadTotal(ctx, h.deps, userID)

	var chatsRaw json.RawMessage
	if raw, ok := getCachedConversationList(ctx, h.deps, userID); ok {
		chatsRaw = raw
	} else {
		// 缓存未命中，内部简要查询会话列表
		chats, err := h.buildConversationList(ctx, userID)
		if err != nil {
			log.Printf("[HomeSync] 查询会话列表失败: %v", err)
			util.WriteError(w, 500, "首页聚合同步失败")
			return
		}
		setCachedConversationList(ctx, h.deps, userID, chats)
		if b, err := json.Marshal(chats); err == nil {
			chatsRaw = b
		} else {
			chatsRaw = json.RawMessage("[]")
		}
	}

	groups := h.getUserGroups(ctx, userID)

	userJSON := map[string]any{
		"id":        user.Id,
		"username":  user.Username,
		"nickname":  user.Nickname,
		"avatar":    avatarToProxy(util.StrVal(user.Avatar)),
		"bio":       user.Bio,
		"role":      user.Role,
		"phone":     user.Phone,
		"createdAt": user.CreatedAt,
	}
	var annJSON any
	if ann != nil {
		annJSON = map[string]any{
			"id": ann.Id, "title": ann.Title, "content": ann.Content,
			"type": ann.Type, "createdAt": ann.CreatedAt,
		}
	}
	util.WriteJSON(w, 200, map[string]any{
		"code": 200,
		"data": map[string]any{
			"user":         userJSON,
			"totalUnread":  totalUnread,
			"announcement": annJSON,
			"chats":        chatsRaw,
			"groups":       groups,
		},
	})
}

// buildConversationList 查库构建会话列表（对应 home.ts 缓存未命中分支）。
func (h *Handler) buildConversationList(ctx context.Context, userID string) ([]convItem, error) {
	type chatRow struct {
		ID            string     `db:"id"`
		ParticipantA  string     `db:"participantA"`
		ParticipantB  string     `db:"participantB"`
		LastMessage   *string    `db:"lastMessage"`
		LastMessageAt *time.Time `db:"lastMessageAt"`
		CreatedAt     time.Time  `db:"createdAt"`
	}
	chats, err := db.QueryToStructs[chatRow](ctx, h.deps.DB,
		`SELECT "id","participantA","participantB","lastMessage","lastMessageAt","createdAt" FROM "Chat"
		 WHERE "participantA"=$1 OR "participantB"=$1
		 ORDER BY "lastMessageAt" DESC, "createdAt" DESC LIMIT 30`, userID)
	if err != nil {
		return nil, err
	}

	peerIDOf := func(c chatRow) string {
		if c.ParticipantA == userID {
			return c.ParticipantB
		}
		return c.ParticipantA
	}
	peerIDs := make([]string, 0, len(chats))
	seen := map[string]bool{}
	for _, c := range chats {
		if pid := peerIDOf(c); !seen[pid] {
			seen[pid] = true
			peerIDs = append(peerIDs, pid)
		}
	}

	type peerRow struct {
		ID       string  `db:"id"`
		Username string  `db:"username"`
		Nickname *string `db:"nickname"`
		Avatar   *string `db:"avatar"`
		Bio      *string `db:"bio"`
	}
	peerMap := map[string]peerRow{}
	if len(peerIDs) > 0 {
		peers, err := db.QueryToStructs[peerRow](ctx, h.deps.DB,
			`SELECT "id","username","nickname","avatar","bio" FROM "User" WHERE "id"=ANY($1)`, peerIDs)
		if err != nil {
			return nil, err
		}
		for _, p := range peers {
			peerMap[p.ID] = p
		}
	}

	items := make([]convItem, 0, len(chats))
	for _, c := range chats {
		pid := peerIDOf(c)
		peer := convPeer{ID: pid, Username: pid, Nickname: pid, Avatar: "", Bio: ""}
		if p, ok := peerMap[pid]; ok {
			nick := util.StrVal(p.Nickname)
			if nick == "" {
				nick = p.Username
			}
			peer = convPeer{
				ID: p.ID, Username: p.Username, Nickname: nick,
				Avatar: avatarToProxy(util.StrVal(p.Avatar)), Bio: util.StrVal(p.Bio),
			}
		}
		var lastAt *int64
		if c.LastMessageAt != nil {
			ms := c.LastMessageAt.UnixMilli()
			lastAt = &ms
		}
		items = append(items, convItem{
			ID: c.ID, ParticipantA: c.ParticipantA, ParticipantB: c.ParticipantB,
			LastMessage: c.LastMessage, LastMessageAt: lastAt,
			CreatedAt:   c.CreatedAt.UnixMilli(),
			UnreadCount: getUnreadCountOfChat(ctx, h.deps, userID, c.ID),
			Peer:        peer,
		})
	}
	return items, nil
}

// getUserGroups 用户所在群列表（对应 group-message.ts getUserGroups）。
func (h *Handler) getUserGroups(ctx context.Context, userID string) []map[string]any {
	type row struct {
		GroupID     string    `db:"groupId"`
		LastAckSeq  int64     `db:"lastAckSeq"`
		ID          string    `db:"id"`
		DialogID    *string   `db:"dialogId"`
		Name        string    `db:"name"`
		Username    *string   `db:"username"`
		Avatar      *string   `db:"avatar"`
		OwnerID     string    `db:"ownerId"`
		Type        string    `db:"type"`
		IsPublic    bool      `db:"isPublic"`
		MemberCount int32     `db:"memberCount"`
		LastMsgSeq  int64     `db:"lastMsgSeq"`
		CreatedAt   time.Time `db:"createdAt"`
		UpdatedAt   time.Time `db:"updatedAt"`
	}
	rows, err := db.QueryToStructs[row](ctx, h.deps.DB,
		`SELECT m."groupId",m."lastAckSeq",g."id",g."dialogId",g."name",g."username",g."avatar",
		        g."ownerId",g."type",g."isPublic",g."memberCount",g."lastMsgSeq",g."createdAt",g."updatedAt"
		 FROM "GroupMember" m JOIN "Group" g ON g."id"=m."groupId"
		 WHERE m."userId"=$1 ORDER BY m."updatedAt" DESC`, userID)
	if err != nil {
		log.Printf("[HomeSync] 查询群列表失败: %v", err)
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, g := range rows {
		unread := g.LastMsgSeq - g.LastAckSeq
		if unread < 0 {
			unread = 0
		}
		out = append(out, map[string]any{
			"id": g.ID, "groupId": g.GroupID, "dialogId": strOrNil(g.DialogID),
			"name": g.Name, "username": strOrNil(g.Username),
			"avatar":  avatarToProxy(util.StrVal(g.Avatar)),
			"ownerId": g.OwnerID, "type": g.Type, "isPublic": g.IsPublic,
			"memberCount": g.MemberCount, "lastMessage": "🔒 [加密消息]",
			"unreadCount": unread,
			"createdAt":   g.CreatedAt.UnixMilli(),
			"updatedAt":   g.UpdatedAt.UnixMilli(),
		})
	}
	return out
}
