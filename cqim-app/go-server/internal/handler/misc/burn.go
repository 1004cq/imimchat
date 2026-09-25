// burn.go — 群阅后即焚，移植自 server/burn-message.ts。
//
// POST /api/group/burn-message（需要登录）
// 客户端通知服务器销毁群消息。★ 鉴权要求（S5 修复，照抄 TS）：
//  1. 调用者必须登录（userAuth）
//  2. 调用者必须是该消息所在群的成员（非成员 403）
//  3. 该消息必须是阅后即焚消息（burnAfterRead 非空），防止滥用此接口擦除普通消息
//
// 另导出 CleanupExpiredBurnMessages / StartBurnCleanupCron（定期清理过期消息）。
package misc

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

type burnMessageBody struct {
	MessageID string `json:"messageId"`
	ChatID    string `json:"chatId"`
}

func (h *Handler) burnMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := middleware.UserFrom(r).Id

	var body burnMessageBody
	if !util.DecodeJSON(w, r, &body) {
		return
	}
	if body.MessageID == "" {
		util.WriteError(w, 400, "缺少 messageId")
		return
	}

	type msgRow struct {
		ID            string `db:"id"`
		GroupID       string `db:"groupId"`
		BurnAfterRead *int32 `db:"burnAfterRead"`
		IsRevoked     bool   `db:"isRevoked"`
	}
	msg, err := db.QueryRowToStruct[msgRow](ctx, h.deps.DB,
		`SELECT "id","groupId","burnAfterRead","isRevoked" FROM "GroupMessage" WHERE "id"=$1`, body.MessageID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "消息不存在")
			return
		}
		log.Printf("[Burn] 查询群消息失败: %v", err)
		util.WriteError(w, 500, "销毁失败")
		return
	}
	if msg.IsRevoked {
		util.WriteJSON(w, 200, map[string]any{"ok": true, "alreadyBurned": true})
		return
	}
	// 只有群成员可以销毁该群的阅后即焚消息
	var memberID string
	if err := h.deps.DB.Pool.QueryRow(ctx,
		`SELECT "id" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`, msg.GroupID, userID).Scan(&memberID); err != nil {
		log.Printf("[Burn] 非成员 %s 尝试销毁群 %s 的消息 %s，已拒绝", userID, msg.GroupID, body.MessageID)
		util.WriteError(w, 403, "非群成员，无权销毁该消息")
		return
	}
	// 只允许销毁标记为阅后即焚的消息
	if msg.BurnAfterRead == nil {
		util.WriteError(w, 403, "该消息不是阅后即焚消息")
		return
	}

	// 标记消息为已撤回（软删除）并清空内容
	if _, err := h.deps.DB.Exec(ctx,
		`UPDATE "GroupMessage" SET "isRevoked"=true,"content"='[消息已销毁]',"extra"=NULL WHERE "id"=$1`, body.MessageID); err != nil {
		log.Printf("[Burn] 销毁群消息失败: %v", err)
		util.WriteError(w, 500, "销毁失败")
		return
	}

	log.Printf("[Burn] 群消息 %s 已被 %s 销毁", body.MessageID, userID)
	util.WriteJSON(w, 200, map[string]any{"ok": true})
}

// CleanupExpiredBurnMessages 清理所有过期的阅后即焚消息（群 + 私聊），返回清理条数。
// 对应 burn-message.ts cleanupExpiredBurnMessages。
func CleanupExpiredBurnMessages(ctx context.Context, d *handler.Deps) int {
	total := 0
	groupN, err := d.DB.Exec(ctx,
		`UPDATE "GroupMessage" SET "isRevoked"=true,"content"='[消息已过期销毁]',"extra"=NULL
		 WHERE "burnExpireAt" IS NOT NULL AND "burnExpireAt" <= NOW() AND "isRevoked"=false`)
	if err != nil {
		log.Printf("[Burn] 清理过期群消息失败: %v", err)
	} else {
		total += int(groupN)
	}
	privateN, err := d.DB.Exec(ctx,
		`UPDATE "PrivateMessage" SET "isRevoked"=true,"content"='[消息已过期销毁]',"extra"=NULL
		 WHERE "burnExpireAt" IS NOT NULL AND "burnExpireAt" <= NOW() AND "isRevoked"=false`)
	if err != nil {
		log.Printf("[Burn] 清理过期私聊消息失败: %v", err)
	} else {
		total += int(privateN)
	}
	if total > 0 {
		log.Printf("[Burn] 定期清理: 销毁 %d 条过期消息 (群:%d 私:%d)", total, groupN, privateN)
	}
	return total
}

// StartBurnCleanupCron 启动定期清理任务（默认 60 秒，与 Node 端 index.ts 一致），返回停止函数。
// 对应 burn-message.ts startBurnCleanupCron。
func StartBurnCleanupCron(d *handler.Deps, interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = time.Minute
	}
	log.Printf("[Burn] 启动定期清理任务, 间隔 %v", interval)
	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				func() {
					defer func() { _ = recover() }()
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					CleanupExpiredBurnMessages(ctx, d)
				}()
			case <-stopCh:
				return
			}
		}
	}()
	return func() { close(stopCh) }
}
