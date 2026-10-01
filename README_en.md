# imimchat / CQIM

[Chinese](./README.md)

Main repo for Web + Node API + Go Gateway. Code: `cqim-app/`.
Production: https://wed.imim.chat

## Encryption scope

- **1:1 private chats:** end-to-end encrypted. Server stores ciphertext only (`msgType=encrypted`).
- **Group chats:** **not** E2EE. The server can read group message bodies. Do not advertise “full E2EE”.
- **Push:** alert body is a generic “encrypted message” string; it does not include chat text.
- Uninstalling the app discards local keys; old private ciphertext may be unrecoverable.

Prisma **schema** is `postgresql`. Production may still run **SQLite** until `DATABASE_URL` is switched and migrations are deployed. Mongo is not the Prisma database.

iOS: `1004cq/imim.chat` (not this repo).
Not MTProto.
TRTC SDKAppID: 1600159677.
