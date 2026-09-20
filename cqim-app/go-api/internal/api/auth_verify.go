package api

// Verification-code routes intentionally share Node's VerifyCode rows and
// Redis keys.  They do not invent a development fallback: a missing delivery
// configuration is reported to the caller before a successful response.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const verifyCodeTTL = 5 * time.Minute

type verifyCodeRecord struct {
	Target    string    `json:"target"`
	Type      string    `json:"type"`
	Channel   string    `json:"channel"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	Used      bool      `json:"used"`
	Attempts  int       `json:"attempts"`
}

type smtpConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Secure      *bool  `json:"secure"`
	AuthUser    string `json:"authUser"`
	User        string `json:"user"`
	AuthPass    string `json:"authPass"`
	Pass        string `json:"pass"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	FromEmail   string `json:"fromEmail"`
}

type aliyunConfig struct {
	AccessKeyID     string                     `json:"accessKeyId"`
	AccessKeySecret string                     `json:"accessKeySecret"`
	SMSSignName     string                     `json:"smsSignName"`
	SMSTemplates    map[string]json.RawMessage `json:"smsTemplates"`
	SMSEnabled      bool                       `json:"smsEnabled"`
}

func isValidPhone(value string) bool {
	if len(value) != 11 || value[0] != '1' || value[1] < '3' || value[1] > '9' { return false }
	for _, c := range value { if c < '0' || c > '9' { return false } }
	return true
}

func isValidEmail(value string) bool {
	if strings.ContainsAny(value, " \t\r\n") { return false }
	at := strings.LastIndex(value, "@")
	return at > 0 && at < len(value)-1 && strings.Contains(value[at+1:], ".")
}

func verifyCodeKey(target, kind, channel string) string { return "verify_code:" + channel + ":" + kind + ":" + target }
func verifyCodeRecentKey(target, channel string) string { return "verify_code_recent:" + channel + ":" + target }

func (s *Server) authSendCode(w http.ResponseWriter, r *http.Request) {
	var in struct { Target string `json:"target"`; Type string `json:"type"`; Channel string `json:"channel"` }
	if decode(r, &in) != nil || in.Target == "" { writeJSON(w, 400, map[string]string{"error":"请输入手机号或邮箱"}); return }
	if in.Type == "" { in.Type = "login" }; if in.Channel == "" { in.Channel = "sms" }
	if in.Channel != "sms" && in.Channel != "email" { writeJSON(w,400,map[string]string{"error":"验证码渠道不正确"}); return }
	if in.Channel == "sms" && !isValidPhone(in.Target) { writeJSON(w,400,map[string]string{"error":"手机号格式不正确"}); return }
	if in.Channel == "email" && !isValidEmail(in.Target) { writeJSON(w,400,map[string]string{"error":"邮箱格式不正确"}); return }
	if s.verifyCodeRecentlySent(r.Context(), in.Target, in.Channel) { writeJSON(w,429,map[string]any{"error":"发送过于频繁，请稍后再试","retryAfter":60}); return }
	if in.Type == "register" || in.Type == "login" {
		column := `"phone"`; existsMessage, missingMessage := "该手机号已注册", "该手机号未注册"
		if in.Channel == "email" { column, existsMessage, missingMessage = `"email"`, "该邮箱已注册", "该邮箱未注册" }
		var exists bool
		if err := s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM \"User\" WHERE "+column+"=$1)", in.Target).Scan(&exists); err != nil { dbError(w,err); return }
		if in.Type == "register" && exists { writeJSON(w,409,map[string]string{"error":existsMessage}); return }
		if in.Type == "login" && !exists { writeJSON(w,404,map[string]string{"error":missingMessage}); return }
	}

	var code string
	if in.Channel == "sms" {
		if err := s.sendAliyunSMS(r.Context(), in.Target, in.Type); err != nil { writeJSON(w,503,map[string]string{"error":err.Error()}); return }
		code = "__aliyun__"
	} else {
		var err error
		code, err = randomVerifyCode(); if err != nil { writeJSON(w,500,map[string]string{"error":"生成验证码失败"}); return }
		if err = s.sendVerificationEmail(r.Context(), in.Target, code); err != nil { writeJSON(w,503,map[string]string{"error":err.Error()}); return }
	}
	expires := time.Now().Add(verifyCodeTTL)
	if _, err := s.db.Exec(r.Context(), `INSERT INTO "VerifyCode" ("id","target","code","type","channel","used","attempts","expiresAt","createdAt") VALUES ($1,$2,$3,$4,$5,false,0,$6,NOW())`, newID(), in.Target, code, in.Type, in.Channel, expires); err != nil { dbError(w,err); return }
	record := verifyCodeRecord{Target:in.Target,Type:in.Type,Channel:in.Channel,Code:code,ExpiresAt:expires,CreatedAt:time.Now(),Used:false,Attempts:0}
	if s.redis != nil { if raw,err:=json.Marshal(record);err==nil { _=s.redis.Set(r.Context(),verifyCodeKey(in.Target,in.Type,in.Channel),raw,verifyCodeTTL).Err(); _=s.redis.Set(r.Context(),verifyCodeRecentKey(in.Target,in.Channel),time.Now().UnixMilli(),time.Minute).Err() } }
	message := "验证码已发送到手机"; if in.Channel == "email" { message="验证码已发送到邮箱" }
	writeJSON(w,200,map[string]any{"success":true,"message":message})
}

