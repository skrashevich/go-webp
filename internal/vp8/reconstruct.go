package vp8

import (
	"image"
)

// frame holds the decoded YCbCr frame buffer.
type frame struct {
	y      []byte
	cb, cr []byte
	yStride int
	cStride int
	width   int
	height  int
	mbW     int // macroblock columns
	mbH     int // macroblock rows
}

// newFrame allocates a frame buffer for the given dimensions.
func newFrame(width, height int) *frame {
	mbW := (width + 15) / 16
	mbH := (height + 15) / 16
	yW := mbW * 16
	yH := mbH * 16
	cW := mbW * 8
	cH := mbH * 8
	f := &frame{
		y:       make([]byte, yW*yH),
		cb:      make([]byte, cW*cH),
		cr:      make([]byte, cW*cH),
		yStride: yW,
		cStride: cW,
		width:   width,
		height:  height,
		mbW:     mbW,
		mbH:     mbH,
	}
	// Fill with 128 (neutral grey).
	for i := range f.y {
		f.y[i] = 128
	}
	for i := range f.cb {
		f.cb[i] = 128
	}
	for i := range f.cr {
		f.cr[i] = 128
	}
	return f
}

// toYCbCr converts the frame buffer to an image.YCbCr.
func (f *frame) toYCbCr() *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, f.width, f.height), image.YCbCrSubsampleRatio420)
	// Copy Y plane.
	for y := 0; y < f.height; y++ {
		src := f.y[y*f.yStride : y*f.yStride+f.width]
		dst := img.Y[y*img.YStride : y*img.YStride+f.width]
		copy(dst, src)
	}
	// Copy Cb and Cr planes.
	cH := (f.height + 1) / 2
	cW := (f.width + 1) / 2
	for y := 0; y < cH; y++ {
		srcCb := f.cb[y*f.cStride : y*f.cStride+cW]
		srcCr := f.cr[y*f.cStride : y*f.cStride+cW]
		dstCb := img.Cb[y*img.CStride : y*img.CStride+cW]
		dstCr := img.Cr[y*img.CStride : y*img.CStride+cW]
		copy(dstCb, srcCb)
		copy(dstCr, srcCr)
	}
	return img
}

