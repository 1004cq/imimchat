package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var mediaMax = map[string]int64{
	"image":   15 << 20,
	"voice":   20 << 20,
	"video":   100 << 20,
	"sticker": 8 << 20,
	"file":    30 << 20,
}

type mediaStore struct {
	base   *url.URL
	access string
	secret string
	region string
	bucket string
	client *http.Client
}

func mediaKind(value string) string {
	if value == "audio" {
		value = "voice"
	}
	if _, ok := mediaMax[value]; !ok {
		return ""
	}
	return value
}

func mediaLimit(kind string) int64 {
	if kind == "video" {
		if value, err := strconv.ParseInt(os.Getenv("MEDIA_MAX_VIDEO_BYTES"), 10, 64); err == nil && value > 0 {
			return value
		}
	}
	return mediaMax[kind]
}

func mediaStoreFromEnv() (*mediaStore, error) {
	endpoint := envOrDefault("MINIO_ENDPOINT", "minio")
	port := envOrDefault("MINIO_PORT", "9000")
	scheme := "http"
	if strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true") {
		scheme = "https"
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		scheme, endpoint = parsed.Scheme, parsed.Host
	}
	if _, _, err := net.SplitHostPort(endpoint); err != nil && !strings.Contains(endpoint, ":") {
		endpoint = net.JoinHostPort(endpoint, port)
	}
	base, err := url.Parse(scheme + "://" + endpoint)
	if err != nil {
		return nil, err
	}
	return &mediaStore{
		base:   base,
		access: envOrDefault("MINIO_ROOT_USER", "cqimminio"),
		secret: envOrDefault("MINIO_ROOT_PASSWORD", "cqimio-secret-change-me"),
		region: envOrDefault("MINIO_REGION", "us-east-1"),
		bucket: envOrDefault("MINIO_BUCKET", "cqim-media"),
		client: http.DefaultClient,
	}, nil
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func canonicalPath(value string) string {
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		encoded = append(encoded, url.PathEscape(part))
	}
	return "/" + strings.Join(encoded, "/")
}

