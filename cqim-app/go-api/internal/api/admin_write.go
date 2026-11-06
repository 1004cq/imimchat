package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// Audit is part of the same transaction as each successful database mutation.
func adminAudit(ctx context.Context, tx pgx.Tx, a adminAccount, action, target, detail, ip string) error {
	var id any
	if a.ID != "" {
		id = a.ID
	}
	_, err := tx.Exec(ctx, `INSERT INTO "AdminLog" ("id","adminId","adminName","action","target","detail","ip","createdAt") VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`, newID(), id, a.Username, action, target, detail, ip)
	return err
}
func adminCookie(token string, age int) *http.Cookie {
	return &http.Cookie{Name: "admin_token", Value: token, Path: "/api/admin", Domain: envOrDefault("ADMIN_DOMAIN", ""), HttpOnly: true, Secure: envOrDefault("NODE_ENV", "") == "production", SameSite: http.SameSiteStrictMode, MaxAge: age}
}
func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.db == nil {
		writeJSON(w, 503, map[string]string{"error": "认证服务不可用"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var in struct{ Username, Password string }
	err := decode(r, &in)
	ip := adminClientIP(r)
	fail := func(status int, message string) {
		e := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			return adminAudit(ctx, tx, adminAccount{Username: in.Username}, "登录失败", "system", "管理员登录失败", ip)
		})
		if e != nil {
			dbError(w, e)
			return
		}
		writeJSON(w, status, map[string]string{"error": message})
	}
	if err != nil || in.Username == "" || in.Password == "" {
		fail(400, "请填写用户名和密码")
		return
	}
	if len(in.Username) > 200 || len(in.Password) > 4096 {
		fail(400, "输入过长")
		return
	}
	// Atomic shared rate limit; fail closed when Redis is unavailable.
	if s.redis == nil {
		fail(503, "登录限流服务不可用")
		return
	}
	key := "admin:login:go:" + ip
	n, e := s.redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],900) end;return n`, []string{key}).Int64()
	if e != nil {
		fail(503, "登录限流服务不可用")
		return
	}
	if n > 5 {
		w.Header().Set("Retry-After", "900")
		fail(429, "登录尝试过多，请稍后再试")
		return
	}
	var a adminAccount
	var hash string
	e = s.db.QueryRow(ctx, `SELECT "id","username","role","password" FROM "AdminAccount" WHERE "username"=$1`, in.Username).Scan(&a.ID, &a.Username, &a.Role, &hash)
	if e != nil && e != pgx.ErrNoRows {
		dbError(w, e)
		return
	}
	if e == pgx.ErrNoRows || !adminVerify(in.Password, hash) {
		fail(401, "用户名或密码错误")
		return
	}
	token, tokenErr := adminNewToken()
	if tokenErr != nil {
		dbError(w, tokenErr)
		return
	}
	e = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if len(hash) == 64 {
			h, err := adminHash(in.Password)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE "AdminAccount" SET "password"=$2,"updatedAt"=NOW() WHERE "id"=$1 AND "password"=$3`, a.ID, h, hash); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO "AdminSession" ("id","adminId","token","expiresAt","createdAt") VALUES ($1,$2,$3,NOW()+INTERVAL '2 hours',NOW())`, newID(), a.ID, token); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM "AdminSession" WHERE "adminId"=$1 AND "expiresAt"<NOW()`, a.ID); err != nil {
			return err
		}
		return adminAudit(ctx, tx, a, "登录", "system", "管理员登录", ip)
	})
	if e != nil {
		dbError(w, e)
		return
	}
	s.redis.Del(ctx, key)
	http.SetCookie(w, adminCookie(token, 7200))
	writeJSON(w, 200, map[string]any{"token": token, "admin": map[string]string{"id": a.ID, "username": a.Username, "role": a.Role}})
}
func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request, a adminAccount, token string) {
	e := pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		if _, e := tx.Exec(r.Context(), `DELETE FROM "AdminSession" WHERE "token"=$1`, token); e != nil {
			return e
		}
		return adminAudit(r.Context(), tx, a, "登出", "system", "", adminClientIP(r))
	})
	if e != nil {
		dbError(w, e)
		return
	}
	http.SetCookie(w, adminCookie("", -1))
	writeJSON(w, 200, map[string]bool{"success": true})
}

