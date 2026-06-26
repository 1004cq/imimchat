package mtproto

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	mtcrypto "github.com/neomsg/neomsg/backend/internal/mtproto/crypto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	"github.com/neomsg/neomsg/backend/internal/mtproto/connmgr"
	"github.com/neomsg/neomsg/backend/internal/mtproto/handshake"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
	"github.com/neomsg/neomsg/backend/internal/mtproto/transport"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

const (
	CRCPing int32 = 0x7abe326e
	CRCPong int32 = 0x347773c5
)

type Connection struct {
	codec    *transport.Codec
	hs       *handshake.State
	authKey  *mtcrypto.AuthKey
	seqNo    int32
	session  int64
	serverID int64

	bridge  *bridge.Handler
	connMgr *connmgr.Manager
	redis   *redisstore.Store

	userID   int64
	deviceID string
	bound    bool
}

type ConnConfig struct {
	Bridge  *bridge.Handler
	ConnMgr *connmgr.Manager
	Redis   *redisstore.Store
}

func NewConnection(codec *transport.Codec, hs *handshake.State, cfg ConnConfig) *Connection {
	return &Connection{
		codec:    codec,
		hs:       hs,
		serverID: 1,
		bridge:   cfg.Bridge,
		connMgr:  cfg.ConnMgr,
		redis:    cfg.Redis,
	}
}

func (c *Connection) UserID() int64    { return c.userID }
func (c *Connection) DeviceID() string { return c.deviceID }

func (c *Connection) Serve() error {
	defer c.cleanup()
	for {
		packet, err := c.codec.ReadPacket()
		if err != nil {
			return err
		}
		resp, err := c.handlePacket(packet)
		if err != nil {
			log.Printf("[MTProto] handle error: %v", err)
			continue
		}
		if resp != nil {
			if err := c.codec.WritePacket(resp); err != nil {
				return err
			}
		}
	}
}

func (c *Connection) cleanup() {
	if c.bound && c.authKey != nil && c.connMgr != nil {
		c.connMgr.Unregister(c.userID, c.deviceID)
		_ = c.redis.UnregisterDeviceSession(context.Background(), c.userID, c.deviceID)
		_ = c.redis.UnbindMTProtoSession(context.Background(), c.authKey.ID)
	}
}

func (c *Connection) handlePacket(packet []byte) ([]byte, error) {
	if len(packet) < 8 {
		return nil, fmt.Errorf("packet too short")
	}
	authKeyID := int64(binary.LittleEndian.Uint64(packet[:8]))
	if authKeyID == 0 {
		resp, err := c.hs.HandleUnencrypted(packet)
		if err != nil {
			return nil, err
		}
		if c.hs.AuthKey.ID != 0 && c.authKey == nil {
			c.OnAuthComplete()
		}
		return resp, nil
	}
	if c.authKey == nil {
		return nil, fmt.Errorf("encrypted packet before auth")
	}
	if len(packet) < 24 {
		return nil, fmt.Errorf("encrypted packet too short")
	}
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	plain, err := mtcrypto.Decrypt(c.authKey.Value, msgKey, packet[24:])
	if err != nil {
		return nil, err
	}
	body, err := c.parseEncryptedBody(plain)
	if err != nil {
		return nil, err
	}
	return c.handleEncrypted(body)
}

func (c *Connection) parseEncryptedBody(plain []byte) ([]byte, error) {
	if len(plain) < 32 {
		return nil, fmt.Errorf("encrypted body too short")
	}
	_ = int64(binary.LittleEndian.Uint64(plain[0:8]))
	c.session = int64(binary.LittleEndian.Uint64(plain[8:16]))
	_ = int64(binary.LittleEndian.Uint64(plain[16:24]))
	_ = int32(binary.LittleEndian.Uint32(plain[24:28]))
	msgLen := int(binary.LittleEndian.Uint32(plain[28:32]))
	if len(plain) < 32+msgLen {
		return nil, fmt.Errorf("truncated encrypted message")
	}
	return plain[32 : 32+msgLen], nil
}

func (c *Connection) handleEncrypted(body []byte) ([]byte, error) {
	r := tl.NewReader(body)
	cid, err := r.ReadInt()
	if err != nil {
		return nil, err
	}

	switch cid {
	case CRCPing:
		pingID, err := r.ReadLong()
		if err != nil {
			return nil, err
		}
		return c.buildEncryptedPong(pingID)

	case bridge.CRCNeoMsgBindSession:
		return c.handleBindSession(body)

	case bridge.CRCNeoMsgInvokeWire:
		return c.handleInvokeWire(body)

	default:
		log.Printf("[MTProto] unhandled encrypted method %#x", cid)
		return nil, nil
	}
}

