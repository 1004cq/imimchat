package web

import (
	"bytes"
	"compress/zlib"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ TRTC UserSig（对应 index.ts GET /api/trtc/usersig） ============
// 算法与腾讯云官方 tls-sig-api-v2 完全一致：
// HMAC-SHA256 签名 → 构造 sigDoc → zlib 压缩 → base64url（腾讯自定义映射）编码。

// GET /api/trtc/usersig — 为当前登录用户生成 TRTC UserSig（optionalAuth，无用户时 401）。
func (h *Handler) trtcUserSig(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFrom(r)
	if u == nil {
		util.WriteError(w, 401, "未登录")
		return
	}

	appIDStr := strings.TrimSpace(os.Getenv("TRTC_SDK_APP_ID"))
	appID, err := strconv.Atoi(appIDStr)
	if err != nil || appID != 1600159677 {
		util.WriteError(w, 500, "TRTC SDK AppID 未配置或不匹配")
		return
	}
	secretKey := os.Getenv("TRTC_SECRET_KEY")
	if secretKey == "" {
		util.WriteError(w, 500, "TRTC 未配置")
		return
	}
	const expire = 86400 // 24 小时

	userID := u.Id
	currTime := time.Now().Unix()

	// Step 1: HMAC-SHA256 签名
	contentToBeSigned := "TLS.identifier:" + userID + "\n" +
		"TLS.sdkappid:" + strconv.Itoa(appID) + "\n" +
		"TLS.time:" + strconv.FormatInt(currTime, 10) + "\n" +
		"TLS.expire:" + strconv.Itoa(expire) + "\n"
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(contentToBeSigned))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Step 2: 构建 sigDoc
	sigDoc := map[string]any{
		"TLS.ver":        "2.0",
		"TLS.identifier": userID,
		"TLS.sdkappid":   appID,
		"TLS.time":       currTime,
		"TLS.expire":     expire,
		"TLS.sig":        sig,
	}
	docBytes, err := json.Marshal(sigDoc)
	if err != nil {
		log.Printf("[TRTC] UserSig 生成失败: %v", err)
		util.WriteError(w, 500, "UserSig 生成失败")
		return
	}

	// Step 3: zlib 压缩 + base64url 编码（+ -> *，/ -> -，= -> _）
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(docBytes); err != nil {
		log.Printf("[TRTC] UserSig 生成失败: %v", err)
		util.WriteError(w, 500, "UserSig 生成失败")
		return
	}
	if err := zw.Close(); err != nil {
		log.Printf("[TRTC] UserSig 生成失败: %v", err)
		util.WriteError(w, 500, "UserSig 生成失败")
		return
	}
	compressed := base64.StdEncoding.EncodeToString(buf.Bytes())
	userSig := strings.NewReplacer("+", "*", "/", "-", "=", "_").Replace(compressed)

	util.WriteJSON(w, 200, map[string]any{
		"sdkAppId":   appID,
		"userId":     userID,
		"userSig":    userSig,
		"expireTime": expire,
	})
}