// reconstructMB reconstructs one macroblock into the frame buffer.
// mbX, mbY are macroblock column and row indices.
// mode16 is the 16x16 luma prediction mode (or -1 if 4x4 mode is used).
// mode4 is the per-4x4-subblock luma prediction modes (used when mode16 < 0).
// modeUV is the chroma prediction mode.
// yCoeffs holds the 16 sets of 16 Y DCT coefficients (one per 4x4 block).
// y2Coeffs holds the 16 Y2 DC coefficients (nil if not used).
// cbCoeffs, crCoeffs hold the 4 sets of UV DCT coefficients.
func reconstructMB(
	f *frame,
	mbX, mbY int,
	mode16 int,
	mode4 [16]int,
	modeUV int,
	yCoeffs [16][16]int16,
	y2Coeffs *[16]int16,
	cbCoeffs, crCoeffs [4][16]int16,
) {
	haveAbove := mbY > 0
	haveLeft := mbX > 0

	// Base pixel coordinates.
	yBase := mbY * 16 * f.yStride + mbX * 16
	cBase := mbY * 8 * f.cStride + mbX * 8

	// --- Luma reconstruction ---
	// Build above/left context buffers.
	// Read directly from the frame buffer to avoid context array overlap bugs.
	aboveY := make([]byte, 16)
	var topLeftY byte
	if haveAbove {
		aboveRow := f.y[(mbY*16-1)*f.yStride+mbX*16:]
		copy(aboveY, aboveRow[:16])
		if haveLeft {
			topLeftY = f.y[(mbY*16-1)*f.yStride+mbX*16-1]
		} else {
			topLeftY = 129
		}
	} else {
		for i := range aboveY {
			aboveY[i] = 127
		}
		topLeftY = 127
	}

	leftY := make([]byte, 16)
	if haveLeft {
		for i := 0; i < 16; i++ {
			leftY[i] = f.y[yBase+i*f.yStride-1]
		}
	} else {
		for i := range leftY {
			leftY[i] = 129
		}
	}

	if mode16 >= 0 {
		// 16x16 intra prediction.
		predY := make([]byte, 16*16)
		predict16(mode16, predY, 16, aboveY, leftY, topLeftY, haveAbove, haveLeft)

		if y2Coeffs != nil {
			// Apply iWHT to get DC coefficients.
			var dcOut [16]int16
			iWHT4x4(y2Coeffs, &dcOut)
			// Set DC of each 4x4 block.
			for blk := 0; blk < 16; blk++ {
				yCoeffs[blk][0] = dcOut[blk]
			}
		}

		for blk := 0; blk < 16; blk++ {
			bx := (blk % 4) * 4
			by := (blk / 4) * 4
			dst := f.y[yBase+by*f.yStride+bx:]
			pred := predY[by*16+bx:]
			coeffs := yCoeffs[blk]
			idct4x4(&coeffs, pred, 16, dst, f.yStride)
		}
	} else {
		// 4x4 intra prediction per sub-block.
		// We need to track reconstructed samples as we go for inter-block prediction.
		reconY := make([]byte, 16*16)
		// Initialise with above/left.
		for i := 0; i < 16; i++ {
			if haveAbove {
				reconY[i] = aboveY[i]
			} else {
				reconY[i] = 127
			}
		}

		// "Above-right" context (reference decoder's "c" values in ybr workspace).
		// These 4 pixels are to the right of the 16 above-Y pixels, used for
		// LD/VL prediction on the rightmost 4x4 column. Computed once per MB
		// and reused for all 4x4 rows (matching reference behavior).
		var aboveRight [4]byte
		if haveAbove {
			mbRight := mbX < f.mbW-1
			if mbRight {
				// Pixels from above row, columns 16..19 of next MB to the right.
				for i := 0; i < 4; i++ {
					aboveRight[i] = f.y[(mbY*16-1)*f.yStride+mbX*16+16+i]
				}
			} else {
				// Rightmost MB: repeat pixel at column 15.
				v := f.y[(mbY*16-1)*f.yStride+mbX*16+15]
				for i := range aboveRight {
					aboveRight[i] = v
				}
			}
		} else {
			for i := range aboveRight {
				aboveRight[i] = 127
			}
		}

		for blk := 0; blk < 16; blk++ {
			bx := (blk % 4) * 4
			by := (blk / 4) * 4

			// Gather above/left for this 4x4 block.
			var above4, left4 [4]byte
			var tl4 byte

			if by == 0 {
				// Top row of MB: use aboveY for above samples.
				if haveAbove {
					copy(above4[:], aboveY[bx:bx+4])
				} else {
					for i := range above4 {
						above4[i] = 127
					}
				}
				if bx == 0 {
					tl4 = topLeftY
				} else {
					tl4 = aboveY[bx-1]
				}
			} else {
				// Interior row: use previously reconstructed row.
				copy(above4[:], reconY[(by-1)*16+bx:(by-1)*16+bx+4])
				if bx == 0 {
					tl4 = leftY[by-1]
				} else {
					tl4 = reconY[(by-1)*16+bx-1]
				}
			}

			if bx == 0 {
				if haveLeft {
					for i := 0; i < 4; i++ {
						left4[i] = leftY[by+i]
					}
				} else {
					for i := range left4 {
						left4[i] = 129
					}
				}
			} else {
				for i := 0; i < 4; i++ {
					left4[i] = reconY[(by+i)*16+bx-1]
				}
			}

			// Gather extended above context (up to 8 pixels) for diagonal modes.
			var above8 [8]byte
			copy(above8[:4], above4[:])
			// Extended above pixels [4..7] for LD/VL/VE modes.
			// When bx+i >= 16 (rightmost 4x4 column), use aboveRight ("c" values).
			if by == 0 {
				// From MB above row.
				for i := 4; i < 8; i++ {
					if bx+i < 16 {
						if haveAbove {
							above8[i] = aboveY[bx+i]
						} else {
							above8[i] = 127
						}
					} else {
						above8[i] = aboveRight[bx+i-16]
					}
				}
			} else {
				for i := 4; i < 8; i++ {
					if bx+i < 16 {
						above8[i] = reconY[(by-1)*16+bx+i]
					} else {
						above8[i] = aboveRight[bx+i-16]
					}
				}
			}

			// Predict and reconstruct.
			pred4 := make([]byte, 4*4)
			// For 4x4 B_PRED, DC prediction ALWAYS uses both above and left.
			// The reference decoder's predFunc4DC has no Top/Left/TopLeft variants —
			// the ybr workspace always has valid border values (127/129 init or
			// previously reconstructed). checkTopLeftPred only applies to 16x16/8x8.
			switch mode4[blk] {
			case predDC4:
				predictDC4(pred4, 4, above4[:], left4[:], true, true)
			case predV4:
				predictVE4(pred4, tl4, above8)
			case predH4:
				predictHE4(pred4, tl4, left4)
			case predTM4:
				predictTM4(pred4, 4, above4[:], left4[:], tl4)
			case predRD4:
				predictRD4(pred4, tl4, above8, left4)
			case predVR4:
				predictVR4(pred4, tl4, above8, left4)
			case predLD4:
				predictLD4(pred4, above8)
			case predVL4:
				predictVL4(pred4, above8)
			case predHD4:
				predictHD4(pred4, tl4, above8, left4)
			case predHU4:
				predictHU4(pred4, left4)
			default:
				predictDC4(pred4, 4, above4[:], left4[:], true, true)
			}

			// Store prediction into reconY so next blocks can use it.
			for ry := 0; ry < 4; ry++ {
				copy(reconY[(by+ry)*16+bx:], pred4[ry*4:ry*4+4])
			}

			// Apply IDCT into frame.
			dst := f.y[yBase+by*f.yStride+bx:]
			coeffs := yCoeffs[blk]
			idct4x4(&coeffs, pred4, 4, dst, f.yStride)

			// Update reconY with final reconstructed values.
			for ry := 0; ry < 4; ry++ {
				for rx := 0; rx < 4; rx++ {
					reconY[(by+ry)*16+bx+rx] = f.y[yBase+(by+ry)*f.yStride+bx+rx]
				}
			}
		}
	}

	// --- Chroma reconstruction ---
	// Read directly from the frame buffer to avoid context array overlap bugs.
	aboveCb := make([]byte, 8)
	aboveCr := make([]byte, 8)
	var topLeftCb, topLeftCr byte
	if haveAbove {
		copy(aboveCb, f.cb[(mbY*8-1)*f.cStride+mbX*8:])
		copy(aboveCr, f.cr[(mbY*8-1)*f.cStride+mbX*8:])
		if haveLeft {
			topLeftCb = f.cb[(mbY*8-1)*f.cStride+mbX*8-1]
			topLeftCr = f.cr[(mbY*8-1)*f.cStride+mbX*8-1]
		} else {
			topLeftCb = 129
			topLeftCr = 129
		}
	} else {
		for i := range aboveCb {
			aboveCb[i] = 127
			aboveCr[i] = 127
		}
		topLeftCb = 127
		topLeftCr = 127
	}

	leftCb := make([]byte, 8)
	leftCr := make([]byte, 8)
	if haveLeft {
		for i := 0; i < 8; i++ {
			leftCb[i] = f.cb[cBase+i*f.cStride-1]
			leftCr[i] = f.cr[cBase+i*f.cStride-1]
		}
	} else {
		for i := range leftCb {
			leftCb[i] = 129
			leftCr[i] = 129
		}
	}

	reconstructChromaMB(f, cBase, modeUV, aboveCb, aboveCr, leftCb, leftCr, topLeftCb, topLeftCr, cbCoeffs, crCoeffs, haveAbove, haveLeft)
}

