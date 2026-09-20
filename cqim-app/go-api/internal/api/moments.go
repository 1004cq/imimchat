package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// registerMomentsRoutes mirrors server/moments.ts.  Media is deliberately a
// reference only: binary uploads stay on the existing /api/media MinIO path.
func (s *Server) registerMomentsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/moments/feed", s.requireUser(s.momentsFeed))
	mux.HandleFunc("GET /api/moments/my", s.requireUser(s.momentsMy))
	mux.HandleFunc("GET /api/moments/topics/hot", s.momentsTopics)
	mux.HandleFunc("GET /api/moments", s.momentsList)
	mux.HandleFunc("GET /api/moments/{$}", s.momentsList)
	mux.HandleFunc("POST /api/moments", s.requireUser(s.momentsCreate))
	mux.HandleFunc("POST /api/moments/{$}", s.requireUser(s.momentsCreate))
	mux.HandleFunc("PUT /api/moments/reorder", s.requireUser(s.momentsReorder))
	mux.HandleFunc("GET /api/moments/{id}", s.momentDetail)
	mux.HandleFunc("PUT /api/moments/{id}", s.requireUser(s.momentUpdate))
	mux.HandleFunc("DELETE /api/moments/{id}", s.requireUser(s.momentDelete))
	mux.HandleFunc("POST /api/moments/{id}/like", s.requireUser(s.momentLike))
	mux.HandleFunc("POST /api/moments/{id}/comments", s.requireUser(s.momentComment))
	mux.HandleFunc("DELETE /api/moments/{momentId}/comments/{commentId}", s.requireUser(s.momentCommentDelete))
	mux.HandleFunc("POST /api/moments/{id}/pin", s.requireUser(s.momentPin))
}

type momentMediaInput struct {
	ID       string `json:"id"`
	MediaID  string `json:"mediaId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Duration int    `json:"duration"`
}

type momentRecord struct {
	ID string; UserID string; Content string; Visibility string; Location *string; Topics *string
	Pinned bool; PinnedAt *time.Time; ViewCount int; SortOrder int; CreatedAt time.Time; UpdatedAt time.Time
}

func (s *Server) optionalMomentUser(r *http.Request) *user {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" { return nil }
	u, err := s.findSessionUser(r.Context(), token)
	if err != nil { return nil }
	return &u
}

func momentTopics(raw *string) []string {
	if raw == nil || *raw == "" { return []string{} }
	return strings.Split(*raw, ",")
}

func momentVisibilityOK(value string) bool { return value == "public" || value == "friends" || value == "private" }

func (s *Server) momentFriends(ctx context.Context, a, b string) bool {
	var ok bool
	_ = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM "Friendship" WHERE ("userA"=$1 AND "userB"=$2) OR ("userA"=$2 AND "userB"=$1))`, a, b).Scan(&ok)
	return ok
}

func (s *Server) momentVisible(ctx context.Context, m momentRecord, u *user) bool {
	if m.Visibility == "public" { return true }
	if u == nil { return false }
	if u.ID == m.UserID { return true }
	return m.Visibility == "friends" && s.momentFriends(ctx, u.ID, m.UserID)
}

func (s *Server) fetchMoment(ctx context.Context, id string) (momentRecord, error) {
	var m momentRecord
	err := s.db.QueryRow(ctx, `SELECT "id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt" FROM "Moment" WHERE "id"=$1`, id).Scan(&m.ID,&m.UserID,&m.Content,&m.Visibility,&m.Location,&m.Topics,&m.Pinned,&m.PinnedAt,&m.ViewCount,&m.SortOrder,&m.CreatedAt,&m.UpdatedAt)
	return m, err
}

