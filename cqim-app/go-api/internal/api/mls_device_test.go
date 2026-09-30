package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMLSDeviceJoinRoutesRequireAuthenticationAndReturnJSON(t *testing.T) {
	handler := (&Server{}).Handler()
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/mls/device-join/request"},
		{http.MethodGet, "/api/mls/device-join/pending?groupId=g1"},
		{http.MethodPost, "/api/mls/device-join/claim"},
		{http.MethodPost, "/api/mls/device-join/complete"},
		{http.MethodGet, "/api/mls/device-join/welcome?groupId=g1&deviceId=d1"},
		{http.MethodPost, "/api/mls/device-join/ack"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s %s content-type=%q", tc.method, tc.path, rec.Header().Get("Content-Type"))
		}
		var payload map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload["error"] == "" {
			t.Fatalf("%s %s invalid JSON body=%s", tc.method, tc.path, rec.Body.String())
		}
	}
}

func TestMLSDeviceRequestIDIsStableAndScoped(t *testing.T) {
	a := mlsDeviceRequestID("group", "user", "device")
	b := mlsDeviceRequestID("group", "user", "device")
	c := mlsDeviceRequestID("group", "user", "other-device")
	if a != b || a == c || len(a) != 64 {
		t.Fatalf("unexpected request IDs: %q %q %q", a, b, c)
	}
}
