package admin

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/middleware"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============================================================
// 动态管理
// ============================================================

type momentAdminRow struct {
	db.Moment
	Username     *string `db:"username"`
	Nickname     *string `db:"nickname"`
	Avatar       *string `db:"avatar"`
	UserID2      *string `db:"userId2"`
	CommentCount int64   `db:"commentCount"`
	LikeCount    int64   `db:"likeCount"`
}

func momentToMap(m *db.Moment, user map[string]any, media []db.MomentMedia, commentCount, likeCount int64, withCount bool) map[string]any {
	item := map[string]any{
		"id": m.Id, "userId": m.UserId, "content": m.Content, "visibility": m.Visibility,
		"location": m.Location, "topics": m.Topics, "isPinned": m.IsPinned, "pinnedAt": m.PinnedAt,
		"viewCount": m.ViewCount, "sortOrder": m.SortOrder, "createdAt": m.CreatedAt, "updatedAt": m.UpdatedAt,
		"user": user, "media": media,
	}
	if withCount {
		item["_count"] = map[string]any{"comments": commentCount, "likes": likeCount}
	}
	return item
}

// momentUserMap 构造内嵌 user 对象（用户不存在时为 null，与 Prisma include 语义一致）。
func momentUserMap(m *momentAdminRow) map[string]any {
	if m.UserID2 == nil {
		return nil
	}
	return map[string]any{"id": m.UserID2, "username": m.Username, "nickname": m.Nickname, "avatar": m.Avatar}
}

// ============ GET /api/admin/moments ============

func (h *Handler) listMoments(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	q := r.URL.Query()
	search := q.Get("search")
	page := atoiMax(q.Get("page"), 1, 1)
	pageSize := atoiMax(q.Get("pageSize"), 20, 1)
	if pageSize > 100 {
		pageSize = 100
	}

	where := ""
	var args []any
	if search != "" {
		where = `WHERE (m."content" LIKE '%'||$1||'%' OR u."username" LIKE '%'||$1||'%')`
		args = append(args, search)
	}

	var total int64
	_ = h.db().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM "Moment" m LEFT JOIN "User" u ON u."id"=m."userId" `+where, args...).Scan(&total)

	rows, err := db.QueryToStructs[momentAdminRow](ctx, h.db(),
		`SELECT m."id",m."userId",m."content",m."visibility",m."location",m."topics",m."isPinned",m."pinnedAt",
		        m."viewCount",m."sortOrder",m."createdAt",m."updatedAt",
		        u."username",u."nickname",u."avatar",u."id" AS "userId2",
		        (SELECT COUNT(*) FROM "MomentComment" c WHERE c."momentId"=m."id") AS "commentCount",
		        (SELECT COUNT(*) FROM "MomentLike" l WHERE l."momentId"=m."id") AS "likeCount"
		 FROM "Moment" m LEFT JOIN "User" u ON u."id"=m."userId" `+where+`
		 ORDER BY m."isPinned" DESC, m."createdAt" DESC
		 LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2),
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		log.Printf("[Admin] 动态列表查询失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}

	momentIDs := make([]string, 0, len(rows))
	for i := range rows {
		momentIDs = append(momentIDs, rows[i].Id)
	}
	mediaMap := map[string][]db.MomentMedia{}
	if len(momentIDs) > 0 {
		medias, err := db.QueryToStructs[db.MomentMedia](ctx, h.db(),
			`SELECT "id","momentId","type","url","width","height","duration","sortOrder","createdAt"
			 FROM "MomentMedia" WHERE "momentId" = ANY($1) ORDER BY "sortOrder" ASC`, momentIDs)
		if err == nil {
			for _, md := range medias {
				if len(mediaMap[md.MomentId]) == 0 { // take 1（与 TS media: { take: 1 } 一致）
					mediaMap[md.MomentId] = []db.MomentMedia{md}
				}
			}
		}
	}

	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		m := &rows[i]
		media := mediaMap[m.Id]
		if media == nil {
			media = []db.MomentMedia{}
		}
		items = append(items, momentToMap(&m.Moment, momentUserMap(m), media, m.CommentCount, m.LikeCount, true))
	}
	util.WriteJSON(w, 200, map[string]any{"moments": items, "total": total, "page": page, "pageSize": pageSize})
}

