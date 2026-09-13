# 仓库现状（2026-09-13）

- 主仓：imimchat（已删 cq）
- 核心目录：cqim-app
- 线上域名：wed.imim.chat
- 主库现状：Prisma SQLite，非 Mongo
- 双机：第一台入口 + Redis Pub/Sub；EdgeOne 不要双 IP 轮询 WS
- iOS：独立仓 imimchatios
- TRTC：1600159677
