package vp8

// zigzag maps scan position (0-15) to spatial coefficient index.
// This is the order in which VP8 encodes/decodes coefficients.
var zigzag = [16]uint8{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}

// cat3456 contains the extra-bit probabilities for categories 3-6.
// These are NOT uniform (128) — they are specific per-bit probabilities.
var cat3456 = [4][12]uint8{
	{173, 148, 140, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	{176, 155, 140, 135, 0, 0, 0, 0, 0, 0, 0, 0},
	{180, 157, 141, 134, 130, 0, 0, 0, 0, 0, 0, 0},
	{254, 254, 243, 230, 196, 177, 153, 140, 133, 130, 129, 0},
}

// encodeCoefficients encodes one 4x4 quantized coefficient block into the bool encoder.
// The token tree matches the reference golang.org/x/image/vp8 parseResiduals4 exactly.
// Coefficients are read in zigzag scan order.
//
// plane: 0=Y-after-DC, 1=Y2(DC-only), 2=UV, 3=Y-no-DC
// ctx: number of non-zero neighbours (0, 1, or 2+)
func encodeCoefficients(enc *boolEncoder, coeffs *[16]int16, plane, ctx int) {
	encodeCoefficientsFrom(enc, coeffs, plane, ctx, 0)
}

// encodeCoefficientsFrom is like encodeCoefficients but starts encoding
// from scan position `first` instead of 0. Used for Y blocks where
// DC is sent separately via WHT (first=1).
func encodeCoefficientsFrom(enc *boolEncoder, coeffs *[16]int16, plane, ctx, first int) {
	probs := defaultCoeffProbs[plane]

	// Find last non-zero coefficient in zigzag scan order.
	lastNZ := -1
	for i := 15; i >= first; i-- {
		if coeffs[zigzag[i]] != 0 {
			lastNZ = i
			break
		}
	}

	// If all coefficients are zero, write EOB immediately.
	if lastNZ < first {
		band := coeffBand[first]
		p := probs[band][ctx]
		enc.writeBool(p[0], false) // EOB
		return
	}

	// The reference decoder (parseResiduals4) reads:
	//   1. p[0] once at start (EOB check — is there any non-zero coeff?)
	//   2. Loop: p[1] (zero or non-zero?)
	//      - If zero: update context, continue (NO p[0] read!)
	//      - If non-zero: decode value + sign, then p[0] for next position EOB check
	//
	// So the encoder must write p[0] only at the start and after non-zero coefficients,
	// NOT before zero coefficients in the middle of the block.

	// Write initial p[0] = true (there IS at least one non-zero).
	{
		band := coeffBand[first]
		p := probs[band][ctx]
		enc.writeBool(p[0], true) // not EOB (we know lastNZ >= first)
	}

	for i := first; i < 16; i++ {
		band := coeffBand[i]
		p := probs[band][ctx]

		c := coeffs[zigzag[i]] // Read in zigzag order!
		if c == 0 {
			// Zero coefficient — write only p[1]=false (no p[0]!).
			enc.writeBool(p[1], false) // DCT_0 (zero token)
			ctx = 0
			continue
		}

		// Not zero — write p[1]=true then value.
		enc.writeBool(p[1], true) // not zero

		abs := c
		if abs < 0 {
			abs = -abs
		}

		// Token tree matching reference parseResiduals4 exactly.
		switch {
		case abs == 1:
			enc.writeBool(p[2], false) // val=1
			ctx = 1
		case abs == 2:
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], false) // 2..4
			enc.writeBool(p[4], false) // val=2
			ctx = 2
		case abs == 3:
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], false) // 2..4
			enc.writeBool(p[4], true)  // 3 or 4
			enc.writeBool(p[5], false) // extra=0 → val=3
			ctx = 2
		case abs == 4:
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], false) // 2..4
			enc.writeBool(p[4], true)  // 3 or 4
			enc.writeBool(p[5], true)  // extra=1 → val=4
			ctx = 2
		case abs <= 6: // CAT1: base 5, 1 extra bit at prob 159
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], true)  // 5+ (categories)
			enc.writeBool(p[6], false) // CAT1 or CAT2
			enc.writeBool(p[7], false) // CAT1
			enc.writeBool(159, abs == 6)
			ctx = 2
		case abs <= 10: // CAT2: base 7, 2 extra bits at probs 165, 145
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], true)  // 5+
			enc.writeBool(p[6], false) // CAT1 or CAT2
			enc.writeBool(p[7], true)  // CAT2
			extra := abs - 7           // 0..3
			enc.writeBool(165, extra >= 2)
			enc.writeBool(145, extra%2 == 1)
			ctx = 2
		case abs <= 18: // CAT3: base 11, 3 extra bits
			enc.writeBool(p[2], true) // >1
			enc.writeBool(p[3], true) // 5+
			enc.writeBool(p[6], true) // CAT3-6
			enc.writeBool(p[8], false)
			enc.writeBool(p[9], false) // cat=0 → CAT3
			encodeExtraBits(enc, int(abs)-11, 0)
			ctx = 2
		case abs <= 34: // CAT4: base 19, 4 extra bits
			enc.writeBool(p[2], true) // >1
			enc.writeBool(p[3], true) // 5+
			enc.writeBool(p[6], true) // CAT3-6
			enc.writeBool(p[8], false)
			enc.writeBool(p[9], true) // cat=1 → CAT4
			encodeExtraBits(enc, int(abs)-19, 1)
			ctx = 2
		case abs <= 66: // CAT5: base 35, 5 extra bits
			enc.writeBool(p[2], true)  // >1
			enc.writeBool(p[3], true)  // 5+
			enc.writeBool(p[6], true)  // CAT3-6
			enc.writeBool(p[8], true)
			enc.writeBool(p[10], false) // cat=2 → CAT5
			encodeExtraBits(enc, int(abs)-35, 2)
			ctx = 2
		default: // CAT6: base 67, 11 extra bits
			enc.writeBool(p[2], true) // >1
			enc.writeBool(p[3], true) // 5+
			enc.writeBool(p[6], true) // CAT3-6
			enc.writeBool(p[8], true)
			enc.writeBool(p[10], true) // cat=3 → CAT6
			encodeExtraBits(enc, int(abs)-67, 3)
			ctx = 2
		}

		// Sign bit.
		enc.writeBool(128, c < 0)

		// EOB check for next position (only after non-zero coefficients).
		if i >= lastNZ || i == 15 {
			// This was the last non-zero or last position — write EOB.
			if i < 15 {
				nextBand := coeffBand[i+1]
				np := probs[nextBand][ctx]
				enc.writeBool(np[0], false) // EOB
			}
			return
		}
		// Not done — write not-EOB for next position.
		nextBand := coeffBand[i+1]
		np := probs[nextBand][ctx]
		enc.writeBool(np[0], true) // not EOB
	}
}

// encodeExtraBits writes the extra magnitude bits for categories 3-6
// using the specific per-bit probabilities from the cat3456 table.
func encodeExtraBits(enc *boolEncoder, extra int, cat int) {
	tab := &cat3456[cat]
	// Count the number of bits (entries until tab[i]==0).
	nBits := 0
	for nBits < 12 && tab[nBits] != 0 {
		nBits++
	}
	// Write bits MSB first, matching the reference decoder's loop.
	for i := 0; i < nBits; i++ {
		bit := (extra >> uint(nBits-1-i)) & 1
		enc.writeBool(tab[i], bit == 1)
	}
}

// coeffBand maps coefficient scan position (0-15) to its VP8 band (0-7).
var coeffBand = [16]int{0, 1, 2, 3, 6, 4, 5, 6, 6, 6, 6, 6, 6, 6, 6, 7}
