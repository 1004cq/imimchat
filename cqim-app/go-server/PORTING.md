# go-server 移植规范（所有子代理必读）

目标：把 `cqim-app/server/*.ts`（Node/Express）逐模块移植为 Go，
API 契约必须与 Node 版完全一致（路由、JSON 字段、状态码、中文错误文案）。

## 依赖与包

- module: `github.com/1004cq/imim.chat/cqim-app/go-server`
- 你的包路径：`internal/handler/<模块名>`，包名与目录名一致
- 共享依赖：`internal/handler.Deps{Cfg, DB, Redis, Auth}`
- 入口约定：`func RegisterRoutes(mux *http.ServeMux, d *handler.Deps)`

## 路由写法（Go 1.23 stdlib）

```go
mux.Handle("POST /api/auth/login", d.Auth.UserAuth(http.HandlerFunc(h.login)))
mux.Handle("GET /api/users/{userId}", http.HandlerFunc(h.getUser)) // 路径参数用 r.PathValue("userId")
```

## 常用工具

- 取当前用户：`u := middleware.UserFrom(r)`（已在 UserAuth 中做 401/403）
- 取管理员：`a := middleware.AdminFrom(r)`
- 写 JSON：`util.WriteJSON(w, 200, map[string]any{...})`
- 写错误：`util.WriteError(w, 400, "中文文案")`（文案照抄 TS）
- 读 body：`var req struct{...}; if !util.DecodeJSON(w, r, &req) { return }`
- 新 ID：`util.NewID()`；token：`util.GenerateToken(48)`
- 密码：`util.HashPassword` / `util.VerifyPassword`
- 查单行：`db.QueryRowToStruct[db.User](ctx, d.DB, "SELECT ...", args...)`
  - `db.IsNotFound(err)` 判断无行
- 查多行：`db.QueryToStructs[db.User](ctx, d.DB, ...)`
- 写：`d.DB.Exec(ctx, "INSERT ...", ...)`
- Redis：`d.Redis.GetString/SetEX/Del/Incr/Publish/SAdd/SRem/SMembers`
- 客户端 IP：`middleware.GetClientIP(r, d.Cfg.TrustProxy)`

## SQL 规范

- 表名列名加双引号：`SELECT "id","username" FROM "User" WHERE "id"=$1`
  （Prisma 建表用的是带大小写的标识符）
- 占位符 `$1,$2...`；时间用 `NOW()`
- 保持与 TS 相同的查询语义（分页、排序、字段过滤）

## JSON 规范

- 响应字段名照抄 TS（camelCase），struct 用 `json:"xxx"` tag
- **绝不返回 password 哈希**；User 序列化前清掉 Password
- 时间用 RFC3339（encoding/json 默认即可）

## 必须保留的安全行为

- 所有原来有 userAuth 的路由必须包 `d.Auth.UserAuth`
- 原来有 adminAuth 的包 `d.Auth.AdminAuth`（+ 按需 RequireAdminRole）
- 限流：`middleware.LoginRateLimit/CodeRateLimit/AdminLoginRateLimit` 按 TS 挂载位置使用
- S1-S9 的 WS 授权修复不要回退（signal 模块除外，由主代理处理）

## 交付

- 每个文件 `package <模块名>`，`go build ./...` 必须通过
- 不要改 `internal/config|db|redisx|middleware|util` 下的文件
- 不要引入新第三方依赖（现有：gorilla/websocket, pgx/v5, go-redis/v9, x/crypto）
- 大文件（>800 行 TS）可拆成 2-3 个 Go 文件