// ============ POST /api/admin/moments/:id/pin 置顶/取消置顶 ============

func (h *Handler) pinMoment(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var req struct {
		Pin bool `json:"pin"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	admin := middleware.AdminFrom(r)

	var m db.Moment
	pinnedAtSQL := `"pinnedAt"=NULL`
	if req.Pin {
		pinnedAtSQL = `"pinnedAt"=NOW()`
	}
	err := h.db().Pool.QueryRow(ctx,
		`UPDATE "Moment" SET "isPinned"=$1,`+pinnedAtSQL+`,"updatedAt"=NOW() WHERE "id"=$2
		 RETURNING "id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt"`,
		req.Pin, id,
	).Scan(&m.Id, &m.UserId, &m.Content, &m.Visibility, &m.Location, &m.Topics, &m.IsPinned, &m.PinnedAt,
		&m.ViewCount, &m.SortOrder, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		log.Printf("[Admin] 置顶动态失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	action := "取消置顶"
	if req.Pin {
		action = "置顶动态"
	}
	h.addLog(ctx, admin.Id, admin.Username, action, "moment:"+m.Id, "", h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true, "moment": m})
}

// ============ DELETE /api/admin/moments/:id 删除动态 ============

func (h *Handler) deleteMoment(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	admin := middleware.AdminFrom(r)
	if _, err := h.db().Exec(ctx, `DELETE FROM "Moment" WHERE "id"=$1`, id); err != nil {
		log.Printf("[Admin] 删除动态失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	h.addLog(ctx, admin.Id, admin.Username, "删除动态", "moment:"+id, "", h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============ DELETE /api/admin/moments 批量删除动态 ============

func (h *Handler) deleteMomentsBatch(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		IDs []string `json:"ids"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		util.WriteError(w, 400, "请提供 ID 列表")
		return
	}
	admin := middleware.AdminFrom(r)
	if _, err := h.db().Exec(ctx, `DELETE FROM "Moment" WHERE "id" = ANY($1)`, req.IDs); err != nil {
		log.Printf("[Admin] 批量删除动态失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	h.addLog(ctx, admin.Id, admin.Username, "批量删除动态", "moments",
		fmt.Sprintf("批量删除 %d 条", len(req.IDs)), h.clientIP(r))
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 媒体管理
// ============================================================

// ============ GET /api/admin/media ============

func (h *Handler) listMedia(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	q := r.URL.Query()
	typ := q.Get("type")
	if typ == "" {
		typ = "image"
	}
	page := atoiMax(q.Get("page"), 1, 1)
	pageSize := atoiMax(q.Get("pageSize"), 30, 1)
	if pageSize > 100 {
		pageSize = 100
	}

	var total int64
	_ = h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "MediaFile" WHERE "type"=$1`, typ).Scan(&total)

	type mediaRow struct {
		db.MediaFile
		Username *string `db:"username"`
		Nickname *string `db:"nickname"`
	}
	rows, err := db.QueryToStructs[mediaRow](ctx, h.db(),
		`SELECT f."id",f."userId",f."type",f."kind",f."url",f."filename",f."mime",f."size",f."width",f."height",
		        f."durationMs",f."posterMediaId",f."sha256",f."diskPath",f."publicPath",f."cosKey",f."createdAt",
		        u."username",u."nickname"
		 FROM "MediaFile" f LEFT JOIN "User" u ON u."id"=f."userId"
		 WHERE f."type"=$1 ORDER BY f."createdAt" DESC LIMIT $2 OFFSET $3`,
		typ, pageSize, (page-1)*pageSize)
	if err != nil {
		log.Printf("[Admin] 媒体列表查询失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}
	files := make([]map[string]any, 0, len(rows))
	for i := range rows {
		f := &rows[i]
		files = append(files, map[string]any{
			"id": f.Id, "userId": f.UserId, "type": f.Type, "kind": f.Kind, "url": f.Url,
			"filename": f.Filename, "mime": f.Mime, "size": f.Size, "width": f.Width, "height": f.Height,
			"durationMs": f.DurationMs, "posterMediaId": f.PosterMediaId, "sha256": f.Sha256,
			"diskPath": f.DiskPath, "publicPath": f.PublicPath, "cosKey": f.CosKey, "createdAt": f.CreatedAt,
			"user": map[string]any{"username": f.Username, "nickname": f.Nickname},
		})
	}
	util.WriteJSON(w, 200, map[string]any{"files": files, "total": total, "page": page, "pageSize": pageSize})
}

// ============ DELETE /api/admin/media/:id ============

func (h *Handler) deleteMedia(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	if _, err := h.db().Exec(ctx, `DELETE FROM "MediaFile" WHERE "id"=$1`, r.PathValue("id")); err != nil {
		log.Printf("[Admin] 删除媒体失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 举报管理
// ============================================================

// ============ GET /api/admin/reports ============

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	q := r.URL.Query()
	status := q.Get("status")
	page := atoiMax(q.Get("page"), 1, 1)

	where := ""
	var args []any
	if status != "" {
		where = `WHERE "status"=$1`
		args = append(args, status)
	}
	var total int64
	_ = h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "Report" `+where, args...).Scan(&total)
	reports, err := db.QueryToStructs[db.Report](ctx, h.db(),
		`SELECT "id","reporterId","targetType","targetId","reason","status","resolvedBy","resolvedAt","createdAt"
		 FROM "Report" `+where+` ORDER BY "createdAt" DESC LIMIT 20 OFFSET $`+strconv.Itoa(len(args)+1),
		append(args, (page-1)*20)...)
	if err != nil {
		log.Printf("[Admin] 举报列表查询失败: %v", err)
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"reports": reports, "total": total, "page": page})
}

