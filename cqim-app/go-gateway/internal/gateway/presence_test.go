package gateway

import (
	"encoding/json"
	"testing"
)

func TestImPushEnvelopeRoundTrip(t *testing.T) {
	original := imPushEnvelope{
		UserID:  "user-a",
		Payload: json.RawMessage(`{"type":"private_message","payload":{"id":"m1"}}`),
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
}
