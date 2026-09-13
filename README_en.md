# imimchat / CQIM

[Chinese](./README.md) | **English**

Private messenger: Web + Node API + Go Gateway. All product code lives in **[cqim-app](./cqim-app/)**.

- Production: https://wed.imim.chat
- Admin: https://wed.imim.chat/admin
- iOS native app is **not** in this repo — see `1004cq/imimchatios`

Transport: HTTPS JSON + WebSocket. Not MTProto.

Prisma currently uses **SQLite**. Plan: migrate to **PostgreSQL**. Redis is for presence and `cqim:im:push`. TRTC SDKAppID is **1600159677**.

WuKongIM is legacy only.

```bash
cd cqim-app && cp .env.example .env && docker compose up -d --build
```