func (s *Server) verifyCodeRecentlySent(ctx context.Context, target, channel string) bool {
	if s.redis != nil { if n,err:=s.redis.Exists(ctx,verifyCodeRecentKey(target,channel)).Result();err==nil && n>0{return true} }
	var found bool
	err:=s.db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM "VerifyCode" WHERE "target"=$1 AND "channel"=$2 AND "createdAt">=NOW()-INTERVAL '60 seconds')`,target,channel).Scan(&found)
	if err != nil { return false }
	if found && s.redis != nil { _=s.redis.Set(ctx,verifyCodeRecentKey(target,channel),time.Now().UnixMilli(),time.Minute).Err() }
	return found
}

func (s *Server) verifySubmittedCode(ctx context.Context, target, code, kind, channel string) (bool,string) {
	key:=verifyCodeKey(target,kind,channel)
	if s.redis != nil {
		if raw,err:=s.redis.Get(ctx,key).Bytes();err==nil {
			var cached verifyCodeRecord
			if json.Unmarshal(raw,&cached)==nil && !cached.Used && !cached.ExpiresAt.Before(time.Now()) {
				if cached.Code == "__aliyun__" { if err:=s.checkAliyunSMS(ctx,target,code);err!=nil{return false,err.Error()}; cached.Used=true; _=s.saveVerifyCodeCache(ctx,key,cached); _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`,target,kind,channel); return true,"" }
				cached.Attempts++
				if cached.Code != code && cached.Attempts >= 5 { cached.Used=true; _=s.saveVerifyCodeCache(ctx,key,cached); _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true,"attempts"="attempts"+1 WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`,target,kind,channel); return false,"验证码已失效，请重新获取" }
				_ = s.saveVerifyCodeCache(ctx,key,cached); _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "attempts"="attempts"+1 WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`,target,kind,channel)
				if cached.Code != code { return false,"验证码错误" }
				cached.Used=true; _=s.saveVerifyCodeCache(ctx,key,cached); _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`,target,kind,channel); return true,""
			}
		}
	}
	var id,stored string; var attempts int
	err:=s.db.QueryRow(ctx,`SELECT "id","code","attempts" FROM "VerifyCode" WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false AND "expiresAt">=NOW() ORDER BY "createdAt" DESC LIMIT 1`,target,kind,channel).Scan(&id,&stored,&attempts)
	if err != nil { if err==pgx.ErrNoRows{return false,"验证码不存在或已过期"}; return false,"验证码校验服务不可用" }
	if stored=="__aliyun__" { if err:=s.checkAliyunSMS(ctx,target,code);err!=nil{return false,err.Error()}; _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true WHERE "id"=$1`,id); return true,"" }
	_,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "attempts"="attempts"+1 WHERE "id"=$1`,id)
	if attempts >= 5 { _,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true WHERE "id"=$1`,id); return false,"验证码已失效，请重新获取" }
	if stored != code { return false,"验证码错误" }
	_,_=s.db.Exec(ctx,`UPDATE "VerifyCode" SET "used"=true WHERE "id"=$1`,id); return true,""
}

func (s *Server) saveVerifyCodeCache(ctx context.Context,key string,record verifyCodeRecord) error { raw,err:=json.Marshal(record);if err!=nil{return err};ttl:=time.Until(record.ExpiresAt);if ttl<=0{ttl=time.Second};return s.redis.Set(ctx,key,raw,ttl).Err() }

