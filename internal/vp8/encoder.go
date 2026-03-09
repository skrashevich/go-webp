package vp8

import (
	"bytes"
	"image"
)

// EncodeVP8 encodes img as a VP8 (lossy) bitstream and returns the raw VP8 data.
// quality is in [0, 100]; higher values produce better quality and larger files.
func EncodeVP8(img image.Image, quality float32) ([]byte, error) {
	yuv := rgbaToYUV(img)
	qp := qualityToQP(quality)
	qParams := newEncodeQuantParams(qp)

	// Create reconstruction buffer with same layout as original.
	recon := &yuvPlanes{
		y:       make([]byte, len(yuv.y)),
		cb:      make([]byte, len(yuv.cb)),
		cr:      make([]byte, len(yuv.cr)),
		yStride: yuv.yStride,
		cStride: yuv.cStride,
		width:   yuv.width,
		height:  yuv.height,
	}
	// Initialize to 128 (neutral gray).
	for i := range recon.y {
		recon.y[i] = 128
	}
	for i := range recon.cb {
		recon.cb[i] = 128
	}
	for i := range recon.cr {
		recon.cr[i] = 128
	}

	var firstPart bytes.Buffer
	var dctPart bytes.Buffer
	hdrEnc := newBoolEncoder(&firstPart)
	dctEnc := newBoolEncoder(&dctPart)

	writeCompressedHeader(hdrEnc, qp, yuv.width, yuv.height)

	mbW := (yuv.width + 15) / 16
	mbH := (yuv.height + 15) / 16

	// Non-zero coefficient tracking for context (matching reference decoder).
	// upNZ[col] = nzMask for the macroblock above (bottom row flags).
	// leftNZ = nzMask for the macroblock to the left (rightmost column flags).
	upNZ := make([]uint8, mbW)
	upNZY2 := make([]uint8, mbW)

	for mbRow := 0; mbRow < mbH; mbRow++ {
		var leftNZ uint8
		var leftNZY2 uint8
		for mbCol := 0; mbCol < mbW; mbCol++ {
			leftMask, upMask, nzY2 := encodeMacroblock(hdrEnc, dctEnc, yuv, recon, mbRow, mbCol, qParams,
				leftNZ, upNZ[mbCol], leftNZY2, upNZY2[mbCol])
			leftNZ = leftMask
			leftNZY2 = nzY2
			upNZ[mbCol] = upMask
			upNZY2[mbCol] = nzY2
		}
	}

	if err := hdrEnc.flush(); err != nil {
		return nil, err
	}
	if err := dctEnc.flush(); err != nil {
		return nil, err
	}

	firstPartData := firstPart.Bytes()
	dctPartData := dctPart.Bytes()

	var out bytes.Buffer
	hdr := encodeFrameHeader{
		width:  yuv.width,
		height: yuv.height,
	}
	if err := writeFrameHeader(&out, len(firstPartData), hdr); err != nil {
		return nil, err
	}
	out.Write(firstPartData)
	out.Write(dctPartData)

	return out.Bytes(), nil
}

// encodeQuantParams holds quantizer parameters used by the encoder,
// matching the decoder's dequantization steps exactly.
type encodeQuantParams struct {
	yDCQ  int16
	yACQ  int16
	y2DCQ int16
	y2ACQ int16
	uvDCQ int16
	uvACQ int16
}

func newEncodeQuantParams(qp int) encodeQuantParams {
	if qp < 0 {
		qp = 0
	}
	if qp > 127 {
		qp = 127
	}
	y2ACQ := int16(acTable[qp]) * 155 / 100
	if y2ACQ < 8 {
		y2ACQ = 8
	}
	uvDCIdx := qp
	if uvDCIdx > 117 {
		uvDCIdx = 117
	}
	return encodeQuantParams{
		yDCQ:  dcTable[qp],
		yACQ:  acTable[qp],
		y2DCQ: dcTable[qp] * 2,
		y2ACQ: y2ACQ,
		uvDCQ: dcTable[uvDCIdx],
		uvACQ: acTable[qp],
	}
}

func dequantizeBlock(coeff *[16]int16, dcQ, acQ int16) {
	coeff[0] *= dcQ
	for i := 1; i < 16; i++ {
		coeff[i] *= acQ
	}
}

