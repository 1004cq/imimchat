Applied in this PR to `cqim-app/server/crypto.ts`:

- `POST /register-key`, `POST /register-bundle`, `POST /replenish-prekeys`, `GET /prekey-count` require `userAuth`.
- Stored `userId` always comes from the session, never from the request body.
- One-time prekeys are capped at 100 (oldest keyIds dropped).
- `GET /get-bundle` still takes the *peer* userId (must stay that way).
- Do **not** verify `signedPreKey.signature` until iOS ships a real signature.