func (s *Server) momentMedia(ctx context.Context, id string) []map[string]any {
	rows, err := s.db.Query(ctx, `SELECT "id","type","url","width","height","duration","sortOrder" FROM "MomentMedia" WHERE "momentId"=$1 ORDER BY "sortOrder" ASC`, id)
	if err != nil { return []map[string]any{} }
	defer rows.Close(); out:=[]map[string]any{}
	for rows.Next() { var mid,typ,url string; var w,h,d *int; var order int; if rows.Scan(&mid,&typ,&url,&w,&h,&d,&order)==nil { out=append(out,map[string]any{"id":mid,"momentId":id,"type":typ,"url":url,"width":w,"height":h,"duration":d,"sortOrder":order}) } }
	return out
}

func (s *Server) momentAuthor(ctx context.Context, id string) map[string]any {
	var uid, name, nick, av, bio string
	var bg *string
	if s.db.QueryRow(ctx, `SELECT "id","username",COALESCE("nickname",''),COALESCE("avatar",''),"backgroundUrl",COALESCE("bio",'') FROM "User" WHERE "id"=$1`, id).
		Scan(&uid, &name, &nick, &av, &bg, &bio) != nil {
		return map[string]any{"id": id, "username": "", "nickname": "", "avatar": "", "backgroundUrl": nil, "bio": ""}
	}
	return map[string]any{
		"id": uid, "username": name, "nickname": nick,
		"avatar": safeAvatarUrl(av), "backgroundUrl": bg, "bio": bio,
	}
}

