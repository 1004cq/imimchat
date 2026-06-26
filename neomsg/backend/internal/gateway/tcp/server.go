package tcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/neomsg/neomsg/backend/internal/gateway/session"
	"github.com/neomsg/neomsg/backend/internal/message"
)

// 帧格式：[4字节长度][Protobuf payload]
const maxFrameSize = 1 << 20 // 1MB

type Server struct {
	addr     string
	sessions *session.Manager
	msgSvc   *message.Service
}

func NewServer(addr string, sessions *session.Manager, msgSvc *message.Service) *Server {
	return &Server{addr: addr, sessions: sessions, msgSvc: msgSvc}
}

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

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("[TCP] accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(raw net.Conn) {
	defer raw.Close()
	log.Printf("[TCP] new connection from %s", raw.RemoteAddr())

	for {
		var length uint32
		if err := binary.Read(raw, binary.BigEndian, &length); err != nil {
			return
		}
		if length > maxFrameSize {
			return
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(raw, buf); err != nil {
			return
		}
		// TODO: 解码并处理 Envelope
		_ = buf
	}
}

type tcpConn struct {
	conn     net.Conn
	userID   int64
	deviceID string
}

func (c *tcpConn) Send(data []byte) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(data)
	return err
}

func (c *tcpConn) Close() error          { return c.conn.Close() }
func (c *tcpConn) UserID() int64         { return c.userID }
func (c *tcpConn) DeviceID() string      { return c.deviceID }

func (c *tcpConn) String() string {
	return fmt.Sprintf("tcp:%d:%s", c.userID, c.deviceID)
}