func (s *Server) authResetPassword(w http.ResponseWriter,r *http.Request) {
	var in struct { Account string `json:"account"`; Code string `json:"code"`; NewPassword string `json:"newPassword"`; Channel string `json:"channel"` }
	if decode(r,&in)!=nil||in.Account==""||in.Code==""||in.NewPassword==""{writeJSON(w,400,map[string]string{"error":"请填写完整信息"});return};if in.Channel==""{in.Channel="sms"};if in.Channel!="sms"&&in.Channel!="email"{writeJSON(w,400,map[string]string{"error":"验证码渠道不正确"});return}
	if err:=checkPasswordStrength(in.NewPassword);err!=""{writeJSON(w,400,map[string]string{"error":err});return};if ok,msg:=s.verifySubmittedCode(r.Context(),in.Account,in.Code,"reset",in.Channel);!ok{writeJSON(w,400,map[string]string{"error":msg});return}
	column:=`"phone"`;if in.Channel=="email"{column=`"email"`};var id string;if err:=s.db.QueryRow(r.Context(),"SELECT \"id\" FROM \"User\" WHERE "+column+"=$1",in.Account).Scan(&id);err!=nil{if err==pgx.ErrNoRows{writeJSON(w,404,map[string]string{"error":"账号不存在"})}else{dbError(w,err)};return}
	hash,err:=bcrypt.GenerateFromPassword([]byte(in.NewPassword),12);if err!=nil{dbError(w,err);return};if _,err=s.db.Exec(r.Context(),`UPDATE "User" SET "password"=$2,"updatedAt"=NOW() WHERE "id"=$1`,id,string(hash));err!=nil{dbError(w,err);return};if _,err=s.db.Exec(r.Context(),`DELETE FROM "UserSession" WHERE "userId"=$1`,id);err!=nil{dbError(w,err);return};writeJSON(w,200,map[string]any{"success":true,"message":"密码重置成功，请重新登录"})
}

func (s *Server) authBindPhone(w http.ResponseWriter,r *http.Request,u user) { var in struct{Phone string `json:"phone"`;Code string `json:"code"`};if decode(r,&in)!=nil||in.Phone==""||in.Code==""{writeJSON(w,400,map[string]string{"error":"请填写手机号和验证码"});return};if !isValidPhone(in.Phone){writeJSON(w,400,map[string]string{"error":"手机号格式不正确"});return};s.bindContact(w,r,u,in.Phone,in.Code,"sms") }
func (s *Server) authBindEmail(w http.ResponseWriter,r *http.Request,u user) { var in struct{Email string `json:"email"`;Code string `json:"code"`};if decode(r,&in)!=nil||in.Email==""||in.Code==""{writeJSON(w,400,map[string]string{"error":"请填写邮箱和验证码"});return};if !isValidEmail(in.Email){writeJSON(w,400,map[string]string{"error":"邮箱格式不正确"});return};s.bindContact(w,r,u,in.Email,in.Code,"email") }
func (s *Server) bindContact(w http.ResponseWriter,r *http.Request,u user,target,code,channel string) { column:=`"phone"`; conflict,message:="该手机号已被其他账号绑定","手机号绑定成功";if channel=="email"{column,conflict,message=`"email"`,"该邮箱已被其他账号绑定","邮箱绑定成功"};var existing string;err:=s.db.QueryRow(r.Context(),"SELECT \"id\" FROM \"User\" WHERE "+column+"=$1",target).Scan(&existing);if err==nil&&existing!=u.ID{writeJSON(w,409,map[string]string{"error":conflict});return};if err!=nil&&err!=pgx.ErrNoRows{dbError(w,err);return};if ok,msg:=s.verifySubmittedCode(r.Context(),target,code,"bind",channel);!ok{writeJSON(w,400,map[string]string{"error":msg});return};verified:=`"phoneVerified"`;if channel=="email"{verified=`"emailVerified"`};if _,err=s.db.Exec(r.Context(),"UPDATE \"User\" SET "+column+"=$2,"+verified+"=true,\"updatedAt\"=NOW() WHERE \"id\"=$1",u.ID,target);err!=nil{dbError(w,err);return};writeJSON(w,200,map[string]any{"success":true,"message":message}) }

func checkPasswordStrength(password string) string { if len(password)<8{return "密码长度不能少于 8 位"};if len(password)>128{return "密码长度不能超过 128 位"};var lower,upper,digit,special bool;for _,c:=range password{lower=lower||(c>='a'&&c<='z');upper=upper||(c>='A'&&c<='Z');digit=digit||(c>='0'&&c<='9');special=special||strings.ContainsRune("!@#$%^&*()_+-=[]{};':\"\\|,.<>/?~`",c)};if !lower{return "密码必须包含小写字母"};if !upper{return "密码必须包含大写字母"};if !digit{return "密码必须包含数字"};if !special{return "密码必须包含特殊字符（如 !@#$%^&*）"};p:=strings.ToLower(password);for _,weak:=range []string{"password","12345678","qwerty123","admin123","abc12345"}{if strings.Contains(p,weak){return "密码过于简单，请使用更复杂的密码"}};return "" }
func randomVerifyCode()(string,error){n,err:=rand.Int(rand.Reader,big.NewInt(900000));if err!=nil{return "",err};return strconv.FormatInt(100000+n.Int64(),10),nil}

