package web

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============ 个人资料（对应 index.ts /api/profile） ============

// memProfiles 内存资料存储（对应 index.ts userProfiles；后续可迁移到 Prisma）。
var memProfiles sync.Map // map[string]map[string]any

func getMemProfile(key string) map[string]any {
	if v, ok := memProfiles.Load(key); ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func memStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// GET /api/profile — 读取用户公开资料（optionalAuth；userId=me 时取 token 用户）。
func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		userID = "me"
	}
	tokenUser := middleware.UserFrom(r)
	if (userID == "me" || userID == "") && tokenUser != nil {
		userID = tokenUser.Id
	}
	ctx := r.Context()

	// 优先从数据库读取真实资料，支持用 cuid 或 username 查询
	dbUser := h.findProfileUser(ctx, userID)

	profileKey := userID
	if dbUser != nil {
		profileKey = dbUser.Id
	}
	mem := getMemProfile(profileKey)
	if mem == nil {
		mem = getMemProfile(userID)
	}

	displayName := strVal(dbUserNickname(dbUser))
	if displayName == "" {
		displayName = memStr(mem, "name")
	}
	if displayName == "" {
		displayName = memStr(mem, "nickname")
	}
	if displayName == "" {
		displayName = strVal(dbUserUsername(dbUser))
	}
	if displayName == "" {
		displayName = registryNickname(profileKey)
	}
	if displayName == "" {
		displayName = registryNickname(userID)
	}
	if displayName == "" {
		displayName = userID
	}

	nickname := strVal(dbUserNickname(dbUser))
	if nickname == "" {
		nickname = memStr(mem, "nickname")
	}
	if nickname == "" {
		nickname = displayName
	}
	username := strVal(dbUserUsername(dbUser))
	if username == "" {
		username = memStr(mem, "wechatId")
	}
	if username == "" {
		username = userID
	}
	bio := strVal(dbUserBio(dbUser))
	if dbUserBio(dbUser) == nil {
		bio = memStr(mem, "bio")
	}
	avatar := safeAvatarUrl(firstNonEmpty(strVal(dbUserAvatar(dbUser)), memStr(mem, "avatar")))

	profile := map[string]any{
		"id":            profileKey,
		"username":      username,
		"name":          displayName,
		"nickname":      nickname,
		"gender":        firstNonEmpty(strVal(dbUserGender(dbUser)), memStr(mem, "gender")),
		"region":        firstNonEmpty(strVal(dbUserRegion(dbUser)), memStr(mem, "region")),
		"phone":         firstNonEmpty(strVal(dbUserPhone(dbUser)), memStr(mem, "phone")),
		"email":         firstNonEmpty(strVal(dbUserEmail(dbUser)), memStr(mem, "email")),
		"wechatId":      username,
		"bio":           bio,
		"avatar":        avatar,
		"backgroundUrl": firstNonEmpty(strVal(dbUserBackground(dbUser)), memStr(mem, "backgroundUrl")),
		"birthday":      firstNonEmpty(strVal(dbUserBirthday(dbUser)), memStr(mem, "birthday")),
	}
	util.WriteJSON(w, 200, map[string]any{"profile": profile})
}