func (c *Connection) handleBindSession(body []byte) ([]byte, error) {
	userID, deviceID, token, err := bridge.ParseBindSession(body)
	if err != nil {
		return c.buildEncrypted(bridge.EncodeBindOk(false))
	}
	if userID == 0 || deviceID == "" || token == "" {
		return c.buildEncrypted(bridge.EncodeBindOk(false))
	}

	c.userID = userID
	c.deviceID = deviceID
	c.bound = true

	ctx := context.Background()
	if c.redis != nil {
		_ = c.redis.BindMTProtoSession(ctx, c.authKey.ID, userID, deviceID)
		_ = c.redis.RegisterDeviceSession(ctx, userID, redisstore.DeviceSession{
			DeviceID:  deviceID,
			SessionID: c.session,
			Platform:  "mtproto",
		})
		_ = c.redis.SetOnline(ctx, userID, deviceID)
	}
	if c.connMgr != nil {
		c.connMgr.Register(userID, deviceID, c)
	}

	log.Printf("[MTProto] session bound user=%d device=%s auth_key=%d", userID, deviceID, c.authKey.ID)
	return c.buildEncrypted(bridge.EncodeBindOk(true))
}

func (c *Connection) handleInvokeWire(body []byte) ([]byte, error) {
	if !c.bound {
		if c.authKey != nil && c.redis != nil {
			uid, did, err := c.redis.GetMTProtoSession(context.Background(), c.authKey.ID)
			if err == nil {
				c.userID, c.deviceID, c.bound = uid, did, true
				if c.connMgr != nil {
					c.connMgr.Register(uid, did, c)
				}
			}
		}
	}
	if !c.bound || c.bridge == nil {
		return nil, fmt.Errorf("session not bound")
	}

	payload, err := bridge.ParseInvokeWire(body)
	if err != nil {
		return nil, err
	}

	frames, err := c.bridge.HandleInvokeWire(context.Background(), c.userID, c.deviceID, payload)
	if err != nil {
		return nil, err
	}
	return c.buildEncrypted(bridge.EncodeWireResult(frames))
}

func (c *Connection) buildEncryptedPong(pingID int64) ([]byte, error) {
	w := tl.NewWriter()
	w.WriteInt(CRCPong)
	w.WriteLong(pingID)
	return c.buildEncrypted(w.Bytes())
}

func (c *Connection) buildEncrypted(body []byte) ([]byte, error) {
	return c.wrapEncrypted(body)
}

func (c *Connection) SendEncrypted(body []byte) error {
	enc, err := c.wrapEncrypted(body)
	if err != nil {
		return err
	}
	return c.codec.WritePacket(enc)
}

func (c *Connection) wrapEncrypted(body []byte) ([]byte, error) {
	if c.authKey == nil {
		c.authKey = &c.hs.AuthKey
	}
	padding := (16 - ((32 + len(body)) % 16)) % 16
	if padding < 12 {
		padding += 16
	}
	inner := make([]byte, 32+len(body)+padding)
	binary.LittleEndian.PutUint64(inner[0:8], 0)
	binary.LittleEndian.PutUint64(inner[8:16], uint64(c.session))
	msgID := time.Now().UnixNano() / int64(time.Millisecond)
	binary.LittleEndian.PutUint64(inner[16:24], uint64(msgID)<<32)
	c.seqNo++
	binary.LittleEndian.PutUint32(inner[24:28], uint32(c.seqNo))
	binary.LittleEndian.PutUint32(inner[28:32], uint32(len(body)))
	copy(inner[32:], body)
	if _, err := rand.Read(inner[32+len(body):]); err != nil {
		return nil, err
	}

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

func (c *Connection) OnAuthComplete() {
	c.authKey = &c.hs.AuthKey
	c.session = c.hs.SessionID
	if c.redis == nil || c.authKey == nil {
		return
	}
	uid, did, err := c.redis.GetMTProtoSession(context.Background(), c.authKey.ID)
	if err != nil {
		return
	}
	c.userID, c.deviceID, c.bound = uid, did, true
	if c.connMgr != nil {
		c.connMgr.Register(uid, did, c)
	}
}