func writeCompressedHeader(enc *boolEncoder, qp, width, height int) {
	enc.writeLiteral(1, 0) // color_space
	enc.writeLiteral(1, 0) // clamping_type

	enc.writeBool(128, false) // update_segmentation = 0

	enc.writeLiteral(1, 0)    // filter_type
	enc.writeLiteral(6, 0)    // loop_filter_level
	enc.writeLiteral(3, 0)    // sharpness_level
	enc.writeBool(128, false) // loop_filter_adj_enable

	enc.writeLiteral(2, 0) // num DCT partitions log2

	enc.writeLiteral(7, uint32(qp)) // y_ac_qi
	enc.writeBool(128, false)       // y_dc_delta
	enc.writeBool(128, false)       // y2_dc_delta
	enc.writeBool(128, false)       // y2_ac_delta
	enc.writeBool(128, false)       // uv_dc_delta
	enc.writeBool(128, false)       // uv_ac_delta

	enc.writeBool(128, false) // refresh_entropy_probs

	// Token probability updates: no updates.
	for i := 0; i < 4; i++ {
		for j := 0; j < 8; j++ {
			for k := 0; k < 3; k++ {
				for l := 0; l < 11; l++ {
					enc.writeBool(coefUpdateProbs[i][j][k][l], false)
				}
			}
		}
	}

	enc.writeBool(128, false) // mb_no_coeff_skip = 0
}

// blockHasNonZero checks if a quantized block has any non-zero coefficients
// starting from the given scan position (in zigzag order).
func blockHasNonZero(coeffs *[16]int16, first int) bool {
	for i := first; i < 16; i++ {
		if coeffs[zigzag[i]] != 0 {
			return true
		}
	}
	return false
}

// nzCtx computes the coefficient context from left and above non-zero flags.
func nzCtx(left, above uint8) int {
	return int(left) + int(above)
}

