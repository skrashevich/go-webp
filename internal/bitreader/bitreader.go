// Package bitreader provides utilities for reading individual bits and
// multi-bit values from an io.Reader.
package bitreader

import (
	"io"
)

// Reader reads bits from an underlying byte stream.
type Reader struct {
	r    io.Reader
	buf  byte
	bits uint // number of valid bits remaining in buf (0-8)
}

// NewReader creates a new Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// ReadBit reads a single bit. Returns the bit as 0 or 1.
func (br *Reader) ReadBit() (uint8, error) {
	if br.bits == 0 {
		b := [1]byte{}
		_, err := io.ReadFull(br.r, b[:])
		if err != nil {
			return 0, err
		}
		br.buf = b[0]
		br.bits = 8
	}
	bit := (br.buf >> 7) & 1
	br.buf <<= 1
	br.bits--
	return bit, nil
}

// ReadBits reads n bits (n <= 32) and returns them as a uint32, MSB first.
func (br *Reader) ReadBits(n uint) (uint32, error) {
	var result uint32
	for i := uint(0); i < n; i++ {
		bit, err := br.ReadBit()
		if err != nil {
			return 0, err
		}
		result = (result << 1) | uint32(bit)
	}
	return result, nil
}

// ReadBitsLSB reads n bits (n <= 32) in LSB-first order (used by VP8L).
func (br *Reader) ReadBitsLSB(n uint) (uint32, error) {
	var result uint32
	for i := uint(0); i < n; i++ {
		bit, err := br.ReadBit()
		if err != nil {
			return 0, err
		}
		result |= uint32(bit) << i
	}
	return result, nil
}

// ReadBool reads a single bit as a boolean.
func (br *Reader) ReadBool() (bool, error) {
	bit, err := br.ReadBit()
	return bit != 0, err
}

// Align discards remaining bits in the current byte to align to the next byte boundary.
func (br *Reader) Align() {
	br.bits = 0
	br.buf = 0
}
