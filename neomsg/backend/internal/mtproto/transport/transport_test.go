package transport_test

import (
	"bytes"
	"testing"

	"github.com/neomsg/neomsg/backend/internal/mtproto/transport"
)

func TestAbridgedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	codec := transport.NewCodec(&buf, transport.ModeAbridged)
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := codec.WritePacket(payload); err != nil {
		t.Fatal(err)
	}
	codec2 := transport.NewCodec(&buf, transport.ModeAbridged)
	got, err := codec2.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %v want %v", got, payload)
	}
}

func TestIntermediateRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	codec := transport.NewCodec(&buf, transport.ModeIntermediate)
	payload := []byte("hello mtproto")
	if err := codec.WritePacket(payload); err != nil {
		t.Fatal(err)
	}
	codec2 := transport.NewCodec(&buf, transport.ModeIntermediate)
	got, err := codec2.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q want %q", got, payload)
	}
}
