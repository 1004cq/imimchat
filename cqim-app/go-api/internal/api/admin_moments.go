package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Only public Moments moderation data is selected. PrivateMessage/GroupMessage
// are never joined; dashboard reads counts and type metadata only.
const adminMomentJSON = `to_jsonb(m)||jsonb_build_object('user',jsonb_build_object('id',u."id",'username',u."username",'nickname',u."nickname",'avatar',u."avatar"),'media',COALESCE((SELECT jsonb_agg(to_jsonb(mm)) FROM (SELECT * FROM "MomentMedia" WHERE "momentId"=m."id" ORDER BY "sortOrder","id" LIMIT %s) mm),'[]'::jsonb),'_count',jsonb_build_object('comments',(SELECT COUNT(*) FROM "MomentComment" c WHERE c."momentId"=m."id"),'likes',(SELECT COUNT(*) FROM "MomentLike" l WHERE l."momentId"=m."id")))`

func adminPage(r *http.Request) (int, int) {
	page := intQuery(r, "page", 1)
	size := intQuery(r, "pageSize", 20)
	if page > 1000000 {
		page = 1000000
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
func (s *Server) adminMoments(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	page, size := adminPage(r)
	search := r.URL.Query().Get("search")
	where := ` FROM "Moment" m JOIN "User" u ON u."id"=m."userId" WHERE ($1='' OR strpos(m."content",$1)>0 OR strpos(u."username",$1)>0)`
	var total int
	if e := s.db.QueryRow(r.Context(), `SELECT COUNT(*)`+where, search).Scan(&total); e != nil {
		dbError(w, e)
		return
	}
	rows, e := s.db.Query(r.Context(), `SELECT `+fmt.Sprintf(adminMomentJSON, "1")+where+` ORDER BY m."isPinned" DESC,m."createdAt" DESC,m."id" LIMIT $2 OFFSET $3`, search, size, (page-1)*size)
	if e != nil {
		dbError(w, e)
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var data json.RawMessage
		if e = rows.Scan(&data); e != nil {
			dbError(w, e)
			return
		}
		items = append(items, adminJSONDates(data))
	}
	if e = rows.Err(); e != nil {
		dbError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"moments": items, "total": total, "page": page, "pageSize": size})
}
func (s *Server) adminMoment(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	var data json.RawMessage
	e := s.db.QueryRow(r.Context(), `SELECT `+fmt.Sprintf(adminMomentJSON, "ALL")+` FROM "Moment" m JOIN "User" u ON u."id"=m."userId" WHERE m."id"=$1`, r.PathValue("id")).Scan(&data)
	if e != nil {
		adminWriteError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"moment": adminJSONDates(data)})
}
func (s *Server) adminComments(w http.ResponseWriter, r *http.Request, _ adminAccount, _ string) {
	page, size := adminPage(r)
	id := r.PathValue("id")
	var exists bool
	if e := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM "Moment" WHERE "id"=$1)`, id).Scan(&exists); e != nil {
		dbError(w, e)
		return
	}
	if !exists {
		adminWriteError(w, pgx.ErrNoRows)
		return
	}
	var total int
	if e := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM "MomentComment" WHERE "momentId"=$1`, id).Scan(&total); e != nil {
		dbError(w, e)
		return
	}
	rows, e := s.db.Query(r.Context(), `SELECT to_jsonb(c)||jsonb_build_object('user',jsonb_build_object('id',u."id",'username',u."username",'nickname',u."nickname",'avatar',u."avatar")) FROM "MomentComment" c JOIN "User" u ON u."id"=c."userId" WHERE c."momentId"=$1 ORDER BY c."createdAt" DESC,c."id" LIMIT $2 OFFSET $3`, id, size, (page-1)*size)
	if e != nil {
		dbError(w, e)
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var data json.RawMessage
		if e = rows.Scan(&data); e != nil {
			dbError(w, e)
			return
		}
		items = append(items, adminJSONDates(data))
	}
	if e = rows.Err(); e != nil {
		dbError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"comments": items, "total": total, "page": page, "pageSize": size})
}
func itoa(n int) string { return strconv.Itoa(n) }

// Prisma Timestamp(3) columns store UTC without a SQL time zone. PostgreSQL
// to_jsonb omits the zone; sending that string makes JS treat it as local time.
func adminJSONDates(raw json.RawMessage) json.RawMessage {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return raw
	}
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				visit(child)
			}
		case map[string]any:
			for key, child := range node {
				switch key {
				case "createdAt", "updatedAt", "pinnedAt", "lastLoginAt":
					if text, ok := child.(string); ok {
						parsed, err := time.Parse(time.RFC3339Nano, text)
						if err != nil {
							parsed, err = time.Parse("2006-01-02T15:04:05.999999999", text)
						}
						if err == nil {
							node[key] = parsed.UTC().Format("2006-01-02T15:04:05.000Z")
						}
					}
				default:
					visit(child)
				}
			}
		}
	}
	visit(document)
	normalized, err := json.Marshal(document)
	if err != nil {
		return raw
	}
	return normalized
}
