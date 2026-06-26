package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/pkg/wireclient"
)

func main() {
	api := flag.String("api", "http://localhost:8090", "NeoMsg API")
	ws := flag.String("ws", "ws://localhost:8080/ws", "WebSocket URL")
	device := flag.String("device", "demo-go-1", "device id")
	chatID := flag.Int64("chat", 1, "chat id")
	text := flag.String("text", "hello from wire client", "message text")
	flag.Parse()

	c := wireclient.New(*api, *ws)
	token, err := c.Login("13800000000", "password", *device)
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	if err := c.Connect(token, *device); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer c.Close()

	ack, err := c.SendMessage(&pb.Message{
		ChatId:  *chatID,
		FromId:  c.UserID(),
		Content: *text,
		MsgType: 0,
	})
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Fprintf(os.Stdout, "ack: msg_id=%d seq=%d success=%v\n", ack.MsgId, ack.SeqId, ack.Success)
}