// bcryptjs (Node) truncates UTF-8 passwords at 72 bytes. Preserve its semantics.
func adminHash(password string) (string, error) {
	p := []byte(password)
	if len(p) > 72 {
		p = p[:72]
	}
	h, e := bcrypt.GenerateFromPassword(p, 12)
	return string(h), e
}
func adminPasswordError(p string, create bool) string {
	if len(utf16.Encode([]rune(p))) < 8 {
		return "密码至少8位"
	}
	if create {
		var lo, up, num bool
		for _, c := range p {
			lo = lo || (c >= 'a' && c <= 'z')
			up = up || (c >= 'A' && c <= 'Z')
			num = num || (c >= '0' && c <= '9')
		}
		if !lo || !up || !num {
			return "密码必须包含大小写字母和数字"
		}
	}
	return ""
}
func adminWriteAllowed(w http.ResponseWriter, a adminAccount) bool {
	if !adminAllowed(a) {
		writeJSON(w, 403, map[string]string{"error": "权限不足"})
		return false
	}
	return true
}
func adminWriteError(w http.ResponseWriter, e error) {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" {
		writeJSON(w, 409, map[string]string{"error": "用户ID、手机号或邮箱已被占用"})
		return
	}
	if e == pgx.ErrNoRows {
		writeJSON(w, 404, map[string]string{"error": "对象不存在"})
		return
	}
	dbError(w, e)
}

const adminUserJSON = `jsonb_build_object('id',u."id",'username',u."username",'nickname',u."nickname",'phone',u."phone",'email',u."email",'bio',u."bio",'avatar',u."avatar",'role',u."role",'isBanned',u."isBanned",'banReason',u."banReason",'createdAt',u."createdAt",'updatedAt',u."updatedAt",'lastLoginAt',u."lastLoginAt")`

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	if !adminWriteAllowed(w, a) {
		return
	}
	var in struct{ Username, Password, Nickname, Phone, Email, Bio, Avatar string }
	if decode(r, &in) != nil {
		groupBad(w, "请求无效")
		return
	}
	if !usernameRE.MatchString(in.Username) {
		groupBad(w, "用户ID只能包含字母、数字和下划线，长度1-20位")
		return
	}
	if msg := adminPasswordError(in.Password, true); msg != "" {
		groupBad(w, msg)
		return
	}
	hash, e := adminHash(in.Password)
	if e != nil {
		dbError(w, e)
		return
	}
	if in.Nickname == "" {
		in.Nickname = in.Username
	}
	id := newID()
	var data json.RawMessage
	e = pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		e := tx.QueryRow(r.Context(), `INSERT INTO "User" AS u ("id","username","password","nickname","phone","email","bio","avatar","updatedAt") VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NOW()) RETURNING `+adminUserJSON, id, in.Username, hash, in.Nickname, in.Phone, in.Email, in.Bio, in.Avatar).Scan(&data)
		if e != nil {
			return e
		}
		return adminAudit(r.Context(), tx, a, "创建用户", "user:"+id, "管理员创建用户", adminClientIP(r))
	})
	if e != nil {
		adminWriteError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "user": adminJSONDates(data)})
}
func (s *Server) adminEditUser(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	if !adminWriteAllowed(w, a) {
		return
	}
	var fields map[string]*string
	if decode(r, &fields) != nil {
		groupBad(w, "请求无效")
		return
	}
	id := r.PathValue("id")
	var result json.RawMessage
	profileChanged := false
	e := pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		var raw []byte
		if e := tx.QueryRow(r.Context(), `SELECT `+adminUserJSON+` FROM "User" u WHERE u."id"=$1 FOR UPDATE`, id).Scan(&raw); e != nil {
			return e
		}
		old := map[string]any{}
		if e := json.Unmarshal(raw, &old); e != nil {
			return e
		}
		sets := []string{}
		args := []any{id}
		changes := []string{}
		for _, key := range []string{"username", "nickname", "phone", "email", "bio", "avatar", "password"} {
			value, present := fields[key]
			if !present {
				continue
			}
			v := deref(value)
			var next any
			if value != nil {
				next = *value
			}
			if key == "username" {
				if v == "" {
					continue
				}
				if !usernameRE.MatchString(v) {
					return adminInputError("用户ID只能包含字母、数字和下划线，长度1-20位")
				}
				next = v
			}
			if key == "password" {
				if v == "" {
					continue
				}
				if msg := adminPasswordError(v, false); msg != "" {
					return adminInputError(msg)
				}
				h, e := adminHash(v)
				if e != nil {
					return e
				}
				next = h
			} else {
				if key != "nickname" && v == "" {
					next = nil
				}
				if (old[key] == nil && next == nil) || (old[key] != nil && old[key] == v) {
					continue
				}
			}
			args = append(args, next)
			sets = append(sets, `"`+key+`"=$`+itoa(len(args)))
			changes = append(changes, key)
			if key != "password" {
				profileChanged = true
			}
			if key == "password" {
				if _, e := tx.Exec(r.Context(), `DELETE FROM "UserSession" WHERE "userId"=$1`, id); e != nil {
					return e
				}
			}
		}
		if len(sets) == 0 {
			return adminInputError("没有需要修改的内容")
		}
		sets = append(sets, `"updatedAt"=NOW()`)
		if e := tx.QueryRow(r.Context(), `UPDATE "User" u SET `+strings.Join(sets, ",")+` WHERE "id"=$1 RETURNING `+adminUserJSON, args...).Scan(&result); e != nil {
			return e
		}
		return adminAudit(r.Context(), tx, a, "编辑用户", "user:"+id, "更新字段: "+strings.Join(changes, ","), adminClientIP(r))
	})
	var invalid adminInputError
	if errors.As(e, &invalid) {
		groupBad(w, string(invalid))
		return
	}
	if e != nil {
		adminWriteError(w, e)
		return
	}
	if profileChanged {
		if profile, err := s.findProfile(r.Context(), id); err == nil {
			s.publishProfileUpdated(r.Context(), profile, time.Now())
		}
	}
	writeJSON(w, 200, map[string]any{"success": true, "user": adminJSONDates(result)})
}