func (s *Server) momentCounts(ctx context.Context, id string) (int,int) { var likes,comments int; _=s.db.QueryRow(ctx,`SELECT (SELECT COUNT(*) FROM "MomentLike" WHERE "momentId"=$1),(SELECT COUNT(*) FROM "MomentComment" WHERE "momentId"=$1)`,id).Scan(&likes,&comments); return likes,comments }
func (s *Server) momentLiked(ctx context.Context,id,uid string) bool { if uid=="" {return false}; var yes bool; _=s.db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM "MomentLike" WHERE "momentId"=$1 AND "userId"=$2)`,id,uid).Scan(&yes);return yes }

func (s *Server) momentLikes(ctx context.Context, id string) []map[string]any {
	rows, e := s.db.Query(ctx, `SELECT l."userId",COALESCE(u."nickname",u."username",l."userId"),COALESCE(u."avatar",''),l."createdAt" FROM "MomentLike" l LEFT JOIN "User" u ON u."id"=l."userId" WHERE l."momentId"=$1 ORDER BY l."createdAt" ASC LIMIT 20`, id)
	if e != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var uid, name, av string
		var t time.Time
		if rows.Scan(&uid, &name, &av, &t) == nil {
			out = append(out, map[string]any{"userId": uid, "userName": name, "userAvatar": safeAvatarUrl(av), "createdAt": t.UnixMilli()})
		}
	}
	return out
}

func (s *Server) momentComments(ctx context.Context, id string, nested bool) []map[string]any {
	rows, e := s.db.Query(ctx, `SELECT c."id",c."userId",c."content",c."replyToId",c."createdAt",COALESCE(u."username",''),COALESCE(u."nickname",''),COALESCE(u."avatar",'') FROM "MomentComment" c LEFT JOIN "User" u ON u."id"=c."userId" WHERE c."momentId"=$1 ORDER BY c."createdAt" ASC`, id)
	if e != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	all := []map[string]any{}
	children := map[string][]map[string]any{}
	roots := []map[string]any{}
	for rows.Next() {
		var cid, uid, content, uname, nick, av string
		var reply *string
		var created time.Time
		if rows.Scan(&cid, &uid, &content, &reply, &created, &uname, &nick, &av) != nil {
			continue
		}
		safeAv := safeAvatarUrl(av)
		user := map[string]any{"id": uid, "username": uname, "nickname": nick, "avatar": safeAv}
		c := map[string]any{
			"id": cid, "momentId": id, "userId": uid, "content": content, "replyToId": reply,
			"createdAt": created.UnixMilli(), "user": user, "userName": firstMomentName(nick, uname), "userAvatar": safeAv,
		}
		all = append(all, c)
		if reply == nil {
			roots = append(roots, c)
		} else {
			children[*reply] = append(children[*reply], c)
		}
	}
	if !nested {
		return all
	}
	for _, root := range roots {
		root["replies"] = children[root["id"].(string)]
	}
	return roots
}

func firstMomentName(nick, username string) string {
	if nick != "" {
		return nick
	}
	return username
}

func (s *Server) momentJSON(ctx context.Context, m momentRecord, viewer *user, detailed bool) map[string]any {
	likes, comments := s.momentCounts(ctx, m.ID)
	author := s.momentAuthor(ctx, m.UserID)
	authorName := firstMomentName(author["nickname"].(string), author["username"].(string))
	authorAvatar := author["avatar"].(string)
	var pinnedAt any
	if m.PinnedAt != nil {
		pinnedAt = m.PinnedAt.UnixMilli()
	}
	out := map[string]any{
		"id": m.ID, "userId": m.UserID, "authorId": m.UserID, "authorName": authorName, "authorAvatar": authorAvatar,
		"content": m.Content, "visibility": m.Visibility, "location": m.Location, "topics": momentTopics(m.Topics),
		"isPinned": m.Pinned, "pinnedAt": pinnedAt, "viewCount": m.ViewCount, "sortOrder": m.SortOrder,
		"createdAt": m.CreatedAt.UnixMilli(), "updatedAt": m.UpdatedAt.UnixMilli(),
		"user": author, "media": s.momentMedia(ctx, m.ID), "likeCount": likes, "commentCount": comments, "isLiked": false,
	}
	if viewer != nil {
		out["isLiked"] = s.momentLiked(ctx, m.ID, viewer.ID)
	}
	if detailed {
		out["likes"] = s.momentLikes(ctx, m.ID)
		out["comments"] = s.momentComments(ctx, m.ID, true)
	}
	return out
}

func (s *Server) momentsFeed(w http.ResponseWriter,r *http.Request,u user) {
	limit:=intQuery(r,"limit",10);if limit>20{limit=20};cursor:=groupQuery(r,"cursor"); friends:=[]string{u.ID}; rows,e:=s.db.Query(r.Context(),`SELECT "userA","userB" FROM "Friendship" WHERE "userA"=$1 OR "userB"=$1`,u.ID);if e==nil{for rows.Next(){var a,b string;if rows.Scan(&a,&b)==nil{if a==u.ID{friends=append(friends,b)}else{friends=append(friends,a)}}};rows.Close()};args:=[]any{friends};where:=`("userId"=ANY($1) AND ("userId"=$2 OR "visibility" IN ('public','friends')))`;args=append(args,u.ID);if cursor!=""{if t,e:=time.Parse(time.RFC3339,cursor);e==nil{args=append(args,t);where+=` AND "createdAt"<$3`}};q:=`SELECT "id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt" FROM "Moment" WHERE `+where+` ORDER BY "isPinned" DESC,"pinnedAt" DESC NULLS LAST,"createdAt" DESC LIMIT `+strconv.Itoa(limit+1);rows,e=s.db.Query(r.Context(),q,args...);if e!=nil{dbError(w,e);return};defer rows.Close();items:=[]momentRecord{};for rows.Next(){var m momentRecord;if rows.Scan(&m.ID,&m.UserID,&m.Content,&m.Visibility,&m.Location,&m.Topics,&m.Pinned,&m.PinnedAt,&m.ViewCount,&m.SortOrder,&m.CreatedAt,&m.UpdatedAt)==nil{items=append(items,m)}};more:=len(items)>limit;if more{items=items[:limit]};out:=[]map[string]any{};for _,m:=range items{out=append(out,s.momentJSON(r.Context(),m,&u,true))};var next any;if more{next=items[len(items)-1].CreatedAt.UTC().Format(time.RFC3339)};writeJSON(w,200,map[string]any{"moments":out,"hasMore":more,"nextCursor":next,"_ts":time.Now().UnixMilli()})
}

func (s *Server) momentsList(w http.ResponseWriter,r *http.Request) {
	viewer:=s.optionalMomentUser(r); uid:=groupQuery(r,"userId");limit:=intQuery(r,"limit",10);if limit>20{limit=20};cursor:=groupQuery(r,"cursor");args:=[]any{};where:=`"visibility"='public'`;if uid!=""{args=append(args,uid);where=`"userId"=$1`;if viewer==nil{where+=` AND "visibility"='public'`}else if viewer.ID!=uid{args=append(args,viewer.ID);where+=` AND ("visibility"='public' OR ("visibility"='friends' AND EXISTS(SELECT 1 FROM "Friendship" f WHERE (f."userA"="Moment"."userId" AND f."userB"=$2) OR (f."userB"="Moment"."userId" AND f."userA"=$2))))`}};if cursor!=""{if t,e:=time.Parse(time.RFC3339,cursor);e==nil{args=append(args,t);where+=` AND "createdAt"<$`+strconv.Itoa(len(args))}};q:=`SELECT "id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt" FROM "Moment" WHERE `+where+` ORDER BY "isPinned" DESC,"pinnedAt" DESC NULLS LAST,"createdAt" DESC LIMIT `+strconv.Itoa(limit+1);rows,e:=s.db.Query(r.Context(),q,args...);if e!=nil{dbError(w,e);return};defer rows.Close();items:=[]momentRecord{};for rows.Next(){var m momentRecord;if rows.Scan(&m.ID,&m.UserID,&m.Content,&m.Visibility,&m.Location,&m.Topics,&m.Pinned,&m.PinnedAt,&m.ViewCount,&m.SortOrder,&m.CreatedAt,&m.UpdatedAt)==nil{items=append(items,m)}};more:=len(items)>limit;if more{items=items[:limit]};out:=[]map[string]any{};for _,m:=range items{out=append(out,s.momentJSON(r.Context(),m,viewer,uid!=""))};var next any;if more{next=items[len(items)-1].CreatedAt.UTC().Format(time.RFC3339)};result:=map[string]any{"moments":out,"hasMore":more,"nextCursor":next,"_ts":time.Now().UnixMilli()};if uid!=""{var total int;_=s.db.QueryRow(r.Context(),`SELECT COUNT(*) FROM "Moment" WHERE "userId"=$1 AND "visibility"='public'`,uid).Scan(&total);result["total"]=total;result["user"]=s.momentAuthor(r.Context(),uid)};writeJSON(w,200,result)
}

func (s *Server) momentsCreate(w http.ResponseWriter,r *http.Request,u user) {
	var in struct{Content string `json:"content"`;Visibility string `json:"visibility"`;Location string `json:"location"`;Topics any `json:"topics"`;Media []momentMediaInput `json:"media"`};if decode(r,&in)!=nil{groupBad(w,"请求无效");return};content:=strings.TrimSpace(in.Content);if len(content)>5000{groupBad(w,"内容超出长度限制（5000 字）");return};if len(in.Media)>9{groupBad(w,"最多上传 9 张图片/视频");return};if content==""&&len(in.Media)==0{groupBad(w,"请输入内容或上传图片/视频");return};if in.Visibility==""{in.Visibility="public"};if !momentVisibilityOK(in.Visibility){groupBad(w,"可见范围无效");return};topics:="";switch v:=in.Topics.(type){case string:topics=v;case []any: parts:=[]string{};for _,x:=range v{if x,ok:=x.(string);ok&&x!=""{parts=append(parts,x)}};topics=strings.Join(parts,",")};if len(topics)>200{topics=topics[:200]};if len(in.Location)>200{in.Location=in.Location[:200]};videos:=0;resolved:=make([]momentMediaInput,0,len(in.Media));for _,m:=range in.Media{if m.Type==""{m.Type="image"};if m.Type!="image"&&m.Type!="video"{writeJSON(w,400,map[string]string{"error":"不支持的媒体类型: "+m.Type});return};if m.Type=="video"{videos++};if videos>1{groupBad(w,"最多上传 1 个视频");return};mid:=m.MediaID;if mid==""{mid=m.ID};if mid!=""{var path string;if e:=s.db.QueryRow(r.Context(),`SELECT COALESCE(NULLIF("publicPath",''),"url") FROM "MediaFile" WHERE "id"=$1`,mid).Scan(&path);e!=nil{writeJSON(w,400,map[string]string{"error":"媒体不存在"});return};m.URL=path}else if strings.HasPrefix(m.URL,"/api/media/"){id:=strings.TrimPrefix(m.URL,"/api/media/");var exists bool;_=s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM "MediaFile" WHERE "id"=$1)`,id).Scan(&exists);if !exists{writeJSON(w,400,map[string]string{"error":"媒体不存在"});return}}else if !strings.HasPrefix(m.URL,"https://"){groupBad(w,"媒体 URL 格式无效");return};resolved=append(resolved,m)};tx,e:=s.db.Begin(r.Context());if e!=nil{dbError(w,e);return};defer tx.Rollback(r.Context());id:=newID();_,e=tx.Exec(r.Context(),`INSERT INTO "Moment" ("id","userId","content","visibility","location","topics","createdAt","updatedAt") VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NOW(),NOW())`,id,u.ID,content,in.Visibility,in.Location,topics);if e!=nil{dbError(w,e);return};for i,m:=range resolved{_,e=tx.Exec(r.Context(),`INSERT INTO "MomentMedia" ("id","momentId","type","url","width","height","duration","sortOrder","createdAt") VALUES ($1,$2,$3,$4,NULLIF($5,0),NULLIF($6,0),NULLIF($7,0),$8,NOW())`,newID(),id,m.Type,m.URL,m.Width,m.Height,m.Duration,i);if e!=nil{dbError(w,e);return}};if e=tx.Commit(r.Context());e!=nil{dbError(w,e);return};m,e:=s.fetchMoment(r.Context(),id);if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]any{"success":true,"moment":s.momentJSON(r.Context(),m,&u,false)})
}

