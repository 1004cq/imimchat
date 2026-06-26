package bridge_test

import (
	"testing"

	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
)

func TestBindSessionRoundTrip(t *testing.T) {
	w := tl.NewWriter()
	w.WriteInt(bridge.CRCNeoMsgBindSession)
	w.WriteLong(42)
	w.WriteString("iphone-1")
	w.WriteString("token-abc")
	body := w.Bytes()

	uid, did, tok, err := bridge.ParseBindSession(body)
	if err != nil {
		t.Fatal(err)
	}
	if uid != 42 || did != "iphone-1" || tok != "token-abc" {
		t.Fatalf("unexpected: %d %s %s", uid, did, tok)
	}
}

func TestInvokeWireParse(t *testing.T) {
	w := tl.NewWriter()
	w.WriteInt(bridge.CRCNeoMsgInvokeWire)
	w.WriteBytes([]byte{1, 2, 3, 4})
	body := w.Bytes()

	payload, err := bridge.ParseInvokeWire(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 4 {
		t.Fatalf("got %v", payload)
	}
}

func TestWireResultEncode(t *testing.T) {
	out := bridge.EncodeWireResult([][]byte{{1, 2}, {3}})
	r := tl.NewReader(out)
	cid, _ := r.ReadInt()
	if cid != bridge.CRCNeoMsgWireResult {
		t.Fatalf("cid %#x", cid)
	}
	count, _ := r.ReadInt()
	if count != 2 {
		t.Fatalf("count %d", count)
	}
}
