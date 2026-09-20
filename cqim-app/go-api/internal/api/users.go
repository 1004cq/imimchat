package api

// User profile routes used by chat header, avatars, and settings.
// JSON shapes match cqim-app/server/index.ts, auth.ts, and home.ts.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{1,20}$`)

func (s *Server) registerUserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/users/search", s.usersSearch)
	mux.HandleFunc("GET /api/users/{userId}/presence", s.userPresence)
	mux.HandleFunc("GET /api/users/{userId}", s.userByID)
	mux.HandleFunc("GET /api/q/profile/{userId}", s.publicProfile)
	mux.HandleFunc("GET /api/profile", s.getProfile)
	mux.HandleFunc("PUT /api/profile", s.putProfile)
	mux.HandleFunc("PUT /api/auth/profile", s.requireUser(s.authPutProfile))
	mux.HandleFunc("GET /api/user/me", s.requireUser(s.userMe))
	mux.HandleFunc("GET /api/home/sync", s.requireUser(s.homeSync))
}

// safeAvatarUrl matches server/index.ts: keep /api/media and non-COS https URLs.
func safeAvatarUrl(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/api/media/") || strings.HasPrefix(raw, "/") {
		return raw
	}
	if strings.HasPrefix(raw, "https://") && !strings.Contains(raw, ".cos.") && !strings.Contains(raw, ".myqcloud.com") {
		return raw
	}
	return ""
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Server) optionalUser(r *http.Request) *user {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return nil
	}
	u, err := s.findSessionUser(r.Context(), token)
	if err != nil {
		return nil
	}
	return &u
}

func specialUser(userID string) map[string]any {
	if userID == "official" {
		return map[string]any{
			"id": "official", "username": "admin", "nickname": "imim 官方",
			"avatar": "/imim-official-avatar.jpg", "bio": "imim 官方账号",
			"online": true, "lastSeen": nil,
			"devices": []map[string]string{{"deviceType": "server", "browser": "System", "os": "imim Cloud"}},
		}
	}
	if userID == "BOT" {
		return map[string]any{
			"id": "BOT", "username": "BOT", "nickname": "imim AI",
			"avatar": "/imim-ai-avatar.jpg", "bio": "imim AI 助手",
			"online": true, "lastSeen": nil,
			"devices": []map[string]string{{"deviceType": "server", "browser": "AI Runtime", "os": "imim Cloud"}},
		}
	}
	return nil
}