// reconstructChromaMB reconstructs the two chroma planes for one MB.
func reconstructChromaMB(
	f *frame,
	cBase int,
	modeUV int,
	aboveCb, aboveCr, leftCb, leftCr []byte,
	topLeftCb, topLeftCr byte,
	cbCoeffs, crCoeffs [4][16]int16,
	haveAbove, haveLeft bool,
) {
	// Predict 8x8 chroma.
	predCb := make([]byte, 8*8)
	predCr := make([]byte, 8*8)

	switch modeUV {
	case predDCUV:
		predictDC8(predCb, 8, aboveCb, leftCb, haveAbove, haveLeft)
		predictDC8(predCr, 8, aboveCr, leftCr, haveAbove, haveLeft)
	case predVUV:
		for y := 0; y < 8; y++ {
			copy(predCb[y*8:y*8+8], aboveCb)
			copy(predCr[y*8:y*8+8], aboveCr)
		}
	case predHUV:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				predCb[y*8+x] = leftCb[y]
				predCr[y*8+x] = leftCr[y]
			}
		}
	case predTMUV:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				vCb := int(aboveCb[x]) + int(leftCb[y]) - int(topLeftCb)
				vCr := int(aboveCr[x]) + int(leftCr[y]) - int(topLeftCr)
				predCb[y*8+x] = clampByte(vCb)
				predCr[y*8+x] = clampByte(vCr)
			}
		}
	}

	// Apply IDCT to each 4x4 chroma sub-block.
	for blk := 0; blk < 4; blk++ {
		bx := (blk % 2) * 4
		by := (blk / 2) * 4

		dstCb := f.cb[cBase+by*f.cStride+bx:]
		dstCr := f.cr[cBase+by*f.cStride+bx:]
		predCbBlk := predCb[by*8+bx:]
		predCrBlk := predCr[by*8+bx:]

		cb := cbCoeffs[blk]
		cr := crCoeffs[blk]
		idct4x4(&cb, predCbBlk, 8, dstCb, f.cStride)
		idct4x4(&cr, predCrBlk, 8, dstCr, f.cStride)
	}
}

