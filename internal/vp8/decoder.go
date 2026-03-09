package vp8

import (
	"errors"
	"fmt"
	"image"
)

// Decode decodes a VP8 bitstream from data and returns the decoded image.
// This is the main entry point for VP8 lossy decoding (RFC 6386).
func Decode(data []byte) (image.Image, error) {
	d := &decoder{data: data}
	return d.decode()
}

// DecodeConfig returns image dimensions without fully decoding.
func DecodeConfig(data []byte) (image.Config, error) {
	if len(data) < 10 {
		return image.Config{}, errors.New("vp8: data too short")
	}
	fh, _, err := parseFrameHeader(data)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: image.NewYCbCr(image.Rectangle{}, image.YCbCrSubsampleRatio420).ColorModel(),
		Width:      int(fh.width),
		Height:     int(fh.height),
	}, nil
}

// decoder holds state for VP8 decoding.
type decoder struct {
	data []byte
	fh   *frameHeader
	ph   *parsedHeader

	// quantizer parameters per segment (4 segments max).
	segQuant [4]segmentQuant

	// DCT partition readers (up to 8).
	parts []*boolDecoder

	// Reconstructed frame.
	frame *frame

	// Per-MB filter params (populated during decode, used by loop filter).
	perMBFilterParams []filterParam

	// Coefficient token probabilities (updated per-frame from defaults).
	coeffProbs [4][8][3][11]uint8

	// Non-zero coefficient context tracking (matching reference decoder).
	// upNZMask[col] = nzMask for the MB above (low 4 bits = Y, high 4 bits = chroma).
	// upNZY2[col] = nzY2 for the MB above.
	upNZMask []uint8
	upNZY2   []uint8

	// 4x4 prediction mode context (reference mode indices).
	// upPred[col] has 4 modes for the bottom row of the MB above column col.
	// leftPred has 4 modes for the right column of the MB to the left.
	upPred   [][4]uint8
	leftPred [4]uint8
}

// segmentQuant holds dequantization multipliers for one segment.
type segmentQuant struct {
	yDCQ  int16
	yACQ  int16
	y2DCQ int16
	y2ACQ int16
	uvDCQ int16
	uvACQ int16
}

func (d *decoder) decode() (image.Image, error) {
	// 1. Parse uncompressed frame header.
	fh, firstPartOffset, err := parseFrameHeader(d.data)
	if err != nil {
		return nil, fmt.Errorf("vp8: frame header: %w", err)
	}
	d.fh = fh

	if !fh.keyFrame {
		return nil, errors.New("vp8: inter-frame (non-keyframe) not supported")
	}

	// 2. Parse compressed header (first partition).
	bd, err := newBoolDecoder(d.data, firstPartOffset)
	if err != nil {
		return nil, fmt.Errorf("vp8: first partition: %w", err)
	}

	d.coeffProbs = defaultCoeffProbs
	ph, err := parseCompressedHeader(bd, fh, &d.coeffProbs)
	if err != nil {
		return nil, fmt.Errorf("vp8: compressed header: %w", err)
	}
	d.ph = ph

	// 3. Compute quantizer parameters per segment.
	d.buildQuantTables()

	// 4. Locate DCT partitions.
	if err := d.locatePartitions(firstPartOffset); err != nil {
		return nil, fmt.Errorf("vp8: partitions: %w", err)
	}

	// 5. Allocate frame buffer.
	d.frame = newFrame(int(fh.width), int(fh.height))

	// 6. Decode macroblocks.
	if err := d.decodeMacroblocks(); err != nil {
		return nil, fmt.Errorf("vp8: macroblock decode: %w", err)
	}

	// 7. Apply loop filter (no-op if level == 0).
	d.applyLoopFilter()

	return d.frame.toYCbCr(), nil
}

