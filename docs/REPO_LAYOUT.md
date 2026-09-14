# 仓库怎么看（整理说明）

本仓私有、不开源。改产品只进 `cqim-app/`。

## 要用

| 路径 | 作用 |
| --- | --- |
| `cqim-app/` | Web + Node API + Go 网关 + Prisma |
| `cqim-app/client` | 前端 |
| `cqim-app/server` | 后端 |
| `cqim-app/go-gateway` | WebSocket |
| `cqim-app/prisma` | 数据库 schema |
| `cqim-app/docs` | CQIM 部署/双机/迁库 |
| `.github/workflows` | CI （typecheck） |
| `README.md` | 现行说明（以这份为准） |
| [LIVE_VS_REPO.md](./LIVE_VS_REPO.md) | 线上扁平 `cqim-release/` ↔ 本仓 `cqim-app/` |

## 遗留（不要当主路径）

| 路径 | 说明 |
| --- | --- |
| `docker/` `nginx/` `scripts/` | 早期 WuKongIM 部署 |
| `docs/architecture-tg.md` 等旧文 | 以根 README 为准 |
| `.codebuddy/` | CodeBuddy IDE，应从 Git 删掉 |

## 清掉 `.codebuddy`（本机一次性）

```bash
git pull
git rm -r .codebuddy
git commit -m "chore: remove .codebuddy"
git push
```

`.gitignore` 已包含 `.codebuddy/`，删掉后不会再进仓。
