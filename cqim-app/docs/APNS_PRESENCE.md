# APNs presence 合同（Web / iOS）

推送是否跳过 **只看** Redis `user:presence:${userId}`。  
`foreground` 才跳过 APNs。Gateway 的 `user:online:*`（WebSocket 在线）**不能**当作免推。

Redis TTL = **90 秒**。未续期则视为 `offline`，会推送。

`POST /api/presence` 已由 #31 挂到 Express（`userAuth` + `setPresence`）。本仓库 Web 客户端负责上报；iOS 在另一仓库。

## API

鉴权与其它用户接口相同：`Authorization: Bearer <session token>`（`userAuth`）。

```bash
# 进后台：即使 WS 仍连着，也必须报 background，否则会误跳过 APNs
curl -sS -X POST "$HOST/api/presence" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"state":"background"}'

# 回前台；正在看的会话可带 activeChatId（私聊精确免推）
curl -sS -X POST "$HOST/api/presence" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"state":"foreground","activeChatId":"chat-id-or-null"}'

# 登出
curl -sS -X POST "$HOST/api/presence" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"state":"offline"}'

# 读取当前用户 presence（调试）
curl -sS "$HOST/api/presence" \
  -H "Authorization: Bearer $TOKEN"
```

`state`：`foreground` | `background` | `offline`  
`activeChatId`：可选。非空则写入 `user:activeChat:${userId}`；`null` / `""` 清除。省略该字段则不改 `activeChat`。

## Web（本仓库已接）

路径：`client/src/lib/presence.ts`、`client/src/contexts/AppContext.tsx`

- `visibilitychange` / `focus` / `pagehide` / `freeze` 上报 foreground 或 background
- 每 45s 心跳续期 TTL
- 打开/关闭会话时同步 `activeChatId`
- 后台 tab 报 `background` → `shouldSkipApns` 为 false（会推）

## iOS（另一仓库必须接）

iOS 不在本仓库。原生层需要：

1. App 进入前台 / `willEnterForeground` / `didBecomeActive` → `POST /api/presence` `{ "state": "foreground", "activeChatId": <当前会话或 null> }`
2. App 进入后台 / `didEnterBackground` / `willResignActive` → **必须** `{ "state": "background" }`。有 WS 也不能省略。
3. 前台每 ≤45s 再打一次同一接口续期。
4. 打开/离开聊天页更新 `activeChatId`。
5. 登出 → `{ "state": "offline" }`。

Device Token 仍走现有接口，与 presence 分开：

- `POST /api/apns/token` `{ "token": "<deviceToken>" }`
- `POST /api/apns/voip-token` `{ "voipToken": "<voipToken>" }`
- `DELETE /api/apns/token`

## 服务端规则

路径：`server/presence.ts`、`server/presence-rules.ts`、`server/push-notify.ts`

- `shouldSkipApns`：`state !== foreground` → 不跳过（推送）
- 前台且未带 `chatId`（群推）→ 跳过
- 前台无私聊 `activeChat` → 跳过所有私聊推送
- 前台有 `activeChat` → 仅跳过正在看的那条私聊
