// profile_sync.go — 用户资料变更实时同步，移植自 server/user-profile-sync.ts。
//
// 统一使用数据库 updatedAt 作为版本时间戳，多端按时间戳去重/合并。
// 发布频道：msg:user_profile_updated（redis.ts publishMessage 的 MSG_CHANNEL_PREFIX + channel）。
//
// 注：TS 用 prisma.friend（userId/status/friendId）取好友，但当前 Prisma schema 实际
// 使用 Friendship(userA/userB) 表（auth 模块已按此移植），此处与之一致。
package misc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// profileSyncChannel 资料更新事件频道（publishMessage('user_profile_updated', …)）。
const profileSyncChannel = "msg:user_profile_updated"

// UserProfileSyncSource 对应 TS UserProfileSyncSource。
type UserProfileSyncSource struct {
	ID            string
	Nickname      *string
	Avatar        *string
	Username      string
	Bio           *string
	BackgroundURL *string
	UpdatedAt     time.Time
}

// profileSyncPayload 发布的消息体（字段与 TS 一致）。
type profileSyncPayload struct {
	UserID          string   `json:"userId"`
	Nickname        *string  `json:"nickname"`
	Avatar          string   `json:"avatar"`
	Username        string   `json:"username"`
	Bio             *string  `json:"bio"`
	BackgroundURL   *string  `json:"backgroundUrl"`
	UpdatedAt       int64    `json:"updatedAt"`
	TargetFriendIDs []string `json:"targetFriendIds"`
	TargetGroupIDs  []string `json:"targetGroupIds"`
}

// PublishUserProfileUpdated 发布用户资料更新事件，通知好友与群成员。
func PublishUserProfileUpdated(d *handler.Deps, u UserProfileSyncSource) error {
	if d.Redis == nil {
		return errRedisUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type idRow struct {
		FriendID string `db:"friendId"`
	}
	friends, _ := db.QueryToStructs[idRow](ctx, d.DB,
		`SELECT CASE WHEN "userA"=$1 THEN "userB" ELSE "userA" END AS "friendId" FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`, u.ID)
	type groupRow struct {
		GroupID string `db:"groupId"`
	}
	groups, _ := db.QueryToStructs[groupRow](ctx, d.DB,
		`SELECT "groupId" FROM "GroupMember" WHERE "userId"=$1`, u.ID)

	friendIDs := make([]string, 0, len(friends))
	for _, f := range friends {
		friendIDs = append(friendIDs, f.FriendID)
	}
	groupIDs := make([]string, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.GroupID)
	}

	payload, err := json.Marshal(profileSyncPayload{
		UserID:          u.ID,
		Nickname:        u.Nickname,
		Avatar:          avatarToProxy(util.StrVal(u.Avatar)),
		Username:        u.Username,
		Bio:             u.Bio,
		BackgroundURL:   u.BackgroundURL,
		UpdatedAt:       u.UpdatedAt.UnixMilli(),
		TargetFriendIDs: friendIDs,
		TargetGroupIDs:  groupIDs,
	})
	if err != nil {
		return err
	}
	return d.Redis.Publish(ctx, profileSyncChannel, string(payload))
}

// PublishUserProfileUpdatedById 按 userId 从库读取并广播（管理员/遗留接口在 update 后调用）。
// 用户不存在时静默返回 nil（与 TS 一致）。
func PublishUserProfileUpdatedById(d *handler.Deps, userID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := db.QueryRowToStruct[db.User](ctx, d.DB,
		`SELECT "id","nickname","avatar","username","bio","backgroundUrl","updatedAt" FROM "User" WHERE "id"=$1`, userID)
	if err != nil {
		if db.IsNotFound(err) {
			return nil
		}
		return err
	}
	return PublishUserProfileUpdated(d, UserProfileSyncSource{
		ID: u.Id, Nickname: u.Nickname, Avatar: u.Avatar, Username: u.Username,
		Bio: u.Bio, BackgroundURL: u.BackgroundUrl, UpdatedAt: u.UpdatedAt,
	})
}
