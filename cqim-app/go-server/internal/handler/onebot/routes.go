package onebot

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var heartbeatOnce sync.Once

// RegisterRoutes 注册 OneBot 路由（入口约定）。
//   - GET /onebot/v11/ws：AstrBot 反向 WS 接入
//   - OneBot v11 HTTP API 备用接口（兼容 HTTP POST/GET 方式，照抄 TS）
//   - POST /api/push-to-onebot、GET /api/onebot-status
func RegisterRoutes(mux *http.ServeMux, d *handler.Deps) {
	mux.Handle("GET /onebot/v11/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWS(d, w, r)
	}))

	// ============ OneBot v11 HTTP API 备用接口（兼容 HTTP POST 方式） ============
	mux.Handle("POST /send_group_msg", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := httpActionParams(w, r)
		if p == nil {
			return
		}
		log.Printf("[OneBot] 收到群聊原始消息: %s", string(truncateJSON(mustMarshal(p["message"]), 500)))
		groupID := pNum(p, "group_id")
		record, hasRecord := extractRecordSegment(pRaw(p, "message"))
		text := segmentsToText(pRaw(p, "message"), "")
		if hasRecord {
			log.Printf("[OneBot] HTTP 群语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
			broadcastBotMessageToGroup(d, groupID, text, nil)
		} else {
			synthesizeAndBroadcastBotVoiceToGroup(d, groupID, text)
		}
		writeActionOK(w)
	}))
	mux.Handle("POST /send_private_msg", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := httpActionParams(w, r)
		if p == nil {
			return
		}
		log.Printf("[OneBot] 收到私聊原始消息: %s", string(truncateJSON(mustMarshal(p["message"]), 500)))
		userID := pStr(p, "user_id")
		record, hasRecord := extractRecordSegment(pRaw(p, "message"))
		text := segmentsToText(pRaw(p, "message"), "")
		if hasRecord {
			log.Printf("[OneBot] HTTP 私聊语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
			broadcastBotMessageToUser(d, userID, text, nil)
		} else {
			synthesizeAndBroadcastBotVoiceToUser(d, userID, text)
		}
		writeActionOK(w)
	}))
	mux.Handle("POST /send_msg", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := httpActionParams(w, r)
		if p == nil {
			return
		}
		record, hasRecord := extractRecordSegment(pRaw(p, "message"))
		text := segmentsToText(pRaw(p, "message"), "")
		log.Printf("[OneBot] HTTP send_msg 解析结果: text=%q, hasVoice=%v", text, hasRecord)
		if hasRecord {
			log.Printf("[OneBot] HTTP send_msg 语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
			if pStr(p, "message_type") == "group" {
				broadcastBotMessageToGroup(d, pNum(p, "group_id"), text, nil)
			} else {
				broadcastBotMessageToUser(d, pStr(p, "user_id"), text, nil)
			}
		} else {
			if pStr(p, "message_type") == "group" {
				synthesizeAndBroadcastBotVoiceToGroup(d, pNum(p, "group_id"), text)
			} else {
				synthesizeAndBroadcastBotVoiceToUser(d, pStr(p, "user_id"), text)
			}
		}
		writeActionOK(w)
	}))
	mux.Handle("POST /delete_msg", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeActionOK(w)
	}))
	mux.Handle("GET /get_login_info", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		util.WriteJSON(w, 200, map[string]any{
			"status": "ok", "retcode": 0,
			"data": map[string]any{"user_id": BotSelfID, "nickname": "imim AI"},
		})
	}))
	mux.Handle("GET /get_group_list", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupsMu.RLock()
		list := make([]map[string]any, 0, len(groupRegistry))
		for id, g := range groupRegistry {
			list = append(list, map[string]any{
				"group_id": id, "group_name": g.name, "member_count": len(g.memberIDs),
			})
		}
		groupsMu.RUnlock()
		util.WriteJSON(w, 200, map[string]any{"status": "ok", "retcode": 0, "data": list})
	}))
	mux.Handle("GET /get_group_info", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupID, _ := parseInt(r.URL.Query().Get("group_id"))
		if g, ok := groupOf(groupID); ok {
			util.WriteJSON(w, 200, map[string]any{"status": "ok", "retcode": 0, "data": map[string]any{
				"group_id": groupID, "group_name": g.name, "member_count": len(g.memberIDs),
			}})
		} else {
			util.WriteJSON(w, 200, map[string]any{"status": "failed", "retcode": 100, "data": nil})
		}
	}))
	mux.Handle("GET /get_group_member_list", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupID, _ := parseInt(r.URL.Query().Get("group_id"))
		members := []map[string]any{}
		if g, ok := groupOf(groupID); ok {
			for _, uid := range g.memberIDs {
				role := "member"
				if uid == "me" {
					role = "owner"
				}
				members = append(members, map[string]any{
					"group_id": groupID, "user_id": uid,
					"nickname": nicknameOf(uid), "role": role,
				})
			}
		}
		util.WriteJSON(w, 200, map[string]any{"status": "ok", "retcode": 0, "data": members})
	}))
	mux.Handle("GET /get_status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		util.WriteJSON(w, 200, map[string]any{
			"status": "ok", "retcode": 0, "data": map[string]any{"online": true, "good": true},
		})
	}))
	mux.Handle("GET /get_version_info", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		util.WriteJSON(w, 200, map[string]any{"status": "ok", "retcode": 0, "data": map[string]any{
			"app_name": "imim", "app_version": "2.0.0", "protocol_version": "v11",
		}})
	}))

	// ============ 前端推送消息给 AstrBot ============
	mux.Handle("POST /api/push-to-onebot", d.Auth.UserAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushToOneBot(d, w, r)
	})))

	// ============ OneBot 连接状态 ============
	mux.Handle("GET /api/onebot-status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupsMu.RLock()
		groups := make([]map[string]any, 0, len(groupRegistry))
		for id, g := range groupRegistry {
			groups = append(groups, map[string]any{"id": id, "name": g.name})
		}
		groupsMu.RUnlock()
		util.WriteJSON(w, 200, map[string]any{
			"connected": clientCount() > 0,
			"clients":   clientCount(),
			"groups":    groups,
		})
	}))

	// 心跳包：每 30 秒向所有 AstrBot 客户端发送心跳（照抄 TS）
	heartbeatOnce.Do(func() {
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for range t.C {
				if clientCount() == 0 {
					continue
				}
				pushOneBotEvent(map[string]any{
					"time":            time.Now().Unix(),
					"self_id":         BotSelfID,
					"post_type":       "meta_event",
					"meta_event_type": "heartbeat",
					"status":          map[string]any{"online": true, "good": true},
					"interval":        30000,
				})
			}
		}()
	})
}

