package api

// Real APNs and Web Push delivery. Credentials are read only from the same
// environment variables as Node; neither private keys nor tokens are logged.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const apnsBundleFallback = "com.imim.chat"

func (s *Server) registerPushRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/apns/token", s.requireUser(s.upsertAPNSToken))
	mux.HandleFunc("POST /api/apns/voip-token", s.requireUser(s.upsertVoIPToken))
	mux.HandleFunc("DELETE /api/apns/token", s.requireUser(s.deleteAPNSToken))
}

func apnsEnv(v string) string { if v=="production"||v=="sandbox" { return v }; if strings.EqualFold(os.Getenv("APNS_PRODUCTION"),"true") { return "production" }; return "sandbox" }
func apnsHost(env string) string { if env=="production" { return "https://api.push.apple.com" }; return "https://api.sandbox.push.apple.com" }
func bundleID() string { if v:=os.Getenv("APPLE_BUNDLE_ID");v!="" {return v}; return apnsBundleFallback }

func (s *Server) upsertAPNSToken(w http.ResponseWriter,r *http.Request,u user) {
	var in struct { Token string `json:"token"`; Platform string `json:"platform"`; Environment string `json:"environment"`; Kind string `json:"kind"` }
	if decode(r,&in)!=nil || in.Token=="" { writeJSON(w,400,map[string]string{"error":"缺少 token"}); return }
	if in.Platform=="" { in.Platform="ios" }; if in.Kind!="voip" { in.Kind="alert" }
	if err:=s.storePushToken(r.Context(),u.ID,in.Token,in.Platform,apnsEnv(in.Environment),in.Kind);err!=nil { writeJSON(w,500,map[string]string{"error":"服务器内部错误"});return }; writeJSON(w,200,map[string]bool{"success":true})
}
func (s *Server) upsertVoIPToken(w http.ResponseWriter,r *http.Request,u user) {
	var in struct { Token string `json:"voipToken"`; Environment string `json:"environment"` }; if decode(r,&in)!=nil||in.Token=="" {writeJSON(w,400,map[string]string{"error":"缺少 voipToken"});return};if err:=s.storePushToken(r.Context(),u.ID,in.Token,"ios",apnsEnv(in.Environment),"voip");err!=nil{writeJSON(w,500,map[string]string{"error":"服务器内部错误"});return};writeJSON(w,200,map[string]bool{"success":true})
}
func (s *Server) storePushToken(ctx context.Context,uid,token,platform,env,kind string) error { _,e:=s.db.Exec(ctx,`INSERT INTO "PushDeviceToken" ("id","userId","token","platform","environment","kind","createdAt","updatedAt") VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW()) ON CONFLICT ("token","kind") DO UPDATE SET "userId"=EXCLUDED."userId","platform"=EXCLUDED."platform","environment"=EXCLUDED."environment","updatedAt"=NOW()`,newID(),uid,token,platform,env,kind);return e }
func (s *Server) deleteAPNSToken(w http.ResponseWriter,r *http.Request,u user) {
	var in struct { Token string `json:"token"`; Kind string `json:"kind"`; Environment string `json:"environment"` }; _=decode(r,&in); if in.Kind!="voip"{in.Kind="alert"};env:=apnsEnv(in.Environment); var id string; var e error
	if in.Token!="" { e=s.db.QueryRow(r.Context(),`SELECT "id" FROM "PushDeviceToken" WHERE "userId"=$1 AND "token"=$2 AND "platform"='ios' ORDER BY "updatedAt" DESC LIMIT 1`,u.ID,in.Token).Scan(&id) } else { e=s.db.QueryRow(r.Context(),`SELECT "id" FROM "PushDeviceToken" WHERE "userId"=$1 AND "platform"='ios' AND "kind"=$2 AND "environment"=$3 ORDER BY "updatedAt" DESC LIMIT 1`,u.ID,in.Kind,env).Scan(&id) }
	deleted:=false;if e==nil { _,e=s.db.Exec(r.Context(),`DELETE FROM "PushDeviceToken" WHERE "id"=$1`,id);deleted=e==nil };if e!=nil&&e.Error()!="no rows in result set"{writeJSON(w,500,map[string]string{"error":"服务器内部错误"});return};writeJSON(w,200,map[string]any{"success":true,"deleted":deleted})
}