// buildQuantTables builds per-segment dequantization tables.
func (d *decoder) buildQuantTables() {
	for seg := 0; seg < 4; seg++ {
		baseQP := d.ph.quant.yACQI
		if d.ph.segment.enabled && d.ph.segment.updateData {
			delta := int(d.ph.segment.quantDelta[seg])
			if d.ph.segment.absoluteValues {
				baseQP = delta
			} else {
				baseQP += delta
			}
		}
		baseQP = clampQP(baseQP)

		yDCQP := clampQP(baseQP + d.ph.quant.yDCDelta)
		y2DCQP := clampQP(baseQP + d.ph.quant.y2DCDelta)
		y2ACQP := clampQP(baseQP + d.ph.quant.y2ACDelta)
		uvDCQP := clampQP(baseQP + d.ph.quant.uvDCDelta)
		uvACQP := clampQP(baseQP + d.ph.quant.uvACDelta)

		uvDCIdx := uvDCQP
		if uvDCIdx > 117 {
			uvDCIdx = 117
		}
		d.segQuant[seg] = segmentQuant{
			yDCQ:  dcTable[yDCQP],
			yACQ:  acTable[baseQP],
			y2DCQ: dcTable[y2DCQP] * 2,
			y2ACQ: acTable[y2ACQP] * 155 / 100,
			uvDCQ: dcTable[uvDCIdx],
			uvACQ: acTable[uvACQP],
		}
		if d.segQuant[seg].y2ACQ < 8 {
			d.segQuant[seg].y2ACQ = 8
		}
	}
}

// locatePartitions finds the byte offsets of DCT data partitions.
// RFC 6386 §9.5: after first partition, partition sizes are listed.
func (d *decoder) locatePartitions(firstPartOffset int) error {
	numParts := d.ph.numParts

	// The first partition ends at firstPartOffset + firstPartSize bytes from
	// start of the VP8 bitstream.
	firstPartEnd := firstPartOffset + int(d.fh.firstPartSize)
	if firstPartEnd > len(d.data) {
		return errors.New("vp8: first partition extends beyond data")
	}

	if numParts == 1 {
		// Single DCT partition immediately follows the first partition.
		bd, err := newBoolDecoder(d.data, firstPartEnd)
		if err != nil {
			return err
		}
		d.parts = []*boolDecoder{bd}
		return nil
	}

	// Multiple partitions: the sizes of all but the last are listed
	// as 3-byte little-endian values immediately after the first partition.
	sizesOffset := firstPartEnd
	sizesLen := (numParts - 1) * 3
	if sizesOffset+sizesLen > len(d.data) {
		return errors.New("vp8: partition size table extends beyond data")
	}

	partOffsets := make([]int, numParts)
	partOffsets[0] = sizesOffset + sizesLen
	for i := 0; i < numParts-1; i++ {
		sz := int(d.data[sizesOffset+i*3]) |
			int(d.data[sizesOffset+i*3+1])<<8 |
			int(d.data[sizesOffset+i*3+2])<<16
		partOffsets[i+1] = partOffsets[i] + sz
	}

	d.parts = make([]*boolDecoder, numParts)
	for i := 0; i < numParts; i++ {
		bd, err := newBoolDecoder(d.data, partOffsets[i])
		if err != nil {
			return fmt.Errorf("vp8: partition %d: %w", i, err)
		}
		d.parts[i] = bd
	}
	return nil
}

