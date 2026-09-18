# imimchat / CQIM

[Chinese](./README.md)

Main repo for Web + Node API + Go Gateway. Code: `cqim-app/`.
Production: https://wed.imim.chat

Prisma **schema** is `postgresql`. Git now has `prisma/migrations/…_init_postgres`. Production may still run **SQLite** until the owner sets `DATABASE_URL` and runs `prisma migrate deploy`. Mongo is not the Prisma database.

iOS: `1004cq/imimchatios` (not this repo).
Not MTProto.
TRTC SDKAppID: 1600159677.
