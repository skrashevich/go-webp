package vp8

import "io"

// boolEncoder implements the VP8 arithmetic (boolean) encoder.
// Direct port of libvpx vp8_encode_bool / vp8_stop_encode.
type boolEncoder struct {
	w      io.Writer
	low    uint32
	range_ uint32
	count  int // starts at -24
	buf    []byte
}

// vp8Norm is the number of leading zeros lookup table (matches libvpx vp8_norm).
var vp8Norm = [256]int{
	0, 7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

func newBoolEncoder(w io.Writer) *boolEncoder {
	return &boolEncoder{
		w:      w,
		range_: 255,
		count:  -24,
	}
}

// writeBool encodes one boolean value with the given probability of false.
// Direct port of libvpx vp8_encode_bool.
func (e *boolEncoder) writeBool(prob uint8, val bool) {
	split := 1 + (((e.range_ - 1) * uint32(prob)) >> 8)

	if val {
		e.low += split
		e.range_ -= split
	} else {
		e.range_ = split
	}

	shift := vp8Norm[e.range_]
	e.range_ <<= uint(shift)
	e.count += shift

	if e.count >= 0 {
		offset := shift - e.count

		if (e.low << uint(offset-1)) & 0x80000000 != 0 {
			// Carry propagation.
			for i := len(e.buf) - 1; i >= 0; i-- {
				if e.buf[i] == 0xff {
					e.buf[i] = 0
				} else {
					e.buf[i]++
					break
				}
			}
		}

		e.buf = append(e.buf, byte(e.low>>uint(24-offset)))
		e.low <<= uint(offset)
		e.low &= 0xffffff
		shift = e.count
		e.count -= 8
	}

	e.low <<= uint(shift)
}

// writeLiteral encodes an n-bit value with uniform probability (MSB first).
func (e *boolEncoder) writeLiteral(n int, val uint32) {
	for n > 0 {
		n--
		e.writeBool(128, (val>>uint(n))&1 == 1)
	}
}

// writeInt encodes a signed integer in [-m, m] range (VP8 signed literal).
func (e *boolEncoder) writeInt(n int, val int) {
	uval := uint32(val)
	if val < 0 {
		uval = uint32(-val)
	}
	e.writeLiteral(n, uval)
	if val != 0 {
		e.writeBool(128, val < 0)
	}
}

// flush finalises the stream and writes remaining bytes.
// Port of libvpx vp8_stop_encode.
func (e *boolEncoder) flush() error {
	for i := 0; i < 32; i++ {
		e.writeBool(128, false)
	}
	if _, err := e.w.Write(e.buf); err != nil {
		return err
	}
	e.buf = e.buf[:0]
	return nil
}