// decodeMacroblocks decodes all macroblocks row by row.
func (d *decoder) decodeMacroblocks() error {
	ph := d.ph
	mbW := ph.mbWidth
	mbH := ph.mbHeight
	numParts := ph.numParts

	// First-partition bool decoder (for MB headers).
	firstPartOffset := 3 // frame tag
	if d.fh.keyFrame {
		firstPartOffset += 10 // start code (3) + width/height (4) but actually 3+4=7, plus 3 tag = start at 3+7=10
	}
	headerBD, err := newBoolDecoder(d.data, firstPartOffset)
	if err != nil {
		return err
	}
	// Re-parse to get past compressed header — we need a fresh bool decoder
	// positioned right after the frame tag for the first partition.
	// The first partition is at offset = frame tag offset = 3 (non-keyframe) or 10 (keyframe).
	// But firstPartSize tells us the size of the first partition.
	firstPartStart := 3
	if d.fh.keyFrame {
		firstPartStart = 10
	}
	headerBD, err = newBoolDecoder(d.data, firstPartStart)
	if err != nil {
		return err
	}
	// Skip past the compressed header we already parsed.
	// We do this by re-parsing it (the bool decoder state advances).
	var dummy [4][8][3][11]uint8
	_, err = parseCompressedHeader(headerBD, d.fh, &dummy)
	if err != nil {
		return fmt.Errorf("vp8: re-parsing compressed header: %w", err)
	}

	// Initialize non-zero context tracking.
	d.upNZMask = make([]uint8, mbW)
	d.upNZY2 = make([]uint8, mbW)
	// Initialize prediction mode context.
	d.upPred = make([][4]uint8, mbW)

	// Precompute filter params table and allocate per-MB storage.
	filterTable := d.computeFilterParamsTable()
	d.perMBFilterParams = make([]filterParam, mbW*mbH)

	for mbY := 0; mbY < mbH; mbY++ {
		// Choose DCT partition for this row (matching reference: mby & (nOP-1)).
		partIdx := mbY & (numParts - 1)
		dctBD := d.parts[partIdx]

		var leftNZMask uint8
		var leftNZY2 uint8
		d.leftPred = [4]uint8{}

		for mbX := 0; mbX < mbW; mbX++ {
			newLeftNZ, newLeftY2, err := d.decodeMB(headerBD, dctBD, mbX, mbY,
				leftNZMask, d.upNZMask[mbX], leftNZY2, d.upNZY2[mbX],
				&filterTable)
			if err != nil {
				return fmt.Errorf("vp8: MB(%d,%d): %w", mbX, mbY, err)
			}
			leftNZMask = newLeftNZ
			leftNZY2 = newLeftY2
		}
	}
	return nil
}

// unpackNZ unpacks 4 bits of a nzMask into individual 0/1 values.
func unpackNZ(mask uint8) [4]uint8 {
	return [4]uint8{
		(mask >> 0) & 1,
		(mask >> 1) & 1,
		(mask >> 2) & 1,
		(mask >> 3) & 1,
	}
}

// packNZ packs 4 individual 0/1 values into bits of a uint8.
func packNZ(vals [4]uint8) uint8 {
	return vals[0] | vals[1]<<1 | vals[2]<<2 | vals[3]<<3
}

