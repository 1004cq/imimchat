# P0 代码改动（2026-09-13）

已提交：
- `server/presence.ts` + `push-notify.ts`：只有 foreground 跳过推送
- Web TUICallKit 去掉硬编码 1600136830
- Gateway `DB_PATH` 默认改空（见 config.go）
- UserSig：`TRTC_SDK_APP_ID` 必填，缺省/无效直接 500，已移除 `|| 1600136830`

还要你手动：
1. 生产 `.env` 确认 `TRTC_SDK_APP_ID=1600159677`（代码已不再回落旧 ID）
2. iOS / Web 进后台调 `POST /api/presence` `{ state: background|foreground, activeChatId }`
3. 空 PG migrate deploy