// ============ POST /api/admin/reports/:id/resolve ============

func (h *Handler) resolveReport(w http.ResponseWriter, r *http.Request) {
	h.setReportStatus(w, r, "resolved")
}

// ============ POST /api/admin/reports/:id/dismiss ============

func (h *Handler) dismissReport(w http.ResponseWriter, r *http.Request) {
	h.setReportStatus(w, r, "dismissed")
}

func (h *Handler) setReportStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	admin := middleware.AdminFrom(r)
	report, err := db.QueryRowToStruct[db.Report](ctx, h.db(),
		`UPDATE "Report" SET "status"=$1,"resolvedBy"=$2,"resolvedAt"=NOW() WHERE "id"=$3
		 RETURNING "id","reporterId","targetType","targetId","reason","status","resolvedBy","resolvedAt","createdAt"`,
		status, admin.Username, id)
	if err != nil {
		log.Printf("[Admin] 处理举报失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "report": report})
}

// ============================================================
// 敏感词
// ============================================================

// ============ GET /api/admin/sensitive-words ============

func (h *Handler) listSensitiveWords(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	words, err := db.QueryToStructs[db.SensitiveWord](ctx, h.db(),
		`SELECT "id","word","category","isActive","createdAt" FROM "SensitiveWord" ORDER BY "createdAt" DESC`)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"words": words})
}

// ============ POST /api/admin/sensitive-words ============

