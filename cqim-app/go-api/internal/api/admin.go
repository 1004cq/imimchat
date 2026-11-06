package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type adminAccount struct{ ID, Username, Role string }

// Explicitly migrated admin routes; all other families retain JSON 501.
func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", s.adminLogin)
	mux.HandleFunc("POST /api/admin/logout", s.requireAdmin(s.adminLogout))
	mux.HandleFunc("GET /api/admin/me", s.requireAdmin(s.adminMe))
	mux.HandleFunc("GET /api/admin/dashboard", s.requireAdmin(s.adminDashboard))
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.adminUsers))
	mux.HandleFunc("GET /api/admin/users/{id}", s.requireAdmin(s.adminUser))
	mux.HandleFunc("POST /api/admin/users/{id}/ban", s.requireAdmin(s.adminBanUser))
	mux.HandleFunc("POST /api/admin/users/create", s.requireAdmin(s.adminCreateUser))
	mux.HandleFunc("PUT /api/admin/users/{id}", s.requireAdmin(s.adminEditUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.adminDeleteUser))
	mux.HandleFunc("GET /api/admin/moments", s.requireAdmin(s.adminMoments))
	mux.HandleFunc("GET /api/admin/moments/{id}", s.requireAdmin(s.adminMoment))
	mux.HandleFunc("DELETE /api/admin/moments/{id}", s.requireAdmin(s.adminDeleteMoment))
	mux.HandleFunc("GET /api/admin/moments/{id}/comments", s.requireAdmin(s.adminComments))
	mux.HandleFunc("DELETE /api/admin/comments/{id}", s.requireAdmin(s.adminDeleteComment))
	mux.HandleFunc("GET /api/admin/logs", s.requireAdmin(s.adminLogs))
}

func adminToken(r *http.Request) string {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	if cookie, err := r.Cookie("admin_token"); err == nil {
		return cookie.Value
	}
	return ""
}

func (s *Server) findAdmin(ctx context.Context, token string) (adminAccount, error) {
	if s.db == nil {
		return adminAccount{}, errors.New("database unavailable")
	}
	var a adminAccount
	err := s.db.QueryRow(ctx, `SELECT a."id",a."username",a."role" FROM "AdminSession" s JOIN "AdminAccount" a ON a."id"=s."adminId" WHERE s."token"=$1 AND s."expiresAt">NOW() LIMIT 1`, token).Scan(&a.ID, &a.Username, &a.Role)
	return a, err
}

func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request, adminAccount, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		token := adminToken(r)
		if token == "" {
			writeJSON(w, 401, map[string]string{"error": "未登录"})
			return
		}
		a, e := s.findAdmin(r.Context(), token)
		if e == pgx.ErrNoRows {
			_, _ = s.db.Exec(r.Context(), `DELETE FROM "AdminSession" WHERE "token"=$1`, token)
			writeJSON(w, 401, map[string]string{"error": "登录已过期"})
			return
		}
		if e != nil {
			writeJSON(w, 503, map[string]string{"error": "认证服务不可用"})
			return
		}
		next(w, r, a, token)
	}
}

