package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/group"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler/push"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// 消息分发
// ============================================================

func (s *Server) handleMessage(c *Client, raw []byte) {
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	typ, _ := msg["type"].(string)
	if typ == "" {
		return
	}
	to, _ := msg["to"].(string)
	roomID, _ := msg["roomId"].(string)
	log.Printf("[Signal] %s from=%s to=%s", typ, c.userID, toOr(roomID, to, "*"))

	payload, _ := msg["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	ctx := context.Background()

	switch typ {
	// ---------- 房间 ----------
	case "join":
		{
			rid := roomID
			if rid == "" {
				rid = "room-" + toOr(to, c.userID, "")
			}
			s.roomMu.Lock()
			room, ok := s.rooms[rid]
			if !ok {
				room = make(map[string]bool)
				s.rooms[rid] = room
			}
			existing := roomMembers(room)
			room[c.userID] = true
			s.roomMu.Unlock()
			c.roomID = rid
			s.broadcastToRoom(rid, mustJSON(map[string]any{
				"type": "room_info", "from": c.userID, "roomId": rid,
				"payload": map[string]any{"event": "peer_joined", "peerId": c.userID, "members": append(existing, c.userID)},
			}), c.userID)
			s.sendTo(c.userID, map[string]any{
				"type": "room_info", "roomId": rid,
				"payload": map[string]any{"event": "joined", "members": existing},
			})
		}
	case "leave":
		{
			if c.roomID != "" {
				rid := c.roomID
				s.roomMu.Lock()
				if room, ok := s.rooms[rid]; ok {
					delete(room, c.userID)
					if len(room) == 0 {
						delete(s.rooms, rid)
					} else {
						members := roomMembers(room)
						s.roomMu.Unlock()
						s.broadcastToRoom(rid, mustJSON(map[string]any{
							"type": "room_info", "from": c.userID, "roomId": rid,
							"payload": map[string]any{"event": "peer_left", "peerId": c.userID, "members": members},
						}), "")
						s.roomMu.Lock()
					}
				}
				s.roomMu.Unlock()
				c.roomID = ""
			}
		}
	case "offer", "answer", "ice":
		{
			if to != "" {
				msg["from"] = c.userID
				s.sendTo(to, msg)
			}
		}

	// ---------- 通话 ----------
	case "call_invite":
		{
			if to != "" {
				msg["from"] = c.userID
				s.sendTo(to, msg)
				// 被叫离线则推送来电通知
				if !s.isUserOnline(to) {
					var nickname, username, avatar string
					_ = s.deps.DB.Pool.QueryRow(ctx,
						`SELECT COALESCE("nickname",'') , "username", COALESCE("avatar",'') FROM "User" WHERE "id"=$1`,
						c.userID).Scan(&nickname, &username, &avatar)
					callerName := strVal(payload["callerName"], firstNonEmpty(nickname, username, "有人"))
					callType, _ := payload["callType"].(string)
					if callType == "" {
						callType = "audio"
					}
					callID, _ := payload["callId"].(string)
					roomID, _ := payload["roomId"].(string)
					// VoIP Push 唤醒后台/杀进程的 App，调起 CallKit（不用普通 APNs）
					_, _ = push.SendVoIPPush(s.deps, push.VoIPPushPayload{
						ToUserID:     to,
						CallerName:   callerName,
						CallID:       callID,
						CallerID:     c.userID,
						CallerAvatar: avatar,
						CallType:     callType,
						RoomID:       roomID,
					})
				}
			}
		}
	case "call_accept", "call_reject", "call_end":
		{
			if to != "" {
				msg["from"] = c.userID
				s.sendTo(to, msg)
			}
		}

	// ---------- 群信令（S2：成员校验） ----------
	case "group_join":
		{
			groupID, _ := payload["groupId"].(string)
			if groupID != "" {
				if !s.isGroupMember(ctx, groupID, c.userID) {
					log.Printf("[GroupSignal] 非成员 %s 尝试订阅群 %s，已拒绝", c.userID, groupID)
					s.sendTo(c.userID, map[string]any{
						"type": "group_message", "groupId": groupID,
						"payload": map[string]any{"error": "非群成员，无权订阅该群消息"},
					})
					break
				}
				s.group.JoinGroupOnline(groupID, c.userID)
				log.Printf("[GroupSignal] %s 加入群在线: %s", c.userID, groupID)
			}
		}
	case "group_leave":
		{
			if groupID, _ := payload["groupId"].(string); groupID != "" {
				s.group.LeaveGroupOnline(groupID, c.userID)
			}
		}
	case "group_send":
		{
			groupID, _ := payload["groupId"].(string)
			content, _ := payload["content"].(string)
			if groupID == "" || content == "" {
				break
			}
			// ★ S2：非群成员禁止发送
			if !s.isGroupMember(ctx, groupID, c.userID) {
				log.Printf("[GroupSignal] 非成员 %s 尝试向群 %s 发送消息，已拒绝", c.userID, groupID)
				s.sendTo(c.userID, map[string]any{
					"type": "group_message", "groupId": groupID,
					"payload": map[string]any{"ack": true, "groupId": groupID, "seq": -1,
						"timestamp": time.Now().UnixMilli(), "localId": strVal(payload["localId"], ""),
						"error": "非群成员，无权发送消息"},
				})
				break
			}
			// 强制 MLS：业务消息必须是 mls_encrypted
			msgType, _ := payload["msgType"].(string)
			if msgType == "" {
				msgType = "mls_encrypted"
			}
			if msgType != "mls_encrypted" && msgType != "system" {
				s.sendTo(c.userID, map[string]any{
					"type": "group_message", "groupId": groupID,
					"payload": map[string]any{"ack": true, "groupId": groupID, "seq": -1,
						"timestamp": time.Now().UnixMilli(), "localId": strVal(payload["localId"], ""),
						"error": "群聊强制要求 MLS 端到端加密，禁止发送明文业务消息"},
				})
				break
			}
			var senderName, senderAvatar string
			_ = s.deps.DB.Pool.QueryRow(ctx,
				`SELECT COALESCE("nickname","username"), COALESCE("avatar",'') FROM "User" WHERE "id"=$1`,
				c.userID).Scan(&senderName, &senderAvatar)
			if n, _ := payload["senderName"].(string); n != "" {
				senderName = n
			}
			replyToID, _ := payload["replyToId"].(string)
			go func() {
				seq, ts, err := s.group.SendGroupMessage(ctx, group.SendParams{
					GroupID: groupID, SenderID: c.userID, SenderName: senderName,
					SenderAvatar: senderAvatar, MsgType: msgType, Content: content,
					ReplyToID: replyToID, Extra: payload["extra"],
				})
				ack := map[string]any{"ack": true, "groupId": groupID, "seq": seq,
					"timestamp": ts, "localId": strVal(payload["localId"], "")}
				if err != nil {
					log.Printf("[GroupSignal] 发送失败: %v", err)
					ack["seq"] = -1
					ack["timestamp"] = time.Now().UnixMilli()
					ack["error"] = err.Error()
				}
				s.sendTo(c.userID, map[string]any{"type": "group_message", "groupId": groupID, "payload": ack})
			}()
		}
	case "group_pull":
		{
			groupID, _ := payload["groupId"].(string)
			if groupID == "" {
				break
			}
			var afterSeq *int64
			if v, ok := payload["lastSeq"]; ok {
				n := toInt64(v)
				afterSeq = &n
			} else if v, ok := payload["afterSeq"]; ok {
				n := toInt64(v)
				afterSeq = &n
			}
			var beforeSeq *int64
			if v, ok := payload["beforeSeq"]; ok {
				n := toInt64(v)
				beforeSeq = &n
			}
			limit := int(toInt64(payload["limit"]))
			go func() {
				result, err := s.group.PullGroupMessages(ctx, group.PullParams{
					GroupID: groupID, UserID: c.userID,
					AfterSeq: afterSeq, BeforeSeq: beforeSeq, Limit: limit,
				})
				if err != nil {
					log.Printf("[GroupSignal] 拉取失败: %v", err)
					return
				}
				pl := map[string]any{"pull": true, "groupId": groupID}
				for k, v := range result {
					pl[k] = v
				}
				s.sendTo(c.userID, map[string]any{"type": "group_message", "groupId": groupID, "payload": pl})
			}()
		}
	case "group_ack":
		{
			groupID, _ := payload["groupId"].(string)
			if groupID != "" {
				if v, ok := payload["lastAckSeq"]; ok {
					seq := toInt64(v)
					go func() {
						if err := s.group.AckGroupMessages(ctx, group.AckParams{
							GroupID: groupID, UserID: c.userID, LastAckSeq: seq,
						}); err != nil {
							log.Printf("[GroupSignal] ACK失败: %v", err)
						}
					}()
				}
			}
		}

	// ---------- 频道 ----------
	case "channel_send":
		{
			channelID, _ := payload["channelId"].(string)
			content, _ := payload["content"].(string)
			if channelID == "" || content == "" {
				break
			}
			// 仅 owner/admin 可发布
			var role string
			err := s.deps.DB.Pool.QueryRow(ctx,
				`SELECT "role" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`,
				channelID, c.userID).Scan(&role)
			if err != nil || (role != "owner" && role != "admin") {
				s.sendTo(c.userID, map[string]any{
					"type": "channel_message",
					"payload": map[string]any{"ack": true, "channelId": channelID,
						"localId": strVal(payload["localId"], ""), "error": "仅频道管理员可发布消息"},
				})
				break
			}
			msgType, _ := payload["msgType"].(string)
			if msgType == "" {
				msgType = "text"
			}
			var senderName, senderAvatar string
			_ = s.deps.DB.Pool.QueryRow(ctx,
				`SELECT COALESCE("nickname","username"), COALESCE("avatar",'') FROM "User" WHERE "id"=$1`,
				c.userID).Scan(&senderName, &senderAvatar)
			if n, _ := payload["senderName"].(string); n != "" {
				senderName = n
			}
			replyToID, _ := payload["replyToId"].(string)
			go func() {
				seq, ts, err := s.group.SendGroupMessage(ctx, group.SendParams{
					GroupID: channelID, SenderID: c.userID, SenderName: senderName,
					SenderAvatar: senderAvatar, MsgType: msgType, Content: content,
					ReplyToID: replyToID, Extra: payload["extra"],
				})
				ack := map[string]any{"ack": true, "channelId": channelID, "seq": seq,
					"timestamp": ts, "localId": strVal(payload["localId"], "")}
				if err != nil {
					ack["seq"] = -1
					ack["timestamp"] = time.Now().UnixMilli()
					ack["error"] = err.Error()
				}
				s.sendTo(c.userID, map[string]any{"type": "channel_message", "payload": ack})
			}()
		}
	case "channel_subscribe":
		{
			if channelID, _ := payload["channelId"].(string); channelID != "" {
				s.group.JoinGroupOnline(channelID, c.userID)
			}
		}
	case "channel_leave":
		{
			if channelID, _ := payload["channelId"].(string); channelID != "" {
				s.group.LeaveGroupOnline(channelID, c.userID)
			}
		}

	// ---------- MLS ----------
	case "mls_add_member":
		{
			groupID, _ := payload["groupId"].(string)
			targetUserID, _ := payload["targetUserId"].(string)
			welcome, wok := payload["welcome"]
			commit, cok := payload["commit"]
			if groupID == "" || targetUserID == "" || !wok || !cok {
				break
			}
			s.sendTo(targetUserID, map[string]any{
				"type": "mls_welcome", "groupId": groupID,
				"welcome": welcome, "senderIdentityKey": payload["senderIdentityKey"],
			})
			for _, uid := range s.group.GetGroupOnlineMembers(groupID) {
				if uid != c.userID && uid != targetUserID {
					s.sendTo(uid, map[string]any{"type": "mls_commit", "groupId": groupID, "commit": commit})
				}
			}
			// 持久化（内部回环，与 TS 一致）
			s.mlsPost("/api/mls/send-welcome", map[string]any{
				"groupId": groupID, "targetUserId": targetUserID,
				"welcome": welcome, "senderIdentityKey": payload["senderIdentityKey"]})
			s.mlsPost("/api/mls/broadcast-commit", map[string]any{
				"groupId": groupID, "commit": commit, "commitType": "add"})
			var epoch any
			if cm, ok := commit.(map[string]any); ok {
				epoch = cm["epoch"]
			}
			var members any
			if wm, ok := welcome.(map[string]any); ok {
				members = wm["members"]
			}
			s.mlsPost("/api/mls/update-group-state", map[string]any{
				"groupId": groupID, "epoch": epoch, "members": members, "commitType": "add"})
		}
	case "mls_remove_member":
		{
			groupID, _ := payload["groupId"].(string)
			targetUserID, _ := payload["targetUserId"].(string)
			commit, cok := payload["commit"]
			if groupID == "" || targetUserID == "" || !cok {
				break
			}
			for _, uid := range s.group.GetGroupOnlineMembers(groupID) {
				if uid != c.userID {
					s.sendTo(uid, map[string]any{"type": "mls_commit", "groupId": groupID, "commit": commit})
				}
			}
			s.mlsPost("/api/mls/broadcast-commit", map[string]any{
				"groupId": groupID, "commit": commit, "commitType": "remove"})
			var epoch any
			if cm, ok := commit.(map[string]any); ok {
				epoch = cm["epoch"]
			}
			s.mlsPost("/api/mls/update-group-state", map[string]any{
				"groupId": groupID, "epoch": epoch, "commitType": "remove"})
		}
	case "mls_update_keys":
		{
			groupID, _ := payload["groupId"].(string)
			commit, cok := payload["commit"]
			if groupID == "" || !cok {
				break
			}
			for _, uid := range s.group.GetGroupOnlineMembers(groupID) {
				if uid != c.userID {
					s.sendTo(uid, map[string]any{"type": "mls_commit", "groupId": groupID, "commit": commit})
				}
			}
			s.mlsPost("/api/mls/broadcast-commit", map[string]any{
				"groupId": groupID, "commit": commit, "commitType": "update"})
			var epoch any
			if cm, ok := commit.(map[string]any); ok {
				epoch = cm["epoch"]
			}
			s.mlsPost("/api/mls/update-group-state", map[string]any{
				"groupId": groupID, "epoch": epoch, "commitType": "update"})
		}

	// ---------- 位置共享 ----------
	case "location_share_start":
		{
			chatID, _ := payload["chatId"].(string)
			duration := int(toInt64(payload["duration"]))
			if duration <= 0 {
				duration = 3600
			}
			initiatorName, _ := payload["initiatorName"].(string)
			shareID := "lshare_" + chatID + "_" + itoa(time.Now().UnixMilli())
			expiresAt := time.Now().UnixMilli() + int64(duration)*1000
			sh := &locationShare{
				chatID: chatID, initiatorID: c.userID,
				expiresAt: expiresAt, duration: duration,
				positions: make(map[string]any),
			}
			sh.timer = time.AfterFunc(time.Duration(duration)*time.Second, func() {
				s.endLocationShare(shareID)
			})
			s.shareMu.Lock()
			s.shares[shareID] = sh
			s.shareMu.Unlock()
			s.roomMu.Lock()
			room, ok := s.rooms[shareID]
			if !ok {
				room = make(map[string]bool)
				s.rooms[shareID] = room
			}
			room[c.userID] = true
			s.roomMu.Unlock()
			s.sendTo(c.userID, map[string]any{
				"type":    "location_share_info",
				"payload": map[string]any{"shareId": shareID, "expiresAt": expiresAt, "members": []string{c.userID}},
			})
			s.broadcastToRoom(chatID, mustJSON(map[string]any{
				"type": "location_share_started", "from": c.userID,
				"payload": map[string]any{"shareId": shareID, "initiatorId": c.userID,
					"initiatorName": initiatorName, "expiresAt": expiresAt, "duration": duration, "chatId": chatID},
			}), "")
		}
	case "location_share_join":
		{
			shareID, _ := payload["shareId"].(string)
			s.shareMu.RLock()
			sh, ok := s.shares[shareID]
			s.shareMu.RUnlock()
			if !ok {
				s.sendTo(c.userID, map[string]any{"type": "location_share_info",
					"payload": map[string]any{"shareId": shareID, "error": "share_not_found"}})
				break
			}
			if time.Now().UnixMilli() > sh.expiresAt {
				s.sendTo(c.userID, map[string]any{"type": "location_share_info",
					"payload": map[string]any{"shareId": shareID, "error": "share_expired"}})
				break
			}
			s.roomMu.Lock()
			room, ok := s.rooms[shareID]
			if !ok {
				room = make(map[string]bool)
				s.rooms[shareID] = room
			}
			room[c.userID] = true
			members := roomMembers(room)
			s.roomMu.Unlock()
			s.broadcastToRoom(shareID, mustJSON(map[string]any{
				"type": "location_share_info", "from": c.userID,
				"payload": map[string]any{"shareId": shareID, "event": "member_joined",
					"memberId": c.userID, "members": members},
			}), c.userID)
			positions := map[string]any{}
			s.shareMu.RLock()
			for uid, pos := range sh.positions {
				positions[uid] = pos
			}
			s.shareMu.RUnlock()
			s.sendTo(c.userID, map[string]any{"type": "location_share_info",
				"payload": map[string]any{"shareId": shareID, "event": "joined",
					"expiresAt": sh.expiresAt, "members": members, "positions": positions}})
		}
	case "location_share_update":
		{
			shareID, _ := payload["shareId"].(string)
			s.shareMu.RLock()
			sh, ok := s.shares[shareID]
			s.shareMu.RUnlock()
			if !ok || time.Now().UnixMilli() > sh.expiresAt {
				break
			}
			pos := map[string]any{
				"lat": payload["lat"], "lng": payload["lng"],
				"accuracy": payload["accuracy"], "heading": payload["heading"],
				"speed": payload["speed"], "timestamp": time.Now().UnixMilli(), "userId": c.userID,
			}
			s.shareMu.Lock()
			sh.positions[c.userID] = pos
			s.shareMu.Unlock()
			s.broadcastToRoom(shareID, mustJSON(map[string]any{
				"type": "location_share_update", "from": c.userID, "payload": pos,
			}), c.userID)
		}
	case "location_share_stop":
		{
			if shareID, _ := payload["shareId"].(string); shareID != "" {
				s.endLocationShare(shareID)
			}
		}

	// ---------- 已读回执 ----------
	case "read_receipt":
		{
			chatID, _ := payload["chatId"].(string)
			if messageIDs, ok := payload["messageIds"].([]any); ok && to != "" {
				s.sendTo(to, map[string]any{
					"type": "read_receipt", "from": c.userID,
					"payload": map[string]any{"chatId": chatID, "messageIds": messageIDs},
				})
			}
		}

	// ---------- 私聊发送（P0：强制加密 + 参与者校验） ----------
	case "private_send":
		{
			pChatID, _ := payload["chatId"].(string)
			pContent, _ := payload["content"].(string)
			pMsgType, _ := payload["msgType"].(string)
			tempID, _ := payload["tempId"].(string)
			if pMsgType != "encrypted" {
				s.sendTo(c.userID, map[string]any{"type": "private_message",
					"payload": map[string]any{"ack": true, "error": "私聊强制要求端到端加密，请发送加密消息", "tempId": tempID}})
				break
			}
			if pChatID == "" || pContent == "" {
				break
			}
			go s.handlePrivateSend(c, payload)
		}
	case "private_typing":
		{
			typingChatID, _ := payload["chatId"].(string)
			if typingChatID != "" && to != "" {
				s.sendTo(to, map[string]any{"type": "private_typing", "from": c.userID,
					"payload": map[string]any{"chatId": typingChatID}})
			}
		}

	// ---------- 心跳 ----------
	case "heartbeat":
		{
			s.refreshUserOnline(c.userID)
			s.mu.RLock()
			cc, ok := s.clients[c.userID]
			s.mu.RUnlock()
			if ok {
				cc.enqueue(mustJSON(map[string]any{"type": "heartbeat_ack", "timestamp": time.Now().UnixMilli()}))
			}
		}

	// ---------- 私聊撤回（补强：校验消息归属与发送者身份） ----------
	case "recall":
		{
			recallChatID, _ := payload["chatId"].(string)
			recallMsgID, _ := payload["messageId"].(string)
			if recallMsgID == "" {
				break
			}
			go func() {
				var msgChatID, senderID string
				err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "chatId","senderId" FROM "PrivateMessage" WHERE "id"=$1`,
					recallMsgID).Scan(&msgChatID, &senderID)
				if err != nil {
					return // 消息不存在：静默忽略
				}
				if recallChatID != "" && msgChatID != recallChatID {
					return // chatId 与消息归属不一致
				}
				if senderID != c.userID {
					log.Printf("[Recall] %s 尝试撤回他人消息 %s，已拒绝", c.userID, recallMsgID)
					return
				}
				// 标记撤回
				_, _ = s.deps.DB.Exec(ctx,
					`UPDATE "PrivateMessage" SET "isRevoked"=true WHERE "id"=$1`, recallMsgID)
				// 对方从会话参与者推导，不信任 msg.to
				var pa, pb string
				if err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "participantA","participantB" FROM "Chat" WHERE "id"=$1`,
					msgChatID).Scan(&pa, &pb); err == nil {
					peer := pa
					if peer == c.userID {
						peer = pb
					}
					if peer != "" {
						s.sendTo(peer, map[string]any{
							"type": "recall_notify", "from": c.userID,
							"payload": map[string]any{"chatId": msgChatID, "messageId": recallMsgID},
						})
					}
				}
				log.Printf("[Recall] 私聊撤回: from=%s msgId=%s", c.userID, recallMsgID)
			}()
		}

	// ---------- 阅后即焚：已读（补强：校验接收方身份与 chatId） ----------
	case "burn_read":
		{
			brChatID, _ := payload["chatId"].(string)
			brMsgID, _ := payload["messageId"].(string)
			if brChatID == "" || brMsgID == "" {
				break
			}
			go func() {
				var m struct {
					ChatID        string
					SenderID      string
					BurnAfterRead *int
					BurnReadAt    *time.Time
				}
				err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "chatId","senderId","burnAfterRead","burnReadAt" FROM "PrivateMessage" WHERE "id"=$1`,
					brMsgID).Scan(&m.ChatID, &m.SenderID, &m.BurnAfterRead, &m.BurnReadAt)
				if err != nil || m.BurnAfterRead == nil || m.BurnReadAt != nil {
					return
				}
				if m.ChatID != brChatID {
					return // chatId 与消息归属不一致
				}
				if m.SenderID == c.userID {
					return // 只有接收方能标记已读
				}
				// 调用者必须是会话参与者（接收方）
				var pa, pb string
				if err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "participantA","participantB" FROM "Chat" WHERE "id"=$1`,
					brChatID).Scan(&pa, &pb); err != nil || (pa != c.userID && pb != c.userID) {
					return
				}
				now := time.Now()
				expireAt := now.Add(time.Duration(*m.BurnAfterRead) * time.Second)
				_, _ = s.deps.DB.Exec(ctx,
					`UPDATE "PrivateMessage" SET "burnReadAt"=$1,"burnExpireAt"=$2,"status"='read' WHERE "id"=$3`,
					now, expireAt, brMsgID)
				s.sendTo(m.SenderID, map[string]any{
					"type": "burn_read", "from": c.userID,
					"payload": map[string]any{"chatId": brChatID, "messageId": brMsgID,
						"readAt": now.UnixMilli(), "burnAfterRead": *m.BurnAfterRead},
				})
			}()
		}

	// ---------- 阅后即焚：销毁（S8） ----------
	case "burn_delete":
		{
			bdChatID, _ := payload["chatId"].(string)
			bdMsgID, _ := payload["messageId"].(string)
			if bdChatID == "" || bdMsgID == "" {
				break
			}
			go func() {
				var msgChatID string
				if err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "chatId" FROM "PrivateMessage" WHERE "id"=$1`, bdMsgID).Scan(&msgChatID); err != nil {
					return
				}
				if msgChatID != bdChatID {
					return
				}
				var pa, pb string
				if err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "participantA","participantB" FROM "Chat" WHERE "id"=$1`,
					bdChatID).Scan(&pa, &pb); err != nil || (pa != c.userID && pb != c.userID) {
					log.Printf("[BurnAfterRead] 非会话参与者 %s 尝试销毁消息 %s，已拒绝", c.userID, bdMsgID)
					return
				}
				_, _ = s.deps.DB.Exec(ctx, `DELETE FROM "PrivateMessage" WHERE "id"=$1`, bdMsgID)
				if to != "" {
					s.sendTo(to, map[string]any{
						"type": "burn_delete", "from": c.userID,
						"payload": map[string]any{"chatId": bdChatID, "messageId": bdMsgID},
					})
				}
			}()
		}

	// ---------- 群撤回（S9） ----------
	case "group_recall":
		{
			recallGroupID, _ := payload["groupId"].(string)
			recallMsgID, _ := payload["messageId"].(string)
			seq := payload["seq"]
			if recallGroupID == "" || recallMsgID == "" {
				break
			}
			go func() {
				var msgGroupID, senderID string
				err := s.deps.DB.Pool.QueryRow(ctx,
					`SELECT "groupId","senderId" FROM "GroupMessage" WHERE "id"=$1`,
					recallMsgID).Scan(&msgGroupID, &senderID)
				if err != nil || msgGroupID != recallGroupID {
					return
				}
				canRecall := senderID == c.userID
				if !canRecall {
					var role string
					if err := s.deps.DB.Pool.QueryRow(ctx,
						`SELECT "role" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`,
						recallGroupID, c.userID).Scan(&role); err == nil {
						canRecall = role == "owner" || role == "admin"
					}
				}
				if !canRecall {
					log.Printf("[GroupRecall] %s 无权撤回群 %s 的消息 %s，已拒绝", c.userID, recallGroupID, recallMsgID)
					return
				}
				_, _ = s.deps.DB.Exec(ctx,
					`UPDATE "GroupMessage" SET "isRevoked"=true WHERE "id"=$1`, recallMsgID)
				s.group.FanoutGroupSignal(recallGroupID, map[string]any{
					"type": "group_recall_notify", "from": c.userID,
					"payload": map[string]any{"groupId": recallGroupID, "messageId": recallMsgID, "seq": seq},
				}, c.userID)
			}()
		}
	}
}

