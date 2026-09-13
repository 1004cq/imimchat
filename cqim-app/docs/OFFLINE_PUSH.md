# CQIM 离线推送方案（对照 OpenIM）

私有仓说明。只学 OpenIM 的判断与分层，不换引擎、不接 Matrix/个推中台替代现有 APNs。

## 结论（先读这三句）

1. 长连接只保证前台实时；后台、锁屏、杀进程走 APNs。
2. **有 WebSocket ≠ 免推。** iOS 后台 WS 常假活，OpenIM 把 iOS background 当离线。
3. CQIM 已有 `server/presence.ts` + `push-notify.ts`：仅 `foreground` skip。缺的是接口、客户端上报、现网部署。

## OpenIM 怎么做

```text
消息落库
  → onlinePusher（各端 WS）
  → 这一端在线推成功？是且前台 → 不离线推
  → 否 / iOS background → offlinePusher（个推 / FCM / JPush / APNs）
```

- 配置：`push.enable` = getui | fcm | jpush。境内 Android 个推，海外 FCM。
iOS 走 APNs（可经 FCM/JPush 转）。
- 免打扰：`globalRecvMsgOpt=2` 或会话 `recvMsgOpt=2`，不是「有连接就不推」。
- 消息可带 `OfflinePushInfo`（title/desc/声音/角标）。
- 推送前 webhook 可改标题、拦截。
- 主动 logout / 被踢：不再离线推。
- 失败打点 `MsgOfflinePushFailedCounter`。

参考：
- https://docs.openim.io/
- https://doc.rentsoft.cn/zh-Hans/guides/solution/offlinePush
- open-im-server `internal/push`；Issue #2623（iOS background = offline）

## CQIM 现状

| 项 | 状态 |
| --- | --- |
| APNs P8 | 已有 `server/apns.ts` |
| 个推 / JPush / FCM / WebPush | 代码有，主路径用 APNs |
| `presence.ts` | 仅 foreground skip |
| `POST /api/presence` | **未挂好** |
| iOS/Web 上报前后台 | **未做** |
| Token 上报 | iOS 曾 TODO |
| 免推开关 | 设置页未闭环 |

线上 `wed.imim.chat`：部署前仍可能按旧逻辑「有 WS 就 skip」。

## 落地（三阶段）

### P0 — 和 OpenIM 判断对齐

1. Express 挂 `POST /api/presence`
   - body: `{ state: "foreground" \| "background", activeChatId?: string }`
   - Redis: `user:presence:{uid}` TTL 90s；可选 `user:activeChat:{uid}`
2. Web / iOS
   - 前台 / `visibilitychange` visible → foreground
   - 后台 / 锁屏 → **立刻** background（不要等 WS 断）
3. `notifyPrivateMessagePush`：`shouldSkipApns` 为 true 才 return；缺 presence 当 offline → **要推**
4. iOS 上报 APNs token；沙盒包打 sandbox，正式包打 production
5. 加密会话推送文案固定「加密消息」，不要明文

验收：A 把 App 切后台，B 发一条，A 锁屏必出系统横幅。

### P1 — 产品开关

- 全局 / 单会话免推（对齐 `recvMsgOpt=2`）
- 前台且正打开同一 `chatId` 才 skip（可用 `activeChatId`）
- logout 清 presence + 可选解绑 token
- APNs 410 清库里废 token

### P2 — 不要现在做

- 为推送再引入 Kafka / 独立 push 微服务
- 用个推替代 iOS APNs
- VoIP Push 发文本（审核会挂）
- 换 OpenIM 整仓

## 接口草稿

```http
POST /api/presence
Authorization: Bearer <token>
Content-Type: application/json

{"state":"background","activeChatId":null}
```

`state` 仅 `foreground` | `background`。关应用可再发一次 background，TTL 过期视为 offline。

## 与 P0 清单关系

本文 = Issue #25 的推送分支。不替代 PostgreSQL 切流、不替代 TRTC `1600159677`。
三件可并行，但推送验收不依赖换库。