// profileUser GET/PUT 共用的资料查询结果。
type profileUser struct {
	Id            string
	Username      string
	Nickname      *string
	Avatar        *string
	BackgroundUrl *string
	Bio           *string
	Phone         *string
	Email         *string
	Gender        *string
	Region        *string
	Birthday      *string
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func dbUserNickname(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Nickname
}
func dbUserUsername(u *profileUser) *string {
	if u == nil || u.Username == "" {
		return nil
	}
	return &u.Username
}
func dbUserAvatar(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Avatar
}
func dbUserBackground(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.BackgroundUrl
}
func dbUserBio(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Bio
}
func dbUserPhone(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Phone
}
func dbUserEmail(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Email
}
func dbUserGender(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Gender
}
func dbUserRegion(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Region
}
func dbUserBirthday(u *profileUser) *string {
	if u == nil {
		return nil
	}
	return u.Birthday
}

// findProfileUser 按 id 查不到则按 username 查。
func (h *Handler) findProfileUser(ctx context.Context, userID string) *profileUser {
	const sel = `"id","username","nickname","avatar","backgroundUrl","bio","phone","email","gender","region","birthday"`
	u, err := db.QueryRowToStruct[profileUser](ctx, h.d.DB,
		`SELECT `+sel+` FROM "User" WHERE "id"=$1`, userID)
	if err == nil {
		return u
	}
	u2, err := db.QueryRowToStruct[profileUser](ctx, h.d.DB,
		`SELECT `+sel+` FROM "User" WHERE "username"=$1`, userID)
	if err == nil {
		return u2
	}
	return nil
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,20}$`)

// PUT /api/profile — 更新当前用户个人资料（optionalAuth）。
func (h *Handler) putProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID        string  `json:"userId"`
		Name          *string `json:"name"`
		Nickname      *string `json:"nickname"`
		Gender        *string `json:"gender"`
		Region        *string `json:"region"`
		Phone         *string `json:"phone"`
		WechatID      *string `json:"wechatId"`
		Bio           *string `json:"bio"`
		Avatar        *string `json:"avatar"`
		BackgroundURL *string `json:"backgroundUrl"`
		Birthday      *string `json:"birthday"`
	}
	if !util.DecodeJSON(w, r, &body) {
		return
	}
	userID := body.UserID
	if userID == "" {
		userID = "me"
	}
	tokenUser := middleware.UserFrom(r)
	if (userID == "me" || userID == "") && tokenUser != nil {
		userID = tokenUser.Id
	}
	ctx := r.Context()

	dbUser := h.findProfileUser(ctx, userID)
	profileKey := userID
	currentUsername := userID
	var dbUserID string
	var dbNickname string
	if dbUser != nil {
		profileKey = dbUser.Id
		currentUsername = dbUser.Username
		dbUserID = dbUser.Id
		dbNickname = strVal(dbUser.Nickname)
	}
	nextUsername := currentUsername
	if body.WechatID != nil {
		nextUsername = strings.TrimSpace(*body.WechatID)
	}

	if nextUsername != "" && !usernameRe.MatchString(nextUsername) {
		util.WriteError(w, 400, "账号ID只能包含字母、数字和下划线，长度1-20位")
		return
	}
	if dbUser != nil && nextUsername != currentUsername {
		var dupID string
		err := h.d.DB.Pool.QueryRow(ctx, `SELECT "id" FROM "User" WHERE "username"=$1`, nextUsername).Scan(&dupID)
		if err == nil && dupID != dbUser.Id {
			util.WriteError(w, 409, "该账号ID已被使用")
			return
		}
	}

	existing := getMemProfile(profileKey)
	if existing == nil {
		existing = getMemProfile(userID)
	}
	if existing == nil {
		existing = map[string]any{}
	}
	or := func(p *string, fallbacks ...string) string {
		if p != nil {
			return *p
		}
		for _, f := range fallbacks {
			if f != "" {
				return f
			}
		}
		return ""
	}
	updated := map[string]any{}
	for k, v := range existing {
		updated[k] = v
	}
	updated["id"] = profileKey
	// 与 TS 保持一致：
	// name = name || nickname || existing.name || dbNickname || currentUsername
	// nickname = nickname || name || existing.nickname || dbNickname || currentUsername
	nameVal := ""
	if body.Name != nil && *body.Name != "" {
		nameVal = *body.Name
	} else if body.Nickname != nil && *body.Nickname != "" {
		nameVal = *body.Nickname
	} else {
		nameVal = firstNonEmpty(memStr(existing, "name"), dbNickname, currentUsername)
	}
	nickVal := ""
	if body.Nickname != nil && *body.Nickname != "" {
		nickVal = *body.Nickname
	} else if body.Name != nil && *body.Name != "" {
		nickVal = *body.Name
	} else {
		nickVal = firstNonEmpty(memStr(existing, "nickname"), dbNickname, currentUsername)
	}
	updated["name"] = nameVal
	updated["nickname"] = nickVal
	updated["gender"] = or(body.Gender, memStr(existing, "gender"))
	updated["region"] = or(body.Region, memStr(existing, "region"))
	updated["phone"] = or(body.Phone, memStr(existing, "phone"))
	wechatID := nextUsername
	if wechatID == "" {
		wechatID = firstNonEmpty(memStr(existing, "wechatId"), currentUsername)
	}
	updated["wechatId"] = wechatID
	updated["bio"] = or(body.Bio, memStr(existing, "bio"))
	updated["avatar"] = safeAvatarUrl(or(body.Avatar, memStr(existing, "avatar")))
	updated["backgroundUrl"] = or(body.BackgroundURL, memStr(existing, "backgroundUrl"))
	updated["birthday"] = or(body.Birthday, memStr(existing, "birthday"))

	memProfiles.Store(profileKey, updated)
	if userID != profileKey {
		memProfiles.Delete(userID)
	}

	// 同步更新 userRegistry 昵称
	if nameVal != "" {
		setRegistryNickname(profileKey, nameVal)
		setRegistryNickname(currentUsername, nameVal)
		setRegistryNickname(wechatID, nameVal)
	}

	// 同步写入数据库（支持修改 username）
	var dbUpdatedAt *int64
	setClauses := []string{}
	args := []any{}
	addSet := func(col string, val any) {
		args = append(args, val)
		setClauses = append(setClauses, `"`+col+`"=$`+itoa(len(args)))
	}
	nullIfEmpty := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	if nickVal != "" || nameVal != "" {
		nv := nickVal
		if nv == "" {
			nv = nameVal
		}
		addSet("nickname", nv)
	}
	addSet("bio", updated["bio"])
	addSet("avatar", updated["avatar"])
	addSet("backgroundUrl", nullIfEmpty(updated["backgroundUrl"].(string)))
	addSet("gender", nullIfEmpty(updated["gender"].(string)))
	addSet("region", nullIfEmpty(updated["region"].(string)))
	addSet("birthday", nullIfEmpty(updated["birthday"].(string)))
	if dbUser != nil && wechatID != "" && wechatID != currentUsername {
		addSet("username", wechatID)
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, `"updatedAt"=NOW()`)
		var pgErr *pgconn.PgError
		if dbUser != nil {
			args = append(args, dbUserID)
			var updatedAt time.Time
			err := h.d.DB.Pool.QueryRow(ctx,
				`UPDATE "User" SET `+strings.Join(setClauses, ",")+` WHERE "id"=$`+itoa(len(args))+` RETURNING "updatedAt"`,
				args...).Scan(&updatedAt)
			if err != nil {
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					util.WriteError(w, 409, "该账号ID已被使用")
				} else {
					util.WriteError(w, 500, "资料更新失败")
				}
				return
			}
			ms := updatedAt.UnixMilli()
			dbUpdatedAt = &ms
		} else {
			argsByID := append(append([]any{}, args...), userID)
			tag, err := h.d.DB.Pool.Exec(ctx,
				`UPDATE "User" SET `+strings.Join(setClauses, ",")+` WHERE "id"=$`+itoa(len(argsByID)), argsByID...)
			if err != nil {
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					util.WriteError(w, 409, "该账号ID已被使用")
				} else {
					util.WriteError(w, 500, "资料更新失败")
				}
				return
			}
			if tag.RowsAffected() == 0 {
				argsByName := append(append([]any{}, args...), userID)
				if _, err := h.d.DB.Pool.Exec(ctx,
					`UPDATE "User" SET `+strings.Join(setClauses, ",")+` WHERE "username"=$`+itoa(len(argsByName)), argsByName...); err != nil {
					if errors.As(err, &pgErr) && pgErr.Code == "23505" {
						util.WriteError(w, 409, "该账号ID已被使用")
					} else {
						util.WriteError(w, 500, "资料更新失败")
					}
					return
				}
			}
		}
		// TS 版此处发布 user_profile_updated 事件（user-profile-sync.js）；
		// Go 版暂无对应模块，跳过（资料读取本就优先读库）。
	}

	respProfile := map[string]any{}
	for k, v := range updated {
		respProfile[k] = v
	}
	respProfile["id"] = profileKey
	respProfile["username"] = wechatID
	respProfile["wechatId"] = wechatID
	if dbUpdatedAt != nil {
		respProfile["updatedAt"] = *dbUpdatedAt
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "profile": respProfile})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
