# imimchat / CQIM

[English](./README_en.md) | **中文**

私有化即时通讯（Web + API + Go Gateway）。核心代码在 **[cqim-app](./cqim-app/)**。

|项|值|
|---|---|
|线上|
https://wed.imim.chat |
|管理后台|
https://wed.imim.chat/admin |
|主仓|
本仓 `imimchat`（已停用旧仓 `cq`）|
|iOS 原生|
另仓 [1004cq/imimchatios](https://github.com/1004cq/imimchatios)，不在本仓|

不做 MTProto / TDLib / 多 DC。传输是 **HTTPS JSON + WebSocket**。

---

## 现状（以代码为准，不以旧文档为准）

|层|实际|
|---|---|
|前端|
TypeScript / React / Vite（`cqim-app/client`）|
|业务|
Node.js / Express（`cqim-app/server`）|
|网关|
Go Gateway（`cqim-app/go-gateway`）|
|协议|
`/api` JSON + `wss://wed.imim.chat/signal`|
|主库|
**Prisma + SQLite**（`cqim-app/prisma`）。计划迁 **PostgreSQL**。README 旧说的 Mongo 不是 Prisma provider。|
|缓存/跨节点|
Redis（在线、`cqim:im:push`）|
|加密|
私聊强制 `msgType=encrypted`（Signal 方向）；群 MLS 方向|
|TRTC|
SDKAppID **1600159677**（应用名 im），禁止回落 1600136830|

WuKongIM / TangSengDaoDao 仅遗留参考，**不是主路径**。

---

## 目录

```text
imimchat/
├── cqim-app/                 #★ 全栈（只改这里）
│   ├── client/
│   ├── server/
│   ├── go-gateway/
│   ├── prisma/
│   ├── deploy/               # 双机 Nginx / compose
│   └── docs/
├── docs/
├── nginx / docker / scripts #旧部署参考
└── README.md
```

---

## 本地起动

```bash
cd cqim-app
cp .env.example .env
docker compose up -d --build
```

详细：[cqim-app/docs/DEPLOY.md](./cqim-app/docs/DEPLOY.md)  
双机：[cqim-app/docs/TWO_NODES.md](./cqim-app/docs/TWO_NODES.md)  
EdgeOne 源站只回 **第一台**，不要用源站组轮询拆 `/signal`。禁止 NFS 共享 SQLite。

---

## P0（仓库与现网已知问题）

1. SQLite `database disk image is malformed` → 迁 PostgreSQL
2. 后台 WS 仍在线时 skip APNs → 仅 foreground 免推
3. TRTC UserSig 与控制台 1600159677 对齐
4. 聊天预览 / 图片加载 / 密钥同步体验
5. 把本 README 与现网保持一致

---

MIT
