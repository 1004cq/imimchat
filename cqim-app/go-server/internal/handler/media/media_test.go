package media

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCosKeyAlias(t *testing.T) {
	if got := CosKeyToAlias("imimchat/头像/u123/abc.jpg"); got != "imimchat/avatars/u123/abc.jpg" {
		t.Fatalf("CosKeyToAlias = %q", got)
	}
	if got := AliasToCosKey("imimchat/avatars/u123/abc.jpg"); got != "imimchat/头像/u123/abc.jpg" {
		t.Fatalf("AliasToCosKey = %q", got)
	}
	c := ResolveCosKeyCandidates("imimchat/avatars/u/1.jpg")
	if len(c) != 2 || c[0] != "imimchat/avatars/u/1.jpg" || c[1] != "imimchat/头像/u/1.jpg" {
		t.Fatalf("ResolveCosKeyCandidates = %v", c)
	}
}

func TestCosURLToProxy(t *testing.T) {
	in := "https://bucket.cos.ap-guangzhou.myqcloud.com/imimchat/%E6%9C%8B%E5%8F%8B%E5%9C%88/u/%E7%85%A7%E7%89%87/x.jpg"
	want := "/api/cos/proxy/imimchat/moments/u/photos/x.jpg"
	if got := CosURLToProxy(in); got != want {
		t.Fatalf("CosURLToProxy = %q, want %q", got, want)
	}
	// 非 COS URL 原样返回
	if got := CosURLToProxy("/api/media/abc"); got != "/api/media/abc" {
		t.Fatalf("CosURLToProxy passthrough = %q", got)
	}
}

func TestAvatarToProxy(t *testing.T) {
	in := "https://bucket.cos.ap-guangzhou.myqcloud.com/imimchat/%E5%A4%B4%E5%83%8F/u/a.png"
	got := AvatarToProxy(in)
	want := "/api/cos/proxy/imimchat/avatars/u/a.png?imageMogr2/thumbnail/200x200/format/webp/quality/80"
	if got != want {
		t.Fatalf("AvatarToProxy = %q, want %q", got, want)
	}
	if got := AvatarToProxy(""); got != "" {
		t.Fatalf("AvatarToProxy empty = %q", got)
	}
}

func TestParseCosURL(t *testing.T) {
	key, q, ok := ParseCosURL("https://b.cos.ap-guangzhou.myqcloud.com/imimchat/%E5%A4%B4%E5%83%8F/u/a.png?imageMogr2/x")
	if !ok || key != "imimchat/头像/u/a.png" || q != "imageMogr2/x" {
		t.Fatalf("ParseCosURL = %q %q %v", key, q, ok)
	}
}

func TestPublicURL(t *testing.T) {
	if got := PublicURL("/x"); got != "https://cq.je/x" {
		t.Fatalf("PublicURL = %q", got)
	}
	if got := PublicURL(""); got != "https://cq.je" {
		t.Fatalf("PublicURL empty = %q", got)
	}
	if got := PublicURL("https://a.com/b"); got != "https://a.com/b" {
		t.Fatalf("PublicURL absolute = %q", got)
	}
}

func TestMediaKinds(t *testing.T) {
	for _, k := range []string{"image", "voice", "video", "sticker", "file"} {
		if !IsMediaKind(k) {
			t.Fatalf("IsMediaKind(%q) = false", k)
		}
	}
	if IsMediaKind("audio") || IsMediaKind("") {
		t.Fatalf("IsMediaKind should reject audio/empty")
	}
	if normalizeKind("audio") != "voice" || normalizeKind("image") != "image" {
		t.Fatalf("normalizeKind wrong")
	}
}

func TestObjectKey(t *testing.T) {
	id := newMediaID()
	if len(id) != 24 {
		t.Fatalf("newMediaID len = %d", len(id))
	}
	key := objectKey("voice", id)
	if !strings.HasPrefix(key, "media/voice/20") || !strings.HasSuffix(key, "/"+id) {
		t.Fatalf("objectKey = %q", key)
	}
	if maxBytesForKind("image") != 15*1024*1024 || maxBytesForKind("sticker") != 8*1024*1024 {
		t.Fatalf("maxBytesForKind wrong")
	}
}

func TestDecodeBase64Lenient(t *testing.T) {
	if got := string(decodeBase64Lenient("aGVsbG8=")); got != "hello" {
		t.Fatalf("decode = %q", got)
	}
	if got := string(decodeBase64Lenient("data:image/png;base64,aGVsbG8=")); got != "hello" {
		t.Fatalf("decode dataURL = %q", got)
	}
}

func TestCamSafeURLEncode(t *testing.T) {
	if got := camSafeURLEncode("头像"); got != "%E5%A4%B4%E5%83%8F" {
		t.Fatalf("camSafeURLEncode = %q", got)
	}
	if got := encodeCosKey("imimchat/头像/a b.jpg"); got != "imimchat/%E5%A4%B4%E5%83%8F/a%20b.jpg" {
		t.Fatalf("encodeCosKey = %q", got)
	}
}

func TestTC3Sign(t *testing.T) {
	a := signTC3Request("id", "key", `{"Urls":["x"],"Area":"mainland"}`, 1700000000)
	b := signTC3Request("id", "key", `{"Urls":["x"],"Area":"mainland"}`, 1700000000)
	if a != b {
		t.Fatalf("TC3 sign not deterministic")
	}
	c := signTC3Request("id", "key", `{"Urls":["y"],"Area":"mainland"}`, 1700000000)
	if a == c {
		t.Fatalf("TC3 sign insensitive to payload")
	}
	if !strings.HasPrefix(a, "TC3-HMAC-SHA256 Credential=id/") {
		t.Fatalf("TC3 auth = %q", a)
	}
}

