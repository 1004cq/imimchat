package gateway

import (
	"encoding/json"
	"testing"
)

func TestImPushEnvelopeRoundTrip(t *testing.T) {
	original := imPushEnvelope{
		UserID:       "user-a",
		Payload:      json.RawMessage(`{"type":"private_message","payload":{"id":"m1"}}`),
		MsgID:        "m1",
		FromNodeID:   "gw-1",
		TargetNodeID: "gw-2",
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded imPushEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.UserID != original.UserID {
		t.Fatalf("userId=%q want %q", decoded.UserID, original.UserID)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Fatalf("payload=%s want %s", decoded.Payload, original.Payload)
	}
	if decoded.MsgID != "m1" || decoded.FromNodeID != "gw-1" || decoded.TargetNodeID != "gw-2" {
		t.Fatalf("metadata mismatch: %+v", decoded)
	}
}

func TestParseOnlineRecordJSONAndLegacy(t *testing.T) {
	rec, ok := parseOnlineRecord(`{"nodeId":"gw-a","connId":"c1","ts":123}`)
	if !ok || rec.NodeID != "gw-a" || rec.ConnID != "c1" || rec.TS != 123 {
		t.Fatalf("json record: %+v ok=%v", rec, ok)
	}

	legacy, ok := parseOnlineRecord("gw-legacy")
	if !ok || legacy.NodeID != "gw-legacy" {
		t.Fatalf("legacy record: %+v ok=%v", legacy, ok)
	}

	if _, ok := parseOnlineRecord(""); ok {
		t.Fatal("empty should fail")
	}
}

func TestExtractMsgID(t *testing.T) {
	payload := []byte(`{"type":"group_message","msgId":"abc","payload":{"clientMsgId":"ignored"}}`)
	if got := extractMsgID(payload, ""); got != "abc" {
		t.Fatalf("got %q want abc", got)
	}
	if got := extractMsgID(payload, "forced"); got != "forced" {
		t.Fatalf("fallback override got %q", got)
	}
	nested := []byte(`{"type":"private_message","payload":{"id":"m-9"}}`)
	if got := extractMsgID(nested, ""); got != "m-9" {
		t.Fatalf("nested id got %q", got)
	}
}

func TestNodePushChannel(t *testing.T) {
	if got := nodePushChannel("gw-2"); got != "cqim:im:push:gw-2" {
		t.Fatalf("got %q", got)
	}
}

func TestUnwrapPayloadString(t *testing.T) {
	raw, _ := json.Marshal("{\"type\":\"x\"}")
	got := unwrapPayload(raw)
	if string(got) != `{"type":"x"}` {
		t.Fatalf("got %s", got)
	}
}