func (s *Server) momentsMy(w http.ResponseWriter,r *http.Request,u user) { rows,e:=s.db.Query(r.Context(),`SELECT "id","userId","content","visibility","location","topics","isPinned","pinnedAt","viewCount","sortOrder","createdAt","updatedAt" FROM "Moment" WHERE "userId"=$1 ORDER BY "sortOrder" ASC,"createdAt" DESC`,u.ID);if e!=nil{dbError(w,e);return};defer rows.Close();out:=[]map[string]any{};for rows.Next(){var m momentRecord;if rows.Scan(&m.ID,&m.UserID,&m.Content,&m.Visibility,&m.Location,&m.Topics,&m.Pinned,&m.PinnedAt,&m.ViewCount,&m.SortOrder,&m.CreatedAt,&m.UpdatedAt)==nil{v:=s.momentJSON(r.Context(),m,&u,false);out=append(out,map[string]any{"id":m.ID,"content":m.Content,"visibility":m.Visibility,"location":m.Location,"isPinned":m.Pinned,"sortOrder":m.SortOrder,"createdAt":m.CreatedAt.UnixMilli(),"media":v["media"],"likeCount":v["likeCount"],"commentCount":v["commentCount"]})}};writeJSON(w,200,map[string]any{"moments":out}) }

