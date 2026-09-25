// email.go — 邮件发送，移植自 server/email.ts。
//
// TS 用 nodemailer；Go 版用标准库 net/smtp 实现（无新依赖）：
//   - secure=true（默认，465 端口）→ 直接 TLS（implicit TLS）
//   - secure=false（587 端口等）→ 明文连接 + STARTTLS（服务端支持时）
//
// SMTP 配置来自 SystemConfig 表的 'smtp' 键（对应 admin.ts getAdminConfig('smtp')）。
// 注意：TS 源码中 "配置"/"失败" 二字存在乱码（配羮/失贩），此处按原意修正为正确中文。
package misc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// smtpConfig 归一化后的 SMTP 配置（对应 email.ts getSmtpConfig 返回结构）。
type smtpConfig struct {
	Host        string
	Port        int
	Secure      bool
	AuthUser    string
	AuthPass    string
	FromName    string
	FromAddress string
	Enabled     bool
}

// getSmtpConfig 读取并归一化 SMTP 配置。
func getSmtpConfig(ctx context.Context, d *handler.Deps) smtpConfig {
	raw := map[string]any{}
	var v string
	if err := d.DB.Pool.QueryRow(ctx, `SELECT "value" FROM "SystemConfig" WHERE "key"='smtp'`).Scan(&v); err == nil {
		_ = json.Unmarshal([]byte(v), &raw)
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := raw[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	port := 465
	switch p := raw["port"].(type) {
	case float64:
		if p > 0 {
			port = int(p)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && i > 0 {
			port = i
		}
	}
	secure := true
	if s, ok := raw["secure"].(bool); ok {
		secure = s
	}
	enabled := false
	if e, ok := raw["enabled"].(bool); ok {
		enabled = e
	}
	fromName := str("fromName")
	if fromName == "" {
		fromName = "imim"
	}
	return smtpConfig{
		Host:        str("host"),
		Port:        port,
		Secure:      secure,
		AuthUser:    str("authUser", "user"),
		AuthPass:    str("authPass", "pass"),
		FromName:    fromName,
		FromAddress: str("fromAddress", "fromEmail"),
		Enabled:     enabled,
	}
}

// SendEmail 发送邮件（对应 email.ts sendEmail）。
// 成功返回 messageId；配置不完整或发送失败返回 error。
func SendEmail(d *handler.Deps, to, subject, html string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := getSmtpConfig(ctx, d)
	if cfg.Host == "" || cfg.AuthUser == "" || cfg.AuthPass == "" {
		log.Printf("[Email] SMTP 配置不完整，无法发送邮件")
		return "", errors.New("SMTP 配置不完整")
	}

	from := cfg.FromAddress
	if from == "" {
		from = cfg.AuthUser
	}
	fromHeader := from
	if cfg.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", mime.BEncoding.Encode("utf-8", cfg.FromName), from)
	}
	msgID := fmt.Sprintf("<%s@%s>", strings.ToLower(util.GenerateToken(24)), cfg.Host)

	var sb strings.Builder
	sb.WriteString("From: " + fromHeader + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + mime.BEncoding.Encode("utf-8", subject) + "\r\n")
	sb.WriteString("Message-ID: " + msgID + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(html)

	if err := sendMailSMTP(cfg, from, to, []byte(sb.String())); err != nil {
		log.Printf("[Email] 邮件发送失败: %v", err)
		return "", err
	}
	log.Printf("[Email] 邮件发送成功: %s -> %s", msgID, to)
	return msgID, nil
}

// sendMailSMTP 经 SMTP 发送原始邮件；secure=true 用隐式 TLS，否则尝试 STARTTLS。
func sendMailSMTP(cfg smtpConfig, from, to string, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if cfg.Secure {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: cfg.Host}}
		conn, err = tlsDialer.Dial("tcp", addr)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()

	if !cfg.Secure {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return err
			}
		}
	}
	if err := c.Auth(smtp.PlainAuth("", cfg.AuthUser, cfg.AuthPass, cfg.Host)); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
