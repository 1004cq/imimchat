package wireclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"github.com/neomsg/neomsg/backend/internal/protocol"
)

type LoginResponse struct {
	AccessToken string `json:"access_token"`
	UserID      int64  `json:"user_id"`
}

// Client WebSocket Wire 协议客户端
type Client struct {
	apiURL string
	wsURL  string
	conn   *websocket.Conn
	codec  *protocol.FrameCodec
	userID int64
}

func New(apiURL, wsURL string) *Client {
	return &Client{apiURL: apiURL, wsURL: wsURL, codec: protocol.NewFrameCodec()}
}

func (c *Client) Login(phone, password, deviceID string) (string, error) {
	body := map[string]string{
		"phone":     phone,
		"password":  password,
		"device_id": deviceID,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(c.apiURL+"/v1/auth/login", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	c.userID = out.UserID
	return out.AccessToken, nil
}

func (c *Client) UserID() int64 { return c.userID }

func (c *Client) Connect(token, deviceID string) error {
	u, err := url.Parse(c.wsURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set("device_id", deviceID)
	q.Set("platform", "go")
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) SendMessage(msg *pb.Message) (*pb.MessageAck, error) {
	pkt := &pb.WirePacket{Payload: &pb.WirePacket_Message{Message: msg}}
	frame, err := c.codec.EncodeWirePacket(pkt)
	if err != nil {
		return nil, err
	}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return nil, err
	}
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	resp, err := c.codec.DecodeWirePacket(data)
	if err != nil {
		return nil, err
	}
	if ack := resp.GetMessageAck(); ack != nil {
		return ack, nil
	}
	return nil, fmt.Errorf("no ack in response")
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
