# imimchat / CQIM

[Chinese](./README.md)

Main repo for Web + Node API + Go Gateway. Code: `cqim-app/`.
Production: https://wed.imim.chat

Prisma **schema** is `postgresql`. Production may still run **SQLite** until `DATABASE_URL` is switched and migrations are deployed. Mongo is not the Prisma database. Go Gateway is still sqlite-only: `DB_PATH` must be an explicit node-local file (empty / `cqim.db` / NFS-shared paths are refused).

iOS: `1004cq/imimchatios` (not this repo).
Not MTProto.
TRTC SDKAppID: 1600159677.