// handleWS 处理 /onebot/v11/ws 的 AstrBot 接入（照抄 TS onebotWss connection 逻辑）。
func handleWS(d *handler.Deps, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// 鉴权检查（可选，通过 Authorization 头或 access_token 查询参数）
	token := r.URL.Query().Get("access_token")
	if token == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if configuredToken := os.Getenv("ONEBOT_ACCESS_TOKEN"); configuredToken != "" && token != configuredToken {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(1008, "Unauthorized"),
			time.Now().Add(5*time.Second))
		_ = conn.Close()
		log.Printf("[OneBot] 拒绝未授权连接")
		return
	}

	addClient(conn)
	// 发送生命周期事件
	_ = wsWrite(conn, mustMarshal(map[string]any{
		"time":            time.Now().Unix(),
		"self_id":         BotSelfID,
		"post_type":       "meta_event",
		"meta_event_type": "lifecycle",
		"sub_type":        "connect",
	}))

	defer func() {
		removeClient(conn)
		_ = conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var a action
		if err := json.Unmarshal(data, &a); err != nil {
			log.Printf("[OneBot] 无法解析动作: %v", err)
			continue
		}
		handleOneBotAction(d, conn, a)
	}
}

// httpActionParams 解析 HTTP 备用接口的 body：body.params || body（照抄 TS）。
func httpActionParams(w http.ResponseWriter, r *http.Request) map[string]any {
	var body map[string]any
	if !util.DecodeJSON(w, r, &body) {
		return nil
	}
	if p, ok := body["params"].(map[string]any); ok {
		return p
	}
	return body
}

