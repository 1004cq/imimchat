package media

import (
	"net/http"
	"testing"

	"github.com/1004cq/imim.chat/cqim-app/go-server/internal/handler"
)

func TestRegisterRoutesNoPanic(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, &handler.Deps{})
	// 未注册路径应 404；已注册方法+路径不应 404（handler 内部可能 500，无妨）
	for _, tc := range []struct {
		method, path string
		want404      bool
	}{
		{"POST", "/api/media/upload", false},
		{"POST", "/api/media/upload-form", false},
		{"GET", "/api/media/abc123", false},
		{"GET", "/api/media/files/x.jpg", false},
		{"POST", "/api/voice/upload", false},
		{"GET", "/api/cos/sts", false},
		{"GET", "/api/nope", true},
	} {
		r, _ := http.NewRequest(tc.method, tc.path, nil)
		// 用带 ResponseRecorder 的方式探测 mux 是否匹配到 handler
		matched := false
		func() {
			defer func() {
				if recover() != nil {
					matched = true // handler panic 也算匹配到路由
				}
			}()
			// 直接用 mux.Handler 做匹配探测
			h, pattern := mux.Handler(r)
			_ = h
			matched = pattern != ""
		}()
		if tc.want404 && matched {
			t.Fatalf("%s %s should not match", tc.method, tc.path)
		}
		if !tc.want404 && !matched {
			t.Fatalf("%s %s should match", tc.method, tc.path)
		}
	}
}