func (s *Server) systemConfig(ctx context.Context,key string,dst any) error { var raw string;if err:=s.db.QueryRow(ctx,`SELECT "value" FROM "SystemConfig" WHERE "key"=$1`,key).Scan(&raw);err!=nil{return err};return json.Unmarshal([]byte(raw),dst) }
func (s *Server) smtpConfig(ctx context.Context)(smtpConfig,error){var cfg smtpConfig;err:=s.systemConfig(ctx,"smtp",&cfg);if err!=nil&&err!=pgx.ErrNoRows{return cfg,err};if cfg.Host==""{cfg.Host=os.Getenv("SMTP_HOST")};if cfg.Port==0{if p,_:=strconv.Atoi(os.Getenv("SMTP_PORT"));p>0{cfg.Port=p}else{cfg.Port=465}};if cfg.AuthUser==""{cfg.AuthUser=cfg.User};if cfg.AuthUser==""{cfg.AuthUser=os.Getenv("SMTP_USER")};if cfg.AuthPass==""{cfg.AuthPass=cfg.Pass};if cfg.AuthPass==""{cfg.AuthPass=os.Getenv("SMTP_PASS")};if cfg.FromAddress==""{cfg.FromAddress=cfg.FromEmail};if cfg.FromAddress==""{cfg.FromAddress=os.Getenv("SMTP_FROM")};return cfg,nil }
func (s *Server) sendVerificationEmail(ctx context.Context,to,code string) error { cfg,err:=s.smtpConfig(ctx);if err!=nil{return fmt.Errorf("读取 SMTP 配置失败")};if cfg.Host==""||cfg.AuthUser==""||cfg.AuthPass==""{return fmt.Errorf("SMTP 配置不完整")};from:=cfg.FromAddress;if from==""{from=cfg.AuthUser};name:=cfg.FromName;if name==""{name="imim"};return sendSMTP(ctx,cfg,from,to,fmt.Sprintf("【%s】验证码",name),fmt.Sprintf("您的验证码是：%s。验证码有效期为 5 分钟，请勿泄露给他人。",code)) }
func sendSMTP(ctx context.Context,cfg smtpConfig,from,to,subject,body string) error { address:=net.JoinHostPort(cfg.Host,strconv.Itoa(cfg.Port));secure:=cfg.Port==465;if cfg.Secure!=nil{secure=*cfg.Secure};var conn net.Conn;var err error;if secure{conn,err=tls.Dial("tcp",address,&tls.Config{ServerName:cfg.Host,MinVersion:tls.VersionTLS12})}else{d:=net.Dialer{};conn,err=d.DialContext(ctx,"tcp",address)};if err!=nil{return fmt.Errorf("SMTP 连接失败")};defer conn.Close();client,err:=smtp.NewClient(conn,cfg.Host);if err!=nil{return fmt.Errorf("SMTP 握手失败")};defer client.Quit();if !secure{if ok,_:=client.Extension("STARTTLS");ok{if err=client.StartTLS(&tls.Config{ServerName:cfg.Host,MinVersion:tls.VersionTLS12});err!=nil{return fmt.Errorf("SMTP TLS 失败")}}};if cfg.AuthUser!=""{if err=client.Auth(smtp.PlainAuth("",cfg.AuthUser,cfg.AuthPass,cfg.Host));err!=nil{return fmt.Errorf("SMTP 认证失败")}};if err=client.Mail(from);err!=nil{return fmt.Errorf("SMTP 发件失败")};if err=client.Rcpt(to);err!=nil{return fmt.Errorf("SMTP 收件人失败")};writer,err:=client.Data();if err!=nil{return fmt.Errorf("SMTP 写入失败")};_,err=io.WriteString(writer,"From: "+from+"\r\nTo: "+to+"\r\nSubject: "+subject+"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n"+body+"\r\n");closeErr:=writer.Close();if err!=nil||closeErr!=nil{return fmt.Errorf("SMTP 发送失败")};return nil }

