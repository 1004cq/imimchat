package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/util"
)

// ============ AI 聊天（对应 index.ts POST /api/ai-chat） ============
// S6：需要登录（userAuth），防止匿名调用盗刷服务端的 OpenAI 额度。

const aiSystemPrompt = "你是 imim AI，一个友好、智能、专业的聊天助手。你可以回答问题、帮助写作、翻译、分析代码等。回答简洁清晰，默认使用中文回复。"

// POST /api/ai-chat — 代理 OpenAI 兼容 API 实现 BOT 聊天回复。
// Body: { messages: [{ role: 'user'|'assistant', content: string }] }
func (h *Handler) aiChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if !util.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Messages) == 0 {
		util.WriteError(w, 400, "无效的消息格式")
		return
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		util.WriteError(w, 500, "AI 服务未配置")
		return
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "gpt-4.1-mini"
	}

	// 保留最近 20 条消息作为上下文
	msgs := req.Messages
	if len(msgs) > 20 {
		msgs = msgs[len(msgs)-20:]
	}
	chatMessages := make([]map[string]string, 0, len(msgs)+1)
	chatMessages = append(chatMessages, map[string]string{"role": "system", "content": aiSystemPrompt})
	for _, m := range msgs {
		chatMessages = append(chatMessages, map[string]string{"role": m.Role, "content": m.Content})
	}
	payload, _ := json.Marshal(map[string]any{
		"model":       model,
		"messages":    chatMessages,
		"max_tokens":  1000,
		"temperature": 0.7,
	})

	endpoint, err := url.Parse(baseURL + "/chat/completions")
	if err != nil {
		log.Printf("[AI Chat] 错误: %v", err)
		util.WriteError(w, 500, "服务内部错误")
		return
	}
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		log.Printf("[AI Chat] 错误: %v", err)
		util.WriteError(w, 500, "服务内部错误")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[AI Chat] 错误: %v", err)
		util.WriteError(w, 500, "服务内部错误")
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		log.Printf("[AI Chat] 错误: %v", err)
		util.WriteError(w, 500, "服务内部错误")
		return
	}

	var data struct {
		Choices []struct {
			Message *struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("[AI Chat] 错误: %v", err)
		util.WriteError(w, 500, "服务内部错误")
		return
	}
	reply := ""
	if len(data.Choices) > 0 && data.Choices[0].Message != nil {
		reply = strings.TrimSpace(data.Choices[0].Message.Content)
	}
	if reply == "" {
		util.WriteError(w, 500, "AI 返回空内容")
		return
	}
	log.Printf("[AI Chat] 回复长度: %d 字符", len([]rune(reply)))
	util.WriteJSON(w, 200, map[string]any{"reply": reply})
}
