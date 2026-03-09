package vp8

// fdct4x4 computes the forward 4x4 DCT of src (row-major, 16 elements).
// Output coefficients are stored in dst.
// Algorithm follows the VP8 spec (integer approximation).
func fdct4x4(src *[16]int16, dst *[16]int16) {
	var tmp [16]int32

	// Row transform (matching libvpx vp8_short_fdct4x4_c).
	// Input values are scaled by 8 to maintain precision through the transform.
	for i := 0; i < 4; i++ {
		a := (int32(src[i*4+0]) + int32(src[i*4+3])) * 8
		b := (int32(src[i*4+1]) + int32(src[i*4+2])) * 8
		c := (int32(src[i*4+1]) - int32(src[i*4+2])) * 8
		d := (int32(src[i*4+0]) - int32(src[i*4+3])) * 8

		tmp[i*4+0] = a + b
		tmp[i*4+1] = (c*2217 + d*5352 + 14500) >> 12
		tmp[i*4+2] = a - b
		tmp[i*4+3] = (d*2217 - c*5352 + 7500) >> 12
	}

	// Column transform (DCT butterfly: [0]+[3], [1]+[2]).
	for j := 0; j < 4; j++ {
		a := tmp[0*4+j] + tmp[3*4+j]
		b := tmp[1*4+j] + tmp[2*4+j]
		c := tmp[1*4+j] - tmp[2*4+j]
		d := tmp[0*4+j] - tmp[3*4+j]

		dst[0*4+j] = int16((a + b + 7) >> 4)
		dst[2*4+j] = int16((a - b + 7) >> 4)

		e := (c*2217 + d*5352 + 12000) >> 16
		if d != 0 {
			e++
		}
		dst[1*4+j] = int16(e)
		dst[3*4+j] = int16((d*2217 - c*5352 + 51000) >> 16)
	}
}

// fwht4x4 computes the forward 4x4 Walsh-Hadamard Transform (used for DC coefficients).
// Butterfly uses [0,3]/[1,2] pairing to match the reference decoder's inverseWHT16.
func fwht4x4(src *[16]int16, dst *[16]int16) {
	var tmp [16]int32

	// Row pass.
	for i := 0; i < 4; i++ {
		a := int32(src[i*4+0]) + int32(src[i*4+3])
		b := int32(src[i*4+1]) + int32(src[i*4+2])
		c := int32(src[i*4+1]) - int32(src[i*4+2])
		d := int32(src[i*4+0]) - int32(src[i*4+3])
		tmp[i*4+0] = a + b
		tmp[i*4+1] = d + c
		tmp[i*4+2] = a - b
		tmp[i*4+3] = d - c
	}

	// Column pass.
	for j := 0; j < 4; j++ {
		a := tmp[0*4+j] + tmp[3*4+j]
		b := tmp[1*4+j] + tmp[2*4+j]
		c := tmp[1*4+j] - tmp[2*4+j]
		d := tmp[0*4+j] - tmp[3*4+j]
		dst[0*4+j] = int16((a + b) >> 1)
		dst[1*4+j] = int16((d + c) >> 1)
		dst[2*4+j] = int16((a - b) >> 1)
		dst[3*4+j] = int16((d - c) >> 1)
	}
}
