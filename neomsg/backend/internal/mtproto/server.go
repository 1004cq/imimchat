package mtproto

import (
	"context"
	"io"
	"log"
	"net"
	"sync"

	"github.com/neomsg/neomsg/backend/internal/auth"
	mtcrypto "github.com/neomsg/neomsg/backend/internal/mtproto/crypto"
	"github.com/neomsg/neomsg/backend/internal/mtproto/bridge"
	"github.com/neomsg/neomsg/backend/internal/mtproto/connmgr"
	"github.com/neomsg/neomsg/backend/internal/mtproto/handshake"
	"github.com/neomsg/neomsg/backend/internal/mtproto/transport"
	redisstore "github.com/neomsg/neomsg/backend/internal/store/redis"
)

type Server struct {
	addr    string
	rsaKey  *mtcrypto.RSAKeyPair
	bridge  *bridge.Handler
	connMgr *connmgr.Manager
	redis   *redisstore.Store
	auth    *auth.Service
	mu      sync.Mutex
	conns   int
}

type ServerConfig struct {
	Addr       string
	RSAKeyPath string
	Bridge     *bridge.Handler
	ConnMgr    *connmgr.Manager
	Redis      *redisstore.Store
	Auth       *auth.Service
}

func NewServer(cfg ServerConfig) (*Server, error) {
	rsaKey, err := mtcrypto.LoadOrGenerateRSAKey(cfg.RSAKeyPath)
	if err != nil {
		return nil, err
	}
	return &Server{
		addr:    cfg.Addr,
		rsaKey:  rsaKey,
		bridge:  cfg.Bridge,
		connMgr: cfg.ConnMgr,
		redis:   cfg.Redis,
		auth:    cfg.Auth,
	}, nil
}

func (s *Server) RSAFingerprint() int64 { return s.rsaKey.Fingerprint() }

func (s *Server) RSAPublicPEM() ([]byte, error) { return s.rsaKey.PublicPEM() }

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	log.Printf("[MTProto] listening on %s (RSA fingerprint=%d)", s.addr, s.rsaKey.Fingerprint())

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("[MTProto] accept: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(raw net.Conn) {
	defer raw.Close()
	s.mu.Lock()
	s.conns++
	id := s.conns
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conns--
		s.mu.Unlock()
	}()

	log.Printf("[MTProto] connection #%d from %s", id, raw.RemoteAddr())

	mode, rw, err := negotiateTransport(raw)
	if err != nil {
		log.Printf("[MTProto] transport negotiate: %v", err)
		return
	}

	codec := transport.NewCodec(rw, mode)
	hs := handshake.NewState(s.rsaKey)
	conn := NewConnection(codec, hs, ConnConfig{
		Bridge:  s.bridge,
		ConnMgr: s.connMgr,
		Redis:   s.redis,
		Auth:    s.auth,
	})

	if err := conn.Serve(); err != nil && err != io.EOF {
		log.Printf("[MTProto] connection #%d closed: %v", id, err)
	}
}

func negotiateTransport(raw net.Conn) (transport.Mode, io.ReadWriter, error) {
	var marker [1]byte
	if _, err := io.ReadFull(raw, marker[:]); err != nil {
		return 0, nil, err
	}
	mode, err := transport.DetectMode(marker[0])
	if err != nil {
		return 0, nil, err
	}
	if marker[0] != transport.AbridgedMarker && marker[0] != transport.IntermediateMarker {
		mode = transport.ModeIntermediate
	}
	return mode, &prefixedConn{Conn: raw, first: marker[0], used: marker[0] == transport.IntermediateMarker}, nil
}

type prefixedConn struct {
	net.Conn
	first byte
	used  bool
}

func (c *prefixedConn) Read(p []byte) (int, error) {
	if !c.used {
		c.used = true
		if len(p) == 0 {
			return 0, nil
		}
		p[0] = c.first
		if len(p) == 1 {
			return 1, nil
		}
		n, err := c.Conn.Read(p[1:])
		return n + 1, err
	}
	return c.Conn.Read(p)
}
