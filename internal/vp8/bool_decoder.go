package vp8

import "errors"

// Lookup tables matching golang.org/x/image/vp8 and libwebp.
// lutShift[i] gives the number of left-shifts needed to renormalize (rangeM1+1)
// back into [128, 255]. Index range: 0..126 (rangeM1 values below 127).
var lutShift = [127]uint8{
	7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

// lutRangeM1[i] gives the new (rangeM1) after shifting. Equivalent to
// ((i+1) << lutShift[i]) - 1.
var lutRangeM1 = [127]uint8{
	127,
	127, 191,
	127, 159, 191, 223,
	127, 143, 159, 175, 191, 207, 223, 239,
	127, 135, 143, 151, 159, 167, 175, 183, 191, 199, 207, 215, 223, 231, 239, 247,
	127, 131, 135, 139, 143, 147, 151, 155, 159, 163, 167, 171, 175, 179, 183, 187,
	191, 195, 199, 203, 207, 211, 215, 219, 223, 227, 231, 235, 239, 243, 247, 251,
	127, 129, 131, 133, 135, 137, 139, 141, 143, 145, 147, 149, 151, 153, 155, 157,
	159, 161, 163, 165, 167, 169, 171, 173, 175, 177, 179, 181, 183, 185, 187, 189,
	191, 193, 195, 197, 199, 201, 203, 205, 207, 209, 211, 213, 215, 217, 219, 221,
	223, 225, 227, 229, 231, 233, 235, 237, 239, 241, 243, 245, 247, 249, 251, 253,
}

// boolDecoder implements the VP8 arithmetic (boolean) decoder.
// This implementation matches golang.org/x/image/vp8 (partition.go).
type boolDecoder struct {
	data    []byte
	pos     int
	rangeM1 uint32 // range minus 1, kept in [127, 254]
	bits    uint32 // shift register holding unconsumed coded bits
	nBits   uint8  // number of valid bits in the shift register
}

// newBoolDecoder creates a decoder reading from data starting at offset pos.
func newBoolDecoder(data []byte, pos int) (*boolDecoder, error) {
	if pos >= len(data) {
		return nil, errors.New("vp8: bool decoder: not enough data")
	}
	return &boolDecoder{
		data:    data,
		pos:     pos,
		rangeM1: 254,
	}, nil
}

// ReadBool reads one boolean with the given probability of being false (0..255).
func (bd *boolDecoder) ReadBool(prob uint8) bool {
	// Ensure at least 8 bits are available in the shift register.
	if bd.nBits < 8 {
		if bd.pos < len(bd.data) {
			bd.bits |= uint32(bd.data[bd.pos]) << (8 - bd.nBits)
			bd.pos++
			bd.nBits += 8
		}
	}

	split := (bd.rangeM1*uint32(prob))>>8 + 1
	bigsplit := split << 8

	bit := bd.bits >= bigsplit
	if bit {
		bd.rangeM1 -= split
		bd.bits -= bigsplit
	} else {
		bd.rangeM1 = split - 1
	}

	// Renormalize using lookup table.
	if bd.rangeM1 < 127 {
		shift := lutShift[bd.rangeM1]
		bd.rangeM1 = uint32(lutRangeM1[bd.rangeM1])
		bd.bits <<= uint(shift)
		bd.nBits -= shift
	}
	return bit
}

// ReadLiteral reads n bits as an unsigned integer (MSB first).
func (bd *boolDecoder) ReadLiteral(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v <<= 1
		if bd.ReadBool(128) {
			v |= 1
		}
	}
	return v
}

// ReadSignedLiteral reads n bits plus a sign bit.
func (bd *boolDecoder) ReadSignedLiteral(n int) int32 {
	v := int32(bd.ReadLiteral(n))
	if bd.ReadBool(128) {
		return -v
	}
	return v
}

// ReadFlag reads a single bit (probability 128 = 50/50).
func (bd *boolDecoder) ReadFlag() bool {
	return bd.ReadBool(128)
}
