package bitreader

import (
	"io"
)

// Writer writes bits to an underlying byte stream.
type Writer struct {
	w    io.Writer
	buf  byte
	bits uint // number of bits written into buf so far (0-8)
}

// NewWriter creates a new Writer that writes to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteBit writes a single bit (0 or 1), MSB first.
func (bw *Writer) WriteBit(bit uint8) error {
	bw.buf = (bw.buf << 1) | (bit & 1)
	bw.bits++
	if bw.bits == 8 {
		return bw.flush()
	}
	return nil
}

// WriteBits writes n bits from val (MSB first).
func (bw *Writer) WriteBits(val uint32, n uint) error {
	for i := int(n) - 1; i >= 0; i-- {
		bit := uint8((val >> uint(i)) & 1)
		if err := bw.WriteBit(bit); err != nil {
			return err
		}
	}
	return nil
}

// WriteBitsLSB writes n bits from val in LSB-first order (used by VP8L).
func (bw *Writer) WriteBitsLSB(val uint32, n uint) error {
	for i := uint(0); i < n; i++ {
		bit := uint8((val >> i) & 1)
		if err := bw.WriteBit(bit); err != nil {
			return err
		}
	}
	return nil
}

// Flush writes any remaining bits (zero-padded) to the underlying writer.
func (bw *Writer) Flush() error {
	if bw.bits == 0 {
		return nil
	}
	// Pad remaining bits with zeros on the right.
	bw.buf <<= (8 - bw.bits)
	return bw.flush()
}

func (bw *Writer) flush() error {
	_, err := bw.w.Write([]byte{bw.buf})
	bw.buf = 0
	bw.bits = 0
	return err
}