// ============================================================
// 私聊发送实现
// ============================================================

var hmacRe = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var validBurnTimers = []int{5, 10, 30, 60, 300, 3600, 86400, 604800}

func (s *Server) handlePrivateSend(c *Client, payload map[string]any) {
	ctx := context.Background()
	pChatID, _ := payload["chatId"].(string)
	pContent, _ := payload["content"].(string)
	tempID, _ := payload["tempId"].(string)
	failAck := func(errMsg string) {
		if tempID != "" {
			s.sendTo(c.userID, map[string]any{"type": "private_message",
				"payload": map[string]any{"ack": true, "error": errMsg, "tempId": tempID}})
		}
	}

	var pa, pb string
	if err := s.deps.DB.Pool.QueryRow(ctx,
		`SELECT "participantA","participantB" FROM "Chat" WHERE "id"=$1`,
		pChatID).Scan(&pa, &pb); err != nil {
		failAck("会话不存在")
		return
	}
	if pa != c.userID && pb != c.userID {
		failAck("无权发送消息")
		return
	}

	// extra 可能是字符串
	var pExtra any
	if rawExtra, ok := payload["extra"]; ok && rawExtra != nil {
		if es, ok := rawExtra.(string); ok {
			var parsed any
			if err := json.Unmarshal([]byte(es), &parsed); err == nil {
				pExtra = parsed
			}
		} else {
			pExtra = rawExtra
		}
	}
	// burnAfterRead 白名单
	var burnSeconds *int
	if v, ok := payload["burnAfterRead"].(float64); ok {
		for _, t := range validBurnTimers {
			if int(v) == t {
				b := t
				burnSeconds = &b
				break
			}
		}
	}
	var hmacVal *string
	if h, ok := payload["hmac"].(string); ok && hmacRe.MatchString(h) {
		hmacVal = &h
	}
	var extraJSON *string
	if pExtra != nil {
		if b, err := json.Marshal(pExtra); err == nil {
			ej := string(b)
			extraJSON = &ej
		}
	}
	replyToID, _ := payload["replyToId"].(string)
	var replyToPtr *string
	if replyToID != "" {
		replyToPtr = &replyToID
	}

	msgID := util.NewID()
	now := time.Now()
	_, err := s.deps.DB.Pool.Exec(ctx,
		`INSERT INTO "PrivateMessage"("id","chatId","senderId","msgType","content","replyToId","extra","status","burnAfterRead","hmac","createdAt","updatedAt")
		 VALUES($1,$2,$3,'encrypted',$4,$5,$6,'sent',$7,$8,$9,$9)`,
		msgID, pChatID, c.userID, pContent, replyToPtr, extraJSON, burnSeconds, hmacVal, now)
	if err != nil {
		log.Printf("[PrivateChat] 发送失败: %v", err)
		failAck("发送失败，请重试")
		return
	}
	_, _ = s.deps.DB.Exec(ctx,
		`UPDATE "Chat" SET "lastMessage"='🔒 [加密消息]',"lastMessageAt"=$1 WHERE "id"=$2`, now, pChatID)

	msgPayload := map[string]any{
		"id": msgID, "chatId": pChatID, "senderId": c.userID,
		"msgType": "encrypted", "content": pContent, "replyToId": replyToID,
		"isRevoked": false, "status": "sent", "createdAt": now.UnixMilli(), "tempId": tempID,
	}
	if pExtra != nil {
		msgPayload["extra"] = pExtra
	}
	if burnSeconds != nil {
		msgPayload["burnAfterRead"] = *burnSeconds
	}
	if hmacVal != nil {
		msgPayload["hmac"] = *hmacVal
	}
	s.sendTo(c.userID, map[string]any{"type": "private_message",
		"payload": merge(map[string]any{"ack": true}, msgPayload)})

	peerID := pa
	if peerID == c.userID {
		peerID = pb
	}
	delivered := s.trySendTo(peerID, mustJSON(map[string]any{"type": "private_message", "payload": msgPayload}))
	if !delivered {
		_ = s.publishImPush(peerID, map[string]any{"type": "private_message", "payload": msgPayload})
	}

	// 未读数 + 会话列表失效
	pipe := s.deps.Redis.RDB.Pipeline()
	pipe.HIncrBy(ctx, unreadHashPrefix+peerID, pChatID, 1)
	pipe.HIncrBy(ctx, unreadHashPrefix+peerID, "total", 1)
	pipe.Expire(ctx, unreadHashPrefix+peerID, 7*24*time.Hour)
	_, _ = pipe.Exec(ctx)
	_ = s.deps.Redis.Del(ctx, convListPrefix+c.userID, convListPrefix+peerID)

	if !delivered && !s.isUserOnline(peerID) {
		_ = push.SendApnsToUser(s.deps, peerID, "新消息", "🔒 [加密消息]",
			map[string]any{"type": "private_message", "chatId": pChatID, "messageId": msgID, "from": c.userID})
	}
}