func (s *Server) aliyunConfig(ctx context.Context)(aliyunConfig,error){var cfg aliyunConfig;err:=s.systemConfig(ctx,"aliyun",&cfg);if err!=nil&&err!=pgx.ErrNoRows{return cfg,err};if cfg.AccessKeyID==""{cfg.AccessKeyID=os.Getenv("ALIYUN_ACCESS_KEY_ID")};if cfg.AccessKeySecret==""{cfg.AccessKeySecret=os.Getenv("ALIYUN_ACCESS_KEY_SECRET")};if cfg.SMSSignName==""{cfg.SMSSignName=os.Getenv("ALIYUN_SMS_SIGN_NAME")};return cfg,nil}
func templateCode(cfg aliyunConfig,scene string) string { key:=scene;if key=="register"{key="login"};if scene=="change_phone"{key="changePhone"};if scene=="verify_phone"{key="verifyPhone"};raw:=cfg.SMSTemplates[key];var v struct{Code string `json:"code"`};_ = json.Unmarshal(raw,&v);return v.Code }
func (s *Server) sendAliyunSMS(ctx context.Context,phone,scene string) error { cfg,err:=s.aliyunConfig(ctx);if err!=nil{return fmt.Errorf("读取短信配置失败")};if !cfg.SMSEnabled||cfg.AccessKeyID==""||cfg.AccessKeySecret==""{return fmt.Errorf("短信服务未配置")};params:=url.Values{"PhoneNumber":{phone},"TemplateParam":{`{"code":"##code##","min":"5"}`},"CodeLength":{"6"},"ValidTime":{"300"}};if c:=templateCode(cfg,scene);c!=""{params.Set("TemplateCode",c)};if cfg.SMSSignName!=""{params.Set("SignName",cfg.SMSSignName)};var out struct{Code string `json:"Code"`;Message string `json:"Message"`};if err=s.aliyunRPC(ctx,cfg,"SendSmsVerifyCode",params,&out);err!=nil{return err};if out.Code!="OK"{if out.Message==""{out.Message="短信发送失败"};return fmt.Errorf("%s",out.Message)};return nil }
func (s *Server) checkAliyunSMS(ctx context.Context,phone,code string) error { cfg,err:=s.aliyunConfig(ctx);if err!=nil{return fmt.Errorf("读取短信配置失败")};if !cfg.SMSEnabled||cfg.AccessKeyID==""||cfg.AccessKeySecret==""{return fmt.Errorf("短信服务未配置")};var out struct{Code string `json:"Code"`;Message string `json:"Message"`};if err=s.aliyunRPC(ctx,cfg,"CheckSmsVerifyCode",url.Values{"PhoneNumber":{phone},"VerifyCode":{code}},&out);err!=nil{return err};if out.Code!="OK"{if out.Message==""{out.Message="验证码错误"};return fmt.Errorf("%s",out.Message)};return nil }
func aliyunEscape(value string) string{return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value),"+","%20"),"*","%2A"),"%7E","~")}
func (s *Server) aliyunRPC(ctx context.Context,cfg aliyunConfig,action string,params url.Values,out any) error { params.Set("Format","JSON");params.Set("Version","2017-05-25");params.Set("AccessKeyId",cfg.AccessKeyID);params.Set("SignatureMethod","HMAC-SHA1");params.Set("Timestamp",time.Now().UTC().Format("2006-01-02T15:04:05Z"));params.Set("SignatureVersion","1.0");nonce,err:=randomVerifyCode();if err!=nil{return err};params.Set("SignatureNonce",nonce+strconv.FormatInt(time.Now().UnixNano(),10));params.Set("Action",action);keys:=make([]string,0,len(params));for k:=range params{keys=append(keys,k)};sort.Strings(keys);pairs:=make([]string,0,len(keys));for _,k:=range keys{pairs=append(pairs,aliyunEscape(k)+"="+aliyunEscape(params.Get(k)))};canonical:=strings.Join(pairs,"&");toSign:="POST&%2F&"+aliyunEscape(canonical);mac:=hmac.New(sha1.New,[]byte(cfg.AccessKeySecret+"&"));_,_=mac.Write([]byte(toSign));params.Set("Signature",base64.StdEncoding.EncodeToString(mac.Sum(nil)));req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"https://dypnsapi.aliyuncs.com/?"+params.Encode(),nil);if err!=nil{return err};resp,err:=http.DefaultClient.Do(req);if err!=nil{return fmt.Errorf("短信通道不可用")};defer resp.Body.Close();if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("短信通道返回错误")};if err=json.NewDecoder(resp.Body).Decode(out);err!=nil{return fmt.Errorf("短信通道响应无效")};return nil }
