// Package onebot — OneBot v11 适配层（server/index.ts OneBot 相关部分移植）。
//
// 覆盖：
//   - /onebot/v11/ws WebSocketServer（AstrBot 接入）与 handleOneBotAction
//   - pushOneBotEvent / pushGroupMessage / pushPrivateMessage / broadcastBotMessageToGroup/User
//   - BOT_TTS 降级：Go 版无语音合成/转存能力，仅推送文本并打日志（不引入新依赖）
//   - startReverseWsClient 反向 WS 客户端（gorilla/websocket 重连循环）
//   - groupRegistry / userRegistry / userProfiles
//
// OneBot v11 事件 JSON 格式照抄 TS（message_type、post_type 等字段），行为与 TS 一致。
package onebot

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// BotSelfID imim BOT 的自定义 self_id（类似 QQ 号），与 TS BOT_SELF_ID=10000 一致。
const BotSelfID = 10000

// imPushChannel 与 signal 模块一致的跨节点投递频道（cqim:im:push）。
// onebot 通过发布 {"userId":..., "payload":...} 让各节点的 signal 服务投递给本机连接。
const imPushChannel = "cqim:im:push"

// groupInfo 已知群组信息（群ID -> 群名称/成员），对应 TS groupRegistry。
type groupInfo struct {
	name      string
	memberIDs []string
}

var (
	groupsMu      sync.RWMutex
	groupRegistry = map[int64]groupInfo{}

	usersMu      sync.RWMutex
	userRegistry = map[string]string{} // userId -> 昵称

	profilesMu   sync.RWMutex
	userProfiles = map[string]map[string]any{}
)

func init() { initRegistry() }

// initRegistry 初始化预设群组/用户数据（对应 TS initGroupRegistry）。
func initRegistry() {
	groupsMu.Lock()
	groupRegistry[3] = groupInfo{name: "灵鸽开发组", memberIDs: []string{"me", "u2", "u3", "u4", "u5"}}
	groupRegistry[5] = groupInfo{name: "老同学群", memberIDs: []string{"me", "u1", "u3", "u6"}}
	groupsMu.Unlock()

	usersMu.Lock()
	userRegistry["me"] = "清风"
	userRegistry["u1"] = "林小溪"
	userRegistry["u2"] = "陈墨白"
	userRegistry["u3"] = "苏清和"
	userRegistry["u4"] = "王竹韵"
	userRegistry["u5"] = "周清漪"
	userRegistry["u6"] = "李晨曦"
	userRegistry["BOT"] = "imim AI"
	userRegistry["official"] = "imim 官方"
	usersMu.Unlock()
}

// nicknameOf 查昵称，未知则回退 userId。
func nicknameOf(userID string) string {
	usersMu.RLock()
	defer usersMu.RUnlock()
	if n, ok := userRegistry[userID]; ok {
		return n
	}
	return userID
}

// groupOf 查群信息。
func groupOf(groupID int64) (groupInfo, bool) {
	groupsMu.RLock()
	defer groupsMu.RUnlock()
	g, ok := groupRegistry[groupID]
	return g, ok
}

// Segment OneBot v11 消息段。
type Segment struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// contentToSegments 将 imim 消息内容转换为 OneBot v11 消息段数组（照抄 TS）。
func contentToSegments(content string) []Segment {
	if content == "" {
		return []Segment{{Type: "text", Data: map[string]string{"text": ""}}}
	}
	// 简单处理：全部作为文本（后续可扩展图片、@ 解析）
	return []Segment{{Type: "text", Data: map[string]string{"text": content}}}
}

var cqCodeRe = regexp.MustCompile(`\[CQ:[^\]]+\]`)

// segmentsToText 将 OneBot 消息段数组转换为纯文本（照抄 TS）。
// rawMessage 可选：数组解析无文字但有 CQ 码字符串时辅助提取。
func segmentsToText(raw json.RawMessage, rawMessage string) string {
	// 字符串形式直接返回
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var segments []Segment
	if err := json.Unmarshal(raw, &segments); err != nil || segments == nil {
		return ""
	}
	var sb strings.Builder
	hasOther := false
	for _, seg := range segments {
		switch seg.Type {
		case "text":
			sb.WriteString(seg.Data["text"])
		case "at":
			qq := seg.Data["qq"]
			if qq == "" {
				qq = seg.Data["name"]
			}
			sb.WriteString("@" + qq)
		default:
			hasOther = true
		}
	}
	text := sb.String()
	// 增强：如果解析数组没拿到文字，但有 rawMessage（通常是 CQ 码字符串），尝试提取
	if strings.TrimSpace(text) == "" && rawMessage != "" {
		text = strings.TrimSpace(cqCodeRe.ReplaceAllString(rawMessage, ""))
	}
	// 纯图片等占位
	if strings.TrimSpace(text) == "" && hasOther {
		for _, s := range segments {
			if s.Type == "image" {
				return "[图片]"
			}
		}
		return ""
	}
	return strings.TrimSpace(text)
}

// recordSegment record 消息段信息。
type recordSegment struct {
	file string
	url  string
}

var recordCQRe = regexp.MustCompile(`\[CQ:record,([^\]]+)\]`)