// writeActionOK OneBot HTTP 备用接口的标准成功响应。
func writeActionOK(w http.ResponseWriter) {
	util.WriteJSON(w, 200, map[string]any{
		"status": "ok", "retcode": 0,
		"data": map[string]any{"message_id": "ob-" + millisStr()},
	})
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// pushToOneBot POST /api/push-to-onebot：前端发送消息时主动推送给 AstrBot（照抄 TS）。
// ★ 需要登录，且 senderId 强制取登录身份：防止伪造任意用户身份给 Bot 发消息。
func pushToOneBot(d *handler.Deps, w http.ResponseWriter, r *http.Request) {
	_ = d
	if clientCount() == 0 {
		util.WriteJSON(w, 200, map[string]any{"ok": false, "reason": "no_astrbot_connected"})
		return
	}

	// ★ 发送者身份以登录态为准，不再信任 body 里的 senderId
	user := middleware.UserFrom(r)
	senderID := ""
	if user != nil {
		senderID = user.Id
	}

	var body struct {
		ChatID   string `json:"chatId"`
		Content  string `json:"content"`
		Nickname string `json:"nickname"`
		IsGroup  bool   `json:"isGroup"`
		GroupID  any    `json:"groupId"`
		VoiceURL string `json:"voiceUrl"`
		IsVoice  bool   `json:"isVoice"`
	}
	if !util.DecodeJSON(w, r, &body) {
		return
	}
	msgID := "im-" + millisStr()

	if body.IsVoice && body.VoiceURL != "" {
		// 语音消息：构造包含 record 消息段的 OneBot 事件
		fullVoiceURL := body.VoiceURL
		if !strings.HasPrefix(fullVoiceURL, "http") {
			port := "3000"
			if p := os.Getenv("PORT"); p != "" {
				port = p
			}
			fullVoiceURL = "http://localhost:" + port + fullVoiceURL
		}
		voiceSegments := []Segment{{Type: "record", Data: map[string]string{"file": fullVoiceURL, "url": fullVoiceURL}}}
		nickname := body.Nickname
		if nickname == "" {
			nickname = nicknameOf(senderID)
		}
		if body.IsGroup && body.GroupID != nil {
			var numGroupID int64
			switch t := body.GroupID.(type) {
			case float64:
				numGroupID = int64(t)
			case string:
				numGroupID, _ = parseInt(t)
				if numGroupID == 0 {
					chatNum, _ := parseInt(strings.TrimPrefix(body.ChatID, "c"))
					numGroupID = chatNum
				}
			}
			pushOneBotEvent(map[string]any{
				"time":         time.Now().Unix(),
				"self_id":      BotSelfID,
				"post_type":    "message",
				"message_type": "group",
				"sub_type":     "normal",
				"message_id":   msgID,
				"group_id":     numGroupID,
				"user_id":      senderID,
				"anonymous":    nil,
				"message":      voiceSegments,
				"raw_message":  "[CQ:record,file=" + fullVoiceURL + "]",
				"font":         0,
				"sender": map[string]any{
					"user_id": senderID, "nickname": nickname, "card": "",
					"sex": "unknown", "age": 0, "area": "", "level": "1",
					"role": "member", "title": "",
				},
			})
		} else {
			pushOneBotEvent(map[string]any{
				"time":         time.Now().Unix(),
				"self_id":      BotSelfID,
				"post_type":    "message",
				"message_type": "private",
				"sub_type":     "friend",
				"message_id":   msgID,
				"user_id":      senderID,
				"message":      voiceSegments,
				"raw_message":  "[CQ:record,file=" + fullVoiceURL + "]",
				"font":         0,
				"sender": map[string]any{
					"user_id": senderID, "nickname": nickname,
					"sex": "unknown", "age": 0,
				},
			})
		}
		log.Printf("[OneBot] 用户语音已推送给 AstrBot: %s", fullVoiceURL)
	} else {
		// 普通文本消息
		if body.IsGroup && body.GroupID != nil {
			var numGroupID int64
			switch t := body.GroupID.(type) {
			case float64:
				numGroupID = int64(t)
			case string:
				numGroupID, _ = parseInt(t)
				if numGroupID == 0 {
					chatNum, _ := parseInt(strings.TrimPrefix(body.ChatID, "c"))
					numGroupID = chatNum
				}
			}
			pushGroupEvent(numGroupID, senderID, body.Content, msgID, body.Nickname)
		} else {
			pushPrivateEvent(senderID, body.Content, msgID, body.Nickname)
		}
	}

	util.WriteJSON(w, 200, map[string]any{"ok": true, "messageId": msgID})
}
