package vp8

// zigzagDecode maps scan position to coefficient index (raster order).
// This matches the reference decoder's zigzag table.
var zigzagDecode = [16]uint8{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}

// bands maps coefficient scan position to its VP8 band (0-7).
// Index 16 is used for the "next" band after the last coefficient.
var bands = [17]uint8{0, 1, 2, 3, 6, 4, 5, 6, 6, 6, 6, 6, 6, 6, 6, 7, 0}

// decodeResiduals4 decodes one 4x4 block of residual coefficients from the
// bool decoder, matching the reference golang.org/x/image/vp8 parseResiduals4.
//
// plane: coefficient plane (0=Y1WithY2, 1=Y2, 2=UV, 3=Y1SansY2).
// context: 0, 1, or 2 based on non-zero neighbors.
// dcQ, acQ: dequantization factors for DC and AC coefficients.
// skipFirstCoeff: if true, skip coefficient 0 (DC provided by WHT).
// coeffs: output coefficients in raster order (ready for IDCT).
//
// Returns 1 if any non-zero coefficient was decoded, 0 otherwise.
func decodeResiduals4(bd *boolDecoder, probs *[8][3][11]uint8, context uint8,
	dcQ, acQ int16, skipFirstCoeff bool, coeffs *[16]int16) uint8 {

	n := 0
	if skipFirstCoeff {
		n = 1
	}
	p := probs[bands[n]][context]

	// First check: any non-zero coefficient at all?
	if !bd.ReadBool(p[0]) {
		return 0
	}

	for n != 16 {
		n++
		// Check for zero coefficient.
		if !bd.ReadBool(p[1]) {
			p = probs[bands[n]][0]
			continue
		}

		var v int32
		if !bd.ReadBool(p[2]) {
			// Literal 1.
			v = 1
			p = probs[bands[n]][1]
		} else {
			if !bd.ReadBool(p[3]) {
				if !bd.ReadBool(p[4]) {
					v = 2
				} else {
					v = 3 + int32(readUint(bd, p[5], 1))
				}
			} else if !bd.ReadBool(p[6]) {
				if !bd.ReadBool(p[7]) {
					// Category 1: base 5, 1 extra bit.
					v = 5 + int32(readUint(bd, 159, 1))
				} else {
					// Category 2: base 7, 2 extra bits.
					v = 7 + 2*int32(readUint(bd, 165, 1)) + int32(readUint(bd, 145, 1))
				}
			} else {
				// Categories 3, 4, 5, or 6.
				b1 := readUint(bd, p[8], 1)
				b0 := readUint(bd, p[9+b1], 1)
				cat := 2*b1 + b0
				tab := &cat3456[cat]
				v = 0
				for i := 0; tab[i] != 0; i++ {
					v *= 2
					v += int32(readUint(bd, tab[i], 1))
				}
				v += 3 + (8 << cat)
			}
			p = probs[bands[n]][2]
		}

		// Coefficient position in raster order.
		z := zigzagDecode[n-1]
		// Dequantize: DC uses dcQ, AC uses acQ.
		q := acQ
		if z == 0 {
			q = dcQ
		}
		c := v * int32(q)
		// Sign bit.
		if bd.ReadBool(128) {
			c = -c
		}
		coeffs[z] = int16(c)

		// Check for EOB after this coefficient.
		if n == 16 || !bd.ReadBool(p[0]) {
			return 1
		}
	}
	return 1
}

// readUint reads n bits with the given probability, MSB first.
func readUint(bd *boolDecoder, prob uint8, n uint8) uint32 {
	var u uint32
	for n > 0 {
		n--
		if bd.ReadBool(prob) {
			u |= 1 << n
		}
	}
	return u
}

// decodeCoefficients decodes one 4x4 coefficient block from the bool decoder.
// This is a compatibility wrapper that delegates to decodeResiduals4.
func decodeCoefficients(bd *boolDecoder, plane, ctx, firstCoeff int, dcQ, acQ int16) ([16]int16, int) {
	var coeffs [16]int16
	probs := &defaultCoeffProbs[plane]
	nz := decodeResiduals4(bd, probs, uint8(ctx), dcQ, acQ, firstCoeff > 0, &coeffs)
	return coeffs, int(nz)
}

// decodeCoefficientsEOB decodes one 4x4 coefficient block using the proper
// token tree matching RFC 6386 §13.2.
func decodeCoefficientsEOB(bd *boolDecoder, plane, ctx, firstCoeff int, dcQ, acQ int16) ([16]int16, int) {
	return decodeCoefficients(bd, plane, ctx, firstCoeff, dcQ, acQ)
}

// nzContext returns the coefficient context (0, 1, or 2) given the number
// of non-zero coefficients in the above and left blocks.
func nzContext(nzAbove, nzLeft int) int {
	nz := 0
	if nzAbove > 0 {
		nz++
	}
	if nzLeft > 0 {
		nz++
	}
	if nz > 2 {
		nz = 2
	}
	return nz
}