// encodeMacroblock encodes one 16x16 macroblock with proper non-zero context tracking.
// Returns (leftNZMask, upNZMask, nzY2) for use as context by subsequent macroblocks.
func encodeMacroblock(hdrEnc, dctEnc *boolEncoder, yuv, recon *yuvPlanes,
	mbRow, mbCol int, qp encodeQuantParams,
	leftNZMask, upNZMask uint8, leftNZY2, upNZY2 uint8) (uint8, uint8, uint8) {

	haveAbove := mbRow > 0
	haveLeft := mbCol > 0

	// === LUMA (Y) ===
	yOff := mbRow*16*yuv.yStride + mbCol*16
	src := yuv.y[yOff:]

	above16 := make([]byte, 16)
	left16 := make([]byte, 16)
	topLeft := byte(128)

	if haveAbove {
		copy(above16, recon.y[yOff-recon.yStride:yOff-recon.yStride+16])
	} else {
		for i := range above16 {
			above16[i] = 127
		}
	}
	if haveLeft {
		for i := 0; i < 16; i++ {
			left16[i] = recon.y[yOff+i*recon.yStride-1]
		}
	} else {
		for i := range left16 {
			left16[i] = 129
		}
	}
	if haveAbove && haveLeft {
		topLeft = recon.y[yOff-recon.yStride-1]
	}

	mode16 := choosePredMode16(src, yuv.yStride, above16, left16, topLeft, haveAbove, haveLeft)
	writeMBMode16(hdrEnc, mode16)

	pred := make([]byte, 16*16)
	predict16(mode16, pred, 16, above16, left16, topLeft, haveAbove, haveLeft)

	// Forward DCT + quantize all 16 sub-blocks, collect DC values.
	var dcCoeffs [16]int16
	var yDCTs [16][16]int16

	for subY := 0; subY < 4; subY++ {
		for subX := 0; subX < 4; subX++ {
			var residual [16]int16
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					sIdx := (subY*4+y)*yuv.yStride + subX*4 + x
					pIdx := (subY*4+y)*16 + subX*4 + x
					residual[y*4+x] = int16(src[sIdx]) - int16(pred[pIdx])
				}
			}
			var dct [16]int16
			fdct4x4(&residual, &dct)
			dcCoeffs[subY*4+subX] = dct[0]
			dct[0] = 0 // DC sent separately via WHT
			quantizeBlock(&dct, qp.yDCQ, qp.yACQ)
			yDCTs[subY*4+subX] = dct
		}
	}

	// WHT on DC coefficients.
	var whtOut [16]int16
	fwht4x4(&dcCoeffs, &whtOut)
	quantizeBlock(&whtOut, qp.y2DCQ, qp.y2ACQ)

	// Encode Y2 (WHT DC block) with proper context.
	y2Ctx := nzCtx(leftNZY2, upNZY2)
	y2NZ := blockHasNonZero(&whtOut, 0)
	encodeCoefficients(dctEnc, &whtOut, 1, y2Ctx)

	// Encode 16 Y AC blocks with proper context tracking.
	// Track non-zero state matching the reference decoder's iteration order.
	// lnz[y] = left neighbor nz for row y; unz[x] = above neighbor nz for column x.
	var lnz [4]uint8
	var unz [4]uint8

	// Initialize from previous macroblock state.
	// leftNZMask lower 4 bits: Y column nz flags (one per row).
	// upNZMask lower 4 bits: Y row nz flags (one per column).
	for i := 0; i < 4; i++ {
		lnz[i] = (leftNZMask >> uint(i)) & 1
		unz[i] = (upNZMask >> uint(i)) & 1
	}

	for subY := 0; subY < 4; subY++ {
		nz := lnz[subY]
		for subX := 0; subX < 4; subX++ {
			blk := subY*4 + subX
			dct := yDCTs[blk]
			ctx := nzCtx(nz, unz[subX])
			hasNZ := blockHasNonZero(&dct, 1) // skip DC (first=1)
			encodeCoefficientsFrom(dctEnc, &dct, 0, ctx, 1)
			if hasNZ {
				nz = 1
			} else {
				nz = 0
			}
			unz[subX] = nz
		}
		lnz[subY] = nz
	}

	// Build Y nzMask from bottom row (unz) and rightmost column (lnz).
	// The reference stores: lower nibble = pack(lnz) for left, pack(unz) for up.
	// But nzMask is used as: lower 4 bits passed to next MB as left (lnz) and down (unz).
	// Actually the reference uses a single nzMask where:
	// - bits 0-3 are the 4 Y nz flags (used for both left and above).
	// - The left MB uses lnz (rightmost column), the above MB uses unz (bottom row).
	// For simplicity, we pack lnz for left use and unz for above use.
	var yLNZMask uint8
	for i := 0; i < 4; i++ {
		yLNZMask |= lnz[i] << uint(i)
	}
	var yUNZMask uint8
	for i := 0; i < 4; i++ {
		yUNZMask |= unz[i] << uint(i)
	}

	// === RECONSTRUCT LUMA ===
	var whtDeq [16]int16
	copy(whtDeq[:], whtOut[:])
	dequantizeBlock(&whtDeq, qp.y2DCQ, qp.y2ACQ)
	var reconDC [16]int16
	iWHT4x4(&whtDeq, &reconDC)

	for subY := 0; subY < 4; subY++ {
		for subX := 0; subX < 4; subX++ {
			blk := subY*4 + subX
			dct := yDCTs[blk]
			dequantizeBlock(&dct, qp.yDCQ, qp.yACQ)
			dct[0] = reconDC[blk]

			predBlock := make([]byte, 16)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					predBlock[y*4+x] = pred[(subY*4+y)*16+subX*4+x]
				}
			}
			reconBlock := make([]byte, 16)
			idct4x4(&dct, predBlock, 4, reconBlock, 4)

			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					recon.y[yOff+(subY*4+y)*recon.yStride+subX*4+x] = reconBlock[y*4+x]
				}
			}
		}
	}

	// === CHROMA (Cb and Cr) ===
	cOff := mbRow*8*yuv.cStride + mbCol*8

	// Chroma prediction references.
	aboveCb := make([]byte, 8)
	leftCb := make([]byte, 8)
	topLeftCb := byte(128)

	if haveAbove {
		copy(aboveCb, recon.cb[cOff-recon.cStride:cOff-recon.cStride+8])
	} else {
		for i := range aboveCb {
			aboveCb[i] = 127
		}
	}
	if haveLeft {
		for i := 0; i < 8; i++ {
			leftCb[i] = recon.cb[cOff+i*recon.cStride-1]
		}
	} else {
		for i := range leftCb {
			leftCb[i] = 129
		}
	}
	if haveAbove && haveLeft {
		topLeftCb = recon.cb[cOff-recon.cStride-1]
	}

	uvMode := choosePredModeUV(yuv.cb[cOff:], yuv.cStride, aboveCb, leftCb, topLeftCb, haveAbove, haveLeft)
	writeUVMode(hdrEnc, uvMode)

	predCb := make([]byte, 8*8)
	predictUV(uvMode, predCb, 8, aboveCb, leftCb, topLeftCb, haveAbove, haveLeft)

	// Chroma NZ tracking: upper 4 bits of nzMask.
	// Reference: for c := 0; c < 4; c += 2 { for y := 0; y < 2; y++ { for x := 0; x < 2; x++ { ... } } }
	// c=0: Cb (indices 0,1 for above, 0,1 for left)
	// c=2: Cr (indices 2,3 for above, 2,3 for left)
	var cLNZ [4]uint8 // left nz for chroma: [0,1]=Cb rows, [2,3]=Cr rows
	var cUNZ [4]uint8 // above nz for chroma: [0,1]=Cb cols, [2,3]=Cr cols
	for i := 0; i < 4; i++ {
		cLNZ[i] = (leftNZMask >> uint(4+i)) & 1
		cUNZ[i] = (upNZMask >> uint(4+i)) & 1
	}

	// Encode and reconstruct Cb (c=0).
	cbNZL, cbNZU := encodeAndReconChroma(dctEnc, yuv.cb[cOff:], yuv.cStride, predCb,
		recon.cb, cOff, recon.cStride, qp.uvDCQ, qp.uvACQ,
		cLNZ[0], cLNZ[1], cUNZ[0], cUNZ[1])

	// Cr prediction references.
	aboveCr := make([]byte, 8)
	leftCr := make([]byte, 8)
	topLeftCr := byte(128)

	if haveAbove {
		copy(aboveCr, recon.cr[cOff-recon.cStride:cOff-recon.cStride+8])
	} else {
		for i := range aboveCr {
			aboveCr[i] = 127
		}
	}
	if haveLeft {
		for i := 0; i < 8; i++ {
			leftCr[i] = recon.cr[cOff+i*recon.cStride-1]
		}
	} else {
		for i := range leftCr {
			leftCr[i] = 129
		}
	}
	if haveAbove && haveLeft {
		topLeftCr = recon.cr[cOff-recon.cStride-1]
	}

	predCr := make([]byte, 8*8)
	predictUV(uvMode, predCr, 8, aboveCr, leftCr, topLeftCr, haveAbove, haveLeft)

	crNZL, crNZU := encodeAndReconChroma(dctEnc, yuv.cr[cOff:], yuv.cStride, predCr,
		recon.cr, cOff, recon.cStride, qp.uvDCQ, qp.uvACQ,
		cLNZ[2], cLNZ[3], cUNZ[2], cUNZ[3])

	// Build output nzMask.
	// For "left" use: lower 4 bits = Y rightmost column, upper 4 bits = UV rightmost.
	// For "above" use: lower 4 bits = Y bottom row, upper 4 bits = UV bottom row.
	// The reference packs both into one byte (left mask and above mask are separate
	// but we store one mask per MB that serves both purposes).
	// Actually, the reference stores leftMB.nzMask separately from upMB[mbx].nzMask.
	// leftMB.nzMask = pack(lnz) | pack(cLNZ) << 4
	// upMB.nzMask = pack(unz) | pack(cUNZ) << 4
	// We return both packed into: leftMask for left use, upMask stored in upNZ[mbCol].
	// Pack nzMask: lower 4 bits = Y, upper 4 bits = UV (bit4=Cb[0], bit5=Cb[1], bit6=Cr[0], bit7=Cr[1]).
	// Left mask uses rightmost column nz per row. Above mask uses bottom row nz per column.
	outLeftNZ := yLNZMask | (cbNZL[0] << 4) | (cbNZL[1] << 5) | (crNZL[0] << 6) | (crNZL[1] << 7)
	outUpNZ := yUNZMask | (cbNZU[0] << 4) | (cbNZU[1] << 5) | (crNZU[0] << 6) | (crNZU[1] << 7)

	var outNZY2 uint8
	if y2NZ {
		outNZY2 = 1
	}

	return outLeftNZ, outUpNZ, outNZY2
}

