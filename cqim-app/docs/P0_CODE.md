# P0 代码改动（2026-09-13）

已提交：
- `server/presence.ts` + `push-notify.ts`：只有 foreground 跳过推送
- Web TUICallKit 去掉硬编码 旧 TRTC AppID
- Gateway `DB_PATH` 默认改空（见 config.go）
- `POST /api/presence`（#31）+ Web 前台/后台上报（见 `APNS_PRESENCE.md`）

还要你手动：
1. 生产 `.env`：`TRTC_SDK_APP_ID=1600159677`，删掉代码里 `|| 旧 TRTC AppID`
2. **iOS**（另一仓库）进后台必须调 `POST /api/presence` `{ state: background|foreground, activeChatId }`。Web 已接。有 WS ≠ 免推。
3. `index.ts` UserSig 禁止默认旧 ID
4. 空 PG migrate deploy
