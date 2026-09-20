package api

import (
    "encoding/json"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "golang.org/x/crypto/bcrypt"
)

// registerLegacyRoutes keeps every Node mount addressable while the larger
// feature families are migrated incrementally. Implemented handlers below are
// the low-risk stateful routes that share the existing Go auth and schema.
func (s *Server) registerLegacyRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /api/web-push/public-key", s.webPushPublicKey)
    mux.HandleFunc("POST /api/web-push/subscription", s.requireUser(s.saveWebPushSubscription))
    mux.HandleFunc("DELETE /api/web-push/subscription", s.requireUser(s.clearWebPushSubscription))
    mux.HandleFunc("POST /api/qr/verify", s.requireUser(s.qrVerify))
    mux.HandleFunc("GET /api/stickers/packs", s.stickerPacks)
    mux.HandleFunc("GET /api/stickers/discover", s.stickerPacks)
    mux.HandleFunc("GET /api/stickers/search", s.stickerSearch)
    mux.HandleFunc("GET /api/stickers/status", s.stickerStatus)
    // Auth and the remaining mounted families are listed explicitly in the
    // README; returning JSON keeps clients from receiving an HTML 404 while
    // their full business migration is staged.
    mux.HandleFunc("GET /api/auth/me", s.requireUser(s.me))
    mux.HandleFunc("POST /api/auth/login", s.authLogin)
    mux.HandleFunc("POST /api/auth/register", s.authRegister)
    mux.HandleFunc("POST /api/auth/send-code", s.authSendCode)
    mux.HandleFunc("POST /api/auth/logout", s.requireUser(s.authLogout))
    mux.HandleFunc("/api/auth/", s.notMigrated)
    mux.HandleFunc("POST /api/media/upload", s.mediaUploadJSON)
    mux.HandleFunc("POST /api/media/upload-form", s.mediaUploadForm)
    mux.HandleFunc("GET /api/media/{id}", s.mediaDownload)
    s.registerGroupRoutes(mux)
    s.registerMomentsRoutes(mux)
    mux.HandleFunc("/api/admin/", s.notMigrated)
}
func (s *Server) notMigrated(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusNotImplemented,map[string]string{"error":"该路由尚未迁移到 go-api"})}
func (s *Server) authLogin(w http.ResponseWriter,r *http.Request){var in struct{Account string `json:"account"`;Password string `json:"password"`};if decode(r,&in)!=nil||in.Account==""||in.Password==""{writeJSON(w,400,map[string]string{"error":"请输入账号和密码"});return};var id,username,hash string;var banned bool;e:=s.db.QueryRow(r.Context(),`SELECT "id","username","password","isBanned" FROM "User" WHERE "username"=$1 OR "email"=$1 OR "phone"=$1 OR "id"=$1 LIMIT 1`,in.Account).Scan(&id,&username,&hash,&banned);if e!=nil||bcrypt.CompareHashAndPassword([]byte(hash),[]byte(in.Password))!=nil{writeJSON(w,401,map[string]string{"error":"账号或密码错误"});return};if banned{writeJSON(w,403,map[string]string{"error":"账号已被封禁"});return};token:=newID()+newID();_,e=s.db.Exec(r.Context(),`INSERT INTO "UserSession" ("id","userId","token","expiresAt","createdAt") VALUES ($1,$2,$3,$4,NOW())`,newID(),id,token,time.Now().Add(30*24*time.Hour));if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]any{"success":true,"token":token,"user":map[string]any{"id":id,"username":username}})}
func (s *Server) authRegister(w http.ResponseWriter,r *http.Request){var in struct{Username string `json:"username"`;Password string `json:"password"`;Nickname string `json:"nickname"`};if decode(r,&in)!=nil||in.Username==""||in.Password==""{writeJSON(w,400,map[string]string{"error":"请填写用户ID和密码"});return};if len(in.Username)>20{writeJSON(w,400,map[string]string{"error":"用户ID格式不正确"});return};hash,e:=bcrypt.GenerateFromPassword([]byte(in.Password),bcrypt.DefaultCost);if e!=nil{dbError(w,e);return};id:=newID();token:=newID()+newID();nick:=in.Nickname;if nick==""{nick="用户"+in.Username};_,e=s.db.Exec(r.Context(),`INSERT INTO "User" ("id","username","password","nickname","createdAt","updatedAt") VALUES ($1,$2,$3,$4,NOW(),NOW())`,id,in.Username,string(hash),nick);if e!=nil{writeJSON(w,409,map[string]string{"error":"该用户ID已被使用"});return};_,e=s.db.Exec(r.Context(),`INSERT INTO "UserSession" ("id","userId","token","expiresAt","createdAt") VALUES ($1,$2,$3,$4,NOW())`,newID(),id,token,time.Now().Add(30*24*time.Hour));if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]any{"success":true,"token":token,"user":map[string]any{"id":id,"username":in.Username,"nickname":nick}})}
func (s *Server) authSendCode(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusNotImplemented,map[string]string{"error":"验证码发送仍需配置现有短信/邮件供应商"})}
func (s *Server) authLogout(w http.ResponseWriter,r *http.Request,u user){token:=bearerToken(r.Header.Get("Authorization"));if token!=""{_,_=s.db.Exec(r.Context(),`DELETE FROM "UserSession" WHERE "token"=$1 AND "userId"=$2`,token,u.ID)};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) webPushPublicKey(w http.ResponseWriter,_ *http.Request){key:=os.Getenv("WEB_PUSH_VAPID_PUBLIC_KEY");if key==""{writeJSON(w,503,map[string]any{"enabled":false});return};writeJSON(w,200,map[string]any{"enabled":true,"publicKey":key})}
func (s *Server) saveWebPushSubscription(w http.ResponseWriter,r *http.Request,u user){if os.Getenv("WEB_PUSH_VAPID_PUBLIC_KEY")==""||os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY")==""{writeJSON(w,503,map[string]string{"error":"Web Push 未配置"});return};var in struct{Subscription map[string]any `json:"subscription"`};if decode(r,&in)!=nil||in.Subscription==nil{writeJSON(w,400,map[string]string{"error":"无效的 PushSubscription"});return};endpoint,_:=in.Subscription["endpoint"].(string);keys,_:=in.Subscription["keys"].(map[string]any);p,_:=keys["p256dh"].(string);a,_:=keys["auth"].(string);if !strings.HasPrefix(endpoint,"https://")||p==""||a==""{writeJSON(w,400,map[string]string{"error":"无效的 PushSubscription"});return};b,_:=json.Marshal(in.Subscription);_,e:=s.db.Exec(r.Context(),`UPDATE "User" SET "webPushSubscription"=$2,"updatedAt"=NOW() WHERE "id"=$1`,u.ID,string(b));if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) clearWebPushSubscription(w http.ResponseWriter,r *http.Request,u user){_,e:=s.db.Exec(r.Context(),`UPDATE "User" SET "webPushSubscription"=NULL,"updatedAt"=NOW() WHERE "id"=$1`,u.ID);if e!=nil{dbError(w,e);return};writeJSON(w,200,map[string]bool{"success":true})}
func (s *Server) qrVerify(w http.ResponseWriter,r *http.Request,u user){var p struct{V int `json:"v"`;UID string `json:"uid"`;IK string `json:"ik"`;Exp int64 `json:"exp"`};if decode(r,&p)!=nil||p.V!=1{writeJSON(w,400,map[string]string{"error":"无效的二维码格式"});return};if p.UID==""{writeJSON(w,400,map[string]string{"error":"二维码缺少必要字段"});return};if p.Exp>0&&time.Now().UnixMilli()>p.Exp{writeJSON(w,400,map[string]any{"error":"二维码已过期，请让对方刷新后重试","expired":true});return};var id,name,nick,av,phone string;e:=s.db.QueryRow(r.Context(),`SELECT "id","username",COALESCE("nickname",''),COALESCE("avatar",''),COALESCE("phone",'') FROM "User" WHERE "id"=$1`,p.UID).Scan(&id,&name,&nick,&av,&phone);if e==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"二维码对应的用户不存在"});return};if e!=nil{dbError(w,e);return};if id==u.ID{writeJSON(w,400,map[string]string{"error":"不能扫描自己的二维码"});return};if nick==""{nick=name};writeJSON(w,200,map[string]any{"valid":true,"basic":p.IK==""||p.IK=="basic","user":map[string]any{"id":id,"name":nick,"phone":phone,"avatar":av}})}
func (s *Server) stickerPacks(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,map[string]any{"packs":[]any{}})}
func (s *Server) stickerSearch(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,map[string]any{"packs":[]any{},"stickers":[]any{}})}
func (s *Server) stickerStatus(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,map[string]any{"enabled":true,"packs":0})}