// encodeAndReconChroma encodes 4 chroma sub-blocks and writes reconstructed pixels.
// Returns ([2]uint8 leftNZ, [2]uint8 upNZ) for the 2 rows and 2 columns.
func encodeAndReconChroma(dctEnc *boolEncoder, src []byte, srcStride int, pred []byte,
	reconPlane []byte, reconOff, reconStride int, dcQ, acQ int16,
	leftNZ0, leftNZ1, upNZ0, upNZ1 uint8) ([2]uint8, [2]uint8) {

	var lnz [2]uint8
	var unz [2]uint8
	lnz[0] = leftNZ0
	lnz[1] = leftNZ1
	unz[0] = upNZ0
	unz[1] = upNZ1

	for subY := 0; subY < 2; subY++ {
		nz := lnz[subY]
		for subX := 0; subX < 2; subX++ {
			var residual [16]int16
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					sIdx := (subY*4+y)*srcStride + subX*4 + x
					pIdx := (subY*4+y)*8 + subX*4 + x
					residual[y*4+x] = int16(src[sIdx]) - int16(pred[pIdx])
				}
			}
			var dct [16]int16
			fdct4x4(&residual, &dct)
			quantizeBlock(&dct, dcQ, acQ)

			ctx := nzCtx(nz, unz[subX])
			hasNZ := blockHasNonZero(&dct, 0)
			encodeCoefficients(dctEnc, &dct, 2, ctx)

			if hasNZ {
				nz = 1
			} else {
				nz = 0
			}
			unz[subX] = nz

			// Reconstruct.
			dequantizeBlock(&dct, dcQ, acQ)
			predBlock := make([]byte, 16)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					predBlock[y*4+x] = pred[(subY*4+y)*8+subX*4+x]
				}
			}
			reconBlock := make([]byte, 16)
			idct4x4(&dct, predBlock, 4, reconBlock, 4)

			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					reconPlane[reconOff+(subY*4+y)*reconStride+subX*4+x] = reconBlock[y*4+x]
				}
			}
		}
		lnz[subY] = nz
	}

	return lnz, unz
}