// predictDC8 fills an 8x8 block with the DC value.
func predictDC8(dst []byte, stride int, above, left []byte, haveAbove, haveLeft bool) {
	sum := 0
	n := 0
	if haveAbove {
		for _, v := range above[:8] {
			sum += int(v)
		}
		n += 8
	}
	if haveLeft {
		for _, v := range left[:8] {
			sum += int(v)
		}
		n += 8
	}
	dc := byte(128)
	if n > 0 {
		dc = byte((sum + n/2) / n)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			dst[y*stride+x] = dc
		}
	}
}

// clip255 clamps an int32 to [0, 255].
func clip255(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// avg2 returns (a+b+1)/2.
func avg2(a, b int32) byte { return byte((a + b + 1) / 2) }

// avg3 returns (a+2*b+c+2)/4.
func avg3(a, b, c int32) byte { return byte((a + 2*b + c + 2) / 4) }

// predictVE4 implements the VE (vertical with smoothing) 4x4 prediction.
// RFC 6386 §12.3. Matches reference golang.org/x/image/vp8 predFunc4VE.
func predictVE4(dst []byte, tl byte, above [8]byte) {
	a := int32(tl)
	b := int32(above[0])
	c := int32(above[1])
	d := int32(above[2])
	e := int32(above[3])
	f := int32(above[4])
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	cde := avg3(c, d, e)
	def := avg3(d, e, f)
	for j := 0; j < 4; j++ {
		dst[j*4+0] = abc
		dst[j*4+1] = bcd
		dst[j*4+2] = cde
		dst[j*4+3] = def
	}
}

// predictHE4 implements the HE (horizontal with smoothing) 4x4 prediction.
// Matches reference predFunc4HE.
func predictHE4(dst []byte, tl byte, left [4]byte) {
	s := int32(left[3])
	r := int32(left[2])
	q := int32(left[1])
	p := int32(left[0])
	a := int32(tl)
	apq := avg3(a, p, q)
	rqp := avg3(r, q, p)
	srq := avg3(s, r, q)
	ssr := avg3(s, s, r)
	for i := 0; i < 4; i++ {
		dst[0*4+i] = apq
		dst[1*4+i] = rqp
		dst[2*4+i] = srq
		dst[3*4+i] = ssr
	}
}

// predictRD4 implements the RD (right-down diagonal) 4x4 prediction.
// Matches reference predFunc4RD.
func predictRD4(dst []byte, tl byte, above [8]byte, left [4]byte) {
	s := int32(left[3])
	r := int32(left[2])
	q := int32(left[1])
	p := int32(left[0])
	a := int32(tl)
	b := int32(above[0])
	c := int32(above[1])
	d := int32(above[2])
	e := int32(above[3])
	srq := avg3(s, r, q)
	rqp := avg3(r, q, p)
	qpa := avg3(q, p, a)
	pab := avg3(p, a, b)
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	cde := avg3(c, d, e)
	dst[0*4+0] = pab
	dst[0*4+1] = abc
	dst[0*4+2] = bcd
	dst[0*4+3] = cde
	dst[1*4+0] = qpa
	dst[1*4+1] = pab
	dst[1*4+2] = abc
	dst[1*4+3] = bcd
	dst[2*4+0] = rqp
	dst[2*4+1] = qpa
	dst[2*4+2] = pab
	dst[2*4+3] = abc
	dst[3*4+0] = srq
	dst[3*4+1] = rqp
	dst[3*4+2] = qpa
	dst[3*4+3] = pab
}

// predictVR4 implements the VR (vertical-right) 4x4 prediction.
// Matches reference predFunc4VR.
func predictVR4(dst []byte, tl byte, above [8]byte, left [4]byte) {
	r := int32(left[2])
	q := int32(left[1])
	p := int32(left[0])
	a := int32(tl)
	b := int32(above[0])
	c := int32(above[1])
	d := int32(above[2])
	e := int32(above[3])
	ab := avg2(a, b)
	bc := avg2(b, c)
	cd := avg2(c, d)
	de := avg2(d, e)
	rqp := avg3(r, q, p)
	qpa := avg3(q, p, a)
	pab := avg3(p, a, b)
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	cde := avg3(c, d, e)
	dst[0*4+0] = ab
	dst[0*4+1] = bc
	dst[0*4+2] = cd
	dst[0*4+3] = de
	dst[1*4+0] = pab
	dst[1*4+1] = abc
	dst[1*4+2] = bcd
	dst[1*4+3] = cde
	dst[2*4+0] = qpa
	dst[2*4+1] = ab
	dst[2*4+2] = bc
	dst[2*4+3] = cd
	dst[3*4+0] = rqp
	dst[3*4+1] = pab
	dst[3*4+2] = abc
	dst[3*4+3] = bcd
}

// predictLD4 implements the LD (left-down diagonal) 4x4 prediction.
// Matches reference predFunc4LD.
func predictLD4(dst []byte, above [8]byte) {
	a := int32(above[0])
	b := int32(above[1])
	c := int32(above[2])
	d := int32(above[3])
	e := int32(above[4])
	f := int32(above[5])
	g := int32(above[6])
	h := int32(above[7])
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	cde := avg3(c, d, e)
	def := avg3(d, e, f)
	efg := avg3(e, f, g)
	fgh := avg3(f, g, h)
	ghh := avg3(g, h, h)
	dst[0*4+0] = abc
	dst[0*4+1] = bcd
	dst[0*4+2] = cde
	dst[0*4+3] = def
	dst[1*4+0] = bcd
	dst[1*4+1] = cde
	dst[1*4+2] = def
	dst[1*4+3] = efg
	dst[2*4+0] = cde
	dst[2*4+1] = def
	dst[2*4+2] = efg
	dst[2*4+3] = fgh
	dst[3*4+0] = def
	dst[3*4+1] = efg
	dst[3*4+2] = fgh
	dst[3*4+3] = ghh
}

// predictVL4 implements the VL (vertical-left) 4x4 prediction.
// Matches reference predFunc4VL.
func predictVL4(dst []byte, above [8]byte) {
	a := int32(above[0])
	b := int32(above[1])
	c := int32(above[2])
	d := int32(above[3])
	e := int32(above[4])
	f := int32(above[5])
	g := int32(above[6])
	h := int32(above[7])
	ab := avg2(a, b)
	bc := avg2(b, c)
	cd := avg2(c, d)
	de := avg2(d, e)
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	cde := avg3(c, d, e)
	def := avg3(d, e, f)
	efg := avg3(e, f, g)
	fgh := avg3(f, g, h)
	dst[0*4+0] = ab
	dst[0*4+1] = bc
	dst[0*4+2] = cd
	dst[0*4+3] = de
	dst[1*4+0] = abc
	dst[1*4+1] = bcd
	dst[1*4+2] = cde
	dst[1*4+3] = def
	dst[2*4+0] = bc
	dst[2*4+1] = cd
	dst[2*4+2] = de
	dst[2*4+3] = efg
	dst[3*4+0] = bcd
	dst[3*4+1] = cde
	dst[3*4+2] = def
	dst[3*4+3] = fgh
}

// predictHD4 implements the HD (horizontal-down) 4x4 prediction.
// Matches reference predFunc4HD.
func predictHD4(dst []byte, tl byte, above [8]byte, left [4]byte) {
	s := int32(left[3])
	r := int32(left[2])
	q := int32(left[1])
	p := int32(left[0])
	a := int32(tl)
	b := int32(above[0])
	c := int32(above[1])
	d := int32(above[2])
	sr := avg2(s, r)
	rq := avg2(r, q)
	qp := avg2(q, p)
	pa := avg2(p, a)
	srq := avg3(s, r, q)
	rqp := avg3(r, q, p)
	qpa := avg3(q, p, a)
	pab := avg3(p, a, b)
	abc := avg3(a, b, c)
	bcd := avg3(b, c, d)
	dst[0*4+0] = pa
	dst[0*4+1] = pab
	dst[0*4+2] = abc
	dst[0*4+3] = bcd
	dst[1*4+0] = qp
	dst[1*4+1] = qpa
	dst[1*4+2] = pa
	dst[1*4+3] = pab
	dst[2*4+0] = rq
	dst[2*4+1] = rqp
	dst[2*4+2] = qp
	dst[2*4+3] = qpa
	dst[3*4+0] = sr
	dst[3*4+1] = srq
	dst[3*4+2] = rq
	dst[3*4+3] = rqp
}

// predictHU4 implements the HU (horizontal-up) 4x4 prediction.
// Matches reference predFunc4HU.
func predictHU4(dst []byte, left [4]byte) {
	s := int32(left[3])
	r := int32(left[2])
	q := int32(left[1])
	p := int32(left[0])
	pq := avg2(p, q)
	qr := avg2(q, r)
	rs := avg2(r, s)
	pqr := avg3(p, q, r)
	qrs := avg3(q, r, s)
	rss := avg3(r, s, s)
	sss := byte(s)
	dst[0*4+0] = pq
	dst[0*4+1] = pqr
	dst[0*4+2] = qr
	dst[0*4+3] = qrs
	dst[1*4+0] = qr
	dst[1*4+1] = qrs
	dst[1*4+2] = rs
	dst[1*4+3] = rss
	dst[2*4+0] = rs
	dst[2*4+1] = rss
	dst[2*4+2] = sss
	dst[2*4+3] = sss
	dst[3*4+0] = sss
	dst[3*4+1] = sss
	dst[3*4+2] = sss
	dst[3*4+3] = sss
}
