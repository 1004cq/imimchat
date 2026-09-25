package onebot

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// action OneBot v11 动作请求（通过 WS 发来）。
type action struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
	Echo   json.RawMessage `json:"echo"`
}

// paramsMap 把 params 解析为 map（数字用 json.Number 保留精度）。
func paramsMap(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(raw) == 0 {
		return m
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return m
}

// pStr 取字符串参数（照抄 TS String(...) 语义：数字转十进制字符串）。
func pStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// pNum 取数字参数（照抄 TS Number(...) 语义）。
func pNum(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
		return 0
	case float64:
		return int64(t)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return int64(f)
		}
		return 0
	default:
		return 0
	}
}

// pRaw 取 message 原始 JSON（字符串或数组）。
func pRaw(m map[string]any, key string) json.RawMessage {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

// handleOneBotAction 处理 AstrBot 通过 WS 发送的动作请求（照抄 TS handleOneBotAction）。
func handleOneBotAction(d *handler.Deps, conn *websocket.Conn, a action) {
	params := paramsMap(a.Params)
	switch a.Action {
	case "send_msg", "send_private_msg", "send_group_msg":
		log.Printf("[OneBot] 收到消息动作: %s, 原始消息体: %s", a.Action, string(truncateJSON(a.Params, 500)))
	default:
		log.Printf("[OneBot] 动作: %s %s", a.Action, string(truncateJSON(a.Params, 100)))
	}

	respond := func(data any, retcode int) {
		status := "ok"
		if retcode != 0 {
			status = "failed"
		}
		echo := any(nil)
		if len(a.Echo) > 0 {
			echo = json.RawMessage(a.Echo)
		}
		resp, _ := json.Marshal(map[string]any{
			"status":  status,
			"retcode": retcode,
			"data":    data,
			"echo":    echo,
		})
		if err := wsWrite(conn, resp); err != nil {
			log.Printf("[OneBot] 响应动作失败: %v", err)
		}
	}

	switch a.Action {
	case "send_group_msg":
		{
			groupID := pNum(params, "group_id")
			msgID := "ob-" + millisStr()
			record, hasRecord := extractRecordSegment(pRaw(params, "message"))
			text := segmentsToText(pRaw(params, "message"), "")
			log.Printf("[OneBot] 解析文本结果: %q | 是否有语音: %v", text, hasRecord)
			if hasRecord {
				// Go 版降级：无语音转存/合成能力，仅转发文本并打日志
				log.Printf("[OneBot] 收到群语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
				broadcastBotMessageToGroup(d, groupID, text, nil)
			} else {
				broadcastBotMessageToGroup(d, groupID, text, nil)
				synthesizeAndBroadcastBotVoiceToGroup(d, groupID, text)
			}
			respond(map[string]any{"message_id": msgID}, 0)
		}

	case "send_private_msg":
		{
			userID := pStr(params, "user_id")
			msgID := "ob-" + millisStr()
			record, hasRecord := extractRecordSegment(pRaw(params, "message"))
			text := segmentsToText(pRaw(params, "message"), "")
			log.Printf("[OneBot] 解析文本结果: %q | 是否有语音: %v", text, hasRecord)
			if hasRecord {
				log.Printf("[OneBot] 收到私聊语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
				broadcastBotMessageToUser(d, userID, text, nil)
			} else {
				broadcastBotMessageToUser(d, userID, text, nil)
				synthesizeAndBroadcastBotVoiceToUser(d, userID, text)
			}
			respond(map[string]any{"message_id": msgID}, 0)
		}

	case "send_msg":
		{
			msgType := pStr(params, "message_type")
			msgID := "ob-" + millisStr()
			log.Printf("[OneBot] send_msg 完整 params: %s", string(truncateJSON(a.Params, 2000)))
			record, hasRecord := extractRecordSegment(pRaw(params, "message"))
			// 增强：传入 raw_message 辅助提取文字（照抄 TS）
			text := segmentsToText(pRaw(params, "message"), pStr(params, "raw_message"))
			log.Printf("[OneBot] send_msg 解析文本结果: %q | 是否有语音: %v", text, hasRecord)
			if hasRecord {
				log.Printf("[OneBot] send_msg 收到语音消息段（file=%.60s），Go 版暂不支持语音转存，仅推送文本", record.file)
				if msgType == "group" {
					broadcastBotMessageToGroup(d, pNum(params, "group_id"), text, nil)
				} else {
					broadcastBotMessageToUser(d, pStr(params, "user_id"), text, nil)
				}
			} else {
				// 关键修复（照抄 TS）：合并发送，不再立即 broadcast 纯文字，
				// 交给合成函数；Go 版降级为直接推送文本。
				if msgType == "group" {
					synthesizeAndBroadcastBotVoiceToGroup(d, pNum(params, "group_id"), text)
				} else {
					synthesizeAndBroadcastBotVoiceToUser(d, pStr(params, "user_id"), text)
				}
			}
			respond(map[string]any{"message_id": msgID}, 0)
		}

	case "delete_msg":
		// 模拟撤回（前端暂时无法实时撤回，返回成功）
		respond(nil, 0)

	case "get_login_info":
		respond(map[string]any{"user_id": BotSelfID, "nickname": "imim AI"}, 0)

	case "get_group_info":
		{
			groupID := pNum(params, "group_id")
			if g, ok := groupOf(groupID); ok {
				respond(map[string]any{
					"group_id":         groupID,
					"group_name":       g.name,
					"member_count":     len(g.memberIDs),
					"max_member_count": 200,
				}, 0)
			} else {
				respond(nil, 100)
			}
		}

	case "get_group_list":
		{
			groupsMu.RLock()
			list := make([]map[string]any, 0, len(groupRegistry))
			for id, g := range groupRegistry {
				list = append(list, map[string]any{
					"group_id":     id,
					"group_name":   g.name,
					"member_count": len(g.memberIDs),
				})
			}
			groupsMu.RUnlock()
			respond(list, 0)
		}

	case "get_group_member_list":
		{
			groupID := pNum(params, "group_id")
			now := time.Now().Unix()
			if g, ok := groupOf(groupID); ok {
				members := make([]map[string]any, 0, len(g.memberIDs))
				for _, uid := range g.memberIDs {
					role := "member"
					if uid == "me" {
						role = "owner"
					}
					members = append(members, map[string]any{
						"group_id":          groupID,
						"user_id":           uid,
						"nickname":          nicknameOf(uid),
						"card":              "",
						"sex":               "unknown",
						"age":               0,
						"area":              "",
						"join_time":         now - 86400*30,
						"last_sent_time":    now,
						"level":             "1",
						"role":              role,
						"unfriendly":        false,
						"title":             "",
						"title_expire_time": 0,
						"card_changeable":   true,
					})
				}
				respond(members, 0)
			} else {
				respond([]any{}, 100)
			}
		}

	case "get_group_member_info":
		{
			groupID := pNum(params, "group_id")
			userID := pStr(params, "user_id")
			role := "member"
			if userID == "me" {
				role = "owner"
			}
			respond(map[string]any{
				"group_id": groupID,
				"user_id":  userID,
				"nickname": nicknameOf(userID),
				"card":     "",
				"role":     role,
			}, 0)
		}

	case "get_stranger_info", "get_friend_info":
		{
			userID := pStr(params, "user_id")
			respond(map[string]any{
				"user_id":  userID,
				"nickname": nicknameOf(userID),
				"sex":      "unknown",
				"age":      0,
			}, 0)
		}

	case "get_friend_list":
		{
			usersMu.RLock()
			friends := []map[string]any{}
			for id, name := range userRegistry {
				if id == "BOT" || id == "official" {
					continue
				}
				friends = append(friends, map[string]any{
					"user_id":  id,
					"nickname": name,
					"remark":   "",
				})
			}
			usersMu.RUnlock()
			respond(friends, 0)
		}

	case "get_status":
		respond(map[string]any{"online": true, "good": true}, 0)

	case "get_version_info":
		respond(map[string]any{
			"app_name":         "imim",
			"app_version":      "2.0.0",
			"protocol_version": "v11",
		}, 0)

	default:
		log.Printf("[OneBot] 未知动作: %s", a.Action)
		respond(nil, 1404)
	}
}

func truncateJSON(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

// voiceData BOT 消息可选语音数据（TS broadcastBotMessageToGroup/User 的 voiceData 参数）。
type voiceData struct {
	voiceURL string
	duration int
}

// broadcastBotMessageToGroup 将 BOT 回复广播到群里所有在线用户（照抄 TS）。
// 通过 Redis 跨节点投递，各节点 signal 服务再推给本机连接。
func broadcastBotMessageToGroup(d *handler.Deps, groupID int64, text string, voice *voiceData) {
	chatID := "c" + strconv.FormatInt(groupID, 10) // c3, c5 等
	msg := map[string]any{
		"type":      "bot_message",
		"chatId":    chatID,
		"senderId":  "BOT",
		"content":   text,
		"messageId": "bot-" + millisStr(),
		"timestamp": time.Now().UnixMilli(),
	}
	if voice != nil {
		msg["msgType"] = "voice"
		msg["voiceUrl"] = voice.voiceURL
		msg["duration"] = voice.duration
	}
	if g, ok := groupOf(groupID); ok {
		for _, uid := range g.memberIDs {
			sendToUser(d, uid, msg)
		}
	}
	voiceTag := ""
	if voice != nil {
		voiceTag = " [语音]"
	}
	log.Printf("[OneBot] BOT 群消息已推送到 chatId=%s%s", chatID, voiceTag)
}

// broadcastBotMessageToUser 将 BOT 回复推送给指定用户（照抄 TS）。
func broadcastBotMessageToUser(d *handler.Deps, userID string, text string, voice *voiceData) {
	msg := map[string]any{
		"type":      "bot_message",
		"chatId":    "cBOT",
		"senderId":  "BOT",
		"content":   text,
		"messageId": "bot-" + millisStr(),
		"timestamp": time.Now().UnixMilli(),
	}
	if voice != nil {
		msg["msgType"] = "voice"
		msg["voiceUrl"] = voice.voiceURL
		msg["duration"] = voice.duration
	}
	sendToUser(d, userID, msg)
	voiceTag := ""
	if voice != nil {
		voiceTag = " [语音]"
	}
	log.Printf("[OneBot] BOT 私聊已推送到 userId=%s%s", userID, voiceTag)
}

// ============ BOT_TTS 降级 ============
//
// TS 版用 OpenAI / Edge TTS 把 BOT 回复合成为语音再推送。
// Go 版无对应语音合成能力：此处只推送文本并打日志注明降级（不引入新依赖）。
var botTTSEnabled = os.Getenv("BOT_TTS_ENABLED") != "0"

func synthesizeAndBroadcastBotVoiceToGroup(d *handler.Deps, groupID int64, text string) {
	if botTTSEnabled {
		log.Printf("[OneBot] BOT TTS 未在 Go 版实现（BOT_TTS_ENABLED 生效时仍降级），直接推送文本 chatId=c%d", groupID)
	}
	broadcastBotMessageToGroup(d, groupID, text, nil)
}

func synthesizeAndBroadcastBotVoiceToUser(d *handler.Deps, userID string, text string) {
	if botTTSEnabled {
		log.Printf("[OneBot] BOT TTS 未在 Go 版实现（BOT_TTS_ENABLED 生效时仍降级），直接推送文本 userId=%s", userID)
	}
	broadcastBotMessageToUser(d, userID, text, nil)
}
