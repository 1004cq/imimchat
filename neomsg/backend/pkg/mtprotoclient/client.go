package mtprotoclient

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"time"

	mtcrypto "github.com/neomsg/neomsg/backend/internal/mtproto/crypto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/handshake"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
	"github.com/neomsg/neomsg/backend/internal/mtproto/transport"
	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	pb "github.com/neomsg/neomsg/backend/internal/protocol/pb/neomsg/v1"
	"google.golang.org/protobuf/proto"
)

// Client NeoMsg MTProto 客户端（Abridged 传输 + Bridge TL）
type Client struct {
	addr      string
	conn      net.Conn
	codec     *transport.Codec
	rsaKey    *rsa.PrivateKey
	authKey   mtcrypto.AuthKey
	sessionID int64
	seqNo     int32
}

func New(addr string) *Client {
	return &Client{addr: addr}
}

// LoadRSAKey 加载与服务端相同的 RSA PEM（开发环境；生产应仅保存公钥并完成标准握手）
func LoadRSAKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid pem")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func (c *Client) Connect() error {
	conn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		return err
	}
	if _, err := conn.Write([]byte{transport.AbridgedMarker}); err != nil {
		conn.Close()
		return err
	}
	c.conn = conn
	c.codec = transport.NewCodec(conn, transport.ModeAbridged)
	return nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Handshake 完成 DH 握手（开发模式：使用与服务端共享的 RSA 私钥文件）
func (c *Client) Handshake(rsaKey *rsa.PrivateKey) error {
	c.rsaKey = rsaKey
	clientNonce, err := mtcrypto.RandomInt128()
	if err != nil {
		return err
	}

	w := tl.NewWriter()
	w.WriteInt(handshake.CRCReqPQMulti)
	w.WriteInt128(clientNonce)
	resp, err := c.invokeUnencrypted(w.Bytes())
	if err != nil {
		return err
	}

	r := tl.NewReader(resp)
	if cid, _ := r.ReadInt(); cid != handshake.CRCResPQ {
		return fmt.Errorf("expected resPQ, got %#x", cid)
	}
	_, _ = r.ReadInt128()
	serverNonce, err := r.ReadInt128()
	if err != nil {
		return err
	}
	pqHex, _ := r.ReadString()
	fps, _ := r.ReadVectorLong()
	if len(fps) == 0 {
		return fmt.Errorf("no rsa fingerprints")
	}

	var pq int64
	fmt.Sscanf(pqHex, "%x", &pq)
	p, q := mtcrypto.FactorPQ(pq)
	newNonce, _ := mtcrypto.RandomInt128()

	inner := tl.NewWriter()
	inner.WriteInt(handshake.CRCPQInnerData)
	inner.WriteInt128(clientNonce)
	inner.WriteInt128(serverNonce)
	inner.WriteString(pqHex)
	inner.WriteString(fmt.Sprintf("%x", uint64(p)))
	inner.WriteString(fmt.Sprintf("%x", uint64(q)))
	inner.WriteInt128(newNonce)
	encInner, err := rsa.EncryptPKCS1v15(rand.Reader, &rsaKey.PublicKey, padRSA(inner.Bytes()))
	if err != nil {
		return err
	}

	w = tl.NewWriter()
	w.WriteInt(handshake.CRCReqDHParams)
	w.WriteInt128(clientNonce)
	w.WriteInt128(serverNonce)
	w.WriteString(fmt.Sprintf("%x", uint64(p)))
	w.WriteString(fmt.Sprintf("%x", uint64(q)))
	w.WriteLong(fps[0])
	w.WriteString(string(encInner))
	resp, err = c.invokeUnencrypted(w.Bytes())
	if err != nil {
		return err
	}

	r = tl.NewReader(resp)
	if cid, _ := r.ReadInt(); cid != handshake.CRCServerDHParamsOK {
		return fmt.Errorf("expected server_DH_params_ok")
	}
	_, _ = r.ReadInt128()
	_, _ = r.ReadInt128()
	encAnswer, _ := r.ReadString()
	serverInner, err := rsa.DecryptPKCS1v15(rand.Reader, rsaKey, []byte(encAnswer))
	if err != nil {
		return fmt.Errorf("decrypt server dh: %w", err)
	}

	ir := tl.NewReader(serverInner)
	ir.ReadInt()
	ir.ReadInt128()
	ir.ReadInt128()
	ir.ReadInt()
	gBServer, _ := ir.ReadBytes()
	serverDHNonce, _ := ir.ReadInt128()

	gClient, _ := rand.Int(rand.Reader, mtcrypto.DHPrime)
	gClientBytes := pad256(gClient.Bytes())
	clientDH := tl.NewWriter()
	clientDH.WriteInt(handshake.CRCClientDHInnerData)
	clientDH.WriteInt128(clientNonce)
	clientDH.WriteInt128(serverNonce)
	clientDH.WriteLong(0)
	clientDH.WriteString(string(gClientBytes))
	encClient, err := rsa.EncryptPKCS1v15(rand.Reader, &rsaKey.PublicKey, padRSA(clientDH.Bytes()))
	if err != nil {
		return err
	}

	w = tl.NewWriter()
	w.WriteInt(handshake.CRCSetClientDHParams)
	w.WriteInt128(clientNonce)
	w.WriteInt128(serverNonce)
	w.WriteString(string(encClient))
	resp, err = c.invokeUnencrypted(w.Bytes())
	if err != nil {
		return err
	}

	rr := tl.NewReader(resp)
	if cid, _ := rr.ReadInt(); cid != handshake.CRCDHGenOK {
		return fmt.Errorf("expected dh_gen_ok")
	}
	_, _ = rr.ReadInt128()
	_, _ = rr.ReadInt128()
	_, _ = rr.ReadString()
	keyID, _ := rr.ReadLong()

	c.authKey.Value = mtcrypto.ComputeAuthKey(gBServer, gClientBytes, clientNonce, serverNonce)
	c.authKey.ComputeID()
	if keyID != 0 && c.authKey.ID != keyID {
		return fmt.Errorf("auth key id mismatch")
	}
	c.sessionID = int64(binary.LittleEndian.Uint64(serverDHNonce[:8]))
	return nil
}

