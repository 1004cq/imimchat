package web

// 链接预览 SSRF 防护的单测：IP 黑名单与 URL 校验均为纯函数，无需外部服务。

import (
	"net"
	"testing"
)

func TestLinkPreviewIPBlocked(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1",
		"10.0.0.1", "172.16.5.4", "192.168.1.1",
		"169.254.169.254", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", // 组播
	}
	for _, s := range blocked {
		if !linkPreviewIPBlocked(net.ParseIP(s)) {
			t.Errorf("应拦截 %s", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34"}
	for _, s := range allowed {
		if linkPreviewIPBlocked(net.ParseIP(s)) {
			t.Errorf("不应拦截公网 IP %s", s)
		}
	}
}

func TestLinkPreviewCheckURL(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/admin",
		"http://10.0.0.1/",
		"http://192.168.0.1:8080/",
		"http://172.16.0.1/",
		"http://[::1]/",
		"http://169.254.169.254/latest/meta-data/", // 云元数据服务
		"http://localhost/",                        // 解析为回环
		"ftp://8.8.8.8/file",                       // 非 http/https scheme
		"file:///etc/passwd",
		"http://user:pass@8.8.8.8/", // 携带认证信息
		"not a url",
		"",
	}
	for _, raw := range blocked {
		if err := linkPreviewCheckURL(raw); err == nil {
			t.Errorf("应拒绝 %q", raw)
		}
	}
	// IP 字面量公网地址：无需 DNS，可直接放行
	allowed := []string{
		"http://8.8.8.8/",
		"https://1.1.1.1:443/path?q=1",
	}
	for _, raw := range allowed {
		if err := linkPreviewCheckURL(raw); err != nil {
			t.Errorf("不应拒绝 %q: %v", raw, err)
		}
	}
}