type pushDevice struct { ID,Token,Environment string }
type apnsResult struct { ok bool; status int; reason string }
func apnsKey() (*ecdsa.PrivateKey,string,error) { raw:=strings.ReplaceAll(os.Getenv("APNS_P8_KEY"),`\n`,"\n");kid:=os.Getenv("APNS_KEY_ID");if raw==""||kid==""||os.Getenv("APPLE_TEAM_ID")=="" {return nil,"",fmt.Errorf("no_p8_keys")};b,_:=pem.Decode([]byte(raw));if b==nil{return nil,"",fmt.Errorf("invalid_p8_key")};v,e:=x509.ParsePKCS8PrivateKey(b.Bytes);if e!=nil{return nil,"",e};k,ok:=v.(*ecdsa.PrivateKey);if !ok{return nil,"",fmt.Errorf("p8 key is not ECDSA")};return k,kid,nil }
func rawES256(der []byte) ([]byte,error) { var s struct { R,S *big.Int };if _,e:=asn1Unmarshal(der,&s);e!=nil{return nil,e};out:=make([]byte,64);s.R.FillBytes(out[:32]);s.S.FillBytes(out[32:]);return out,nil }
// Minimal DER ECDSA parser; ECDSA signatures are two positive INTEGERs.
func asn1Unmarshal(b []byte,out *struct{R,S *big.Int})([]byte,error){if len(b)<8||b[0]!=0x30{return nil,fmt.Errorf("invalid DER")};n:=int(b[1]);i:=2;if n&0x80!=0{z:=n&0x7f;n=0;for ;z>0;z--{if i>=len(b){return nil,fmt.Errorf("invalid DER")};n=n<<8|int(b[i]);i++}};if i+n>len(b)||b[i]!=2{return nil,fmt.Errorf("invalid DER")};i++;rl:=int(b[i]);i++;if i+rl>=len(b)||b[i+rl]!=2{return nil,fmt.Errorf("invalid DER")};r:=new(big.Int).SetBytes(b[i:i+rl]);i+=rl+1;sl:=int(b[i]);i++;if i+sl>len(b){return nil,fmt.Errorf("invalid DER")};out.R=r;out.S=new(big.Int).SetBytes(b[i:i+sl]);return b[i+sl:],nil}
func jwtES256(k *ecdsa.PrivateKey,claims map[string]any,header map[string]any)(string,error){enc:=base64.RawURLEncoding;h,_:=json.Marshal(header);c,_:=json.Marshal(claims);input:=enc.EncodeToString(h)+"."+enc.EncodeToString(c);digest:=sha256.Sum256([]byte(input));der,e:=ecdsa.SignASN1(rand.Reader,k,digest[:]);if e!=nil{return "",e};raw,e:=rawES256(der);if e!=nil{return "",e};return input+"."+enc.EncodeToString(raw),nil}
func apnsClient() *http.Client { return &http.Client{Timeout:10*time.Second,Transport:&http.Transport{TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS12},ForceAttemptHTTP2:true}} }
func sendAPNS(ctx context.Context,device pushDevice,payload any,topic,kind string) apnsResult {k,kid,e:=apnsKey();if e!=nil{log.Printf("[APNs] %v",e);return apnsResult{reason:e.Error()}};token,e:=jwtES256(k,map[string]any{"iss":os.Getenv("APPLE_TEAM_ID"),"iat":time.Now().Unix()},map[string]any{"alg":"ES256","kid":kid});if e!=nil{return apnsResult{reason:e.Error()}};body,_:=json.Marshal(payload);req,e:=http.NewRequestWithContext(ctx,http.MethodPost,apnsHost(apnsEnv(device.Environment))+"/3/device/"+device.Token,bytes.NewReader(body));if e!=nil{return apnsResult{reason:e.Error()}};req.Header.Set("authorization","bearer "+token);req.Header.Set("apns-topic",topic);req.Header.Set("apns-push-type",kind);req.Header.Set("apns-priority","10");req.Header.Set("apns-expiration","0");resp,e:=apnsClient().Do(req);if e!=nil{return apnsResult{reason:e.Error()}};defer resp.Body.Close();data,_:=io.ReadAll(resp.Body);r:=apnsResult{ok:resp.StatusCode==200,status:resp.StatusCode};if !r.ok {var x struct{Reason string `json:"reason"`};_ =json.Unmarshal(data,&x);r.reason=x.Reason};return r}
func invalidAPNS(r apnsResult) bool {return r.status==410||r.reason=="Unregistered"||r.reason=="BadDeviceToken"}
func (s *Server) sendAPNs(ctx context.Context,to,title,body string,data map[string]any) bool {rows,e:=s.db.Query(ctx,`SELECT "id","token","environment" FROM "PushDeviceToken" WHERE "userId"=$1 AND "platform"='ios' AND "kind"='alert'`,to);if e!=nil{return false};defer rows.Close();ok:=false;for rows.Next(){var d pushDevice;if rows.Scan(&d.ID,&d.Token,&d.Environment)!=nil{continue};x:=map[string]any{"aps":map[string]any{"alert":map[string]string{"title":title,"body":body},"mutable-content":1,"sound":"default","badge":1}};for k,v:=range data{x[k]=v};r:=sendAPNS(ctx,d,x,bundleID(),"alert");if r.ok{ok=true}else if invalidAPNS(r){_,_=s.db.Exec(ctx,`DELETE FROM "PushDeviceToken" WHERE "id"=$1`,d.ID)}};return ok}