func (c *Client) BindSession(userID int64, deviceID, token string) error {
	w := tl.NewWriter()
	w.WriteInt(bridge.CRCNeoMsgBindSession)
	w.WriteLong(userID)
	w.WriteString(deviceID)
	w.WriteString(token)
	resp, err := c.invokeEncrypted(w.Bytes())
	if err != nil {
		return err
	}
	r := tl.NewReader(resp)
	cid, _ := r.ReadInt()
	if cid != bridge.CRCNeoMsgBindOk {
		return fmt.Errorf("unexpected bind response %#x", cid)
	}
	ok, _ := r.ReadInt()
	if ok != 1 {
		return fmt.Errorf("bind rejected")
	}
	return nil
}

func (c *Client) SendMessage(msg *pb.Message) (*pb.MessageAck, error) {
	pkt := &pb.WirePacket{Payload: &pb.WirePacket_Message{Message: msg}}
	payload, err := proto.Marshal(pkt)
	if err != nil {
		return nil, err
	}
	w := tl.NewWriter()
	w.WriteInt(bridge.CRCNeoMsgInvokeWire)
	w.WriteBytes(payload)
	resp, err := c.invokeEncrypted(w.Bytes())
	if err != nil {
		return nil, err
	}
	return parseAckFromResult(resp)
}

func (c *Client) ReadPush() (*pb.Message, error) {
	raw, err := c.codec.ReadPacket()
	if err != nil {
		return nil, err
	}
	body, err := c.decryptPacket(raw)
	if err != nil {
		return nil, err
	}
	r := tl.NewReader(body)
	cid, _ := r.ReadInt()
	if cid != bridge.CRCNeoMsgPushWire {
		return nil, fmt.Errorf("not pushWire: %#x", cid)
	}
	frame, err := r.ReadBytes()
	if err != nil {
		return nil, err
	}
	var pkt pb.WirePacket
	if err := proto.Unmarshal(frame, &pkt); err != nil {
		return nil, err
	}
	return pkt.GetMessage(), nil
}