func (h *Handler) createSensitiveWord(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Word     string `json:"word"`
		Category string `json:"category"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Word == "" {
		util.WriteError(w, 400, "请填写敏感词")
		return
	}
	if exists(ctx, h, `SELECT 1 FROM "SensitiveWord" WHERE "word"=$1`, req.Word) {
		util.WriteError(w, 409, "已存在")
		return
	}
	word, err := db.QueryRowToStruct[db.SensitiveWord](ctx, h.db(),
		`INSERT INTO "SensitiveWord"("id","word","category","createdAt") VALUES($1,$2,$3,NOW())
		 RETURNING "id","word","category","isActive","createdAt"`,
		util.NewID(), req.Word, strOrNil(req.Category))
	if err != nil {
		log.Printf("[Admin] 创建敏感词失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "word": word})
}

// ============ PUT /api/admin/sensitive-words/:id ============

func (h *Handler) updateSensitiveWord(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		IsActive *bool `json:"isActive"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if req.IsActive != nil {
		if _, err := h.db().Exec(ctx, `UPDATE "SensitiveWord" SET "isActive"=$1 WHERE "id"=$2`, *req.IsActive, id); err != nil {
			util.WriteError(w, 500, "操作失败")
			return
		}
	}
	word, err := db.QueryRowToStruct[db.SensitiveWord](ctx, h.db(),
		`SELECT "id","word","category","isActive","createdAt" FROM "SensitiveWord" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "敏感词不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "word": word})
}

// ============ DELETE /api/admin/sensitive-words/:id ============

func (h *Handler) deleteSensitiveWord(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	if _, err := h.db().Exec(ctx, `DELETE FROM "SensitiveWord" WHERE "id"=$1`, r.PathValue("id")); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// IP 黑名单
// ============================================================

// ============ GET /api/admin/ip-blacklist ============

func (h *Handler) listIPBlacklist(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	list, err := db.QueryToStructs[db.IpBlacklist](ctx, h.db(),
		`SELECT "id","ip","reason","createdAt","updatedAt" FROM "IpBlacklist" ORDER BY "createdAt" DESC`)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"list": list})
}

// ============ POST /api/admin/ip-blacklist ============

func (h *Handler) createIPBlacklist(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.IP == "" {
		util.WriteError(w, 400, "请填写 IP")
		return
	}
	if exists(ctx, h, `SELECT 1 FROM "IpBlacklist" WHERE "ip"=$1`, req.IP) {
		util.WriteError(w, 409, "已在黑名单")
		return
	}
	entry, err := db.QueryRowToStruct[db.IpBlacklist](ctx, h.db(),
		`INSERT INTO "IpBlacklist"("id","ip","reason","createdAt","updatedAt") VALUES($1,$2,$3,NOW(),NOW())
		 RETURNING "id","ip","reason","createdAt","updatedAt"`,
		util.NewID(), req.IP, strOrNil(req.Reason))
	if err != nil {
		log.Printf("[Admin] 添加IP黑名单失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "entry": entry})
}

// ============ DELETE /api/admin/ip-blacklist/:id ============

func (h *Handler) deleteIPBlacklist(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	if _, err := h.db().Exec(ctx, `DELETE FROM "IpBlacklist" WHERE "id"=$1`, r.PathValue("id")); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 非法请求日志
// ============================================================

// ============ GET /api/admin/illegal-requests ============

func (h *Handler) listIllegalRequests(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	page := atoiMax(r.URL.Query().Get("page"), 1, 1)
	var total int64
	_ = h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "IllegalRequest"`).Scan(&total)
	list, err := db.QueryToStructs[db.IllegalRequest](ctx, h.db(),
		`SELECT "id","ip","path","method","userAgent","reason","statusCode","createdAt"
		 FROM "IllegalRequest" ORDER BY "createdAt" DESC LIMIT 20 OFFSET $1`, (page-1)*20)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"list": list, "total": total, "page": page})
}

// ============ DELETE /api/admin/illegal-requests 清空 ============

