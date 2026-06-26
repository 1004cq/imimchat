package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/pkg/mtprotoclient"
	"github.com/neomsg/neomsg/backend/pkg/wireclient"
)

func main() {
	api := flag.String("api", "http://localhost:8090", "NeoMsg API")
	mtproto := flag.String("mtproto", "localhost:10443", "MTProto TCP address")
	rsaPEM := flag.String("rsa-pem", "/data/rsa.pem", "RSA private PEM (same as server MTPROTO_RSA_KEY)")
	device := flag.String("device", "demo-mtproto-1", "device id")
	chatID := flag.Int64("chat", 1, "chat id")
	text := flag.String("text", "hello from mtproto client", "message text")
	flag.Parse()

	wc := wireclient.New(*api, "")
	token, err := wc.Login("13800000000", "password", *device)
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	userID := wc.UserID()

	rsaKey, err := mtprotoclient.LoadRSAKey(*rsaPEM)
	if err != nil {
		log.Fatalf("load rsa pem %s: %v", *rsaPEM, err)
	}

	mc := mtprotoclient.New(*mtproto)
	if err := mc.Connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer mc.Close()

	if err := mc.Handshake(rsaKey); err != nil {
		log.Fatalf("handshake: %v", err)
	}
	if err := mc.BindSession(userID, *device, token); err != nil {
		log.Fatalf("bind: %v", err)
	}

	ack, err := mc.SendMessage(&pb.Message{
		ChatId:  *chatID,
		FromId:  userID,
		Content: *text,
		MsgType: 0,
	})
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Fprintf(os.Stdout, "ack: msg_id=%d seq=%d success=%v\n", ack.MsgId, ack.SeqId, ack.Success)
}