// extractRecordSegment 从消息中提取 record 消息段（照抄 TS，支持 CQ 码与数组两种格式）。
func extractRecordSegment(raw json.RawMessage) (recordSegment, bool) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		m := recordCQRe.FindStringSubmatch(asString)
		if m == nil {
			return recordSegment{}, false
		}
		rs := recordSegment{}
		for _, pair := range strings.Split(m[1], ",") {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "file":
				rs.file = kv[1]
			case "url":
				rs.url = kv[1]
			}
		}
		return rs, true
	}
	var segments []Segment
	if err := json.Unmarshal(raw, &segments); err != nil {
		return recordSegment{}, false
	}
	for _, s := range segments {
		if s.Type == "record" {
			return recordSegment{file: s.Data["file"], url: s.Data["url"]}, true
		}
	}
	return recordSegment{}, false
}

// ============ AstrBot 客户端连接管理 ============

var (
	clientsMu sync.RWMutex
	clients   = map[*websocket.Conn]struct{}{}

	// wsWriteMu 串行化所有 onebot 连接的写操作（gorilla 要求单连接并发写互斥）。
	wsWriteMu sync.Mutex
)

// wsWrite 线程安全地向 onebot 连接写一帧。
func wsWrite(c *websocket.Conn, payload []byte) error {
	wsWriteMu.Lock()
	defer wsWriteMu.Unlock()
	return c.WriteMessage(websocket.TextMessage, payload)
}

func addClient(c *websocket.Conn) {
	clientsMu.Lock()
	clients[c] = struct{}{}
	n := len(clients)
	clientsMu.Unlock()
	log.Printf("[OneBot] AstrBot 已连接 (当前客户端数: %d)", n)
}

func removeClient(c *websocket.Conn) {
	clientsMu.Lock()
	delete(clients, c)
	n := len(clients)
	clientsMu.Unlock()
	log.Printf("[OneBot] AstrBot 断开 (剩余: %d)", n)
}

func clientCount() int {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	return len(clients)
}

// pushOneBotEvent 向所有已连接的 AstrBot 客户端推送 OneBot 事件（照抄 TS）。
func pushOneBotEvent(event map[string]any) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[OneBot] 序列化事件失败: %v", err)
		return
	}
	pushed := 0
	clientsMu.RLock()
	for c := range clients {
		if err := wsWrite(c, payload); err == nil {
			pushed++
		}
	}
	clientsMu.RUnlock()
	if pushed > 0 {
		log.Printf("[OneBot] 推送事件 post_type=%v 给 %d 个客户端", event["post_type"], pushed)
	}
}

func pushGroupEvent(groupID int64, userID, content, messageID, nickname string) {
	if nickname == "" {
		nickname = nicknameOf(userID)
	}
	pushOneBotEvent(map[string]any{
		"time":         time.Now().Unix(),
		"self_id":      BotSelfID,
		"post_type":    "message",
		"message_type": "group",
		"sub_type":     "normal",
		"message_id":   messageID,
		"group_id":     groupID,
		"user_id":      userID,
		"anonymous":    nil,
		"message":      contentToSegments(content),
		"raw_message":  content,
		"font":         0,
		"sender": map[string]any{
			"user_id":  userID,
			"nickname": nickname,
			"card":     "",
			"sex":      "unknown",
			"age":      0,
			"area":     "",
			"level":    "1",
			"role":     "member",
			"title":    "",
		},
	})
}

// pushPrivateEvent 推送私聊消息事件给 AstrBot（照抄 TS pushPrivateMessage）。
func pushPrivateEvent(userID, content, messageID, nickname string) {
	if nickname == "" {
		nickname = nicknameOf(userID)
	}
	pushOneBotEvent(map[string]any{
		"time":         time.Now().Unix(),
		"self_id":      BotSelfID,
		"post_type":    "message",
		"message_type": "private",
		"sub_type":     "friend",
		"message_id":   messageID,
		"user_id":      userID,
		"message":      contentToSegments(content),
		"raw_message":  content,
		"font":         0,
		"sender": map[string]any{
			"user_id":  userID,
			"nickname": nickname,
			"sex":      "unknown",
			"age":      0,
		},
	})
}

// PushGroupMessage 导出供其他模块调用：把一条群消息事件推送给 AstrBot。
// sender 取 BOT（imim AI），messageId 自动生成（im-<毫秒>），nickname 取注册表。
func PushGroupMessage(d *handler.Deps, groupID int64, text string) {
	_ = d
	pushGroupEvent(groupID, "BOT", text, "im-"+millisStr(), "")
}

// PushPrivateMessage 导出供其他模块调用：把一条私聊消息事件推送给 AstrBot。
// messageId 自动生成（im-<毫秒>），nickname 取注册表。
func PushPrivateMessage(d *handler.Deps, userID string, text string) {
	_ = d
	pushPrivateEvent(userID, text, "im-"+millisStr(), "")
}

func millisStr() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

// parseInt 解析十进制整数（忽略前后空白，失败返回 0）。
func parseInt(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

// ============ 向 IM 客户端投递 BOT 消息 ============

// sendToUser 把消息投递给指定 IM 用户：发布到 cqim:im:push，
// 各节点 signal 服务订阅后会投递给本机持有的连接（对应 TS sendTo 本机 miss 时走 Redis 发布）。
func sendToUser(d *handler.Deps, userID string, msg map[string]any) {
	if d == nil || d.Redis == nil {
		log.Printf("[OneBot] Redis 不可用，BOT 消息丢弃 userId=%s", userID)
		return
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[OneBot] 序列化 BOT 消息失败: %v", err)
		return
	}
	env, _ := json.Marshal(map[string]any{"userId": userID, "payload": json.RawMessage(raw)})
	if err := d.Redis.Publish(context.Background(), imPushChannel, string(env)); err != nil {
		log.Printf("[OneBot] PUBLISH BOT 消息失败 userId=%s: %v", userID, err)
	}
}
