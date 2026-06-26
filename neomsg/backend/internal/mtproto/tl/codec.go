// Package tl implements MTProto TL serialization primitives.
package tl

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var ErrUnderflow = errors.New("tl: buffer underflow")

type Reader struct {
	data []byte
	pos  int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) Remaining() int { return len(r.data) - r.pos }

func (r *Reader) ReadInt() (int32, error) {
	if r.Remaining() < 4 {
		return 0, ErrUnderflow
	}
	v := int32(binary.LittleEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v, nil
}

func (r *Reader) ReadLong() (int64, error) {
	if r.Remaining() < 8 {
		return 0, ErrUnderflow
	}
	v := int64(binary.LittleEndian.Uint64(r.data[r.pos:]))
	r.pos += 8
	return v, nil
}

func (r *Reader) ReadInt128() ([16]byte, error) {
	var out [16]byte
	if r.Remaining() < 16 {
		return out, ErrUnderflow
	}
	copy(out[:], r.data[r.pos:r.pos+16])
	r.pos += 16
	return out, nil
}

func (r *Reader) ReadBytes() ([]byte, error) {
	if r.Remaining() < 1 {
		return nil, ErrUnderflow
	}
	first := int(r.data[r.pos])
	r.pos++
	var length int
	if first < 254 {
		length = first
	} else {
		if r.Remaining() < 3 {
			return nil, ErrUnderflow
		}
		length = int(r.data[r.pos]) | int(r.data[r.pos+1])<<8 | int(r.data[r.pos+2])<<16
		r.pos += 3
	}
	padding := (4 - ((length + 1) % 4)) % 4
	if first >= 254 {
		padding = (4 - ((length + 4) % 4)) % 4
	}
	if r.Remaining() < length+padding {
		return nil, ErrUnderflow
	}
	out := make([]byte, length)
	copy(out, r.data[r.pos:r.pos+length])
	r.pos += length + padding
	return out, nil
}

func (r *Reader) ReadString() (string, error) {
	b, err := r.ReadBytes()
	return string(b), err
}

func (r *Reader) ReadVectorLong() ([]int64, error) {
	cid, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	if cid != 0x1cb5c415 {
		return nil, fmt.Errorf("tl: expected vector, got %#x", cid)
	}
	count, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	out := make([]int64, count)
	for i := 0; i < int(count); i++ {
		out[i], err = r.ReadLong()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

type Writer struct {
	buf []byte
}

func NewWriter() *Writer { return &Writer{} }

func (w *Writer) WriteInt(v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteLong(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	w.buf = append(w.buf, b[:]...)
}

func (w *Writer) WriteInt128(v [16]byte) {
	w.buf = append(w.buf, v[:]...)
}

func (w *Writer) WriteBytes(v []byte) {
	n := len(v)
	if n < 254 {
		w.buf = append(w.buf, byte(n))
	} else {
		w.buf = append(w.buf, 254)
		w.buf = append(w.buf, byte(n), byte(n>>8), byte(n>>16))
	}
	w.buf = append(w.buf, v...)
	padding := (4 - (len(w.buf) % 4)) % 4
	for i := 0; i < padding; i++ {
		w.buf = append(w.buf, 0)
	}
}

func (w *Writer) WriteString(s string) { w.WriteBytes([]byte(s)) }

func (w *Writer) WriteVectorLong(v []int64) {
	w.WriteInt(0x1cb5c415)
	w.WriteInt(int32(len(v)))
	for _, item := range v {
		w.WriteLong(item)
	}
}

func (w *Writer) Bytes() []byte { return w.buf }
