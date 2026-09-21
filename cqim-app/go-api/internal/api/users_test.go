package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeAvatarURL(t *testing.T) {
	cases := map[string]string{
		"":                              "",
		"/api/media/abc":                "/api/media/abc",
		"/imim-official-avatar.jpg":     "/imim-official-avatar.jpg",
		"https://cdn.example.com/a.png": "https://cdn.example.com/a.png",
		"https://bucket.cos.ap-guangzhou.myqcloud.com/a": "",
		"https://foo.myqcloud.com/a":                     "",
		"http://insecure.example/a":                      "",
	}
	for in, want := range cases {
		if got := safeAvatarUrl(in); got != want {
			t.Fatalf("safeAvatarUrl(%q)=%q want %q", in, got, want)
		}
	}
}

func TestUserRoutesJSONNotPlain404(t *testing.T) {
	handler := (&Server{}).Handler()
	cases := []struct {
		path   string
		status int
		err    string
	}{
		{"/api/users/search", http.StatusBadRequest, "请输入至少1个字符"},
		{"/api/users/missing/presence", http.StatusOK, ""},
		{"/api/profile", http.StatusUnauthorized, "未登录"},
		{"/api/user/me", http.StatusUnauthorized, "未登录"},
		{"/api/home/sync", http.StatusUnauthorized, "未登录"},
		{"/api/q/profile/nobody", http.StatusServiceUnavailable, "服务器错误"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "404 page not found") {
			t.Fatalf("%s returned net/http default 404", tc.path)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s content-type %q", tc.path, rec.Header().Get("Content-Type"))
		}
		if tc.err != "" {
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("%s json: %v", tc.path, err)
			}
			if payload["error"] != tc.err {
				t.Fatalf("%s error=%q", tc.path, payload["error"])
			}
		}
	}
}

func TestOfficialAndBotProfiles(t *testing.T) {
	handler := (&Server{}).Handler()
	for _, id := range []string{"official", "BOT"} {
		req := httptest.NewRequest(http.MethodGet, "/api/users/"+id, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", id, rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["id"] != id {
			t.Fatalf("id=%v", payload["id"])
		}
		if payload["avatar"] == "" {
			t.Fatalf("%s missing avatar", id)
		}
	}
}

func TestAuthProfileIsRegistered(t *testing.T) {
	handler := (&Server{}).Handler()
	req := httptest.NewRequest(http.MethodPut, "/api/auth/profile", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "尚未迁移") || strings.Contains(rec.Body.String(), "404 page not found") {
		t.Fatalf("auth profile should not be the 501 catch-all: %s", rec.Body.String())
	}
}

func TestPresenceSpecialUsers(t *testing.T) {
	handler := (&Server{}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/users/official/presence", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &payload) != nil {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if payload["online"] != true || payload["userId"] != "official" {
		t.Fatalf("%v", payload)
	}
}