func parseAckFromResult(body []byte) (*pb.MessageAck, error) {
	r := tl.NewReader(body)
	if cid, _ := r.ReadInt(); cid != bridge.CRCNeoMsgWireResult {
		return nil, fmt.Errorf("expected wireResult")
	}
	count, _ := r.ReadInt()
	for i := 0; i < int(count); i++ {
		frame, err := r.ReadBytes()
		if err != nil {
			return nil, err
		}
		var pkt pb.WirePacket
		if err := proto.Unmarshal(frame, &pkt); err != nil {
			continue
		}
		if ack := pkt.GetMessageAck(); ack != nil {
			return ack, nil
		}
	}
	return nil, fmt.Errorf("no ack in result")
}

func (c *Client) invokeUnencrypted(body []byte) ([]byte, error) {
	packet := wrapUnencrypted(body)
	if err := c.codec.WritePacket(packet); err != nil {
		return nil, err
	}
	raw, err := c.codec.ReadPacket()
	if err != nil {
		return nil, err
	}
	if len(raw) < 20 {
		return nil, fmt.Errorf("short response")
	}
	msgLen := int(binary.LittleEndian.Uint32(raw[16:20]))
	return raw[20 : 20+msgLen], nil
}

func (c *Client) invokeEncrypted(body []byte) ([]byte, error) {
	packet, err := c.wrapEncrypted(body)
	if err != nil {
		return nil, err
	}
	if err := c.codec.WritePacket(packet); err != nil {
		return nil, err
	}
	raw, err := c.codec.ReadPacket()
	if err != nil {
		return nil, err
	}
	return c.decryptPacket(raw)
}

func wrapUnencrypted(body []byte) []byte {
	padding := 16
	buf := make([]byte, 20+len(body)+padding)
	binary.LittleEndian.PutUint64(buf[8:16], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(len(body)))
	copy(buf[20:], body)
	rand.Read(buf[20+len(body):])
	return buf
}

func padRSA(data []byte) []byte {
	out := make([]byte, 256)
	copy(out[256-len(data):], data)
	return out
}

func pad256(b []byte) []byte {
	if len(b) >= 256 {
		return b[len(b)-256:]
	}
	out := make([]byte, 256)
	copy(out[256-len(b):], b)
	return out
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}

// unused helper kept for tests
var _ = reverse

func (c *Client) wrapEncrypted(body []byte) ([]byte, error) {
	padding := (16 - ((32 + len(body)) % 16)) % 16
	if padding < 12 {
		padding += 16
	}
	inner := make([]byte, 32+len(body)+padding)
	binary.LittleEndian.PutUint64(inner[8:16], uint64(c.sessionID))
	msgID := time.Now().UnixNano() / int64(time.Millisecond)
	binary.LittleEndian.PutUint64(inner[16:24], uint64(msgID)<<32)
	c.seqNo++
	binary.LittleEndian.PutUint32(inner[24:28], uint32(c.seqNo))
	binary.LittleEndian.PutUint32(inner[28:32], uint32(len(body)))
	copy(inner[32:], body)
	rand.Read(inner[32+len(body):])

	encrypted, msgKey, err := mtcrypto.Encrypt(c.authKey.Value, inner)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 8+16+len(encrypted))
	binary.LittleEndian.PutUint64(out[0:8], uint64(c.authKey.ID))
	copy(out[8:24], msgKey[:])
	copy(out[24:], encrypted)
	return out, nil
}

func (c *Client) decryptPacket(packet []byte) ([]byte, error) {
	if len(packet) < 24 {
		return nil, fmt.Errorf("short packet")
	}
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	plain, err := mtcrypto.Decrypt(c.authKey.Value, msgKey, packet[24:])
	if err != nil {
		return nil, err
	}
	msgLen := int(binary.LittleEndian.Uint32(plain[28:32]))
	return plain[32 : 32+msgLen], nil
}
