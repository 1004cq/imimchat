package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestAdminCredentials(t *testing.T) {
	password := "Aa1" + newID()
	if !adminVerify(password, adminLegacyHash(password)) || adminVerify(password+"x", adminLegacyHash(password)) {
		t.Fatal("legacy verification")
	}
	hash, err := adminHash(password)
	if err != nil {
		t.Fatal(err)
	}
	if !adminVerify(password, hash) || adminVerify(password+"x", hash) {
		t.Fatal("bcrypt verification")
	}
	long := strings.Repeat("x", 72)
	hash, err = adminHash(long + "a")
	if err != nil || !adminVerify(long+"b", hash) {
		t.Fatal("bcryptjs truncation compatibility")
	}
	for _, p := range []string{"short", "abcdefgh", "ABCDEFGH", "12345678", "Abcdefgh"} {
		if adminPasswordError(p, true) == "" {
			t.Fatal("weak password accepted")
		}
	}
	if adminPasswordError(password, true) != "" || adminPasswordError("abcdefgh", false) != "" {
		t.Fatal("Node password policy mismatch")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "admin_token", Value: "cookie"})
	r.Header.Set("Authorization", "Bearer bearer")
	if adminToken(r) != "bearer" {
		t.Fatal("Bearer precedence")
	}
	r.Header.Del("Authorization")
	if adminToken(r) != "cookie" {
		t.Fatal("cookie")
	}
	token, err := adminNewToken()
	if err != nil || len(token) != 48 {
		t.Fatal("token")
	}
	c := adminCookie(token, 7200)
	if !c.HttpOnly || c.Path != "/api/admin" || c.MaxAge != 7200 || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie policy")
	}
}

