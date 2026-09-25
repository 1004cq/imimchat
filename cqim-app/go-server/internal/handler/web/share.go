package web

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ 投诉 / 外链解析 / 外链落地页（对应 index.ts） ============

// POST /api/report — 用户投诉（TS 版为手动 session 校验，非 userAuth 中间件）。
func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	token := strings.Replace(r.Header.Get("Authorization"), "Bearer ", "", 1)
	if token == "" {
		util.WriteError(w, 401, "未登录")
		return
	}
	var userID string
	err := h.d.DB.Pool.QueryRow(r.Context(),
		`SELECT "userId" FROM "UserSession" WHERE "token"=$1 AND "expiresAt" > NOW()`, token).Scan(&userID)
	if err != nil {
		util.WriteError(w, 401, "登录已过期")
		return
	}
	var body struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Reason     string `json:"reason"`
	}
	if !util.DecodeJSON(w, r, &body) {
		return
	}
	if body.TargetType == "" || body.TargetID == "" || body.Reason == "" {
		util.WriteError(w, 400, "参数不完整")
		return
	}
	reportID := util.NewID()
	if _, err := h.d.DB.Exec(r.Context(),
		`INSERT INTO "Report"("id","reporterId","targetType","targetId","reason") VALUES($1,$2,$3,$4,$5)`,
		reportID, userID, body.TargetType, body.TargetID, body.Reason); err != nil {
		util.WriteError(w, 500, "提交失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "reportId": reportID})
}

// GET /api/q/profile/{userId} — 获取用户公开信息（外链页面用，无需登录）。
func (h *Handler) qProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	type row struct {
		Id            string  `db:"id"`
		Username      string  `db:"username"`
		Nickname      *string `db:"nickname"`
		Avatar        *string `db:"avatar"`
		BackgroundUrl *string `db:"backgroundUrl"`
		Bio           *string `db:"bio"`
	}
	u, err := db.QueryRowToStruct[row](r.Context(), h.d.DB,
		`SELECT "id","username","nickname","avatar","backgroundUrl","bio" FROM "User" WHERE "id"=$1`, userID)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "用户不存在")
		} else {
			util.WriteError(w, 500, "服务器错误")
		}
		return
	}
	var momentCount int64
	_ = h.d.DB.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM "Moment" WHERE "userId"=$1`, u.Id).Scan(&momentCount)
	name := u.Username
	if u.Nickname != nil && *u.Nickname != "" {
		name = *u.Nickname
	}
	util.WriteJSON(w, 200, map[string]any{"profile": map[string]any{
		"id": u.Id, "name": name,
		"avatar":        safeAvatarUrl(strVal(u.Avatar)),
		"backgroundUrl": strVal(u.BackgroundUrl),
		"bio":           strVal(u.Bio),
		"momentCount":   momentCount,
	}})
}

// GET /api/im/resolve/{slug} — 外链解析（TG 风格：添加好友 / 加入群组 / 邀请链接）。
// 公开接口，无需登录。返回 { type: 'user' | 'group' | 'invite', data: {...} }。
func (h *Handler) imResolve(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if strings.TrimSpace(slug) == "" {
		util.WriteError(w, 400, "无效的链接")
		return
	}
	ctx := r.Context()

	// “+hash” 格式：私人邀请链接
	if strings.HasPrefix(slug, "+") {
		hash := slug[1:]
		var link db.InviteLink
		lp, err := db.QueryRowToStruct[db.InviteLink](ctx, h.d.DB,
			`SELECT * FROM "InviteLink" WHERE "hash"=$1`, hash)
		if err != nil {
			if db.IsNotFound(err) {
				util.WriteError(w, 404, "邀请链接不存在")
			} else {
				log.Printf("[InviteLink] resolve 失败: %v", err)
				util.WriteError(w, 500, "服务器错误")
			}
			return
		}
		link = *lp
		if link.IsRevoked {
			util.WriteError(w, 410, "邀请链接已被撤销")
			return
		}
		if link.ExpireAt != nil && !link.ExpireAt.After(nowUTC()) {
			util.WriteError(w, 410, "邀请链接已过期")
			return
		}
		if link.MaxUses > 0 && link.UsedCount >= link.MaxUses {
			util.WriteError(w, 410, "邀请链接已达到最大使用次数")
			return
		}
		var g db.Group
		gp, err := db.QueryRowToStruct[db.Group](ctx, h.d.DB,
			`SELECT * FROM "Group" WHERE "id"=$1`, link.GroupId)
		if err != nil {
			log.Printf("[InviteLink] resolve 失败: %v", err)
			util.WriteError(w, 500, "服务器错误")
			return
		}
		g = *gp
		util.WriteJSON(w, 200, map[string]any{"type": "invite", "data": map[string]any{
			"hash": link.Hash, "groupId": g.Id, "groupName": g.Name,
			"groupUsername": strVal(g.Username), "groupAvatar": safeAvatarUrl(strVal(g.Avatar)),
			"memberCount": g.MemberCount, "groupType": g.Type,
		}})
		return
	}

	// 优先查询用户（按 username 精确匹配）
	type userRow struct {
		Id            string  `db:"id"`
		Username      string  `db:"username"`
		Nickname      *string `db:"nickname"`
		Avatar        *string `db:"avatar"`
		Bio           *string `db:"bio"`
		BackgroundUrl *string `db:"backgroundUrl"`
		IsBanned      bool    `db:"isBanned"`
		IsBot         bool    `db:"isBot"`
	}
	if u, err := db.QueryRowToStruct[userRow](ctx, h.d.DB,
		`SELECT "id","username","nickname","avatar","bio","backgroundUrl","isBanned","isBot" FROM "User" WHERE "username"=$1`, slug); err == nil && !u.IsBanned {
		nickname := u.Username
		if u.Nickname != nil && *u.Nickname != "" {
			nickname = *u.Nickname
		}
		util.WriteJSON(w, 200, map[string]any{"type": "user", "data": map[string]any{
			"id": u.Id, "username": u.Username, "nickname": nickname,
			"avatar": safeAvatarUrl(strVal(u.Avatar)), "bio": strVal(u.Bio),
			"backgroundUrl": strVal(u.BackgroundUrl), "isBot": u.IsBot,
		}})
		return
	}

	// 查询群组（按 username 精确匹配）
	type groupRow struct {
		Id          string  `db:"id"`
		Username    *string `db:"username"`
		Name        string  `db:"name"`
		Avatar      *string `db:"avatar"`
		MemberCount int32   `db:"memberCount"`
		Type        string  `db:"type"`
		IsPublic    bool    `db:"isPublic"`
	}
	groupData := func(g *groupRow, usernameNil bool) map[string]any {
		username := strVal(g.Username)
		var yu any = username
		if usernameNil || username == "" {
			yu = nil
		}
		return map[string]any{
			"id": g.Id, "username": yu, "name": g.Name,
			"avatar": safeAvatarUrl(strVal(g.Avatar)), "memberCount": g.MemberCount,
			"groupType": g.Type, "isPublic": g.IsPublic,
		}
	}
	if g, err := db.QueryRowToStruct[groupRow](ctx, h.d.DB,
		`SELECT "id","username","name","avatar","memberCount","type","isPublic" FROM "Group" WHERE "username"=$1`, slug); err == nil {
		util.WriteJSON(w, 200, map[string]any{"type": "group", "data": groupData(g, false)})
		return
	}

	// 兼容旧版：按内部 id 查询群组
	if g, err := db.QueryRowToStruct[groupRow](ctx, h.d.DB,
		`SELECT "id","username","name","avatar","memberCount","type","isPublic" FROM "Group" WHERE "id"=$1`, slug); err == nil {
		util.WriteJSON(w, 200, map[string]any{"type": "group", "data": groupData(g, true)})
		return
	}

	// 按 dialogId 查询（TG 风格数字 ID）
	if u, err := db.QueryRowToStruct[userRow](ctx, h.d.DB,
		`SELECT "id","username","nickname","avatar","bio","backgroundUrl","isBanned","isBot" FROM "User" WHERE "dialogId"=$1`, slug); err == nil && !u.IsBanned {
		nickname := u.Username
		if u.Nickname != nil && *u.Nickname != "" {
			nickname = *u.Nickname
		}
		util.WriteJSON(w, 200, map[string]any{"type": "user", "data": map[string]any{
			"id": u.Id, "username": u.Username, "nickname": nickname,
			"avatar": safeAvatarUrl(strVal(u.Avatar)), "bio": strVal(u.Bio),
			"isBot": u.IsBot,
		}})
		return
	}
	if g, err := db.QueryRowToStruct[groupRow](ctx, h.d.DB,
		`SELECT "id","username","name","avatar","memberCount","type","isPublic" FROM "Group" WHERE "dialogId"=$1`, slug); err == nil {
		util.WriteJSON(w, 200, map[string]any{"type": "group", "data": groupData(g, true)})
		return
	}

	util.WriteError(w, 404, "未找到用户或群组")
}

// nowUTC 当前 UTC 时间（用于邀请链接过期判断，对应 TS new Date()）。
func nowUTC() time.Time { return time.Now().UTC() }

// ============ 外链落地页（服务端注入 OG 元标签） ============

const ogSiteName = "灵鸽 IM"

var titleTagRe = regexp.MustCompile(`<title>[^<]*</title>`)

// injectOGTags 读取 index.html，替换 title 并在 </head> 前注入 OG 标签。
func (h *Handler) injectOGTags(w http.ResponseWriter, ogTags []string) {
	dir := staticDir()
	indexPath := dir + "/index.html"
	htmlBytes, err := os.ReadFile(indexPath)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	html := string(htmlBytes)
	tags := ""
	for _, t := range ogTags {
		if t != "" {
			tags += t + "\n    "
		}
	}
	tags = strings.TrimRight(tags, "\n    ")
	html = titleTagRe.ReplaceAllString(html, "")
	html = strings.Replace(html, "</head>", "    "+tags+"\n  </head>", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setNoCacheHeaders(w)
	w.WriteHeader(200)
	_, _ = w.Write([]byte(html))
}

// GET /im/{slug} — 外链落地页（添加好友 / 加入群组），必须在 SPA 通配路由之前。
func (h *Handler) imLanding(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ogTitle := "加入 " + ogSiteName
	ogDescription := "点击链接添加好友或加入群组"
	ogImage := ""
	ogType := "website"
	ctx := r.Context()

	if strings.HasPrefix(slug, "+") {
		hash := slug[1:]
		var link db.InviteLink
		if lp, err := db.QueryRowToStruct[db.InviteLink](ctx, h.d.DB,
			`SELECT * FROM "InviteLink" WHERE "hash"=$1`, hash); err == nil && !lp.IsRevoked {
			link = *lp
			var name, avatar string
			var memberCount int32
			if gp, err := db.QueryRowToStruct[db.Group](ctx, h.d.DB,
				`SELECT * FROM "Group" WHERE "id"=$1`, link.GroupId); err == nil {
				name, avatar, memberCount = gp.Name, strVal(gp.Avatar), gp.MemberCount
			}
			if name != "" {
				ogTitle = "加入群组「" + escHTML(name) + "」— " + ogSiteName
				ogDescription = itoa(int(memberCount)) + " 名成员 · 通过邀请链接加入群聊"
				ogImage = safeAvatarUrl(avatar)
			}
		}
	} else {
		type urow struct {
			Nickname *string `db:"nickname"`
			Username string  `db:"username"`
			Avatar   *string `db:"avatar"`
			Bio      *string `db:"bio"`
			IsBanned bool    `db:"isBanned"`
		}
		if u, err := db.QueryRowToStruct[urow](ctx, h.d.DB,
			`SELECT "nickname","username","avatar","bio","isBanned" FROM "User" WHERE "username"=$1`, slug); err == nil && !u.IsBanned {
			displayName := u.Username
			if u.Nickname != nil && *u.Nickname != "" {
				displayName = *u.Nickname
			}
			ogTitle = "添加 " + escHTML(displayName) + " 为好友 — " + ogSiteName
			if u.Bio != nil && *u.Bio != "" {
				ogDescription = escHTML(*u.Bio)
			} else {
				ogDescription = "点击添加 " + escHTML(displayName) + " 为好友"
			}
			ogImage = safeAvatarUrl(strVal(u.Avatar))
			ogType = "profile"
		} else {
			type grow struct {
				Name        string  `db:"name"`
				Avatar      *string `db:"avatar"`
				MemberCount int32   `db:"memberCount"`
			}
			var g *grow
			if gp, err := db.QueryRowToStruct[grow](ctx, h.d.DB,
				`SELECT "name","avatar","memberCount" FROM "Group" WHERE "username"=$1`, slug); err == nil {
				g = gp
			} else if gp, err := db.QueryRowToStruct[grow](ctx, h.d.DB,
				`SELECT "name","avatar","memberCount" FROM "Group" WHERE "id"=$1`, slug); err == nil {
				g = gp
			}
			if g != nil {
				ogTitle = "加入群组「" + escHTML(g.Name) + "」— " + ogSiteName
				ogDescription = itoa(int(g.MemberCount)) + " 名成员 · 点击加入群聊"
				ogImage = safeAvatarUrl(strVal(g.Avatar))
				ogType = "website"
			}
		}
	}

	pageURL := publicURL("/im/" + url.PathEscape(slug))
	ogTags := []string{
		`<meta property="og:type" content="` + ogType + `" />`,
		`<meta property="og:title" content="` + escHTML(ogTitle) + `" />`,
		`<meta property="og:description" content="` + escHTML(ogDescription) + `" />`,
		`<meta property="og:url" content="` + pageURL + `" />`,
		`<meta property="og:site_name" content="` + ogSiteName + `" />`,
		nonEmptyTag(`<meta property="og:image" content="`+ogImage+`" />`, ogImage),
		`<meta name="twitter:card" content="summary" />`,
		`<meta name="twitter:title" content="` + escHTML(ogTitle) + `" />`,
		`<meta name="twitter:description" content="` + escHTML(ogDescription) + `" />`,
		nonEmptyTag(`<meta name="twitter:image" content="`+ogImage+`" />`, ogImage),
		`<title>` + escHTML(ogTitle) + `</title>`,
	}
	h.injectOGTags(w, ogTags)
}

func nonEmptyTag(tag, val string) string {
	if val == "" {
		return ""
	}
	return tag
}

// GET /q/{userId} — 301 跳转到 /pyq/{userId}。
func (h *Handler) qRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/pyq/"+url.PathEscape(r.PathValue("userId")), http.StatusMovedPermanently)
}

// GET /pyq/{userId} — 朋友圈外链页面（服务端注入 OG 元标签）。
func (h *Handler) pyqLanding(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	ctx := r.Context()
	type urow struct {
		Username string  `db:"username"`
		Nickname *string `db:"nickname"`
		Avatar   *string `db:"avatar"`
		Bio      *string `db:"bio"`
	}
	var displayName = "朋友圈"
	var bioRaw, avatarRaw string
	var momentCount int64
	if u, err := db.QueryRowToStruct[urow](ctx, h.d.DB,
		`SELECT "username","nickname","avatar","bio" FROM "User" WHERE "id"=$1`, userID); err == nil {
		if u.Nickname != nil && *u.Nickname != "" {
			displayName = *u.Nickname
		} else {
			displayName = u.Username
		}
		if u.Bio != nil {
			bioRaw = *u.Bio
		}
		avatarRaw = strVal(u.Avatar)
		_ = h.d.DB.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM "Moment" WHERE "userId"=$1 AND "visibility"='public'`, userID).Scan(&momentCount)
	}
	safeDisplayName := escHTML(displayName)
	bio := escHTML(bioRaw)
	avatarURL := safeAvatarUrl(avatarRaw)
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	} else if fh := r.Header.Get("X-Forwarded-Proto"); fh != "" {
		proto = strings.Split(fh, ",")[0]
	}
	pageURL := proto + "://" + r.Host + "/pyq/" + userID
	var description string
	if bio != "" {
		description = bio
		if momentCount > 0 {
			description += "（共 " + itoa(int(momentCount)) + " 条动态）"
		}
	} else {
		description = "查看 " + safeDisplayName + " 的朋友圈动态"
		if momentCount > 0 {
			description += "，共 " + itoa(int(momentCount)) + " 条"
		}
	}
	ogTags := []string{
		`<meta property="og:type" content="profile" />`,
		`<meta property="og:title" content="` + safeDisplayName + ` 的朋友圈 — ` + ogSiteName + `" />`,
		`<meta property="og:description" content="` + description + `" />`,
		`<meta property="og:url" content="` + pageURL + `" />`,
		`<meta property="og:site_name" content="` + ogSiteName + `" />`,
		nonEmptyTag(`<meta property="og:image" content="`+avatarURL+`" />`, avatarURL),
		`<meta name="twitter:card" content="summary" />`,
		`<meta name="twitter:title" content="` + safeDisplayName + ` 的朋友圈 — ` + ogSiteName + `" />`,
		`<meta name="twitter:description" content="` + description + `" />`,
		nonEmptyTag(`<meta name="twitter:image" content="`+avatarURL+`" />`, avatarURL),
		`<title>` + safeDisplayName + ` 的朋友圈 — ` + ogSiteName + `</title>`,
	}
	h.injectOGTags(w, ogTags)
}