func (h *Handler) clearIllegalRequests(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db().Exec(h.ctx(r), `DELETE FROM "IllegalRequest"`); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 登录日志
// ============================================================

// ============ GET /api/admin/login-logs ============

func (h *Handler) listLoginLogs(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	q := r.URL.Query()
	filter := q.Get("filter")
	if filter == "" {
		filter = "all"
	}
	page := atoiMax(q.Get("page"), 1, 1)
	where := ""
	switch filter {
	case "success":
		where = `WHERE "success"=true`
	case "fail":
		where = `WHERE "success"=false`
	}
	var total int64
	_ = h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "LoginLog" `+where).Scan(&total)
	list, err := db.QueryToStructs[db.LoginLog](ctx, h.db(),
		`SELECT "id","userId","username","email","phone","ip","userAgent","country","city","success","failReason","loginType","createdAt"
		 FROM "LoginLog" `+where+` ORDER BY "createdAt" DESC LIMIT 20 OFFSET $1`, (page-1)*20)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"list": list, "total": total, "page": page})
}

// ============ DELETE /api/admin/login-logs 清空 ============

func (h *Handler) clearLoginLogs(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db().Exec(h.ctx(r), `DELETE FROM "LoginLog"`); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 公告
// ============================================================

// ============ GET /api/admin/announcements ============

func (h *Handler) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	list, err := db.QueryToStructs[db.Announcement](ctx, h.db(),
		`SELECT "id","title","content","type","isActive","createdBy","createdAt","updatedAt"
		 FROM "Announcement" ORDER BY "createdAt" DESC`)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"announcements": list})
}

// ============ POST /api/admin/announcements ============

func (h *Handler) createAnnouncement(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Type    string `json:"type"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Title == "" || req.Content == "" {
		util.WriteError(w, 400, "请填写标题和内容")
		return
	}
	if req.Type == "" {
		req.Type = "info"
	}
	admin := middleware.AdminFrom(r)
	a, err := db.QueryRowToStruct[db.Announcement](ctx, h.db(),
		`INSERT INTO "Announcement"("id","title","content","type","isActive","createdBy","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,true,$5,NOW(),NOW())
		 RETURNING "id","title","content","type","isActive","createdBy","createdAt","updatedAt"`,
		util.NewID(), req.Title, req.Content, req.Type, admin.Username)
	if err != nil {
		log.Printf("[Admin] 创建公告失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "announcement": a})
}

// ============ PUT /api/admin/announcements/:id ============

func (h *Handler) updateAnnouncement(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	var sets []string
	var args []any
	n := 1
	for _, col := range []string{"title", "content", "type", "isActive"} {
		if v, ok := body[col]; ok {
			sets = append(sets, fmt.Sprintf(`"%s"=$%d`, col, n))
			args = append(args, v)
			n++
		}
	}
	if len(sets) > 0 {
		sets = append(sets, `"updatedAt"=NOW()`)
		args = append(args, id)
		if _, err := h.db().Exec(ctx,
			`UPDATE "Announcement" SET `+strings.Join(sets, ",")+` WHERE "id"=$`+strconv.Itoa(n), args...); err != nil {
			log.Printf("[Admin] 更新公告失败: %v", err)
			util.WriteError(w, 500, "操作失败")
			return
		}
	}
	a, err := db.QueryRowToStruct[db.Announcement](ctx, h.db(),
		`SELECT "id","title","content","type","isActive","createdBy","createdAt","updatedAt" FROM "Announcement" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "公告不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "announcement": a})
}

// ============ DELETE /api/admin/announcements/:id ============

func (h *Handler) deleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db().Exec(h.ctx(r), `DELETE FROM "Announcement" WHERE "id"=$1`, r.PathValue("id")); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 操作日志
// ============================================================

// ============ GET /api/admin/logs ============

func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	page := atoiMax(r.URL.Query().Get("page"), 1, 1)
	var total int64
	_ = h.db().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM "AdminLog"`).Scan(&total)
	logs, err := db.QueryToStructs[db.AdminLog](ctx, h.db(),
		`SELECT "id","adminId","adminName","action","target","detail","ip","createdAt"
		 FROM "AdminLog" ORDER BY "createdAt" DESC LIMIT 20 OFFSET $1`, (page-1)*20)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"logs": logs, "total": total, "page": page})
}

// ============================================================
// 自定义外链页面
// ============================================================

// ============ GET /api/admin/custom-pages ============

func (h *Handler) listCustomPages(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	pages, err := db.QueryToStructs[db.CustomPage](ctx, h.db(),
		`SELECT "id","title","slug","content","published","sortOrder","createdAt","updatedAt"
		 FROM "CustomPage" ORDER BY "sortOrder" ASC, "createdAt" DESC`)
	if err != nil {
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"pages": pages})
}