func adminAllowed(a adminAccount) bool { return a.Role == "superadmin" || a.Role == "admin" }
func adminClientIP(r *http.Request) string {
	if raw := r.Header.Get("X-Forwarded-For"); raw != "" {
		return strings.TrimSpace(strings.Split(raw, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func adminNewToken() (string, error) {
	b := make([]byte, 36)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func adminLegacyHash(password string) string {
	h := sha256.Sum256([]byte(password + "cqim_salt_2024"))
	return hex.EncodeToString(h[:])
}
func adminVerify(password, hash string) bool {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		p := []byte(password)
		if len(p) > 72 {
			p = p[:72]
		}
		return bcrypt.CompareHashAndPassword([]byte(hash), p) == nil
	}
	return len(hash) == 64 && subtle.ConstantTimeCompare([]byte(adminLegacyHash(password)), []byte(hash)) == 1
}
func (s *Server) adminMe(w http.ResponseWriter, _ *http.Request, a adminAccount, _ string) {
	writeJSON(w, 200, map[string]string{"id": a.ID, "username": a.Username, "role": a.Role})
}

func (s *Server) adminCount(ctx context.Context, table string, unavailable *[]string, metric string) int64 {
	var n int64
	if e := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); e != nil {
		*unavailable = append(*unavailable, metric)
		return 0
	}
	return n
}
func (s *Server) adminOnline(ctx context.Context) (int64, bool) {
	if s.redis == nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var cursor uint64
	seen := map[string]bool{}
	for {
		keys, next, e := s.redis.Scan(ctx, cursor, "user:online:*", 200).Result()
		if e != nil {
			return 0, false
		}
		for _, k := range keys {
			seen[k] = true
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return int64(len(seen)), true
}

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	unavailable := []string{}
	ctx := r.Context()
	totalUsers := s.adminCount(ctx, `"User"`, &unavailable, "totalUsers")
	banned := int64(0)
	if e := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "User" WHERE "isBanned"=true`).Scan(&banned); e != nil {
		unavailable = append(unavailable, "bannedUsers")
	}
	moments := s.adminCount(ctx, `"Moment"`, &unavailable, "totalMoments")
	comments := s.adminCount(ctx, `"MomentComment"`, &unavailable, "totalComments")
	media := s.adminCount(ctx, `"MediaFile"`, &unavailable, "totalMedia")
	privateMsgs := s.adminCount(ctx, `"PrivateMessage"`, &unavailable, "privateMessages")
	groupMsgs := s.adminCount(ctx, `"GroupMessage"`, &unavailable, "groupMessages")
	ipBlacklist := s.adminCount(ctx, `"IpBlacklist"`, &unavailable, "totalIpBlacklist")
	announcements := s.adminCount(ctx, `"Announcement"`, &unavailable, "totalAnnouncements")
	pendingReports := int64(0)
	if e := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "Report" WHERE "status"='pending'`).Scan(&pendingReports); e != nil {
		unavailable = append(unavailable, "pendingReports")
	}
	sensitive := int64(0)
	if e := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "SensitiveWord" WHERE "isActive"=true`).Scan(&sensitive); e != nil {
		unavailable = append(unavailable, "totalSensitiveWords")
	}
	loginLogs := s.adminCount(ctx, `"LoginLog"`, &unavailable, "totalLoginLogs")
	illegal := s.adminCount(ctx, `"IllegalRequest"`, &unavailable, "totalIllegalRequests")
	online, ok := s.adminOnline(ctx)
	if !ok {
		unavailable = append(unavailable, "onlineUsers")
	}
	daily := s.adminDaily(ctx, &unavailable)
	types := s.adminMessageTypes(ctx, &unavailable)
	recentUsers := s.adminRecentUsers(ctx, &unavailable)
	recentMoments := s.adminRecentMoments(ctx, &unavailable)
	recentLogins := s.adminRecentLogins(ctx, &unavailable)
	totalMessages := privateMsgs + groupMsgs
	if slices.Contains(unavailable, "privateMessages") || slices.Contains(unavailable, "groupMessages") {
		totalMessages = 0
		unavailable = append(unavailable, "totalMessages")
	}
	slices.Sort(unavailable)
	unavailable = slices.Compact(unavailable)
	writeJSON(w, 200, map[string]any{"overview": map[string]any{"totalUsers": totalUsers, "onlineUsers": online, "bannedUsers": banned, "totalMessages": totalMessages, "pendingReports": pendingReports, "totalSensitiveWords": sensitive, "totalAnnouncements": announcements}, "dailyStats": daily, "messageTypes": types, "stats": map[string]any{"totalUsers": totalUsers, "bannedUsers": banned, "totalMoments": moments, "totalComments": comments, "totalMedia": media, "totalLoginLogs": loginLogs, "totalIpBlacklist": ipBlacklist, "totalIllegalRequests": illegal}, "recentUsers": recentUsers, "recentMoments": recentMoments, "recentLogins": recentLogins, "unavailableMetrics": unavailable})
}

func (s *Server) adminDaily(ctx context.Context, unavailable *[]string) []map[string]any {
	return s.adminDailyAt(ctx, unavailable, time.Now().Local())
}
func (s *Server) adminDailyAt(ctx context.Context, unavailable *[]string, now time.Time) []map[string]any {
	start := now
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location()).AddDate(0, 0, -6)
	out := []map[string]any{}
	for i := 0; i < 7; i++ {
		from := start.AddDate(0, 0, i)
		to := from.AddDate(0, 0, 1)
		var users, pm, gm, active int64
		e1 := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "User" WHERE "createdAt">=$1 AND "createdAt"<$2`, from.UTC(), to.UTC()).Scan(&users)
		e2 := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "PrivateMessage" WHERE "createdAt">=$1 AND "createdAt"<$2`, from.UTC(), to.UTC()).Scan(&pm)
		e3 := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM "GroupMessage" WHERE "createdAt">=$1 AND "createdAt"<$2`, from.UTC(), to.UTC()).Scan(&gm)
		e4 := s.db.QueryRow(ctx, `SELECT COUNT(DISTINCT "senderId") FROM (SELECT "senderId" FROM "PrivateMessage" WHERE "createdAt">=$1 AND "createdAt"<$2 UNION SELECT "senderId" FROM "GroupMessage" WHERE "createdAt">=$1 AND "createdAt"<$2) t`, from.UTC(), to.UTC()).Scan(&active)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			*unavailable = append(*unavailable, "dailyStats")
		}
		if e2 != nil || e3 != nil {
			pm, gm = 0, 0
		}
		out = append(out, map[string]any{"date": from.Format("1/2"), "newUsers": users, "activeUsers": active, "messages": pm + gm})
	}
	return out
}
func (s *Server) adminMessageTypes(ctx context.Context, unavailable *[]string) []map[string]any {
	rows, e := s.db.Query(ctx, `SELECT "msgType",SUM(n) FROM (SELECT "msgType",COUNT(*)::bigint n FROM "PrivateMessage" GROUP BY "msgType" UNION ALL SELECT "msgType",COUNT(*)::bigint n FROM "GroupMessage" GROUP BY "msgType") x GROUP BY "msgType" ORDER BY SUM(n) DESC`)
	if e != nil {
		*unavailable = append(*unavailable, "messageTypes")
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var typ string
		var n int64
		if e = rows.Scan(&typ, &n); e != nil {
			*unavailable = append(*unavailable, "messageTypes")
			return []map[string]any{}
		} else {
			out = append(out, map[string]any{"type": typ, "count": n})
		}
	}
	if rows.Err() != nil {
		*unavailable = append(*unavailable, "messageTypes")
		return []map[string]any{}
	}
	return out
}

