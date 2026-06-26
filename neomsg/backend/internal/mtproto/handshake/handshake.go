package handshake

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	mtcrypto "github.com/neomsg/neomsg/backend/internal/mtproto/crypto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/tl"
)

const (
	CRCReqPQMulti         int32 = -0x32221523 // 0xcdd42ee4
	CRCResPQ              int32 = 0x05162463
	CRCPQInnerData        int32 = -0x7c3f3d14 // 0x83c95aec
	CRCReqDHParams        int32 = -0x28ed8a4a // 0xd712e4be
	CRCServerDHParamsOK   int32 = -0x2f17f5a4 // 0xd0e8075c
	CRCServerDHInnerData  int32 = -0x4a3f2bac // 0xb5890dba
	CRCSetClientDHParams  int32 = -0x0afb6c4e // 0xf5045f1f
	CRCClientDHInnerData  int32 = -0x5998ff28 // 0x6643b654
	CRCDHGenOK            int32 = 0x3bcbf734
	CRCDHGenRetry         int32 = 0x46dc1fb9
	CRCDHGenFail          int32 = -0x595627e6 // 0xa69dae02
)

type State struct {
	RSA           *mtcrypto.RSAKeyPair
	ClientNonce   [16]byte
	ServerNonce   [16]byte
	NewNonce      [16]byte
	P             int32
	Q             int32
	GA            [256]byte
	GB            [256]byte
	ServerDHNonce [16]byte
	AuthKey       mtcrypto.AuthKey
	SessionID     int64
}

func NewState(rsaKey *mtcrypto.RSAKeyPair) *State {
	return &State{RSA: rsaKey}
}

func (s *State) HandleUnencrypted(payload []byte) ([]byte, error) {
	if len(payload) < 20 {
		return nil, fmt.Errorf("handshake: packet too short")
	}
	authKeyID := int64(binary.LittleEndian.Uint64(payload[0:8]))
	if authKeyID != 0 {
		return nil, fmt.Errorf("handshake: expected unencrypted packet")
	}
	_ = int64(binary.LittleEndian.Uint64(payload[8:16])) // message_id
	msgLen := int32(binary.LittleEndian.Uint32(payload[16:20]))
	if len(payload) < 20+int(msgLen) {
		return nil, fmt.Errorf("handshake: truncated message")
	}
	body := payload[20 : 20+msgLen]

	r := tl.NewReader(body)
	cid, err := r.ReadInt()
	if err != nil {
		return nil, err
	}

	switch cid {
	case CRCReqPQMulti:
		return s.onReqPQ(r)
	case CRCReqDHParams:
		return s.onReqDHParams(r)
	case CRCSetClientDHParams:
		return s.onSetClientDHParams(r)
	default:
		return nil, fmt.Errorf("handshake: unknown constructor %#x", cid)
	}
}

func (s *State) onReqPQ(r *tl.Reader) ([]byte, error) {
	nonce, err := r.ReadInt128()
	if err != nil {
		return nil, err
	}
	s.ClientNonce = nonce
	serverNonce, err := mtcrypto.RandomInt128()
	if err != nil {
		return nil, err
	}
	s.ServerNonce = serverNonce

	pq := int64(0x17ED48941A08F981) // demo PQ, factors quickly
	s.P, s.Q = mtcrypto.FactorPQ(pq)

	w := tl.NewWriter()
	w.WriteInt(CRCResPQ)
	w.WriteInt128(s.ClientNonce)
	w.WriteInt128(s.ServerNonce)
	w.WriteString(fmt.Sprintf("%x", pq))
	w.WriteVectorLong([]int64{s.RSA.Fingerprint()})
	return wrapUnencrypted(w.Bytes())
}

