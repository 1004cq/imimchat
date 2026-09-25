package onebot

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

// StartReverseClient 启动反向 WS 客户端，主动连接到 AstrBot 的 WS 服务端。
//
// URL 从环境变量 ONEBOT_WS_URL 读取；未设置则不启动（直接返回）。
// 断线后指数退避重连：3s 起步，最高 60s；连接成功后重置为 3s。
// 连接成功后会把该连接加入 onebot 客户端集合，复用现有推送逻辑。
func StartReverseClient(ctx context.Context, d *handler.Deps) {
	url := os.Getenv("ONEBOT_WS_URL")
	if url == "" {
		log.Println("[ReverseWS] ONEBOT_WS_URL 未设置，跳过 AstrBot 连接")
		return
	}
	token := os.Getenv("ONEBOT_ACCESS_TOKEN")

	const maxReconnectDelay = 60 * time.Second
	reconnectDelay := 3 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		if !connectReverse(ctx, d, url, token) {
			// 拨号失败：退避后重试
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
			}
			reconnectDelay = minDuration(reconnectDelay*2, maxReconnectDelay)
			continue
		}
		// 连接曾成功建立（随后断开）：退避后重连
		reconnectDelay = 3 * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
		reconnectDelay = minDuration(reconnectDelay*2, maxReconnectDelay)
	}
}

// connectReverse 建立一次反向连接并阻塞到断开；返回 false 表示拨号失败。
func connectReverse(ctx context.Context, d *handler.Deps, url, token string) bool {
	log.Printf("[ReverseWS] 正在连接 AstrBot: %s", url)
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	header.Set("X-Self-ID", strconv.Itoa(BotSelfID))
	header.Set("X-Client-Role", "Universal")
	header.Set("User-Agent", "cqim/1.0 OneBot/11")

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		log.Printf("[ReverseWS] 连接失败: %v", err)
		return false
	}
	log.Printf("[ReverseWS] 已连接到 AstrBot (%s)", url)
	// 将此连接加入 onebotClients，复用现有推送逻辑
	addClient(conn)
	// 发送生命周期连接事件
	_ = wsWrite(conn, mustMarshal(map[string]any{
		"time":            time.Now().Unix(),
		"self_id":         BotSelfID,
		"post_type":       "meta_event",
		"meta_event_type": "lifecycle",
		"sub_type":        "connect",
	}))

	// ctx 取消时主动关闭
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(5*time.Second))
			_ = conn.Close()
		case <-done:
		}
	}()

	var closeCode int
	var closeReason string
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				closeCode = ce.Code
				closeReason = ce.Text
			}
			break
		}
		var a action
		if err := json.Unmarshal(data, &a); err != nil {
			log.Printf("[ReverseWS] 无法解析动作: %v", err)
			continue
		}
		// 复用现有的 handleOneBotAction 处理逻辑
		handleOneBotAction(d, conn, a)
	}
	close(done)
	removeClient(conn)
	_ = conn.Close()
	log.Printf("[ReverseWS] 连接断开 code=%d reason=%s，重连中...", closeCode, closeReason)
	return true
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