// decodeMB decodes one macroblock with proper non-zero context tracking.
// Returns (leftNZMask, leftNZY2, error).
func (d *decoder) decodeMB(headerBD, dctBD *boolDecoder,
	mbX, mbY int, leftNZMask, upNZMask, leftNZY2, upNZY2 uint8,
	filterTable *[4][2]filterParam) (uint8, uint8, error) {

	ph := d.ph
	seg := 0

	// Read segment ID if segmentation map is present.
	// RFC 6386 §9.3: binary tree — always reads exactly 2 bits.
	// Match reference: golang.org/x/image/vp8/reconstruct.go reconstruct().
	if ph.segment.enabled && ph.segment.updateMap {
		if !headerBD.ReadBool(ph.segment.prob[0]) {
			// Left branch: segment 0 or 1
			if headerBD.ReadBool(ph.segment.prob[1]) {
				seg = 1
			} else {
				seg = 0
			}
		} else {
			// Right branch: segment 2 or 3
			if headerBD.ReadBool(ph.segment.prob[2]) {
				seg = 3
			} else {
				seg = 2
			}
		}
	}
	q := d.segQuant[seg]

	// mb_skip_coeff: only read if useSkipProb is set in header.
	skipCoeff := false
	if ph.useSkipProb {
		skipCoeff = headerBD.ReadBool(ph.skipProb)
	}
	// Intra prediction modes.
	var mode16 int
	var mode4 [16]int
	var modeUV int

	// Luma mode (RFC 6386 §11.2, keyframe).
	// First bit (prob 145): true = Y16 mode, false = B_PRED (4x4).
	if headerBD.ReadBool(145) {
		// Y16 mode: flat tree matching reference (golang.org/x/image/vp8/pred.go parsePredModeY16).
		var refMode uint8
		if !headerBD.ReadBool(156) {
			if !headerBD.ReadBool(163) {
				mode16 = predDC16
				refMode = refPredDC
			} else {
				mode16 = predV16
				refMode = refPredVE
			}
		} else if !headerBD.ReadBool(128) {
			mode16 = predH16
			refMode = refPredHE
		} else {
			mode16 = predTM16
			refMode = refPredTM
		}
		// Update mode context: all 4 slots set to this mode.
		for i := 0; i < 4; i++ {
			d.upPred[mbX][i] = refMode
			d.leftPred[i] = refMode
		}
	} else {
		// B_PRED: 4x4 sub-block modes with context-dependent probabilities.
		// Matches reference: golang.org/x/image/vp8/pred.go parsePredModeY4.
		mode16 = -1
		for j := 0; j < 4; j++ {
			p := d.leftPred[j]
			for i := 0; i < 4; i++ {
				above := d.upPred[mbX][i]
				prob := &predProb[above][p]
				var refMode uint8
				if !headerBD.ReadBool(prob[0]) {
					refMode = refPredDC
				} else if !headerBD.ReadBool(prob[1]) {
					refMode = refPredTM
				} else if !headerBD.ReadBool(prob[2]) {
					refMode = refPredVE
				} else if !headerBD.ReadBool(prob[3]) {
					if !headerBD.ReadBool(prob[4]) {
						refMode = refPredHE
					} else if !headerBD.ReadBool(prob[5]) {
						refMode = refPredRD
					} else {
						refMode = refPredVR
					}
				} else if !headerBD.ReadBool(prob[6]) {
					refMode = refPredLD
				} else if !headerBD.ReadBool(prob[7]) {
					refMode = refPredVL
				} else if !headerBD.ReadBool(prob[8]) {
					refMode = refPredHD
				} else {
					refMode = refPredHU
				}
				mode4[j*4+i] = refToOurMode[refMode]
				p = refMode
				d.upPred[mbX][i] = refMode
			}
			d.leftPred[j] = p
		}
	}

	// Chroma mode.
	{
		p := keyFrameUVModeProbs
		if !headerBD.ReadBool(p[0]) {
			modeUV = predDCUV
		} else if !headerBD.ReadBool(p[1]) {
			modeUV = predVUV
		} else if !headerBD.ReadBool(p[2]) {
			modeUV = predHUV
		} else {
			modeUV = predTMUV
		}
	}

	// Decode DCT coefficients with proper non-zero context tracking.
	var yCoeffs [16][16]int16
	var y2Coeffs *[16]int16
	var cbCoeffs, crCoeffs [4][16]int16

	// nzY2 context: the reference only modifies nzY16 when the current MB
	// is Y16 mode. For B_PRED MBs, the Y2 NZ context is preserved from the
	// last Y16 MB at that position. Default to the previous values.
	newNZY2 := leftNZY2
	var newLeftNZMask uint8

	if !skipCoeff {
		usePredY16 := mode16 >= 0
		plane := 3 // Y1SansY2 (B_PRED)
		if usePredY16 {
			plane = 0 // Y1WithY2
		}

		// Y2 block (if 16x16 mode).
		if usePredY16 {
			y2Coeffs = new([16]int16)
			y2Probs := &d.coeffProbs[1]
			nz := decodeResiduals4(dctBD, y2Probs, leftNZY2+upNZY2, q.y2DCQ, q.y2ACQ, false, y2Coeffs)
			newNZY2 = nz
			d.upNZY2[mbX] = nz
		}

		// Y blocks: 4 rows of 4 columns, tracking nz per column and row.
		lnz := unpackNZ(leftNZMask & 0x0f)
		unz := unpackNZ(upNZMask & 0x0f)
		yProbs := &d.coeffProbs[plane]

		for y := 0; y < 4; y++ {
			nz := lnz[y]
			for x := 0; x < 4; x++ {
				blk := y*4 + x
				nz = decodeResiduals4(dctBD, yProbs, nz+unz[x], q.yDCQ, q.yACQ, usePredY16, &yCoeffs[blk])
				unz[x] = nz
			}
			lnz[y] = nz
		}

		// Chroma blocks: Cb (4 blocks as 2x2), then Cr (4 blocks as 2x2).
		clnz := unpackNZ(leftNZMask >> 4)
		cunz := unpackNZ(upNZMask >> 4)
		uvProbs := &d.coeffProbs[2]

		// Cb blocks.
		for cy := 0; cy < 2; cy++ {
			nz := clnz[cy]
			for cx := 0; cx < 2; cx++ {
				blk := cy*2 + cx
				nz = decodeResiduals4(dctBD, uvProbs, nz+cunz[cx], q.uvDCQ, q.uvACQ, false, &cbCoeffs[blk])
				cunz[cx] = nz
			}
			clnz[cy] = nz
		}
		// Cr blocks.
		for cy := 0; cy < 2; cy++ {
			nz := clnz[cy+2]
			for cx := 0; cx < 2; cx++ {
				blk := cy*2 + cx
				nz = decodeResiduals4(dctBD, uvProbs, nz+cunz[cx+2], q.uvDCQ, q.uvACQ, false, &crCoeffs[blk])
				cunz[cx+2] = nz
			}
			clnz[cy+2] = nz
		}

		newLeftNZMask = packNZ(lnz) | (packNZ(clnz) << 4)
		d.upNZMask[mbX] = packNZ(unz) | (packNZ(cunz) << 4)
	} else {
		// Skip: all coefficients are zero, reset nz state.
		// Y2 NZ context is only reset for Y16 mode (matching reference).
		if mode16 >= 0 {
			newNZY2 = 0
			d.upNZY2[mbX] = 0
		}
		newLeftNZMask = 0
		d.upNZMask[mbX] = 0
	}

	// Reconstruct macroblock into frame.
	reconstructMB(d.frame, mbX, mbY, mode16, mode4, modeUV,
		yCoeffs, y2Coeffs, cbCoeffs, crCoeffs)

	// Store per-MB filter params (matching reference: filterParams[segment][btou(!usePredY16)]).
	if filterTable != nil {
		modeIdx := 0 // Y16
		if mode16 < 0 {
			modeIdx = 1 // B_PRED
		}
		fs := filterTable[seg][modeIdx]
		// inner = inner || !skip (reference line 378)
		fs.inner = fs.inner || !skipCoeff
		d.perMBFilterParams[mbY*d.ph.mbWidth+mbX] = fs
	}

	return newLeftNZMask, newNZY2, nil
}

// decode4x4Mode decodes one B_PRED 4x4 intra mode from the bool decoder.
// RFC 6386 §11.1: modes encoded with a tree.
func decode4x4Mode(bd *boolDecoder) int {
	// Tree: DC=0, TM=1, VE=2, HE=3, LD=4, RD=5, VR=6, VL=7, HD=8, HU=9
	// Simplified: use probabilities from spec Table 11-2 (B_MODE_PROB).
	// For simplicity use uniform tree.
	if !bd.ReadBool(128) {
		return predDC4 // DC
	}
	if !bd.ReadBool(128) {
		return predTM4 // TM
	}
	if !bd.ReadBool(128) {
		return predV4 // VE
	}
	if !bd.ReadBool(128) {
		return predH4 // HE
	}
	if !bd.ReadBool(128) {
		return predLD4 // LD
	}
	if !bd.ReadBool(128) {
		return predRD4 // RD
	}
	if !bd.ReadBool(128) {
		return predVR4 // VR
	}
	if !bd.ReadBool(128) {
		return predVL4 // VL
	}
	if !bd.ReadBool(128) {
		return predHD4 // HD
	}
	return predHU4 // HU
}