// Opt-in, isolated integration suite. Only a dedicated imim_admin_test database
// is accepted; migrations run in a fresh random schema, never in public.
func TestAdminIntegration(t *testing.T) {
	dsn := os.Getenv("ADMIN_TEST_DATABASE_URL")
	addr := os.Getenv("ADMIN_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("set ADMIN_TEST_DATABASE_URL and ADMIN_TEST_REDIS_ADDR for disposable integration services")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "imim_admin_test" {
		t.Fatal("refusing non-test database")
	}
	root, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := "admin_test_" + strings.ReplaceAll(newID(), "-", "")
	if _, err = root.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(ctx, `DROP SCHEMA "`+schema+`" CASCADE`)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations, err := filepath.Glob("../../../prisma/migrations/*/migration.sql")
	if err != nil || len(migrations) == 0 {
		t.Fatal("missing migrations", err)
	}
	for _, path := range migrations {
		sql, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(sql)); e != nil {
			t.Fatalf("migration %s: %v", filepath.Base(filepath.Dir(path)), e)
		}
	}
	rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer rc.Close()
	if err = rc.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, redis: rc}
	h := s.Handler()
	password := "Aa1" + newID()
	adminID := newID()
	name := "admin_" + newID()
	sql := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	sql(`INSERT INTO "AdminAccount"("id","username","password","updatedAt") VALUES($1,$2,$3,NOW())`, adminID, name, adminLegacyHash(password))
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if e := db.QueryRow(ctx, q, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	var token string
	ip := schema // unique rate-limit identity, no collision with another suite
	defer rc.Del(ctx, "admin:login:go:"+ip)
	request := func(method, path string, body any, auth string, want int) map[string]any {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", ip)
		if auth == "cookie" {
			r.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
		} else if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d; body %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(w.Body.String(), `"password"`) || strings.Contains(w.Body.String(), password) {
			t.Fatal("secret exposed")
		}
		return out
	}
	t.Run("login_and_shared_sessions", func(t *testing.T) {
		request("GET", "/api/admin/me", nil, "", 401)
		request("POST", "/api/admin/login", map[string]string{"username": name, "password": "invalid"}, "", 401)
		if count(`SELECT COUNT(*) FROM "AdminLog" WHERE "action"='登录失败' AND "adminId" IS NULL`) != 1 {
			t.Fatal("failed audit")
		}
		out := request("POST", "/api/admin/login", map[string]string{"username": name, "password": password}, "", 200)
		token = out["token"].(string)
		var hash string
		var ttl float64
		if e := db.QueryRow(ctx, `SELECT "password" FROM "AdminAccount" WHERE "id"=$1`, adminID).Scan(&hash); e != nil {
			t.Fatal(e)
		}
		if !strings.HasPrefix(hash, "$2") || !adminVerify(password, hash) {
			t.Fatal("upgrade")
		}
		if e := db.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM ("expiresAt"-"createdAt"))::float8 FROM "AdminSession" WHERE "token"=$1`, token).Scan(&ttl); e != nil || ttl != 7200 {
			t.Fatal("2h TTL", ttl, e)
		}
		request("GET", "/api/admin/me", nil, token, 200)
		request("GET", "/api/admin/me", nil, "cookie", 200)
		// Node sessions use this exact table, with no Go-specific state.
		sql(`INSERT INTO "AdminSession"("id","adminId","token","expiresAt") VALUES($1,$2,$3,NOW()+INTERVAL '1 hour')`, newID(), adminID, "node-session")
		request("GET", "/api/admin/me", nil, "node-session", 200)
		sql(`UPDATE "AdminSession" SET "expiresAt"=NOW()-INTERVAL '1 second' WHERE "token"='node-session'`)
		request("GET", "/api/admin/me", nil, "node-session", 401)
		request("GET", "/api/admin/not-migrated", nil, token, 501)
		request("GET", "/api/admin/not-migrated", nil, "", 401)
	})
	var uid string
	t.Run("user_create_search_edit_reset", func(t *testing.T) {
		request("POST", "/api/admin/users/create", map[string]string{"username": "bad name", "password": password}, token, 400)
		request("POST", "/api/admin/users/create", map[string]string{"username": "test_user", "password": "weak"}, token, 400)
		out := request("POST", "/api/admin/users/create", map[string]string{"username": "test_user", "password": password, "nickname": "search%literal"}, token, 200)
		uid = out["user"].(map[string]any)["id"].(string)
		request("POST", "/api/admin/users/create", map[string]string{"username": "test_user", "password": password}, token, 409)
		request("GET", "/api/admin/users/"+uid, nil, token, 200)
		request("GET", "/api/admin/users/missing", nil, token, 404)
		out = request("GET", "/api/admin/users?search=%25&pageSize=1", nil, token, 200)
		if out["total"] != float64(1) {
			t.Fatal("literal search", out)
		}
		out = request("GET", "/api/admin/users?search=%27%20OR%201%3D1--", nil, token, 200)
		if out["total"] != float64(0) {
			t.Fatal("search injection")
		}
		sql(`INSERT INTO "UserSession"("id","userId","token","expiresAt") VALUES($1,$2,$3,NOW()+INTERVAL '1 day'),($4,$2,$5,NOW()+INTERVAL '1 day')`, newID(), uid, newID(), newID(), newID())
		request("PUT", "/api/admin/users/"+uid, map[string]string{"nickname": "updated", "password": password + "x"}, token, 200)
		if count(`SELECT COUNT(*) FROM "UserSession" WHERE "userId"=$1`, uid) != 0 {
			t.Fatal("reset did not revoke all sessions")
		}
		var hash string
		db.QueryRow(ctx, `SELECT "password" FROM "User" WHERE "id"=$1`, uid).Scan(&hash)
		if !adminVerify(password+"x", hash) {
			t.Fatal("reset hash")
		}
		request("PUT", "/api/admin/users/"+uid, map[string]string{"nickname": "updated"}, token, 400)
		request("PUT", "/api/admin/users/"+uid, map[string]any{"nickname": nil}, token, 200)
		request("PUT", "/api/admin/users/"+uid, map[string]any{"nickname": nil}, token, 400)
		request("PUT", "/api/admin/users/"+uid, map[string]string{"nickname": "updated"}, token, 200)

	})
	t.Run("role_and_atomic_audit", func(t *testing.T) {
		sql(`UPDATE "AdminAccount" SET "role"='moderator' WHERE "id"=$1`, adminID)
		request("GET", "/api/admin/users", nil, token, 200)
		for _, item := range [][2]string{{"POST", "/api/admin/users/create"}, {"PUT", "/api/admin/users/" + uid}, {"POST", "/api/admin/users/" + uid + "/ban"}, {"DELETE", "/api/admin/users/" + uid}, {"DELETE", "/api/admin/moments/x"}, {"DELETE", "/api/admin/comments/x"}} {
			request(item[0], item[1], map[string]any{}, token, 403)
		}
		sql(`UPDATE "AdminAccount" SET "role"='admin' WHERE "id"=$1`, adminID)
		sql(`ALTER TABLE "AdminLog" ADD CONSTRAINT test_audit_failure CHECK ("action" <> '编辑用户') NOT VALID`)
		sql(`INSERT INTO "UserSession"("id","userId","token","expiresAt") VALUES($1,$2,$3,NOW()+INTERVAL '1 day')`, newID(), uid, newID())
		request("PUT", "/api/admin/users/"+uid, map[string]string{"nickname": "rollback", "password": password}, token, 500)
		if count(`SELECT COUNT(*) FROM "User" WHERE "id"=$1 AND "nickname"='updated'`, uid) != 1 || count(`SELECT COUNT(*) FROM "UserSession" WHERE "userId"=$1`, uid) != 1 {
			t.Fatal("audit failure must rollback profile and sessions")
		}
		sql(`ALTER TABLE "AdminLog" DROP CONSTRAINT test_audit_failure`)
	})
	t.Run("ban_presence_and_dashboard", func(t *testing.T) {
		sql(`INSERT INTO "Chat"("id","participantA","participantB","updatedAt") VALUES('test-chat',$1,'other',NOW())`, uid)
		sql(`INSERT INTO "PrivateMessage"("id","chatId","senderId","content","msgType") VALUES('test-pm','test-chat',$1,'PRIVATE_CONTENT_MUST_NOT_LEAK','encrypted')`, uid)
		sql(`INSERT INTO "Group"("id","name","ownerId","updatedAt") VALUES('test-group','fixture',$1,NOW())`, uid)
		sql(`INSERT INTO "GroupMessage"("id","groupId","seq","senderId","content","msgType") VALUES('test-gm','test-group',1,$1,'GROUP_CONTENT_MUST_NOT_LEAK','encrypted')`, uid)

		key := "user:online:" + uid
		rc.Set(ctx, key, "test-node", time.Minute)
		defer rc.Del(ctx, key)
		// > SCAN count ensures multiple cursor pages are exercised; unrelated keys excluded.
		keys := []string{}
		for i := 0; i < 220; i++ {
			k := fmt.Sprintf("user:online:%s:%d", schema, i)
			keys = append(keys, k)
			rc.Set(ctx, k, "test-node", time.Minute)
		}
		defer rc.Del(ctx, keys...)
		before, ok := s.adminOnline(ctx)
		if !ok || before < 221 {
			t.Fatal("SCAN count", before)
		}
		out := request("GET", "/api/admin/dashboard", nil, token, 200)
		if out["overview"].(map[string]any)["totalMessages"] != float64(2) || len(out["messageTypes"].([]any)) != 1 {
			t.Fatal("message metadata aggregate", out["overview"])
		}
		serialized, _ := json.Marshal(out)
		if strings.Contains(string(serialized), "CONTENT_MUST_NOT_LEAK") {
			t.Fatal("message body leaked")
		}
		if out["overview"].(map[string]any)["onlineUsers"] != float64(before) {
			t.Fatal("dashboard online")
		}
		pub := rc.Subscribe(ctx, imPushChannel)
		defer pub.Close()
		if _, err := pub.Receive(ctx); err != nil {
			t.Fatal(err)
		}
		out = request("POST", "/api/admin/users/"+uid+"/ban", map[string]any{"ban": true, "reason": "test"}, token, 200)
		if out["disconnectConfirmed"] != false {
			t.Fatal("unverified disconnect reported")
		}
		if count(`SELECT COUNT(*) FROM "User" WHERE "id"=$1 AND "isBanned"`, uid) != 1 || count(`SELECT COUNT(*) FROM "UserSession" WHERE "userId"=$1`, uid) != 0 || rc.Exists(ctx, key).Val() != 0 {
			t.Fatal("ban invariants")
		}
		eventCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		msg, e := pub.ReceiveMessage(eventCtx)
		if e != nil || !strings.Contains(msg.Payload, "session_revoked") {
			t.Fatal("revocation notification", e)
		}
		request("GET", "/api/admin/users?status=banned", nil, token, 200)
		request("POST", "/api/admin/users/"+uid+"/ban", map[string]bool{"ban": false}, token, 200)
		request("POST", "/api/admin/users/"+uid+"/ban", map[string]string{"ban": "yes"}, token, 400)
		failedRedis := redis.NewClient(&redis.Options{Addr: addr})
		failedRedis.Close()
		s.redis = failedRedis
		out = request("GET", "/api/admin/dashboard", nil, token, 200)
		if out["overview"].(map[string]any)["onlineUsers"] != float64(0) || !strings.Contains(fmt.Sprint(out["unavailableMetrics"]), "onlineUsers") {
			t.Fatal("missing Redis metric")
		}
		request("POST", "/api/admin/users/"+uid+"/ban", map[string]bool{"ban": true}, token, 503)
		s.redis = rc
		request("POST", "/api/admin/users/"+uid+"/ban", map[string]bool{"ban": false}, token, 200)
		// Prisma dates are UTC timestamp-without-time-zone; day bounds must
		// convert local midnight to UTC rather than discard its zone.
		zone := time.FixedZone("test +8", 8*60*60)
		now := time.Now().In(zone)
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 30, 0, 0, zone)
		sql(`UPDATE "User" SET "createdAt"=$2 WHERE "id"=$1`, uid, midnight.UTC())
		daily := s.adminDailyAt(ctx, new([]string), now)
		if daily[len(daily)-1]["newUsers"] != int64(1) {
			t.Fatal("daily UTC boundary", daily)
		}
		sql(`ALTER TABLE "GroupMessage" RENAME TO "UnavailableGroupMessage"`)
		out = request("GET", "/api/admin/dashboard", nil, token, 200)
		if out["overview"].(map[string]any)["totalMessages"] != float64(0) || !strings.Contains(fmt.Sprint(out["unavailableMetrics"]), "totalMessages") {
			t.Fatal("missing SQL metric")
		}
		sql(`ALTER TABLE "UnavailableGroupMessage" RENAME TO "GroupMessage"`)
	})
	t.Run("moments_comments_and_delete", func(t *testing.T) {
		sql(`INSERT INTO "Moment"("id","userId","content","updatedAt") VALUES('moment-test',$1,'moderation fixture',NOW())`, uid)
		sql(`INSERT INTO "MomentMedia"("id","momentId","type","url") VALUES('media-a','moment-test','image','/fixture/a'),('media-b','moment-test','image','/fixture/b')`)
		sql(`INSERT INTO "MomentComment"("id","momentId","userId","content","updatedAt") VALUES('comment-test','moment-test',$1,'fixture',NOW())`, uid)
		out := request("GET", "/api/admin/moments?search=fixture", nil, token, 200)
		items := out["moments"].([]any)
		if len(items) != 1 || len(items[0].(map[string]any)["media"].([]any)) != 1 {
			t.Fatal("list projection")
		}
		out = request("GET", "/api/admin/moments/moment-test", nil, token, 200)
		if len(out["moment"].(map[string]any)["media"].([]any)) != 2 {
			t.Fatal("detail media")
		}
		out = request("GET", "/api/admin/moments/moment-test/comments", nil, token, 200)
		if out["total"] != float64(1) {
			t.Fatal("comments")
		}
		request("DELETE", "/api/admin/comments/comment-test", nil, token, 200)
		request("DELETE", "/api/admin/comments/comment-test", nil, token, 404)
		request("DELETE", "/api/admin/moments/moment-test", nil, token, 200)
		if count(`SELECT COUNT(*) FROM "MomentMedia"`) != 0 {
			t.Fatal("media cascade")
		}
		request("GET", "/api/admin/moments/moment-test", nil, token, 404)
		request("GET", "/api/admin/moments/moment-test/comments", nil, token, 404)
		request("DELETE", "/api/admin/users/"+uid, nil, token, 200)
		request("DELETE", "/api/admin/users/"+uid, nil, token, 404)
	})
	t.Run("audit_logout_rate_limit", func(t *testing.T) {
		out := request("GET", "/api/admin/logs", nil, token, 200)
		if out["total"].(float64) < 10 {
			t.Fatal("missing mutation audits")
		}
		if count(`SELECT COUNT(*) FROM "AdminLog" WHERE "action"<>'登录失败' AND ("adminId" IS NULL OR "target" IS NULL OR "ip" IS NULL)`) != 0 {
			t.Fatal("incomplete audit identity")
		}
		request("POST", "/api/admin/logout", nil, "cookie", 200)
		request("GET", "/api/admin/me", nil, token, 401)
		for i := 0; i < 6; i++ {
			want := 401
			if i == 5 {
				want = 429
			}
			request("POST", "/api/admin/login", map[string]string{"username": name, "password": "invalid"}, "", want)
		}
		if count(`SELECT COUNT(*) FROM "AdminLog" WHERE "action"='登录失败'`) != 7 {
			t.Fatal("failed/throttled login audit")
		}
	})
}

func TestAdminJSONDates(t *testing.T) {
	raw := json.RawMessage(`{"createdAt":"2026-11-06T03:22:55.538","updatedAt":"2026-11-06T11:22:55.538+08:00","pinnedAt":null,"content":"2026-11-06T03:22:55.538","media":[{"createdAt":"2026-11-06T03:22:55"}]}`)
	var value map[string]any
	if err := json.Unmarshal(adminJSONDates(raw), &value); err != nil {
		t.Fatal(err)
	}
	if value["createdAt"] != "2026-11-06T03:22:55.538Z" || value["updatedAt"] != value["createdAt"] || value["pinnedAt"] != nil || value["content"] != "2026-11-06T03:22:55.538" {
		t.Fatal("UTC serialization changed values", value)
	}
	if value["media"].([]any)[0].(map[string]any)["createdAt"] != "2026-11-06T03:22:55.000Z" {
		t.Fatal("nested media date")
	}
}