type adminInputError string

func (e adminInputError) Error() string { return string(e) }
func (s *Server) adminBanUser(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	if !adminWriteAllowed(w, a) {
		return
	}
	var in struct {
		Ban    *bool
		Reason string
	}
	if decode(r, &in) != nil || in.Ban == nil {
		groupBad(w, "ban 必须为布尔值")
		return
	}
	id := r.PathValue("id")
	var result json.RawMessage
	if *in.Ban && in.Reason == "" {
		in.Reason = "管理员操作"
	}
	e := pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		if e := tx.QueryRow(r.Context(), `UPDATE "User" u SET "isBanned"=$2,"banReason"=CASE WHEN $2 THEN $3 ELSE NULL END,"updatedAt"=NOW() WHERE "id"=$1 RETURNING `+adminUserJSON, id, *in.Ban, in.Reason).Scan(&result); e != nil {
			return e
		}
		if *in.Ban {
			if _, e := tx.Exec(r.Context(), `DELETE FROM "UserSession" WHERE "userId"=$1`, id); e != nil {
				return e
			}
		}
		action := "解封用户"
		if *in.Ban {
			action = "封禁用户"
		}
		return adminAudit(r.Context(), tx, a, action, "user:"+id, in.Reason, adminClientIP(r))
	})
	if e != nil {
		adminWriteError(w, e)
		return
	}
	if *in.Ban && !s.adminInvalidatePresence(r.Context(), id) {
		writeJSON(w, 503, map[string]any{"error": "封禁已保存且会话已撤销，在线状态清理失败，请重试", "applied": true})
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "user": adminJSONDates(result), "sessionsRevoked": *in.Ban, "disconnectConfirmed": false})
}

// Current gateway forwards payloads but has no administrative socket-close consumer.
// This event notifies clients; it must not be reported as a confirmed socket close.
func (s *Server) adminInvalidatePresence(ctx context.Context, id string) bool {
	if s.redis == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.redis.Del(ctx, "user:online:"+id, "user:presence:"+id, "user:activeChat:"+id).Err() != nil {
		return false
	}
	data, _ := json.Marshal(map[string]any{"userId": id, "payload": map[string]string{"type": "session_revoked", "reason": "admin_action"}})
	return s.redis.Publish(ctx, imPushChannel, string(data)).Err() == nil
}
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	s.adminDelete(w, r, a, "User", "user:", "删除用户")
}
func (s *Server) adminDeleteMoment(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	s.adminDelete(w, r, a, "Moment", "moment:", "删除动态")
}
func (s *Server) adminDeleteComment(w http.ResponseWriter, r *http.Request, a adminAccount, _ string) {
	s.adminDelete(w, r, a, "MomentComment", "comment:", "删除评论")
}
func (s *Server) adminDelete(w http.ResponseWriter, r *http.Request, a adminAccount, table, prefix, action string) {
	if !adminWriteAllowed(w, a) {
		return
	}
	id := r.PathValue("id")
	e := pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		tag, e := tx.Exec(r.Context(), `DELETE FROM "`+table+`" WHERE "id"=$1`, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return adminAudit(r.Context(), tx, a, action, prefix+id, "", adminClientIP(r))
	})
	if e != nil {
		adminWriteError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"success": true})
}