// Optional dashboard datasets fail independently and are explicitly marked unavailable.
func (s *Server) adminRecentRows(ctx context.Context, query, metric string, unavailable *[]string) []json.RawMessage {
	rows, err := s.db.Query(ctx, query)
	out := []json.RawMessage{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var data json.RawMessage
			if err = rows.Scan(&data); err != nil {
				break
			}
			out = append(out, adminJSONDates(data))
		}
		if err == nil {
			err = rows.Err()
		}
	}
	if err != nil {
		*unavailable = append(*unavailable, metric)
		return []json.RawMessage{}
	}
	return out
}
func (s *Server) adminRecentUsers(ctx context.Context, unavailable *[]string) []json.RawMessage {
	return s.adminRecentRows(ctx, `SELECT to_jsonb(x) FROM (SELECT "id","username","nickname","email","isBanned","createdAt" FROM "User" ORDER BY "createdAt" DESC LIMIT 5) x`, "recentUsers", unavailable)
}
func (s *Server) adminRecentMoments(ctx context.Context, unavailable *[]string) []json.RawMessage {
	return s.adminRecentRows(ctx, `SELECT to_jsonb(m)||jsonb_build_object('user',jsonb_build_object('username',u."username",'nickname',u."nickname")) FROM "Moment" m JOIN "User" u ON u."id"=m."userId" ORDER BY m."createdAt" DESC LIMIT 5`, "recentMoments", unavailable)
}
func (s *Server) adminRecentLogins(ctx context.Context, unavailable *[]string) []json.RawMessage {
	return s.adminRecentRows(ctx, `SELECT to_jsonb(x) FROM (SELECT "id","userId","username","email","phone","ip","userAgent","country","city","success","failReason","loginType","createdAt" FROM "LoginLog" ORDER BY "createdAt" DESC LIMIT 5) x`, "recentLogins", unavailable)
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	search := r.URL.Query().Get("search")
	page, size := adminPage(r)
	status := groupQuery(r, "status")
	where := `WHERE true`
	args := []any{}
	if search != "" {
		args = append(args, search)
		p := "$" + strconv.Itoa(len(args))
		where += ` AND (strpos(u."username",` + p + `)>0 OR strpos(COALESCE(u."email",''),` + p + `)>0 OR strpos(COALESCE(u."nickname",''),` + p + `)>0 OR strpos(COALESCE(u."phone",''),` + p + `)>0)`
	}
	if status == "banned" || status == "active" {
		args = append(args, status == "banned")
		where += ` AND u."isBanned"=$` + strconv.Itoa(len(args))
	}
	var total int
	if e := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM "User" u `+where, args...).Scan(&total); e != nil {
		dbError(w, e)
		return
	}
	args = append(args, size, (page-1)*size)
	q := `SELECT u."id",u."username",u."nickname",u."email",u."phone",u."avatar",u."bio",u."isBanned",u."banReason",u."role",u."createdAt",u."updatedAt",u."lastLoginAt",u."lastLoginIp",(SELECT COUNT(*) FROM "Moment" m WHERE m."userId"=u."id") FROM "User" u ` + where + ` ORDER BY u."createdAt" DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, e := s.db.Query(r.Context(), q, args...)
	if e != nil {
		dbError(w, e)
		return
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var id, name, role string
		var nick, email, phone, av, bio, reason, lastIP *string
		var banned bool
		var created, updated time.Time
		var last *time.Time
		var posts int
		if e = rows.Scan(&id, &name, &nick, &email, &phone, &av, &bio, &banned, &reason, &role, &created, &updated, &last, &lastIP, &posts); e != nil {
			dbError(w, e)
			return
		} else {
			users = append(users, map[string]any{"id": id, "username": name, "nickname": nick, "email": email, "phone": phone, "avatar": av, "bio": bio, "isBanned": banned, "banReason": reason, "role": role, "createdAt": created, "updatedAt": updated, "lastLoginAt": last, "lastLoginIp": lastIP, "postsCount": posts, "_count": map[string]int{"moments": posts}})
		}
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	writeJSON(w, 200, map[string]any{"users": users, "total": total, "page": page, "pageSize": size})
}
func (s *Server) adminUser(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	id := r.PathValue("id")
	var name, role string
	var nick, email, phone, av, bio, reason, lastIP *string
	var banned bool
	var created, updated time.Time
	var last *time.Time
	var posts int
	e := s.db.QueryRow(r.Context(), `SELECT u."username",u."role",u."nickname",u."email",u."phone",u."avatar",u."bio",u."isBanned",u."banReason",u."createdAt",u."updatedAt",u."lastLoginAt",u."lastLoginIp",(SELECT COUNT(*) FROM "Moment" m WHERE m."userId"=u."id") FROM "User" u WHERE u."id"=$1`, id).Scan(&name, &role, &nick, &email, &phone, &av, &bio, &banned, &reason, &created, &updated, &last, &lastIP, &posts)
	if e == pgx.ErrNoRows {
		writeJSON(w, 404, map[string]string{"error": "用户不存在"})
		return
	}
	if e != nil {
		dbError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"user": map[string]any{"id": id, "username": name, "nickname": nick, "email": email, "phone": phone, "avatar": av, "bio": bio, "isBanned": banned, "banReason": reason, "role": role, "createdAt": created, "updatedAt": updated, "lastLoginAt": last, "lastLoginIp": lastIP, "postsCount": posts, "_count": map[string]int{"moments": posts}}})
}
func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	page, _ := adminPage(r)
	var total int
	if e := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM "AdminLog"`).Scan(&total); e != nil {
		dbError(w, e)
		return
	}
	rows, e := s.db.Query(r.Context(), `SELECT "id","adminId","adminName","action","target","detail","ip","createdAt" FROM "AdminLog" ORDER BY "createdAt" DESC LIMIT 20 OFFSET $1`, (page-1)*20)
	if e != nil {
		dbError(w, e)
		return
	}
	defer rows.Close()
	logs := []map[string]any{}
	for rows.Next() {
		var id, action string
		var aid, name, target, detail, ip *string
		var created time.Time
		if e = rows.Scan(&id, &aid, &name, &action, &target, &detail, &ip, &created); e != nil {
			dbError(w, e)
			return
		} else {
			logs = append(logs, map[string]any{"id": id, "adminId": aid, "adminName": name, "action": action, "target": target, "detail": detail, "ip": ip, "createdAt": created})
		}
	}
	if rows.Err() != nil {
		dbError(w, rows.Err())
		return
	}
	writeJSON(w, 200, map[string]any{"logs": logs, "total": total, "page": page})
}