// ============================================================
// helpers
// ============================================================

func (s *Server) isGroupMember(ctx context.Context, groupID, userID string) bool {
	var id string
	err := s.deps.DB.Pool.QueryRow(ctx,
		`SELECT "id" FROM "GroupMember" WHERE "groupId"=$1 AND "userId"=$2`,
		groupID, userID).Scan(&id)
	return err == nil
}

// mlsPost 内部回环调用 MLS 持久化接口（与 TS 的 fetch localhost 一致）。
func (s *Server) mlsPost(path string, body map[string]any) {
	go func() {
		b, _ := json.Marshal(body)
		url := "http://127.0.0.1:" + s.deps.Cfg.Port + path
		req, err := http.NewRequest("POST", url, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[MLS] POST %s 失败: %v", path, err)
			return
		}
		resp.Body.Close()
	}()
}

func (s *Server) endLocationShare(shareID string) {
	s.shareMu.Lock()
	sh, ok := s.shares[shareID]
	if ok {
		if sh.timer != nil {
			sh.timer.Stop()
		}
		delete(s.shares, shareID)
	}
	s.shareMu.Unlock()
	if !ok {
		return
	}
	s.roomMu.Lock()
	delete(s.rooms, shareID)
	s.roomMu.Unlock()
	s.broadcastToRoom(sh.chatID, mustJSON(map[string]any{
		"type":    "location_share_ended",
		"payload": map[string]any{"shareId": shareID, "chatId": sh.chatID},
	}), "")
}

func toOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func strVal(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		var i int64
		for _, ch := range n {
			if ch < '0' || ch > '9' {
				break
			}
			i = i*10 + int64(ch-'0')
		}
		return i
	default:
		return 0
	}
}

func merge(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
