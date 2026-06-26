package mtproto

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	mtcrypto "github.com/neomsg/neomsg/backend/internal/mtproto/crypto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/handshake"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
	"github.com/neomsg/neomsg/backend/internal/mtproto/transport"
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
}

func NewConnection(codec *transport.Codec, hs *handshake.State) *Connection {
	return &Connection{codec: codec, hs: hs, serverID: 1}
}

func (c *Connection) Serve() error {
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
	_ = int64(binary.LittleEndian.Uint64(plain[0:8]))   // salt
	c.session = int64(binary.LittleEndian.Uint64(plain[8:16]))
	_ = int64(binary.LittleEndian.Uint64(plain[16:24])) // message_id
	_ = int32(binary.LittleEndian.Uint32(plain[24:28])) // seq_no
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
	default:
		log.Printf("[MTProto] unhandled encrypted method %#x (stub)", cid)
		return nil, nil
	}
}

func (c *Connection) buildEncryptedPong(pingID int64) ([]byte, error) {
	if c.authKey == nil {
		c.authKey = &c.hs.AuthKey
	}
	w := tl.NewWriter()
	w.WriteInt(CRCPong)
	w.WriteLong(pingID)
	return c.wrapEncrypted(w.Bytes())
}

func (c *Connection) wrapEncrypted(body []byte) ([]byte, error) {
	padding := (16 - ((32 + len(body)) % 16)) % 16
	if padding < 12 {
		padding += 16
	}
	inner := make([]byte, 32+len(body)+padding)
	binary.LittleEndian.PutUint64(inner[0:8], 0) // salt
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

// OnAuthComplete should be called after dh_gen_ok to enable encrypted mode.
func (c *Connection) OnAuthComplete() {
	c.authKey = &c.hs.AuthKey
	c.session = c.hs.SessionID
}