type webSubscription struct { Endpoint string `json:"endpoint"`; Keys struct { P256dh string `json:"p256dh"`; Auth string `json:"auth"` } `json:"keys"` }
func b64(v string)([]byte,error){return base64.RawURLEncoding.DecodeString(strings.TrimRight(v,"="))}
func hkdfBytes(hash func() hash.Hash, secret,salt,info []byte,n int) ([]byte,error){out:=make([]byte,n);_,e:=io.ReadFull(hkdf.New(hash,secret,salt,info),out);return out,e}
func encryptWebPush(sub webSubscription,payload []byte)([]byte,error){clientPub,e:=b64(sub.Keys.P256dh);if e!=nil||len(clientPub)!=65{return nil,fmt.Errorf("invalid p256dh")};auth,e:=b64(sub.Keys.Auth);if e!=nil{return nil,e};x,y:=elliptic.Unmarshal(elliptic.P256(),clientPub);if x==nil{return nil,fmt.Errorf("invalid p256dh")};server,e:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if e!=nil{return nil,e};shared,_:=elliptic.P256().ScalarMult(x,y,server.D.Bytes());serverPub:=elliptic.Marshal(elliptic.P256(),server.PublicKey.X,server.PublicKey.Y);info:=append([]byte("WebPush: info\x00"),clientPub...);info=append(info,serverPub...);ikm,e:=hkdfBytes(sha256.New,shared,auth,info,32);if e!=nil{return nil,e};salt:=make([]byte,16);if _,e=rand.Read(salt);e!=nil{return nil,e};cek,e:=hkdfBytes(sha256.New,ikm,salt,[]byte("Content-Encoding: aes128gcm\x00"),16);if e!=nil{return nil,e};nonce,e:=hkdfBytes(sha256.New,ikm,salt,[]byte("Content-Encoding: nonce\x00"),12);if e!=nil{return nil,e};block,e:=aes.NewCipher(cek);if e!=nil{return nil,e};g,e:=cipher.NewGCM(block);if e!=nil{return nil,e};plain:=append(append([]byte{},payload...),2);sealed:=g.Seal(nil,nonce,plain,nil);out:=make([]byte,0,16+4+1+65+len(sealed));out=append(out,salt...);rs:=make([]byte,4);binary.BigEndian.PutUint32(rs,4096);out=append(out,rs...);out=append(out,byte(len(serverPub)));out=append(out,serverPub...);return append(out,sealed...),nil}
func vapidToken(endpoint string)(string,error){priv,e:=b64(os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY"));if e!=nil{return "",e};k, e:=ecdh.P256().NewPrivateKey(priv);if e!=nil{return "",e};ek:=k.Bytes();d:=new(big.Int).SetBytes(ek);key:=&ecdsa.PrivateKey{PublicKey:ecdsa.PublicKey{Curve:elliptic.P256()},D:d};key.PublicKey.X,key.PublicKey.Y=elliptic.P256().ScalarBaseMult(ek);u,e:=url.Parse(endpoint);if e!=nil{return "",e};sub:=os.Getenv("WEB_PUSH_VAPID_SUBJECT");if sub==""{sub="mailto:security@example.invalid"};return jwtES256(key,map[string]any{"aud":u.Scheme+"://"+u.Host,"exp":time.Now().Add(12*time.Hour).Unix(),"sub":sub},map[string]any{"alg":"ES256","typ":"JWT"})}
func (s *Server) sendWebPush(ctx context.Context,to,chatID,msgID,senderID string) bool {pub:=os.Getenv("WEB_PUSH_VAPID_PUBLIC_KEY");if pub==""||os.Getenv("WEB_PUSH_VAPID_PRIVATE_KEY")==""{return false};var raw *string;e:=s.db.QueryRow(ctx,`SELECT "webPushSubscription" FROM "User" WHERE "id"=$1`,to).Scan(&raw);if e!=nil||raw==nil||*raw==""{return false};var sub webSubscription;if json.Unmarshal([]byte(*raw),&sub)!=nil||!strings.HasPrefix(sub.Endpoint,"https://"){return false};payload,_:=json.Marshal(map[string]any{"type":"encrypted_message","chatId":chatID,"messageId":msgID,"senderId":senderID,"encrypted":true});enc,e:=encryptWebPush(sub,payload);if e!=nil{return false};jwt,e:=vapidToken(sub.Endpoint);if e!=nil{return false};req,e:=http.NewRequestWithContext(ctx,http.MethodPost,sub.Endpoint,bytes.NewReader(enc));if e!=nil{return false};req.Header.Set("Content-Type","application/octet-stream");req.Header.Set("Content-Encoding","aes128gcm");req.Header.Set("TTL","90");req.Header.Set("Urgency","high");req.Header.Set("Authorization","vapid t="+jwt+", k="+pub);resp,e:=apnsClient().Do(req);if e!=nil{return false};defer resp.Body.Close();if resp.StatusCode==404||resp.StatusCode==410{_,_=s.db.Exec(ctx,`UPDATE "User" SET "webPushSubscription"=NULL,"updatedAt"=NOW() WHERE "id"=$1`,to)};return resp.StatusCode>=200&&resp.StatusCode<300}
func (s *Server) notifyPrivateMessagePush(ctx context.Context,to,sender,chatID,msgID string) {state,active:="offline","";if s.redis!=nil{state,_=s.redis.Get(ctx,"user:presence:"+to).Result();active,_=s.redis.Get(ctx,"user:activeChat:"+to).Result()};if state=="foreground"&&active==chatID{return};var nick,name string;_ =s.db.QueryRow(ctx,`SELECT COALESCE("nickname",''),"username" FROM "User" WHERE "id"=$1`,sender).Scan(&nick,&name);if nick==""{nick=name};if nick==""{nick="有人"};s.sendAPNs(ctx,to,"新消息",nick,map[string]any{"chatId":chatID,"senderId":sender});s.sendWebPush(ctx,to,chatID,msgID,sender)}
