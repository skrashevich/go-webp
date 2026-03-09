// Package vp8l implements the VP8L (WebP lossless) codec.
package vp8l

import "errors"

// bitReader reads bits LSB-first from a byte slice.
type bitReader struct {
	data     []byte
	pos      int  // current byte position
	bitPos   uint // bit position within current byte (0-7)
	val      uint64
	nbits    uint
	bitsRead int // total bits consumed (for debugging)
}

func newBitReader(data []byte) *bitReader {
	br := &bitReader{data: data}
	br.fill()
	return br
}

func (br *bitReader) fill() {
	for br.nbits <= 56 && br.pos < len(br.data) {
		br.val |= uint64(br.data[br.pos]) << br.nbits
		br.nbits += 8
		br.pos++
	}
}

// readBits reads n bits (LSB-first) and returns them as uint32.
func (br *bitReader) readBits(n uint) (uint32, error) {
	if n == 0 {
		return 0, nil
	}
	if br.nbits < n {
		br.fill()
	}
	if br.nbits < n {
		return 0, errors.New("vp8l: unexpected end of bitstream")
	}
	val := uint32(br.val & ((1 << n) - 1))
	br.val >>= n
	br.nbits -= n
	br.bitsRead += int(n)
	return val, nil
}

// readBit reads a single bit.
func (br *bitReader) readBit() (bool, error) {
	v, err := br.readBits(1)
	return v != 0, err
}

// bitWriter writes bits LSB-first into a byte slice.
type bitWriter struct {
	data  []byte
	val   uint64
	nbits uint
}

func newBitWriter() *bitWriter {
	return &bitWriter{}
}

// writeBits writes n bits (LSB-first).
func (bw *bitWriter) writeBits(val uint32, n uint) {
	bw.val |= uint64(val) << bw.nbits
	bw.nbits += n
	for bw.nbits >= 8 {
		bw.data = append(bw.data, byte(bw.val))
		bw.val >>= 8
		bw.nbits -= 8
	}
}

// writeBit writes a single bit.
func (bw *bitWriter) writeBit(v bool) {
	if v {
		bw.writeBits(1, 1)
	} else {
		bw.writeBits(0, 1)
	}
}

// flush flushes any remaining bits (padding with zeros).
func (bw *bitWriter) flush() []byte {
	if bw.nbits > 0 {
		bw.data = append(bw.data, byte(bw.val))
		bw.val = 0
		bw.nbits = 0
	}
	return bw.data
}

// bytes returns the accumulated bytes without flushing.
func (bw *bitWriter) bytes() []byte {
	return bw.flush()
}