func (s *Server) momentDetail(w http.ResponseWriter,r *http.Request) { viewer:=s.optionalMomentUser(r);m,e:=s.fetchMoment(r.Context(),r.PathValue("id"));if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};if e!=nil{dbError(w,e);return};if !s.momentVisible(r.Context(),m,viewer){writeJSON(w,404,map[string]string{"error":"动态不存在"});return};writeJSON(w,200,s.momentJSON(r.Context(),m,viewer,true)) }
func (s *Server) momentDelete(w http.ResponseWriter,r *http.Request,u user){m,e:=s.fetchMoment(r.Context(),r.PathValue("id"));if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};if e!=nil{dbError(w,e);return};if m.UserID!=u.ID{writeJSON(w,403,map[string]string{"error":"无权删除"});return};_,e=s.db.Exec(r.Context(),`DELETE FROM "Moment" WHERE "id"=$1`,m.ID);if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) momentLike(w http.ResponseWriter,r *http.Request,u user){id:=r.PathValue("id");if _,e:=s.fetchMoment(r.Context(),id);e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};var exists bool;_=s.db.QueryRow(r.Context(),`SELECT EXISTS(SELECT 1 FROM "MomentLike" WHERE "momentId"=$1 AND "userId"=$2)`,id,u.ID).Scan(&exists);if exists{_,e:=s.db.Exec(r.Context(),`DELETE FROM "MomentLike" WHERE "momentId"=$1 AND "userId"=$2`,id,u.ID);if e!=nil{dbError(w,e);return}}else{_,e:=s.db.Exec(r.Context(),`INSERT INTO "MomentLike" ("id","momentId","userId","createdAt") VALUES ($1,$2,$3,NOW())`,newID(),id,u.ID);if e!=nil{dbError(w,e);return}};likes,_:=s.momentCounts(r.Context(),id);if !exists{s.momentPublish(r.Context(),"moment_like_notify",id,u.ID,"",nil)};writeJSON(w,200,map[string]any{"liked":!exists,"likeCount":likes})}
func (s *Server) momentComment(w http.ResponseWriter,r *http.Request,u user){var in struct{Content string `json:"content"`;ReplyToID *string `json:"replyToId"`};if decode(r,&in)!=nil||strings.TrimSpace(in.Content)==""{groupBad(w,"评论内容不能为空");return};if len(in.Content)>1000{groupBad(w,"评论内容超出长度限制（1000 字） ");return};id:=r.PathValue("id");if _,e:=s.fetchMoment(r.Context(),id);e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};cid:=newID();_,e:=s.db.Exec(r.Context(),`INSERT INTO "MomentComment" ("id","momentId","userId","content","replyToId","createdAt","updatedAt") VALUES ($1,$2,$3,$4,$5,NOW(),NOW())`,cid,id,u.ID,strings.TrimSpace(in.Content),in.ReplyToID);if e!=nil{dbError(w,e);return};comment:=map[string]any{"id":cid,"momentId":id,"userId":u.ID,"content":strings.TrimSpace(in.Content),"replyToId":in.ReplyToID,"createdAt":time.Now(),"user":map[string]any{"id":u.ID,"username":u.Username,"nickname":u.Nickname,"avatar":safeAvatarUrl(deref(u.Avatar))}};s.momentPublish(r.Context(),"moment_comment_notify",id,u.ID,strings.TrimSpace(in.Content),in.ReplyToID);writeJSON(w,200,map[string]any{"success":true,"comment":comment})}
func (s *Server) momentCommentDelete(w http.ResponseWriter,r *http.Request,u user){var uid,owner string;e:=s.db.QueryRow(r.Context(),`SELECT c."userId",m."userId" FROM "MomentComment" c JOIN "Moment" m ON m."id"=c."momentId" WHERE c."id"=$1 AND c."momentId"=$2`,r.PathValue("commentId"),r.PathValue("momentId")).Scan(&uid,&owner);if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"评论不存在"});return};if e!=nil{dbError(w,e);return};if uid!=u.ID&&owner!=u.ID{writeJSON(w,403,map[string]string{"error":"无权删除"});return};_,e=s.db.Exec(r.Context(),`DELETE FROM "MomentComment" WHERE "id"=$1`,r.PathValue("commentId"));if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) momentPin(w http.ResponseWriter,r *http.Request,u user){var in struct{Pin bool `json:"pin"`};if decode(r,&in)!=nil{groupBad(w,"请求无效");return};m,e:=s.fetchMoment(r.Context(),r.PathValue("id"));if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};if e!=nil{dbError(w,e);return};if m.UserID!=u.ID{writeJSON(w,403,map[string]string{"error":"只有作者可以置顶"});return};if in.Pin{_,_=s.db.Exec(r.Context(),`UPDATE "Moment" SET "isPinned"=false,"pinnedAt"=NULL,"updatedAt"=NOW() WHERE "userId"=$1 AND "isPinned"=true`,u.ID)};var t any;if in.Pin{t=time.Now()};_,e=s.db.Exec(r.Context(),`UPDATE "Moment" SET "isPinned"=$2,"pinnedAt"=$3,"updatedAt"=NOW() WHERE "id"=$1`,m.ID,in.Pin,t);if e!=nil{dbError(w,e);return};updated,_:=s.fetchMoment(r.Context(),m.ID);writeJSON(w,200,map[string]any{"success":true,"moment":s.momentJSON(r.Context(),updated,&u,false)})}
func (s *Server) momentsReorder(w http.ResponseWriter,r *http.Request,u user){var in struct{IDs []string `json:"ids"`};if decode(r,&in)!=nil||len(in.IDs)==0{groupBad(w,"请提供排序列表");return};tx,e:=s.db.Begin(r.Context());if e!=nil{dbError(w,e);return};defer tx.Rollback(r.Context());for i,id:=range in.IDs{_,e=tx.Exec(r.Context(),`UPDATE "Moment" SET "sortOrder"=$3,"updatedAt"=NOW() WHERE "id"=$1 AND "userId"=$2`,id,u.ID,i+1);if e!=nil{dbError(w,e);return}};if e=tx.Commit(r.Context());e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) momentUpdate(w http.ResponseWriter,r *http.Request,u user){var in struct{Content *string `json:"content"`;Visibility *string `json:"visibility"`;Location *string `json:"location"`};if decode(r,&in)!=nil{groupBad(w,"请求无效");return};m,e:=s.fetchMoment(r.Context(),r.PathValue("id"));if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"动态不存在"});return};if e!=nil{dbError(w,e);return};if m.UserID!=u.ID{writeJSON(w,403,map[string]string{"error":"只有作者可以编辑"});return};content:=m.Content;visibility:=m.Visibility;location:=m.Location;if in.Content!=nil{content=strings.TrimSpace(*in.Content);if len(content)>5000{groupBad(w,"内容超出长度限制（5000 字）");return}};if in.Visibility!=nil{if !momentVisibilityOK(*in.Visibility){groupBad(w,"可见范围无效");return};visibility=*in.Visibility};if in.Location!=nil{v:=*in.Location;if len(v)>200{v=v[:200]};location=&v};_,e=s.db.Exec(r.Context(),`UPDATE "Moment" SET "content"=$2,"visibility"=$3,"location"=$4,"updatedAt"=NOW() WHERE "id"=$1`,m.ID,content,visibility,location);if e!=nil{dbError(w,e);return};updated,_:=s.fetchMoment(r.Context(),m.ID);writeJSON(w,200,map[string]any{"success":true,"moment":s.momentJSON(r.Context(),updated,&u,false)})}
func (s *Server) momentsTopics(w http.ResponseWriter,r *http.Request){rows,e:=s.db.Query(r.Context(),`SELECT "topics" FROM "Moment" WHERE "topics" IS NOT NULL LIMIT 500`);if e!=nil{dbError(w,e);return};defer rows.Close();counts:=map[string]int{};for rows.Next(){var raw string;if rows.Scan(&raw)==nil{for _,t:=range strings.Split(raw,","){if t!=""{counts[t]++}}}};out:=[]map[string]any{};for n,c:=range counts{out=append(out,map[string]any{"name":n,"count":c})};writeJSON(w,200,map[string]any{"topics":out})}
func (s *Server) momentPublish(ctx context.Context, typ,id,actor,content string,reply *string){if s.redis==nil{return};var target string;_ = s.db.QueryRow(ctx,`SELECT "userId" FROM "Moment" WHERE "id"=$1`,id).Scan(&target);if target==""||target==actor{return};payload:=map[string]any{"type":typ,"targetUserId":target,"payload":map[string]any{"momentId":id,"userId":actor,"content":content,"replyToId":reply}};b,_:=json.Marshal(payload);_ = s.redis.Publish(ctx,"moment_events",b).Err()}