// ============ POST /api/admin/custom-pages ============

func (h *Handler) createCustomPage(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	var req struct {
		Title     string `json:"title"`
		Slug      string `json:"slug"`
		Content   string `json:"content"`
		Published bool   `json:"published"`
		SortOrder int32  `json:"sortOrder"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Title == "" || req.Slug == "" || req.Content == "" {
		util.WriteError(w, 400, "请填写标题、slug 和内容")
		return
	}
	if exists(ctx, h, `SELECT 1 FROM "CustomPage" WHERE "slug"=$1`, req.Slug) {
		util.WriteError(w, 409, "slug 已存在")
		return
	}
	page, err := db.QueryRowToStruct[db.CustomPage](ctx, h.db(),
		`INSERT INTO "CustomPage"("id","title","slug","content","published","sortOrder","createdAt","updatedAt")
		 VALUES($1,$2,$3,$4,$5,$6,NOW(),NOW())
		 RETURNING "id","title","slug","content","published","sortOrder","createdAt","updatedAt"`,
		util.NewID(), req.Title, req.Slug, req.Content, req.Published, req.SortOrder)
	if err != nil {
		log.Printf("[Admin] 创建自定义页面失败: %v", err)
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "page": page})
}

// ============ PUT /api/admin/custom-pages/:id ============

func (h *Handler) updateCustomPage(w http.ResponseWriter, r *http.Request) {
	ctx := h.ctx(r)
	id := r.PathValue("id")
	var body map[string]any
	if !decodeMap(w, r, &body) {
		return
	}
	var sets []string
	var args []any
	n := 1
	for _, col := range []string{"title", "slug", "content", "published", "sortOrder"} {
		if v, ok := body[col]; ok {
			sets = append(sets, fmt.Sprintf(`"%s"=$%d`, col, n))
			args = append(args, v)
			n++
		}
	}
	if len(sets) > 0 {
		sets = append(sets, `"updatedAt"=NOW()`)
		args = append(args, id)
		if _, err := h.db().Exec(ctx,
			`UPDATE "CustomPage" SET `+strings.Join(sets, ",")+` WHERE "id"=$`+strconv.Itoa(n), args...); err != nil {
			log.Printf("[Admin] 更新自定义页面失败: %v", err)
			util.WriteError(w, 500, "操作失败")
			return
		}
	}
	page, err := db.QueryRowToStruct[db.CustomPage](ctx, h.db(),
		`SELECT "id","title","slug","content","published","sortOrder","createdAt","updatedAt" FROM "CustomPage" WHERE "id"=$1`, id)
	if err != nil {
		if db.IsNotFound(err) {
			util.WriteError(w, 404, "页面不存在")
			return
		}
		util.WriteError(w, 500, "查询失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true, "page": page})
}

// ============ DELETE /api/admin/custom-pages/:id ============

func (h *Handler) deleteCustomPage(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db().Exec(h.ctx(r), `DELETE FROM "CustomPage" WHERE "id"=$1`, r.PathValue("id")); err != nil {
		util.WriteError(w, 500, "操作失败")
		return
	}
	util.WriteJSON(w, 200, map[string]any{"success": true})
}

// ============================================================
// 小工具
// ============================================================

// decodeMap 解析请求体为 map，空 body 视为 {}（与 TS ...req.body 语义一致）。
func decodeMap(w http.ResponseWriter, r *http.Request, v *map[string]any) bool {
	*v = map[string]any{}
	if r.ContentLength == 0 {
		return true
	}
	return util.DecodeJSON(w, r, v)
}
