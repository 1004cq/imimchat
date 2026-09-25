package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/db"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ Redis 验证码记录（照抄 redis.ts 键格式与语义） ============

const (
	verifyCodePrefix       = "verify_code:"
	verifyCodeRecentPrefix = "verify_code_recent:"
)

// verifyCodeRecord 对应 TS 的 RedisVerifyCodeRecord。
type verifyCodeRecord struct {
	Target    string `json:"target"`
	Type      string `json:"type"`
	Channel   string `json:"channel"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
	CreatedAt string `json:"createdAt"`
	Used      bool   `json:"used"`
	Attempts  int    `json:"attempts"`
}

func verifyCodeKey(target, typ, channel string) string {
	return verifyCodePrefix + channel + ":" + typ + ":" + target
}

func verifyCodeRecentKey(target, channel string) string {
	return verifyCodeRecentPrefix + channel + ":" + target
}

func (h *Handler) hasRecentVerifyCodeSend(ctx context.Context, target, channel string) bool {
	// redis.ts 用 ttl > 0 判断；redisx 无 TTL 接口，key 存在即视为仍在 60s 窗口内
	// （该 key 只通过 setex 写入，过期即消失）。
	_, ok, err := h.d.Redis.GetString(ctx, verifyCodeRecentKey(target, channel))
	return err == nil && ok
}

func (h *Handler) markRecentVerifyCodeSend(ctx context.Context, target, channel string, ttlSeconds int) {
	_ = h.d.Redis.SetEX(ctx, verifyCodeRecentKey(target, channel),
		strconv.FormatInt(time.Now().UnixMilli(), 10), time.Duration(ttlSeconds)*time.Second)
}

func (h *Handler) setVerifyCodeRecord(ctx context.Context, rec verifyCodeRecord) {
	exp, err := time.Parse(time.RFC3339Nano, rec.ExpiresAt)
	if err != nil {
		exp = time.Now().Add(5 * time.Minute)
	}
	ttl := exp.Sub(time.Now())
	if ttl < time.Second {
		ttl = time.Second
	}
	payload, _ := json.Marshal(rec)
	_ = h.d.Redis.SetEX(ctx, verifyCodeKey(rec.Target, rec.Type, rec.Channel), string(payload), ttl)
}

func (h *Handler) getVerifyCodeRecord(ctx context.Context, target, typ, channel string) *verifyCodeRecord {
	raw, ok, err := h.d.Redis.GetString(ctx, verifyCodeKey(target, typ, channel))
	if err != nil || !ok || raw == "" {
		return nil
	}
	var rec verifyCodeRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return nil
	}
	return &rec
}

// updateVerifyCodeRecord 读取 → updater 变换 → 写回，返回变换后的记录。
func (h *Handler) updateVerifyCodeRecord(ctx context.Context, target, typ, channel string, updater func(*verifyCodeRecord) *verifyCodeRecord) *verifyCodeRecord {
	cur := h.getVerifyCodeRecord(ctx, target, typ, channel)
	if cur == nil {
		return nil
	}
	next := updater(cur)
	h.setVerifyCodeRecord(ctx, *next)
	return next
}

// ============ 发送验证码 ============

/**
* POST /api/auth/send-code
* body: { target, type, channel}
* target: 手机号或邮箱；type: register | login | bind | reset；channel: sms | email
 */
func (h *Handler) sendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target  string `json:"target"`
		Type    string `json:"type"`
		Channel string `json:"channel"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if req.Type == "" {
		req.Type = "login"
	}
	if req.Channel == "" {
		req.Channel = "sms"
	}
	target, typ, channel := req.Target, req.Type, req.Channel
	ctx := r.Context()

	if target == "" {
		util.WriteError(w, 400, "请输入手机号或邮箱")
		return
	}
	if channel == "sms" && !isValidPhone(target) {
		util.WriteError(w, 400, "手机号格式不正确")
		return
	}
	if channel == "email" && !isValidEmail(target) {
		util.WriteError(w, 400, "邮箱格式不正确")
		return
	}

	// 频率限制：同一目标 60 秒内只能发一次，优先使用 Redis
	if h.hasRecentVerifyCodeSend(ctx, target, channel) {
		util.WriteJSON(w, 429, map[string]any{"error": "发送过于频繁，请稍后再试", "retryAfter": 60})
		return
	}
	recent, err := db.QueryRowToStruct[db.VerifyCode](ctx, h.d.DB,
		`SELECT * FROM "VerifyCode" WHERE "target"=$1 AND "channel"=$2 AND "createdAt" >= NOW() - INTERVAL '60 seconds' ORDER BY "createdAt" DESC LIMIT 1`,
		target, channel)
	if err != nil && !db.IsNotFound(err) {
		util.WriteError(w, 500, "服务器内部错误")
		return
	}
	if recent != nil {
		h.markRecentVerifyCodeSend(ctx, target, channel, 60)
		util.WriteJSON(w, 429, map[string]any{"error": "发送过于频繁，请稍后再试", "retryAfter": 60})
		return
	}

	// 注册时检查是否已存在
	if typ == "register" {
		var existing *db.User
		if channel == "sms" {
			existing, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, target)
		} else {
			existing, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, target)
		}
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
		if existing != nil {
			if channel == "sms" {
				util.WriteError(w, 409, "该手机号已注册")
			} else {
				util.WriteError(w, 409, "该邮箱已注册")
			}
			return
		}
	}

	// 登录时检查是否存在（手机号/邮箱登录必须已注册）
	if typ == "login" {
		var existing *db.User
		if channel == "sms" {
			existing, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "phone"=$1`, target)
		} else {
			existing, err = db.QueryRowToStruct[db.User](ctx, h.d.DB, `SELECT * FROM "User" WHERE "email"=$1`, target)
		}
		if err != nil && !db.IsNotFound(err) {
			util.WriteError(w, 500, "服务器内部错误")
			return
		}
		if existing == nil {
			if channel == "sms" {
				util.WriteError(w, 404, "该手机号未注册")
			} else {
				util.WriteError(w, 404, "该邮箱未注册")
			}
			return
		}
	}

	if channel == "sms" {
		// SMS 渠道：原经阿里云 dypnsapi 发送，验证码由阿里云管理。
		// Go 版未集成阿里云 SDK，sendAliyunSms 始终返回失败，走与 Node 一致的
		// 降级逻辑：本地生成验证码存库。
		result := h.sendAliyunSms(ctx, target, typ)
		if !result.success {
			log.Printf("[Auth] 短信发送失败(%s) -> %s", result.message, target)
			fallbackCode := generateVerifyCode()
			expiresAt := time.Now().Add(5 * time.Minute)
			h.storeVerifyCode(ctx, target, fallbackCode, typ, channel, expiresAt)
			h.markRecentVerifyCodeSend(ctx, target, channel, 60)
			resp := map[string]any{"success": true, "message": "验证码已发送"}
			if h.d.Cfg.NodeEnv != "production" {
				resp["_dev"] = fallbackCode
				resp["_smsError"] = result.message
			}
			util.WriteJSON(w, 200, resp)
			return
		}
		// 发送成功：在数据库中存储一条占位记录（code 为空），用于频率限制判断
		// 实际验证码由阿里云管理，校验时调用 checkAliyunSmsCode
		expiresAt := time.Now().Add(5 * time.Minute)
		h.storeVerifyCode(ctx, target, "__aliyun__", typ, channel, expiresAt)
		h.markRecentVerifyCodeSend(ctx, target, channel, 60)
	} else {
		// 邮箱验证码 - 本地生成并存储
		code := generateVerifyCode()
		expiresAt := time.Now().Add(5 * time.Minute)
		h.storeVerifyCode(ctx, target, code, typ, channel, expiresAt)
		h.markRecentVerifyCodeSend(ctx, target, channel, 60)
		log.Printf("[Auth] 邮箱验证码: %s -> %s", code, target)
		siteCfg := h.getSystemConfig(ctx, "site")
		siteURL := "https://im.cqcq.chat"
		if siteCfg != nil {
			if u, ok := siteCfg["url"].(string); ok && u != "" {
				siteURL = u
			}
		}
		logoURL := siteURL + "/imim-email-logo.jpg"
		html := `<div style="padding: 20px; background-color: #f5f5f5; font-family: sans-serif;">
<div style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); overflow: hidden;">
<div style="background: linear-gradient(135deg, #1a237e 0%, #283593 100%); padding: 24px; text-align: center;">
<img src="` + logoURL + `" alt="imim" style="width: 64px; height: 64px; border-radius: 50%; border: 3px solid rgba(255,255,255,0.3); object-fit: cover;" />
<h1 style="color: #ffffff; margin: 12px 0 0; font-size: 20px; font-weight: 600;">imim</h1>
</div>
<div style="padding: 30px;">
<h2 style="color: #333; margin-top: 0; font-size: 18px;">邮箱验证码</h2>
<p style="color: #666; font-size: 15px; line-height: 1.6;">您好，您正在进行邮箱验证操作，验证码如下：</p>
<div style="font-size: 36px; font-weight: bold; color: #1a237e; letter-spacing: 6px; margin: 24px 0; text-align: center; background: #f0f2ff; padding: 16px; border-radius: 8px;">` + code + `</div>
<p style="color: #999; font-size: 13px; line-height: 1.5;">验证码有效期为 5 分钟，请勿泄露给他人。<br/>如非本人操作，请忽略此邮件。</p>
</div>
<div style="background: #f8f9fa; padding: 16px; text-align: center; border-top: 1px solid #eee;">
<p style="color: #aaa; font-size: 12px; margin: 0;">此邮件由 imim 系统自动发送，请勿直接回复</p>
</div>
</div>
</div>`
		h.sendEmail(ctx, target, "验证码", html)
	}

	msg := "验证码已发送到邮箱"
	if channel == "sms" {
		msg = "验证码已发送到手机"
	}
	util.WriteJSON(w, 200, map[string]any{
		"success": true,
		"message": msg,
	})
}

// storeVerifyCode 同时写入 DB 与 Redis 记录（照抄 send-code 中的存储逻辑）。
func (h *Handler) storeVerifyCode(ctx context.Context, target, code, typ, channel string, expiresAt time.Time) {
	_, _ = h.d.DB.Exec(ctx,
		`INSERT INTO "VerifyCode" ("id","target","code","type","channel","used","attempts","expiresAt","createdAt") VALUES ($1,$2,$3,$4,$5,false,0,$6,NOW())`,
		util.NewID(), target, code, typ, channel, expiresAt)
	h.setVerifyCodeRecord(ctx, verifyCodeRecord{
		Target:    target,
		Type:      typ,
		Channel:   channel,
		Code:      code,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Used:      false,
		Attempts:  0,
	})
}

// ============ 验证码校验（内部函数，照抄 verifyCode） ============

type codeCheckResult struct {
	valid bool
	err   string
}

// verifyCodeConsumeLua 原子消费验证码：校验 + 次数 + 消费一次完成。
// 返回: OK | WRONG:<attempts> | USED | LOCKED | NOT_FOUND
const verifyCodeConsumeLua = `
local raw = redis.call('GET', KEYS[1])
if not raw then
  return {'NOT_FOUND'}
end
local rec = cjson.decode(raw)
if rec.used then
  if rec.locked then
    return {'LOCKED'}
  end
  return {'USED'}
end
local attempts = tonumber(rec.attempts) or 0
local maxAttempts = tonumber(ARGV[2])
if attempts >= maxAttempts then
  rec.used = true
  rec.locked = true
  redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
  return {'LOCKED'}
end
if rec.code ~= ARGV[1] then
  attempts = attempts + 1
  rec.attempts = attempts
  if attempts >= maxAttempts then
    rec.used = true
    rec.locked = true
  end
  redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
  return {'WRONG', tostring(attempts)}
end
rec.used = true
rec.attempts = attempts + 1
redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
return {'OK'}
`

// verifyCodeReserveLua 阿里云短信验证码：先原子预留一次尝试机会（尝试数+1），
// 外部调阿里云 API 校验通过后再用 verifyCodeConfirmLua 原子确认消费。
const verifyCodeReserveLua = `
local raw = redis.call('GET', KEYS[1])
if not raw then
  return {'NOT_FOUND'}
end
local rec = cjson.decode(raw)
if rec.used then
  if rec.locked then
    return {'LOCKED'}
  end
  return {'USED'}
end
local attempts = tonumber(rec.attempts) or 0
if attempts >= tonumber(ARGV[1]) then
  rec.used = true
  rec.locked = true
  redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
  return {'LOCKED'}
end
rec.attempts = attempts + 1
redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
return {'RESERVED', rec.code or ''}
`

// verifyCodeConfirmLua 确认消费：仅当 used=false 时置 true（CAS），防并发复用。
const verifyCodeConfirmLua = `
local raw = redis.call('GET', KEYS[1])
if not raw then
  return {'NOT_FOUND'}
end
local rec = cjson.decode(raw)
if rec.used then
  return {'USED'}
end
rec.used = true
redis.call('SET', KEYS[1], cjson.encode(rec), 'KEEPTTL')
return {'OK'}
`

// luaResult 取 Lua 返回数组的首个元素。
func luaResult(v any) (string, []string) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return "", nil
	}
	s, _ := arr[0].(string)
	rest := make([]string, 0, len(arr)-1)
	for _, e := range arr[1:] {
		es, _ := e.(string)
		rest = append(rest, es)
	}
	return s, rest
}

// verifyCode 校验验证码：优先 Redis（Lua 原子消费），Redis 无记录时走 DB（原子 UPDATE）。
func (h *Handler) verifyCode(ctx context.Context, target, code, typ, channel string) codeCheckResult {
	key := verifyCodeKey(target, typ, channel)

	// 先看 Redis 是否有记录（key 存在即未过期，因为写入时按 expiresAt 设了 TTL）。
	if raw, ok, _ := h.d.Redis.GetString(ctx, key); ok && raw != "" {
		var rec verifyCodeRecord
		if json.Unmarshal([]byte(raw), &rec) == nil && !rec.Used {
			if channel == "sms" && rec.Code == "__aliyun__" {
				return h.verifyAliyunCode(ctx, target, code, typ, key)
			}
			res, rest := luaResult(mustEval(h, ctx, verifyCodeConsumeLua, key, code, "5"))
			switch res {
			case "OK":
				_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true,"attempts"="attempts"+1 WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, channel)
				return codeCheckResult{true, ""}
			case "WRONG":
				attempts := 0
				if len(rest) > 0 {
					attempts, _ = strconv.Atoi(rest[0])
				}
				_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "attempts"="attempts"+1,"used"=CASE WHEN "attempts"+1>=5 THEN true ELSE "used" END WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, channel)
				if attempts >= 5 {
					return codeCheckResult{false, "验证码已失效，请重新获取"}
				}
				return codeCheckResult{false, "验证码错误"}
			case "LOCKED":
				_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, channel)
				return codeCheckResult{false, "验证码已失效，请重新获取"}
			case "USED":
				return codeCheckResult{false, "验证码已使用"}
			default:
				return codeCheckResult{false, "验证码不存在或已过期"}
			}
		}
	}

	// ---- DB 兜底路径（Redis 无记录）：消费步骤用原子 UPDATE 防并发复用 ----
	record, err := db.QueryRowToStruct[db.VerifyCode](ctx, h.d.DB,
		`SELECT * FROM "VerifyCode" WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false AND "expiresAt" >= NOW() ORDER BY "createdAt" DESC LIMIT 1`,
		target, typ, channel)
	if err != nil || record == nil {
		return codeCheckResult{false, "验证码不存在或已过期"}
	}

	if channel == "sms" && record.Code == "__aliyun__" {
		checkResult := h.checkAliyunSmsCode(ctx, target, code)
		if !checkResult.valid {
			msg := checkResult.err
			if msg == "" {
				msg = "验证码错误"
			}
			return codeCheckResult{false, msg}
		}
		// 原子消费：只有 used=false 的行能被置 true
		n, _ := h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true WHERE "id"=$1 AND "used"=false`, record.Id)
		if n == 1 {
			return codeCheckResult{true, ""}
		}
		return codeCheckResult{false, "验证码已使用"}
	}

	if record.Attempts >= 5 {
		_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true WHERE "id"=$1`, record.Id)
		return codeCheckResult{false, "验证码已失效，请重新获取"}
	}

	if record.Code != code {
		_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "attempts"="attempts"+1,"used"=CASE WHEN "attempts"+1>=5 THEN true ELSE "used" END WHERE "id"=$1 AND "used"=false`, record.Id)
		if record.Attempts+1 >= 5 {
			return codeCheckResult{false, "验证码已失效，请重新获取"}
		}
		return codeCheckResult{false, "验证码错误"}
	}

	// 验证码正确：原子消费，防并发复用
	n, _ := h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true,"attempts"="attempts"+1 WHERE "id"=$1 AND "used"=false AND "code"=$2`, record.Id, code)
	if n == 1 {
		return codeCheckResult{true, ""}
	}
	return codeCheckResult{false, "验证码已使用"}
}

// verifyAliyunCode 阿里云短信验证码校验：Redis 预留 → 调阿里云 API → 原子确认。
func (h *Handler) verifyAliyunCode(ctx context.Context, target, code, typ, key string) codeCheckResult {
	res, rest := luaResult(mustEval(h, ctx, verifyCodeReserveLua, key, "5"))
	switch res {
	case "LOCKED":
		_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, "sms")
		return codeCheckResult{false, "验证码已失效，请重新获取"}
	case "USED":
		return codeCheckResult{false, "验证码已使用"}
	case "RESERVED":
		// 预留成功，继续调阿里云校验
		_ = rest
	default:
		return codeCheckResult{false, "验证码不存在或已过期"}
	}

	checkResult := h.checkAliyunSmsCode(ctx, target, code)
	if !checkResult.valid {
		msg := checkResult.err
		if msg == "" {
			msg = "验证码错误"
		}
		_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "attempts"="attempts"+1 WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, "sms")
		return codeCheckResult{false, msg}
	}

	// 阿里云校验通过：原子确认消费
	confirm, _ := luaResult(mustEval(h, ctx, verifyCodeConfirmLua, key))
	if confirm != "OK" {
		return codeCheckResult{false, "验证码已使用"}
	}
	_, _ = h.d.DB.Exec(ctx, `UPDATE "VerifyCode" SET "used"=true WHERE "target"=$1 AND "type"=$2 AND "channel"=$3 AND "used"=false`, target, typ, "sms")
	return codeCheckResult{true, ""}
}

// mustEval 执行 Lua，失败返回 nil（调用方按 NOT_FOUND 处理）。
func mustEval(h *Handler, ctx context.Context, script, key string, args ...any) any {
	v, err := h.d.Redis.Eval(ctx, script, []string{key}, args...)
	if err != nil {
		log.Printf("[Auth] 验证码 Lua 执行失败: %v", err)
		return nil
	}
	return v
}