func (s *Server) usersSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入至少1个字符"})
		return
	}
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "搜索失败"})
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT "id","username",COALESCE("nickname",''),COALESCE("avatar",''),COALESCE("bio",'')
		FROM "User"
		WHERE "isBanned"=false AND ("id"=$1 OR "username"=$1 OR "phone"=$1 OR "email"=$1)
		LIMIT 10`, q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "搜索失败"})
		return
	}
	defer rows.Close()
	users := []map[string]any{}
	for rows.Next() {
		var id, username, nickname, avatar, bio string
		if rows.Scan(&id, &username, &nickname, &avatar, &bio) != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "搜索失败"})
			return
		}
		if nickname == "" {
			nickname = username
		}
		users = append(users, map[string]any{
			"id": id, "username": username, "nickname": nickname,
			"avatar": safeAvatarUrl(avatar), "bio": bio,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) userByID(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if special := specialUser(userID); special != nil {
		writeJSON(w, http.StatusOK, special)
		return
	}
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "服务器错误"})
		return
	}
	var id, username, nickname, avatar, bio, background string
	err := s.db.QueryRow(r.Context(), `
		SELECT "id","username",COALESCE("nickname",''),COALESCE("avatar",''),COALESCE("bio",''),COALESCE("backgroundUrl",'')
		FROM "User" WHERE "id"=$1`, userID).Scan(&id, &username, &nickname, &avatar, &bio, &background)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误"})
		return
	}
	if nickname == "" {
		nickname = username
	}
	online, lastSeen, devices := s.presenceFor(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "username": username, "nickname": nickname,
		"avatar": safeAvatarUrl(avatar), "bio": bio, "backgroundUrl": background,
		"online": online, "lastSeen": lastSeen, "devices": devices,
	})
}

func (s *Server) userPresence(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if userID == "official" || userID == "BOT" {
		osName := "imim Cloud"
		browser := "System"
		if userID == "BOT" {
			browser = "AI Runtime"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"userId": userID, "online": true, "lastSeen": nil,
			"devices": []map[string]string{{"deviceType": "server", "browser": browser, "os": osName}},
		})
		return
	}
	online, lastSeen, devices := s.presenceFor(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"userId": userID, "online": online, "lastSeen": lastSeen, "devices": devices,
	})
}

func (s *Server) presenceFor(ctx context.Context, userID string) (bool, any, []any) {
	online := false
	var lastSeen any
	devices := []any{}
	if s.redis == nil {
		return false, nil, devices
	}
	if n, err := s.redis.Exists(ctx, "online:"+userID).Result(); err == nil && n > 0 {
		online = true
	}
	if !online {
		if raw, err := s.redis.Get(ctx, "last_seen:"+userID).Result(); err == nil {
			if ts, convErr := strconv.ParseInt(raw, 10, 64); convErr == nil && ts > 0 {
				lastSeen = ts
			}
		}
	}
	if online {
		if values, err := s.redis.HGetAll(ctx, "devices:"+userID).Result(); err == nil {
			for _, raw := range values {
				var device map[string]any
				if json.Unmarshal([]byte(raw), &device) == nil {
					devices = append(devices, device)
				}
			}
		}
	}
	return online, lastSeen, devices
}

func (s *Server) publicProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "服务器错误"})
		return
	}
	var id, username, nickname, avatar, bio string
	var background *string
	var momentCount int
	err := s.db.QueryRow(r.Context(), `
		SELECT u."id",u."username",COALESCE(u."nickname",''),COALESCE(u."avatar",''),u."backgroundUrl",COALESCE(u."bio",''),
		       (SELECT COUNT(*) FROM "Moment" m WHERE m."userId"=u."id")
		FROM "User" u WHERE u."id"=$1`, userID).Scan(&id, &username, &nickname, &avatar, &background, &bio, &momentCount)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误"})
		return
	}
	name := nickname
	if name == "" {
		name = username
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile": map[string]any{
			"id": id, "name": name, "avatar": safeAvatarUrl(avatar),
			"backgroundUrl": background, "bio": bio, "momentCount": momentCount,
		},
	})
}

type profileRow struct {
	ID, Username, Nickname, Gender, Region, Birthday string
	Phone, Email, Avatar, Bio, Background            string
}

func (s *Server) findProfile(ctx context.Context, userID string) (profileRow, error) {
	var p profileRow
	query := `
		SELECT "id","username",COALESCE("nickname",''),COALESCE("gender",''),COALESCE("region",''),
		       COALESCE("birthday",''),COALESCE("phone",''),COALESCE("email",''),
		       COALESCE("avatar",''),COALESCE("bio",''),COALESCE("backgroundUrl",'')
		FROM "User" WHERE "id"=$1`
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&p.ID, &p.Username, &p.Nickname, &p.Gender, &p.Region, &p.Birthday,
		&p.Phone, &p.Email, &p.Avatar, &p.Bio, &p.Background,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, strings.Replace(query, `"id"=$1`, `"username"=$1`, 1), userID).Scan(
			&p.ID, &p.Username, &p.Nickname, &p.Gender, &p.Region, &p.Birthday,
			&p.Phone, &p.Email, &p.Avatar, &p.Bio, &p.Background,
		)
	}
	return p, err
}

func profileJSON(p profileRow) map[string]any {
	name := p.Nickname
	if name == "" {
		name = p.Username
	}
	return map[string]any{
		"id": p.ID, "username": p.Username, "name": name, "nickname": name,
		"gender": p.Gender, "region": p.Region, "phone": p.Phone, "email": p.Email,
		"wechatId": p.Username, "bio": p.Bio, "avatar": safeAvatarUrl(p.Avatar),
		"backgroundUrl": p.Background, "birthday": p.Birthday,
	}
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	current := s.optionalUser(r)
	if userID == "" || userID == "me" {
		if current == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		userID = current.ID
	}
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "服务器错误"})
		return
	}
	p, err := s.findProfile(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profileJSON(p)})
}

func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	current := s.optionalUser(r)
	var in struct {
		UserID        string  `json:"userId"`
		Name          string  `json:"name"`
		Nickname      string  `json:"nickname"`
		Gender        *string `json:"gender"`
		Region        *string `json:"region"`
		Phone         *string `json:"phone"`
		WechatID      *string `json:"wechatId"`
		Bio           *string `json:"bio"`
		Avatar        *string `json:"avatar"`
		BackgroundURL *string `json:"backgroundUrl"`
		Birthday      *string `json:"birthday"`
	}
	if decode(r, &in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求无效"})
		return
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" || userID == "me" {
		if current == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		userID = current.ID
	} else if current == nil || (current.ID != userID && current.Username != userID) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
		return
	}
	s.updateUserProfile(w, r, userID, profileUpdate{
		Nickname:      firstNonEmpty(in.Nickname, in.Name),
		Username:      deref(in.WechatID),
		Gender:        in.Gender,
		Region:        in.Region,
		Bio:           in.Bio,
		Avatar:        in.Avatar,
		BackgroundURL: in.BackgroundURL,
		Birthday:      in.Birthday,
	})
}

func (s *Server) authPutProfile(w http.ResponseWriter, r *http.Request, current user) {
	var in struct {
		Nickname      *string `json:"nickname"`
		Avatar        *string `json:"avatar"`
		BackgroundURL *string `json:"backgroundUrl"`
		Bio           *string `json:"bio"`
		Username      *string `json:"username"`
		Gender        *string `json:"gender"`
		Region        *string `json:"region"`
		Birthday      *string `json:"birthday"`
	}
	if decode(r, &in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求无效"})
		return
	}
	s.updateUserProfile(w, r, current.ID, profileUpdate{
		Nickname:      deref(in.Nickname),
		Username:      deref(in.Username),
		Gender:        in.Gender,
		Region:        in.Region,
		Bio:           in.Bio,
		Avatar:        in.Avatar,
		BackgroundURL: in.BackgroundURL,
		Birthday:      in.Birthday,
		HasNickname:   in.Nickname != nil,
		HasUsername:   in.Username != nil,
		AuthStyle:     true,
	})
}

type profileUpdate struct {
	Nickname, Username              string
	Gender, Region, Bio             *string
	Avatar, BackgroundURL, Birthday *string
	HasNickname, HasUsername        bool
	AuthStyle                       bool
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *Server) updateUserProfile(w http.ResponseWriter, r *http.Request, userID string, in profileUpdate) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "资料更新失败"})
		return
	}
	p, err := s.findProfile(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	sets := []string{`"updatedAt"=NOW()`}
	args := []any{}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, column+`=$`+strconv.Itoa(len(args)))
	}
	if in.AuthStyle {
		if in.HasNickname {
			add(`"nickname"`, in.Nickname)
			p.Nickname = in.Nickname
		}
		if in.Bio != nil {
			add(`"bio"`, *in.Bio)
			p.Bio = *in.Bio
		}
		if in.Avatar != nil {
			add(`"avatar"`, *in.Avatar)
			p.Avatar = *in.Avatar
		}
		if in.BackgroundURL != nil {
			bg := strings.TrimSpace(*in.BackgroundURL)
			if bg == "" {
				add(`"backgroundUrl"`, nil)
				p.Background = ""
			} else {
				add(`"backgroundUrl"`, bg)
				p.Background = bg
			}
		}
		if in.Gender != nil {
			add(`"gender"`, emptyToNil(*in.Gender))
			p.Gender = *in.Gender
		}
		if in.Region != nil {
			add(`"region"`, emptyToNil(*in.Region))
			p.Region = *in.Region
		}
		if in.Birthday != nil {
			add(`"birthday"`, emptyToNil(*in.Birthday))
			p.Birthday = *in.Birthday
		}
		if in.HasUsername && in.Username != "" && in.Username != p.Username {
			if !usernameRE.MatchString(in.Username) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "账号ID只能包含字母、数字和下划线，长度1-20位"})
				return
			}
			var other string
			dup := s.db.QueryRow(r.Context(), `SELECT "id" FROM "User" WHERE "username"=$1`, in.Username).Scan(&other)
			if dup == nil && other != p.ID {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "该账号ID已被使用"})
				return
			}
			if dup != nil && !errors.Is(dup, pgx.ErrNoRows) {
				dbError(w, dup)
				return
			}
			add(`"username"`, in.Username)
			p.Username = in.Username
		}
	} else {
		if in.Nickname != "" {
			add(`"nickname"`, in.Nickname)
			p.Nickname = in.Nickname
		}
		if in.Bio != nil {
			add(`"bio"`, *in.Bio)
			p.Bio = *in.Bio
		}
		if in.Avatar != nil {
			add(`"avatar"`, *in.Avatar)
			p.Avatar = *in.Avatar
		}
		if in.BackgroundURL != nil {
			add(`"backgroundUrl"`, emptyToNil(*in.BackgroundURL))
			p.Background = *in.BackgroundURL
		}
		if in.Gender != nil {
			add(`"gender"`, emptyToNil(*in.Gender))
			p.Gender = *in.Gender
		}
		if in.Region != nil {
			add(`"region"`, emptyToNil(*in.Region))
			p.Region = *in.Region
		}
		if in.Birthday != nil {
			add(`"birthday"`, emptyToNil(*in.Birthday))
			p.Birthday = *in.Birthday
		}
		if in.Username != "" && in.Username != p.Username {
			if !usernameRE.MatchString(in.Username) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "账号ID只能包含字母、数字和下划线，长度1-20位"})
				return
			}
			var other string
			dup := s.db.QueryRow(r.Context(), `SELECT "id" FROM "User" WHERE "username"=$1`, in.Username).Scan(&other)
			if dup == nil && other != p.ID {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "该账号ID已被使用"})
				return
			}
			if dup != nil && !errors.Is(dup, pgx.ErrNoRows) {
				dbError(w, dup)
				return
			}
			add(`"username"`, in.Username)
			p.Username = in.Username
		}
	}
	if len(args) == 0 {
		s.writeUpdatedProfile(w, p, time.Now())
		return
	}
	args = append(args, p.ID)
	q := `UPDATE "User" SET ` + strings.Join(sets, ",") + ` WHERE "id"=$` + strconv.Itoa(len(args)) + ` RETURNING "updatedAt"`
	var updatedAt time.Time
	if err := s.db.QueryRow(r.Context(), q, args...).Scan(&updatedAt); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "该账号ID已被使用"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "资料更新失败"})
		return
	}
	s.publishProfileUpdated(r.Context(), p, updatedAt)
	s.writeUpdatedProfile(w, p, updatedAt)
}

func (s *Server) writeUpdatedProfile(w http.ResponseWriter, p profileRow, updatedAt time.Time) {
	name := p.Nickname
	if name == "" {
		name = p.Username
	}
	avatar := safeAvatarUrl(p.Avatar)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"user": map[string]any{
			"id": p.ID, "username": p.Username, "nickname": p.Nickname,
			"phone": p.Phone, "email": p.Email, "avatar": avatar,
			"backgroundUrl": p.Background, "bio": p.Bio,
			"gender": p.Gender, "region": p.Region, "birthday": p.Birthday,
			"updatedAt": updatedAt.UnixMilli(),
		},
		"profile": map[string]any{
			"id": p.ID, "username": p.Username, "wechatId": p.Username,
			"name": name, "nickname": name, "avatar": avatar,
			"backgroundUrl": p.Background, "bio": p.Bio,
			"phone": p.Phone, "email": p.Email,
			"gender": p.Gender, "region": p.Region, "birthday": p.Birthday,
			"updatedAt": updatedAt.UnixMilli(),
		},
	})
}

func (s *Server) publishProfileUpdated(ctx context.Context, p profileRow, updatedAt time.Time) {
	if s.redis == nil || s.db == nil {
		return
	}
	friendRows, err := s.db.Query(ctx, `
		SELECT CASE WHEN "userA"=$1 THEN "userB" ELSE "userA" END
		FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`, p.ID)
	if err != nil {
		log.Printf("[profile] 读取好友失败: %v", err)
		return
	}
	defer friendRows.Close()
	friends := []string{}
	for friendRows.Next() {
		var id string
		if friendRows.Scan(&id) == nil {
			friends = append(friends, id)
		}
	}
	groupRows, err := s.db.Query(ctx, `SELECT "groupId" FROM "GroupMember" WHERE "userId"=$1`, p.ID)
	if err != nil {
		log.Printf("[profile] 读取群组失败: %v", err)
		return
	}
	defer groupRows.Close()
	groups := []string{}
	for groupRows.Next() {
		var id string
		if groupRows.Scan(&id) == nil {
			groups = append(groups, id)
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"userId": p.ID, "nickname": p.Nickname, "avatar": safeAvatarUrl(p.Avatar),
		"username": p.Username, "bio": p.Bio, "backgroundUrl": p.Background,
		"updatedAt": updatedAt.UnixMilli(), "targetFriendIds": friends, "targetGroupIds": groups,
	})
	_ = s.redis.Publish(ctx, "msg:user_profile_updated", payload).Err()
}

func (s *Server) userMe(w http.ResponseWriter, _ *http.Request, current user) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id": current.ID, "userId": current.ID, "username": current.Username,
		"nickname": deref(current.Nickname), "avatar": safeAvatarUrl(deref(current.Avatar)),
		"user": current,
	})
}

func (s *Server) homeSync(w http.ResponseWriter, r *http.Request, current user) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "首页聚合同步失败"})
		return
	}
	var username, nickname, avatar, bio, role, phone string
	var created time.Time
	err := s.db.QueryRow(r.Context(), `
		SELECT "username",COALESCE("nickname",''),COALESCE("avatar",''),COALESCE("bio",''),
		       COALESCE("role",'user'),COALESCE("phone",''),"createdAt"
		FROM "User" WHERE "id"=$1`, current.ID).Scan(&username, &nickname, &avatar, &bio, &role, &phone, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "首页聚合同步失败"})
		return
	}
	chats, err := s.listPrivateChats(r.Context(), current.ID, 30)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "首页聚合同步失败"})
		return
	}
	groups, err := s.listUserGroups(r.Context(), current.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "首页聚合同步失败"})
		return
	}
	var announcement any
	var aid, title, content, typ string
	var aCreated time.Time
	aerr := s.db.QueryRow(r.Context(), `
		SELECT "id","title","content","type","createdAt" FROM "Announcement"
		WHERE "isActive"=true ORDER BY "createdAt" DESC LIMIT 1`).Scan(&aid, &title, &content, &typ, &aCreated)
	if aerr == nil {
		announcement = map[string]any{"id": aid, "title": title, "content": content, "type": typ, "createdAt": aCreated}
	}
	totalUnread := 0
	if s.redis != nil {
		if raw, rerr := s.redis.HGet(r.Context(), "unread:"+current.ID, "total").Result(); rerr == nil {
			if n, conv := strconv.Atoi(raw); conv == nil {
				totalUnread = n
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 200,
		"data": map[string]any{
			"user": map[string]any{
				"id": current.ID, "username": username, "nickname": nickname,
				"avatar": safeAvatarUrl(avatar), "bio": bio, "role": role,
				"phone": phone, "createdAt": created,
			},
			"totalUnread": totalUnread, "announcement": announcement,
			"chats": chats, "groups": groups,
		},
	})
}

func (s *Server) listPrivateChats(ctx context.Context, userID string, limit int) ([]any, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(ctx, `
		SELECT c."id",c."participantA",c."participantB",c."lastMessage",c."lastMessageAt",c."createdAt",
		       p."id",p."username",COALESCE(p."nickname",''),COALESCE(p."avatar",''),COALESCE(p."bio",''),
		       (SELECT COUNT(*) FROM "PrivateMessage" m WHERE m."chatId"=c."id" AND m."senderId"<>$1 AND m."status"<>'read' AND m."isRevoked"=false)
		FROM "Chat" c
		JOIN "User" p ON p."id"=CASE WHEN c."participantA"=$1 THEN c."participantB" ELSE c."participantA" END
		WHERE c."participantA"=$1 OR c."participantB"=$1
		ORDER BY c."lastMessageAt" DESC NULLS LAST,c."createdAt" DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var c chat
		var id, name, nick, av, bio string
		var unread int
		if err := rows.Scan(&c.ID, &c.A, &c.B, &c.Last, &c.LastAt, &c.Created, &id, &name, &nick, &av, &bio, &unread); err != nil {
			return nil, err
		}
		out = append(out, s.chatJSON(c, userID, map[string]string{
			"id": id, "username": name, "nickname": nick, "avatar": av, "bio": bio,
		}, unread))
	}
	return out, nil
}

