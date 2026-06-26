package crypto

import (
	"crypto/aes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/gotd/ige"
)

type Side int

const (
	Client Side = iota
	Server
)

// AuthKey is a 256-byte MTProto authorization key.
type AuthKey struct {
	ID    int64
	Value [256]byte
}

func (k *AuthKey) ComputeID() {
	hash := sha1.Sum(k.Value[:])
	k.ID = int64(binary.LittleEndian.Uint64(hash[len(hash)-8:]))
}

// Keys derives AES key and IV for MTProto 2.0 IGE mode.
func Keys(authKey [256]byte, msgKey [16]byte, side Side) (key, iv [32]byte) {
	x := 0
	if side == Client {
		x = 8
	}
	a := sha256.Sum256(append(msgKey[:], authKey[x:x+36]...))
	b := sha256.Sum256(append(authKey[40+x:40+x+36], msgKey[:]...))
	copy(key[:], a[:])
	copy(key[16:], b[:16])
	copy(iv[:], b[16:])
	copy(iv[16:], a[8:24])
	return key, iv
}

func Encrypt(authKey [256]byte, data []byte) ([]byte, [16]byte, error) {
	if len(data)%16 != 0 {
		return nil, [16]byte{}, fmt.Errorf("crypto: plaintext must be 16-byte aligned")
	}
	msgKey := MessageKey(authKey, data, Client)
	key, iv := Keys(authKey, msgKey, Client)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, [16]byte{}, err
	}
	out := make([]byte, len(data))
	ige.NewIGEEncrypter(block, iv[:]).CryptBlocks(out, data)
	return out, msgKey, nil
}

func Decrypt(authKey [256]byte, msgKey [16]byte, data []byte) ([]byte, error) {
	key, iv := Keys(authKey, msgKey, Server)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	ige.NewIGEDecrypter(block, iv[:]).CryptBlocks(out, data)
	return out, nil
}

func MessageKey(authKey [256]byte, data []byte, side Side) [16]byte {
	x := 0
	if side == Client {
		x = 8
	}
	sum := sha256.Sum256(append(authKey[88+x:88+x+32], data...))
	var out [16]byte
	copy(out[:], sum[8:24])
	return out
}
