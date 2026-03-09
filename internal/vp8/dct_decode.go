package vp8

// idct4x4 computes the inverse 4x4 DCT transform (RFC 6386 §14.3).
// This implementation matches the reference golang.org/x/image/vp8 inverseDCT4
// bit-exactly to prevent prediction drift between encoder and decoder.
func idct4x4(coeffs *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	const (
		c1 = 85627 // 65536 * cos(pi/8) * sqrt(2)
		c2 = 35468 // 65536 * sin(pi/8) * sqrt(2)
	)
	var m [4][4]int32

	// First pass: column transform (even-odd butterfly on rows 0,2 / 1,3).
	for i := 0; i < 4; i++ {
		a := int32(coeffs[0*4+i]) + int32(coeffs[2*4+i])
		b := int32(coeffs[0*4+i]) - int32(coeffs[2*4+i])
		c := (int32(coeffs[1*4+i])*c2)>>16 - (int32(coeffs[3*4+i])*c1)>>16
		d := (int32(coeffs[1*4+i])*c1)>>16 + (int32(coeffs[3*4+i])*c2)>>16
		m[i][0] = a + d
		m[i][1] = b + c
		m[i][2] = b - c
		m[i][3] = a - d
	}

	// Second pass: row transform with >>3 final shift.
	for j := 0; j < 4; j++ {
		dc := m[0][j] + 4
		a := dc + m[2][j]
		b := dc - m[2][j]
		c := (m[1][j]*c2)>>16 - (m[3][j]*c1)>>16
		d := (m[1][j]*c1)>>16 + (m[3][j]*c2)>>16
		dst[j*dstStride+0] = clampByte(int(pred[j*predStride+0]) + int((a+d)>>3))
		dst[j*dstStride+1] = clampByte(int(pred[j*predStride+1]) + int((b+c)>>3))
		dst[j*dstStride+2] = clampByte(int(pred[j*predStride+2]) + int((b-c)>>3))
		dst[j*dstStride+3] = clampByte(int(pred[j*predStride+3]) + int((a-d)>>3))
	}
}

// idct4x4DCOnly performs the inverse DCT when only the DC coefficient is set.
// This is the fast path used when the AC coefficients are all zero.
func idct4x4DCOnly(dc int16, pred []byte, predStride int, dst []byte, dstStride int) {
	// DC-only: all output values = (dc + 4) >> 3 added to prediction.
	val := int((int32(dc) + 4) >> 3)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			dst[y*dstStride+x] = clampByte(int(pred[y*predStride+x]) + val)
		}
	}
}

// iWHT4x4 computes the inverse 4x4 Walsh-Hadamard Transform used for the
// Y2 (luma DC) block (RFC 6386 §14.4).
// Butterfly uses [0,3]/[1,2] pairing to match the reference decoder's inverseWHT16.
// Input: 16 dequantized DC coefficients (one per 4x4 Y block).
// Output: the 16 DC values to feed into each 4x4 Y block's IDCT.
func iWHT4x4(coeffs *[16]int16, out *[16]int16) {
	var tmp [16]int32

	// Column pass.
	for i := 0; i < 4; i++ {
		a := int32(coeffs[0*4+i]) + int32(coeffs[3*4+i])
		b := int32(coeffs[1*4+i]) + int32(coeffs[2*4+i])
		c := int32(coeffs[1*4+i]) - int32(coeffs[2*4+i])
		d := int32(coeffs[0*4+i]) - int32(coeffs[3*4+i])

		tmp[0*4+i] = a + b
		tmp[1*4+i] = d + c
		tmp[2*4+i] = a - b
		tmp[3*4+i] = d - c
	}

	// Row pass.
	for i := 0; i < 4; i++ {
		dc := tmp[i*4+0] + 3
		a := dc + tmp[i*4+3]
		b := tmp[i*4+1] + tmp[i*4+2]
		c := tmp[i*4+1] - tmp[i*4+2]
		d := dc - tmp[i*4+3]

		out[i*4+0] = int16((a + b) >> 3)
		out[i*4+1] = int16((d + c) >> 3)
		out[i*4+2] = int16((a - b) >> 3)
		out[i*4+3] = int16((d - c) >> 3)
	}
}
