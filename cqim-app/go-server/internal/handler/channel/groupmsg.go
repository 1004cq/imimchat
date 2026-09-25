package channel

// group-message.ts 中被 channel.ts 调用的函数移植（sendGroupMessage / pullGroupMessages /
// ackGroupMessages / getGroupUnreadCounts / joinGroup / getGroupInfo / getGroupMembers），
// 保持与 TS 相同的查询语义、权限语义和返回形状。
//
// 注意：group-message.ts 本体由另一模块移植；这里只保留 channel 所需的最小实现。
// 未来 group 模块落地后可收敛到统一实现。

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// IM 推送频道（publish-im.ts 的 IM_PUSH_CHANNEL）

// ============ joinGroup ============

// joinGroup 加入群/频道：lastAckSeq 初始化为当前最新 seq；频道不发送入群系统消息。
func joinGroup(ctx context.Context, d *handler.Deps, groupID, userID string) error {
	g, err := db.QueryRowToStruct[db.Group](ctx, d.DB,
		`SELECT "lastMsgSeq","type" FROM "Group" WHERE "id"=$1`, groupID)
	if err != nil {
		if db.IsNotFound(err) {
			return errors.New("群组不存在")
		}
		return err
	}
	tx, err := d.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := time.Now()
	if _, err := tx.Exec(ctx,
		`INSERT INTO "GroupMember"("id","groupId","userId","role","lastAckSeq","joinTime","createdAt","updatedAt")
		 VALUES($1,$2,$3,'member',$4,$5,$5,$5)`,
		util.NewID(), groupID, userID, g.LastMsgSeq, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE "Group" SET "memberCount"="memberCount"+1 WHERE "id"=$1`, groupID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// 频道订阅不发送"加入了群聊"系统消息，保持频道消息流干净（与 TS 一致）
	return nil
}

// ============ getGroupInfo ============

// getGroupInfo 获取群信息；lastMsgSeq 转字符串；memberCount 取实际成员数（与 TS _count 一致）。
func getGroupInfo(ctx context.Context, d *handler.Deps, groupID string) (map[string]any, error) {
	g, err := db.QueryRowToStruct[db.Group](ctx, d.DB,
		`SELECT * FROM "Group" WHERE "id"=$1`, groupID)
	if err != nil {
		if db.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var memberCount int64
	_ = d.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "GroupMember" WHERE "groupId"=$1`, groupID).Scan(&memberCount)
	return map[string]any{
		"id": g.Id, "dialogId": strVal(g.DialogId), "name": g.Name,
		"username": strVal(g.Username), "avatar": strVal(g.Avatar),
		"ownerId": g.OwnerId, "type": g.Type, "isPublic": g.IsPublic,
		"maxMembers": g.MaxMembers, "memberCount": memberCount,
		"lastMsgSeq":  strconv.FormatInt(g.LastMsgSeq, 10),
		"lastMsgTime": g.LastMsgTime, "announcement": strVal(g.Announcement),
		"createdAt": g.CreatedAt, "updatedAt": g.UpdatedAt,
	}, nil
}

// ============ getGroupMembers ============

// getGroupMembers 分页获取群成员：按 role, joinTime 排序；lastAckSeq 转字符串；补齐用户昵称头像。
func getGroupMembers(ctx context.Context, d *handler.Deps, groupID string, page, pageSize int) (map[string]any, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 100
	}
	members, err := db.QueryToStructs[db.GroupMember](ctx, d.DB,
		`SELECT * FROM "GroupMember" WHERE "groupId"=$1 ORDER BY "role" ASC, "joinTime" ASC LIMIT $2 OFFSET $3`,
		groupID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	var total int64
	_ = d.DB.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "GroupMember" WHERE "groupId"=$1`, groupID).Scan(&total)

	userIDs := uniqueStrings(members, func(m db.GroupMember) string { return m.UserId })
	userMap := map[string]*db.User{}
	if len(userIDs) > 0 {
		users, err := db.QueryToStructs[db.User](ctx, d.DB,
			`SELECT "id","nickname","username","avatar" FROM "User" WHERE "id" = ANY($1)`, userIDs)
		if err == nil {
			for i := range users {
				userMap[users[i].Id] = &users[i]
			}
		}
	}

	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		u := userMap[m.UserId]
		name := strVal(m.Nickname)
		if name == "" && u != nil {
			name = strVal(u.Nickname)
		}
		if name == "" && u != nil {
			name = u.Username
		}
		if name == "" {
			name = m.UserId
		}
		var avatar string
		if u != nil {
			avatar = avatarToProxy(strVal(u.Avatar))
		}
		out = append(out, map[string]any{
			"id": m.Id, "groupId": m.GroupId, "userId": m.UserId, "role": m.Role,
			"nickname":   strVal(m.Nickname),
			"lastAckSeq": strconv.FormatInt(m.LastAckSeq, 10),
			"joinTime":   m.JoinTime, "muteUntil": m.MuteUntil,
			"createdAt": m.CreatedAt, "updatedAt": m.UpdatedAt,
			"name": name, "avatar": avatar,
		})
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	return map[string]any{
		"members": out, "total": total, "page": page, "pageSize": pageSize, "totalPages": totalPages,
	}, nil
}

// ============ 小工具 ============

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uniqueStrings[T any](items []T, pick func(T) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, it := range items {
		if v := pick(it); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