func (m *mediaStore) signedRequest(ctx context.Context, method, objectPath string, body []byte, extra map[string]string) (*http.Response, error) {
	path := canonicalPath(objectPath)
	requestURL := *m.base
	requestURL.Path = path
	requestURL.RawPath = path
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	payloadHash := sha256Hex(body)
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	req.Header.Set("Host", m.base.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	for key, value := range extra {
		req.Header.Set(key, value)
	}
	canonicalHeaders := map[string]string{"host": m.base.Host}
	for key, values := range req.Header {
		if len(values) == 0 {
			continue
		}
		canonicalHeaders[strings.ToLower(key)] = strings.Join(values, ",")
	}
	keys := make([]string, 0, len(canonicalHeaders))
	for key := range canonicalHeaders {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var headers strings.Builder
	for _, key := range keys {
		headers.WriteString(key)
		headers.WriteByte(':')
		headers.WriteString(strings.Join(strings.Fields(canonicalHeaders[key]), " "))
		headers.WriteByte('\n')
	}
	signedHeaders := strings.Join(keys, ";")
	canonicalRequest := method + "\n" + path + "\n\n" + headers.String() + "\n" + signedHeaders + "\n" + payloadHash
	scope := date + "/" + m.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	keyDate := hmacSHA256([]byte("AWS4"+m.secret), date)
	keyRegion := hmacSHA256(keyDate, m.region)
	keyService := hmacSHA256(keyRegion, "s3")
	signingKey := hmacSHA256(keyService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+m.access+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return m.client.Do(req)
}

func (m *mediaStore) ensureBucket(ctx context.Context) error {
	response, err := m.signedRequest(ctx, http.MethodHead, "/"+m.bucket, nil, nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	response, err = m.signedRequest(ctx, http.MethodPut, "/"+m.bucket, nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 || response.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("create bucket: %s", response.Status)
}

func (m *mediaStore) put(ctx context.Context, key string, data []byte, mime, filename string) error {
	response, err := m.signedRequest(ctx, http.MethodPut, "/"+m.bucket+"/"+key, data, map[string]string{
		"Content-Type":                  mime,
		"X-Amz-Meta-Original-Filename": filename,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("put object: %s", response.Status)
	}
	return nil
}

func (m *mediaStore) remove(ctx context.Context, key string) {
	response, err := m.signedRequest(ctx, http.MethodDelete, "/"+m.bucket+"/"+key, nil, nil)
	if err == nil {
		_ = response.Body.Close()
	}
}

func (m *mediaStore) get(ctx context.Context, key string) (*http.Response, error) {
	response, err := m.signedRequest(ctx, http.MethodGet, "/"+m.bucket+"/"+key, nil, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("get object: %s", response.Status)
	}
	return response, nil
}

func mediaID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

func mediaURL(id string) string { return "/api/media/" + id }

func choose(first, second, fallback string) string {
	if first != "" {
		return first
	}
	if second != "" {
		return second
	}
	return fallback
}

func (s *Server) mediaUploadJSON(w http.ResponseWriter, r *http.Request) {
	s.requireUser(s.mediaUploadJSONAuth)(w, r)
}

func (s *Server) mediaUploadJSONAuth(w http.ResponseWriter, r *http.Request, u user) {
	var input struct {
		Kind          string `json:"kind"`
		Type          string `json:"type"`
		MediaType     string `json:"mediaType"`
		Mime          string `json:"mime"`
		MimeType      string `json:"mimeType"`
		Filename      string `json:"filename"`
		Name          string `json:"name"`
		Data          string `json:"data"`
		DataBase64    string `json:"dataBase64"`
		File          string `json:"file"`
		Content       string `json:"content"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		DurationMS    int    `json:"durationMs"`
		PosterMediaID string `json:"posterMediaId"`
	}
	if decode(r, &input) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_data"})
		return
	}
	requestedKind := choose(input.Kind, input.Type, input.MediaType)
	kind := mediaKind(requestedKind)
	if requestedKind == "" {
		kind = mediaKind("file")
	}
	if requestedKind != "" && kind == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_kind", "allow": []string{"image", "voice", "video", "sticker", "file"}})
		return
	}
	encoded := choose(input.Data, input.DataBase64, choose(input.File, input.Content, ""))
	if comma := strings.IndexByte(encoded, ','); comma >= 0 {
		encoded = encoded[comma+1:]
	}
	if len(encoded) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_data"})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || len(raw) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_data"})
		return
	}
	s.storeMedia(w, r, u, kind, raw, choose(input.Mime, input.MimeType, "application/octet-stream"), choose(input.Filename, input.Name, ""), input.Width, input.Height, input.DurationMS, input.PosterMediaID, false)
}

func (s *Server) mediaUploadForm(w http.ResponseWriter, r *http.Request) {
	s.requireUser(s.mediaUploadFormAuth)(w, r)
}

func (s *Server) mediaUploadFormAuth(w http.ResponseWriter, r *http.Request, u user) {
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_multipart"})
		return
	}
	kind := mediaKind(choose(r.FormValue("kind"), r.FormValue("mediaType"), "file"))
	file, header, err := r.FormFile("file")
	if err != nil || kind == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_upload"})
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 200<<20+1))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload_failed"})
		return
	}
	if int64(len(raw)) > 200<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file_too_large"})
		return
	}
	s.storeMedia(w, r, u, kind, raw, header.Header.Get("Content-Type"), header.Filename, 0, 0, 0, "", true)
}

func (s *Server) storeMedia(w http.ResponseWriter, r *http.Request, u user, kind string, raw []byte, mime, filename string, width, height, duration int, poster string, formResponse bool) {
	if mime == "" {
		mime = "application/octet-stream"
	}
	limit := mediaLimit(kind)
	if int64(len(raw)) > limit {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("file_too_large:%s:%d", kind, limit)})
		return
	}
	store, err := mediaStoreFromEnv()
	if err != nil || store.ensureBucket(r.Context()) != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload_failed"})
		return
	}
	id := mediaID()
	key := fmt.Sprintf("media/%s/%d/%s", kind, time.Now().UTC().Year(), id)
	hash := sha256.Sum256(raw)
	if err = store.put(r.Context(), key, raw, mime, filename); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload_failed"})
		return
	}
	url := mediaURL(id)
	_, err = s.db.Exec(r.Context(), `INSERT INTO "MediaFile" ("id","userId","type","kind","url","filename","mime","size","width","height","durationMs","posterMediaId","sha256","diskPath","publicPath","createdAt") VALUES ($1,$2,$3,$3,$4,$5,$6,$7,NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),NULLIF($11,''),$12,$13,$4,NOW())`, id, u.ID, kind, url, nilString(filename), mime, len(raw), width, height, duration, nilString(poster), hex.EncodeToString(hash[:]), key)
	if err != nil {
		store.remove(r.Context(), key)
		dbError(w, err)
		return
	}
	if formResponse {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "kind": kind, "url": url, "fileName": nilIfEmpty(filename), "storage": "minio", "size": len(raw), "mime": mime})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "kind": kind, "url": url, "size": len(raw), "mime": mime, "storage": "minio"})
}

func nilString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Server) mediaDownload(w http.ResponseWriter, r *http.Request) {
	var key, mime string
	var size int64
	if err := s.db.QueryRow(r.Context(), `SELECT "diskPath",COALESCE("mime",'application/octet-stream'),COALESCE("size",0) FROM "MediaFile" WHERE "id"=$1`, r.PathValue("id")).Scan(&key, &mime, &size); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	store, err := mediaStoreFromEnv()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "missing_object"})
		return
	}
	object, err := store.get(r.Context(), key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "missing_object"})
		return
	}
	defer object.Body.Close()
	if size <= 0 {
		size = object.ContentLength
	}
	w.Header().Set("Content-Type", mime)
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, object.Body)
}