func (s *Server) listUserGroups(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g."id",g."dialogId",g."name",g."username",g."avatar",g."ownerId",g."type",g."isPublic",
		       g."memberCount",g."lastMsgSeq",m."lastAckSeq",g."createdAt",g."updatedAt"
		FROM "GroupMember" m JOIN "Group" g ON g."id"=m."groupId"
		WHERE m."userId"=$1 ORDER BY g."updatedAt" DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, owner, typ string
		var dialog, username, avatar *string
		var pub bool
		var count, last, ack int64
		var created, updated time.Time
		if rows.Scan(&id, &dialog, &name, &username, &avatar, &owner, &typ, &pub, &count, &last, &ack, &created, &updated) != nil {
			continue
		}
		unread := last - ack
		if unread < 0 {
			unread = 0
		}
		out = append(out, map[string]any{
			"id": id, "groupId": id, "dialogId": dialog, "name": name, "username": username,
			"avatar": safeAvatarUrl(deref(avatar)), "ownerId": owner, "type": typ, "isPublic": pub,
			"memberCount": count, "lastMessage": "🔒 [加密消息]", "unreadCount": unread,
			"createdAt": created.UnixMilli(), "updatedAt": updated.UnixMilli(),
		})
	}
	return out, nil
}

func emptyToNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
