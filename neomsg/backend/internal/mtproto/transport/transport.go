// Package transport implements MTProto 2.0 transport codecs.
package transport

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	AbridgedMarker     byte = 0xef
	IntermediateMarker byte = 0xee
)

type Mode int

const (
	ModeAbridged Mode = iota
	ModeIntermediate
)

type Codec struct {
	mode Mode
	rw   io.ReadWriter
}

func NewCodec(rw io.ReadWriter, mode Mode) *Codec {
	return &Codec{mode: mode, rw: rw}
}

func DetectMode(first byte) (Mode, error) {
	switch first {
	case AbridgedMarker:
		return ModeAbridged, nil
	case IntermediateMarker:
		return ModeIntermediate, nil
	default:
		return ModeIntermediate, nil
	}
}

func (c *Codec) WritePacket(payload []byte) error {
	switch c.mode {
	case ModeAbridged:
		return writeAbridged(c.rw, payload)
	default:
		return writeIntermediate(c.rw, payload)
	}
}

func (c *Codec) ReadPacket() ([]byte, error) {
	switch c.mode {
	case ModeAbridged:
		return readAbridged(c.rw)
	default:
		return readIntermediate(c.rw)
	}
}

func writeIntermediate(w io.Writer, payload []byte) error {
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readIntermediate(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 || size > 16*1024*1024 {
		return nil, fmt.Errorf("transport: invalid packet size %d", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeAbridged(w io.Writer, payload []byte) error {
	size := len(payload)
	if size%4 != 0 {
		return fmt.Errorf("transport: abridged payload must be 4-byte aligned")
	}
	quads := size / 4
	switch {
	case quads <= 0x7e:
		if _, err := w.Write([]byte{byte(quads)}); err != nil {
			return err
		}
	case quads <= 0x3fffff:
		var b [4]byte
		b[0] = 0x7f
		binary.LittleEndian.PutUint32(b[:], uint32(quads))
		if _, err := w.Write(b[:4]); err != nil {
			return err
		}
	default:
		return fmt.Errorf("transport: packet too large")
	}
	_, err := w.Write(payload)
	return err
}

func readAbridged(r io.Reader) ([]byte, error) {
	var first [1]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return nil, err
	}
	var quads int
	if first[0] < 0x7f {
		quads = int(first[0])
	} else {
		var rest [3]byte
		if _, err := io.ReadFull(r, rest[:]); err != nil {
			return nil, err
		}
		quads = int(rest[0]) | int(rest[1])<<8 | int(rest[2])<<16
	}
	size := quads * 4
	if size == 0 || size > 16*1024*1024 {
		return nil, fmt.Errorf("transport: invalid abridged size %d", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