// writeMBMode16 encodes the luma 16x16 prediction mode.
func writeMBMode16(enc *boolEncoder, mode int) {
	// Signal 16x16 prediction mode (reference: d.usePredY16 = d.fp.readBit(145))
	enc.writeBool(145, true)

	// Encode which 16x16 mode using the flat tree matching the reference decoder
	// (golang.org/x/image/vp8/pred.go parsePredModeY16):
	//   156=false, 163=false → DC
	//   156=false, 163=true  → V
	//   156=true,  128=false → H
	//   156=true,  128=true  → TM
	switch mode {
	case predDC16:
		enc.writeBool(156, false)
		enc.writeBool(163, false)
	case predV16:
		enc.writeBool(156, false)
		enc.writeBool(163, true)
	case predH16:
		enc.writeBool(156, true)
		enc.writeBool(128, false)
	case predTM16:
		enc.writeBool(156, true)
		enc.writeBool(128, true)
	}
}

// writeUVMode encodes the chroma prediction mode.
func writeUVMode(enc *boolEncoder, mode int) {
	p := keyFrameUVModeProbs
	if mode == predDCUV {
		enc.writeBool(p[0], false)
	} else {
		enc.writeBool(p[0], true)
		if mode == predVUV {
			enc.writeBool(p[1], false)
		} else {
			enc.writeBool(p[1], true)
			if mode == predHUV {
				enc.writeBool(p[2], false)
			} else {
				enc.writeBool(p[2], true)
			}
		}
	}
}

// choosePredModeUV selects the best chroma prediction mode for an 8x8 block.
func choosePredModeUV(src []byte, stride int, above, left []byte, topLeft byte, haveAbove, haveLeft bool) int {
	pred := make([]byte, 8*8)
	best := predDCUV
	bestSAD := 1<<31 - 1

	for mode := 0; mode < numPredModesUV; mode++ {
		if mode == predVUV && !haveAbove {
			continue
		}
		if mode == predHUV && !haveLeft {
			continue
		}
		if mode == predTMUV && (!haveAbove || !haveLeft) {
			continue
		}
		predictUV(mode, pred, 8, above, left, topLeft, haveAbove, haveLeft)
		s := 0
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				d := int(src[y*stride+x]) - int(pred[y*8+x])
				if d < 0 {
					d = -d
				}
				s += d
			}
		}
		if s < bestSAD {
			bestSAD = s
			best = mode
		}
	}
	return best
}

// predictUV fills an 8x8 chroma block using the given mode.
func predictUV(mode int, dst []byte, stride int, above, left []byte, topLeft byte, haveAbove, haveLeft bool) {
	switch mode {
	case predDCUV:
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
	case predVUV:
		for y := 0; y < 8; y++ {
			copy(dst[y*stride:y*stride+8], above[:8])
		}
	case predHUV:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				dst[y*stride+x] = left[y]
			}
		}
	case predTMUV:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				v := int(above[x]) + int(left[y]) - int(topLeft)
				dst[y*stride+x] = clampByte(v)
			}
		}
	}
}
