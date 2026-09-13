# P0 代码改动（2026-09-13）

已提交：
- `server/presence.ts` + `push-notify.ts`：只有 foreground 跳过推送
- Web TUICallKit 去掉硬编码 1600136830
- Gateway `DB_PATH` 默认改空（见 config.go）

还要你手动：
1. 生产 `.env`：`TRTC_SDK_APP_ID=1600159677`，删掉代码里 `|| 1600136830`
2. iOS / Web 进后台调 `POST /api/presence` `{ state: background|foreground, activeChatId }`
3. `index.ts` UserSig 禁止默认旧 ID
4. 空 PG migrate deploy