func TestCosSignatureShape(t *testing.T) {
	sig := cosSignature("secret", "get", "/a/b.jpg", nil, nil, "1700000000;1700003600")
	if len(sig) != 40 { // sha1 hex
		t.Fatalf("cosSignature len = %d (%q)", len(sig), sig)
	}
	sig2 := cosSignature("secret", "get", "/a/b.jpg", nil, nil, "1700000000;1700003600")
	if sig != sig2 {
		t.Fatalf("cosSignature not deterministic")
	}
}

// TestCosSignatureAgainstSDK 用 cos-nodejs-sdk-v5 真实输出做交叉验证。
// Node (Date.now 固定为 1785000000000):
//
//	cos.getObjectUrl({Bucket:'testbucket-123', Region:'ap-guangzhou',
//	  Key:'imimchat/头像/u1/x.jpg', Sign:true, Expires:3600})
//
// => ...?q-sign-algorithm=sha1&q-ak=AKIDTEST123
//
//	&q-sign-time=1784999999;1785003599&q-key-time=1784999999;1785003599
//	&q-header-list=host&q-url-param-list=&q-signature=948b50760ac4ef6f4047d8a8f66676732feded05
func TestCosSignatureAgainstSDK(t *testing.T) {
	got := cosSignature(
		"testsecretkey456",
		"get",
		"/imimchat/头像/u1/x.jpg",
		map[string]string{"host": "testbucket-123.cos.ap-guangzhou.myqcloud.com"},
		nil,
		"1784999999;1785003599",
	)
	want := "948b50760ac4ef6f4047d8a8f66676732feded05"
	if got != want {
		t.Fatalf("cosSignature = %q, want SDK %q", got, want)
	}
}

func TestS3SigV4RoundTrip(t *testing.T) {
	// 服务端视角独立验签：从收到的请求重算签名，与 Authorization 头比对。
	// 能抓到签名时与实际发送时的 canonical URI 不一致等 bug。
	const accessKey = "TESTACCESS"
	const secretKey = "TESTSECRET"
	const region = "us-east-1"

	verify := func(r *http.Request) (bool, string) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
			return false, "bad auth prefix"
		}
		// 解析 Credential/SignedHeaders/Signature
		var credScope, signedHeaders, signature string
		for _, part := range strings.Split(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "), ", ") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "Credential":
				credScope = kv[1]
			case "SignedHeaders":
				signedHeaders = kv[1]
			case "Signature":
				signature = kv[1]
			}
		}
		credParts := strings.SplitN(credScope, "/", 2)
		if len(credParts) != 2 || credParts[0] != accessKey {
			return false, "bad credential"
		}
		scope := credParts[1]
		dateStamp := strings.SplitN(scope, "/", 2)[0]
		amzDate := r.Header.Get("x-amz-date")
		payloadHash := r.Header.Get("x-amz-content-sha256")

		// 服务端按收到的请求重建 canonical request
		var ch strings.Builder
		for _, h := range strings.Split(signedHeaders, ";") {
			var v string
			if h == "host" {
				v = r.Host
			} else {
				v = r.Header.Get(h)
			}
			ch.WriteString(h + ":" + strings.TrimSpace(v) + "\n")
		}
		canonicalRequest := strings.Join([]string{
			r.Method, r.URL.EscapedPath(), "", ch.String(), signedHeaders, payloadHash,
		}, "\n")
		stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
		kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
		kRegion := hmacSHA256(kDate, region)
		kService := hmacSHA256(kRegion, "s3")
		kSigning := hmacSHA256(kService, "aws4_request")
		want := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
		if subtle.ConstantTimeCompare([]byte(want), []byte(signature)) != 1 {
			return false, "signature mismatch:\n got " + signature + "\nwant " + want
		}
		return true, ""
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, reason := verify(r)
		if !ok {
			t.Errorf("SigV4 verify failed for %s %s: %s", r.Method, r.URL.EscapedPath(), reason)
			w.WriteHeader(403)
			return
		}
		switch r.Method {
		case "HEAD":
			w.Header().Set("Content-Length", "1234")
			w.WriteHeader(200)
		case "PUT":
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(200)
		case "GET":
			w.Header().Set("Content-Length", "5")
			w.Write([]byte("hello"))
		case "DELETE":
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	c := &s3Client{
		scheme:     u.Scheme,
		host:       u.Host,
		region:     region,
		accessKey:  accessKey,
		secretKey:  secretKey,
		bucket:     "test-bucket",
		httpClient: srv.Client(),
	}
	ctx := context.Background()

	if err := c.ensureBucket(ctx); err != nil {
		t.Fatalf("ensureBucket: %v", err)
	}
	// 含中文与空格的 key：检验路径编码在签名与发送间一致
	key := "media/voice/2026/abc123"
	if err := c.putObject(ctx, key, []byte("voice-data"), "audio/ogg", "a b.ogg"); err != nil {
		t.Fatalf("putObject: %v", err)
	}
	size, err := c.statObject(ctx, key)
	if err != nil {
		t.Fatalf("statObject: %v", err)
	}
	if size != 1234 {
		t.Fatalf("statObject size = %d", size)
	}
	rc, err := c.getObject(ctx, key)
	if err != nil {
		t.Fatalf("getObject: %v", err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "hello" {
		t.Fatalf("getObject body = %q", body)
	}
	if err := c.removeObject(ctx, key); err != nil {
		t.Fatalf("removeObject: %v", err)
	}
}
