// qr.go — 二维码校验 API，移植自 server/qr.ts。
//
// POST /api/qr/verify（需要登录）
// 校验扫描到的 QR Payload 合法性：版本号 v===1、uid 对应用户存在、未过期。
// 返回目标用户基本信息（前端展示确认弹窗）。二维码缺失 ik（基础二维码
// imim://user/{id}）时仍允许添加好友（basic=true）。
package misc

import (
	"log"
	"net/http"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// qrVerifyPayload 对应 TS 的 { v, uid, name, phone, ik, regId, fp, ts, exp, sig? }，
// 仅取校验用到的字段。
type qrVerifyPayload struct {
	V   *int   `json:"v"`
	UID string `json:"uid"`
	Ik  string `json:"ik"`
	Exp *int64 `json:"exp"`
}

func (h *Handler) qrVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := middleware.UserFrom(r)

	var payload qrVerifyPayload
	if !util.DecodeJSON(w, r, &payload) {
		return
	}

	// 基础字段校验
	if payload.V == nil || *payload.V != 1 {
		util.WriteError(w, 400, "无效的二维码格式")
		return
	}
	if payload.UID == "" {
		util.WriteError(w, 400, "二维码缺少必要字段")
		return
	}
	// 基础二维码（imim://user/{id}）无 E2EE 公钥，仍允许添加好友
	isBasicQr := payload.Ik == "" || payload.Ik == "basic"

	// 过期校验
	if payload.Exp != nil && time.Now().UnixMilli() > *payload.Exp {
		util.WriteJSON(w, 400, map[string]any{"error": "二维码已过期，请让对方刷新后重试", "expired": true})
		return
	}

	// 用户存在性校验
	user, err := db.QueryRowToStruct[db.User](ctx, h.deps.DB,
		`SELECT "id","username","nickname","avatar","phone" FROM "User" WHERE "id"=$1`, payload.UID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "二维码对应的用户不存在")
			return
		}
		log.Printf("[QR] 校验失败: %v", err)
		util.WriteError(w, 500, "校验失败，请重试")
		return
	}

	// 不能扫自己的二维码
	if user.Id == currentUser.Id {
		util.WriteError(w, 400, "不能扫描自己的二维码")
		return
	}

	name := util.StrVal(user.Nickname)
	if name == "" {
		name = user.Username
	}
	util.WriteJSON(w, 200, map[string]any{
		"valid": true,
		"basic": isBasicQr,
		"user": map[string]any{
			"id":     user.Id,
			"name":   name,
			"phone":  util.StrVal(user.Phone),
			"avatar": avatarToProxy(util.StrVal(user.Avatar)),
		},
	})
}