func (s *State) onReqDHParams(r *tl.Reader) ([]byte, error) {
	nonce, err := r.ReadInt128()
	if err != nil {
		return nil, err
	}
	serverNonce, err := r.ReadInt128()
	if err != nil {
		return nil, err
	}
	if nonce != s.ClientNonce || serverNonce != s.ServerNonce {
		return nil, fmt.Errorf("handshake: nonce mismatch")
	}
	p, err := r.ReadString()
	if err != nil {
		return nil, err
	}
	q, err := r.ReadString()
	if err != nil {
		return nil, err
	}
	_ = p
	_ = q
	publicKeyFP, err := r.ReadLong()
	if err != nil {
		return nil, err
	}
	if publicKeyFP != s.RSA.Fingerprint() {
		return nil, fmt.Errorf("handshake: unknown rsa fingerprint")
	}
	encrypted, err := r.ReadString()
	if err != nil {
		return nil, err
	}
	inner, err := s.RSA.DecryptPQInnerData([]byte(encrypted))
	if err != nil {
		return nil, fmt.Errorf("handshake: decrypt pq inner: %w", err)
	}
	ir := tl.NewReader(inner)
	innerCID, err := ir.ReadInt()
	if err != nil {
		return nil, err
	}
	if innerCID != CRCPQInnerData {
		return nil, fmt.Errorf("handshake: expected p_q_inner_data")
	}
	if _, err = ir.ReadInt128(); err != nil {
		return nil, err
	}
	if _, err = ir.ReadInt128(); err != nil {
		return nil, err
	}
	pqStr, err := ir.ReadString()
	if err != nil {
		return nil, err
	}
	_ = pqStr
	pVal, err := ir.ReadString()
	if err != nil {
		return nil, err
	}
	qVal, err := ir.ReadString()
	if err != nil {
		return nil, err
	}
	_ = pVal
	_ = qVal
	newNonce, err := ir.ReadInt128()
	if err != nil {
		return nil, err
	}
	s.NewNonce = newNonce

	gA, err := mtcrypto.RandomInt128()
	if err != nil {
		return nil, err
	}
	var gaBytes [256]byte
	copy(gaBytes[:], gA[:])
	gBBytes, _, err := mtcrypto.NewServerDHInner(gaBytes, s.ClientNonce, s.ServerNonce)
	if err != nil {
		return nil, err
	}
	s.GA = gaBytes
	s.GB = gBBytes

	innerW := tl.NewWriter()
	innerW.WriteInt(CRCServerDHInnerData)
	innerW.WriteInt128(s.ClientNonce)
	innerW.WriteInt128(s.ServerNonce)
	innerW.WriteInt(3)
	innerW.WriteBytes(mtcrypto.DHPrime.Bytes())
	innerW.WriteBytes(gBBytes[:])
	serverDHNonce, err := mtcrypto.RandomInt128()
	if err != nil {
		return nil, err
	}
	s.ServerDHNonce = serverDHNonce
	innerW.WriteInt128(serverDHNonce)

	encryptedAnswer, err := s.RSA.EncryptPQInnerData(innerW.Bytes())
	if err != nil {
		return nil, err
	}

	w := tl.NewWriter()
	w.WriteInt(CRCServerDHParamsOK)
	w.WriteInt128(s.ClientNonce)
	w.WriteInt128(s.ServerNonce)
	w.WriteString(string(encryptedAnswer))
	return wrapUnencrypted(w.Bytes())
}

func (s *State) onSetClientDHParams(r *tl.Reader) ([]byte, error) {
	nonce, err := r.ReadInt128()
	if err != nil {
		return nil, err
	}
	serverNonce, err := r.ReadInt128()
	if err != nil {
		return nil, err
	}
	if nonce != s.ClientNonce || serverNonce != s.ServerNonce {
		return nil, fmt.Errorf("handshake: nonce mismatch")
	}
	encrypted, err := r.ReadString()
	if err != nil {
		return nil, err
	}
	inner, err := s.RSA.DecryptPQInnerData([]byte(encrypted))
	if err != nil {
		return nil, fmt.Errorf("handshake: decrypt client dh: %w", err)
	}
	ir := tl.NewReader(inner)
	innerCID, err := ir.ReadInt()
	if err != nil {
		return nil, err
	}
	if innerCID != CRCClientDHInnerData {
		return nil, fmt.Errorf("handshake: expected client_DH_inner_data")
	}
	if _, err = ir.ReadInt128(); err != nil {
		return nil, err
	}
	if _, err = ir.ReadInt128(); err != nil {
		return nil, err
	}
	retryID, err := ir.ReadLong()
	if err != nil {
		return nil, err
	}
	_ = retryID
	gBClient, err := ir.ReadString()
	if err != nil {
		return nil, err
	}

	authKeyBytes := mtcrypto.ComputeAuthKey(s.GA[:], []byte(gBClient), s.ClientNonce, s.ServerNonce)
	s.AuthKey.Value = authKeyBytes
	s.AuthKey.ComputeID()
	sid, err := mtcrypto.RandomInt64()
	if err != nil {
		return nil, err
	}
	s.SessionID = sid

	w := tl.NewWriter()
	w.WriteInt(CRCDHGenOK)
	w.WriteInt128(s.ClientNonce)
	w.WriteInt128(s.ServerNonce)
	w.WriteString(string(authKeyBytes[:128]))
	w.WriteLong(s.AuthKey.ID)
	return wrapUnencrypted(w.Bytes())
}

func wrapUnencrypted(body []byte) ([]byte, error) {
	padding := (4 - ((len(body) + 20) % 4)) % 4
	if padding < 12 {
		padding += 16
	}
	buf := make([]byte, 20+len(body)+padding)
	binary.LittleEndian.PutUint64(buf[0:8], 0)
	msgID := time.Now().UnixNano() / int64(time.Millisecond)
	binary.LittleEndian.PutUint64(buf[8:16], uint64(msgID)<<32)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(len(body)))
	copy(buf[20:], body)
	if _, err := rand.Read(buf[20+len(body):]); err != nil {
		return nil, err
	}
	return buf, nil
}
