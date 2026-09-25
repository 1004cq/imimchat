package sticker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	telegramMaxStickers  = 200
	telegramMaxFileBytes = 8 * 1024 * 1024
)

// telegramToken 读取 TELEGRAM_BOT_TOKEN（对应 sticker.ts 模块加载时的读取）。
func telegramToken() string {
	return strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
}

var telegramHTTPClient = &http.Client{Timeout: 60 * time.Second}

type telegramSticker struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	IsAnimated   bool   `json:"is_animated"`
	IsVideo      bool   `json:"is_video"`
	Emoji        string `json:"emoji"`
	SetName      string `json:"set_name"`
	FileSize     int64  `json:"file_size"`
}

type telegramStickerSet struct {
	Name       string            `json:"name"`
	Title      string            `json:"title"`
	IsAnimated bool              `json:"is_animated"`
	IsVideo    bool              `json:"is_video"`
	Stickers   []telegramSticker `json:"stickers"`
}

type telegramFile struct {
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

// telegramAPI 调用 Telegram Bot API（对应 sticker.ts 的 telegramApi）。
func telegramAPI(ctx context.Context, method string, body map[string]string) (json.RawMessage, error) {
	token := telegramToken()
	if token == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN 未配置")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := telegramHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("Telegram API %s 请求失败", method)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !out.OK || len(out.Result) == 0 {
		msg := out.Description
		if msg == "" {
			msg = fmt.Sprintf("Telegram API %s 请求失败", method)
		}
		return nil, errors.New(msg)
	}
	return out.Result, nil
}

// downloadTelegramFile 下载 Telegram 文件到目标路径（对应 downloadTelegramFile，
// 含 8MB 上限与临时文件 + 原子 rename）。
func downloadTelegramFile(ctx context.Context, filePath, targetPath string) error {
	token := telegramToken()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.telegram.org/file/bot"+token+"/"+filePath, nil)
	if err != nil {
		return err
	}
	resp, err := telegramHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram 文件下载失败 (%d)", resp.StatusCode)
	}
	if resp.ContentLength > telegramMaxFileBytes {
		return errors.New("贴纸文件超过大小限制")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, telegramMaxFileBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > telegramMaxFileBytes {
		return errors.New("贴纸文件超过大小限制")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp-%d-%d", targetPath, os.Getpid(), time.Now().UnixMilli())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

var telegramShortNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

// telegramPackID 生成 telegram 贴纸包 id（对应 telegramPackId）。
func telegramPackID(shortName string) string {
	sum := sha256.Sum256([]byte(shortName))
	return "telegram_" + hex.EncodeToString(sum[:])[:18]
}

// importTelegramPack 从 Telegram 导入贴纸包并落盘到 filesDir（对应 importTelegramPack）。
func importTelegramPack(ctx context.Context, filesDir, input string) (*StickerPack, error) {
	shortName := parseStickerPackInput(input)
	if !telegramShortNameRe.MatchString(shortName) {
		return nil, errors.New("Telegram 贴纸包短名格式不正确")
	}
	rawSet, err := telegramAPI(ctx, "getStickerSet", map[string]string{"name": shortName})
	if err != nil {
		return nil, err
	}
	var set telegramStickerSet
	if err := json.Unmarshal(rawSet, &set); err != nil {
		return nil, err
	}
	packID := telegramPackID(shortName)
	packDir := "telegram/" + packID
	stickers := make([]StickerItem, 0)
	for _, st := range set.Stickers {
		if st.IsVideo {
			continue
		}
		if len(stickers) >= telegramMaxStickers {
			break
		}
		st := st
		func() {
			rawFile, err := telegramAPI(ctx, "getFile", map[string]string{"file_id": st.FileID})
			if err != nil {
				log.Printf("[Sticker] Telegram 贴纸下载失败 (%s): %v", st.FileUniqueID, err)
				return
			}
			var tf telegramFile
			if err := json.Unmarshal(rawFile, &tf); err != nil || tf.FilePath == "" {
				return
			}
			ext := "webp"
			if st.IsAnimated {
				ext = "tgs"
			}
			relFile := packDir + "/" + st.FileUniqueID + "." + ext
			if err := downloadTelegramFile(ctx, tf.FilePath, filepath.Join(filesDir, filepath.FromSlash(relFile))); err != nil {
				log.Printf("[Sticker] Telegram 贴纸下载失败 (%s): %v", st.FileUniqueID, err)
				return
			}
			emoji := st.Emoji
			if emoji == "" {
				emoji = "🙂"
			}
			name := st.Emoji
			if name == "" {
				name = st.FileUniqueID
			}
			keywords := []string{}
			if st.Emoji != "" {
				keywords = []string{st.Emoji}
			}
			w, hgt := st.Width, st.Height
			stickers = append(stickers, StickerItem{
				ID:       st.FileUniqueID,
				Emoji:    emoji,
				Name:     name,
				File:     &relFile,
				Format:   ext,
				Width:    &w,
				Height:   &hgt,
				Keywords: keywords,
			})
		}()
	}
	if len(stickers) == 0 {
		return nil, errors.New("Telegram 贴纸包没有可用资源或下载失败")
	}
	name := set.Title
	if name == "" {
		name = shortName
	}
	return &StickerPack{
		ID:          packID,
		ShortName:   shortName,
		ShareURL:    buildShareURL(shortName, ""),
		Name:        name,
		Icon:        stickers[0].Emoji,
		Description: "Telegram 贴纸包 " + shortName,
		SourceType:  "local",
		Keywords:    []string{shortName, "telegram"},
		Stickers:    stickers,
	}, nil
}
