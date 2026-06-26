package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"os"
)

type RSAKeyPair struct {
	Private *rsa.PrivateKey
	Public  *rsa.PublicKey
}

func LoadOrGenerateRSAKey(path string) (*RSAKeyPair, error) {
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			block, _ := pem.Decode(data)
			if block != nil {
				key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
				if err == nil {
					return &RSAKeyPair{Private: key, Public: &key.PublicKey}, nil
				}
			}
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &RSAKeyPair{Private: key, Public: &key.PublicKey}, nil
}

func (k *RSAKeyPair) PublicPEM() ([]byte, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(k.Public)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), nil
}

func (k *RSAKeyPair) Fingerprint() int64 {
	pubDER, err := x509.MarshalPKIXPublicKey(k.Public)
	if err != nil {
		return 0
	}
	sum := sha1.Sum(pubDER)
	return int64(binary.LittleEndian.Uint64(sum[len(sum)-8:]))
}

// EncryptPQInnerData encrypts p_q_inner_data for server_DH_params_ok.
func (k *RSAKeyPair) EncryptPQInnerData(data []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, k.Public, padRSA(data))
}

// DecryptPQInnerData decrypts req_DH_params encrypted payload.
func (k *RSAKeyPair) DecryptPQInnerData(data []byte) ([]byte, error) {
	out, err := rsa.DecryptPKCS1v15(rand.Reader, k.Private, data)
	if err != nil {
		return nil, err
	}
	return unpadRSA(out)
}

func padRSA(data []byte) []byte {
	if len(data) > 245 {
		panic("rsa payload too large")
	}
	out := make([]byte, 256)
	copy(out[256-len(data):], data)
	return out
}

func unpadRSA(data []byte) ([]byte, error) {
	for i, b := range data {
		if b != 0 {
			return data[i:], nil
		}
	}
	return nil, fmt.Errorf("rsa: empty payload")
}
