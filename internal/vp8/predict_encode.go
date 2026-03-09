package vp8

// Intra prediction modes for 16x16 luma blocks.
const (
	predDC16 = iota
	predV16
	predH16
	predTM16
	numPredModes16 = 4
)

// Intra prediction modes for 4x4 luma blocks.
const (
	predDC4 = iota
	predV4
	predH4
	predTM4
	predLD4
	predRD4
	predVR4
	predVL4
	predHD4
	predHU4
	numPredModes4 = 10
)

// Intra prediction modes for chroma (UV) 8x8 blocks.
const (
	predDCUV = iota
	predVUV
	predHUV
	predTMUV
	numPredModesUV = 4
)

// predictDC16 fills a 16x16 block with the DC value derived from top/left samples.
// above is a 16-byte slice of pixels above the block; left is 16 pixels to the left.
// For edge macroblocks where neighbours are absent, a fixed value (128) is used.
func predictDC16(dst []byte, stride int, above []byte, left []byte, haveAbove, haveLeft bool) {
	sum := 0
	n := 0
	if haveAbove {
		for _, v := range above {
			sum += int(v)
		}
		n += 16
	}
	if haveLeft {
		for _, v := range left {
			sum += int(v)
		}
		n += 16
	}
	dc := byte(128)
	if n > 0 {
		dc = byte((sum + n/2) / n)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			dst[y*stride+x] = dc
		}
	}
}

// predictV16 fills a 16x16 block by copying top row downwards.
func predictV16(dst []byte, stride int, above []byte) {
	for y := 0; y < 16; y++ {
		copy(dst[y*stride:y*stride+16], above)
	}
}

// predictH16 fills a 16x16 block by extending each left pixel horizontally.
func predictH16(dst []byte, stride int, left []byte) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			dst[y*stride+x] = left[y]
		}
	}
}

// predictTM16 fills a 16x16 block using True Motion prediction.
func predictTM16(dst []byte, stride int, above []byte, left []byte, topLeft byte) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			v := int(above[x]) + int(left[y]) - int(topLeft)
			dst[y*stride+x] = clampByte(v)
		}
	}
}

// predict16 fills a 16x16 block with one of the four intra modes.
func predict16(mode int, dst []byte, stride int, above []byte, left []byte, topLeft byte, haveAbove, haveLeft bool) {
	switch mode {
	case predDC16:
		predictDC16(dst, stride, above, left, haveAbove, haveLeft)
	case predV16:
		predictV16(dst, stride, above)
	case predH16:
		predictH16(dst, stride, left)
	case predTM16:
		predictTM16(dst, stride, above, left, topLeft)
	}
}

// sad16 computes the Sum of Absolute Differences between a 16x16 region of src
// and a 16x16 prediction block.
func sad16(src []byte, srcStride int, pred []byte, predStride int) int {
	sum := 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			d := int(src[y*srcStride+x]) - int(pred[y*predStride+x])
			if d < 0 {
				d = -d
			}
			sum += d
		}
	}
	return sum
}

// choosePredMode16 evaluates all numPredModes16 modes and returns the one
// with the lowest SAD against src.
func choosePredMode16(src []byte, srcStride int, above []byte, left []byte, topLeft byte, haveAbove, haveLeft bool) int {
	pred := make([]byte, 16*16)
	bestMode := predDC16
	bestSAD := 1<<31 - 1
	for mode := 0; mode < numPredModes16; mode++ {
		if mode == predV16 && !haveAbove {
			continue
		}
		if mode == predH16 && !haveLeft {
			continue
		}
		if mode == predTM16 && (!haveAbove || !haveLeft) {
			continue
		}
		predict16(mode, pred, 16, above, left, topLeft, haveAbove, haveLeft)
		s := sad16(src, srcStride, pred, 16)
		if s < bestSAD {
			bestSAD = s
			bestMode = mode
		}
	}
	return bestMode
}

// predictDC4 fills a 4x4 block with the DC value.
func predictDC4(dst []byte, stride int, above []byte, left []byte, haveAbove, haveLeft bool) {
	sum := 0
	n := 0
	if haveAbove {
		for _, v := range above[:4] {
			sum += int(v)
		}
		n += 4
	}
	if haveLeft {
		for _, v := range left[:4] {
			sum += int(v)
		}
		n += 4
	}
	dc := byte(128)
	if n > 0 {
		dc = byte((sum + n/2) / n)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			dst[y*stride+x] = dc
		}
	}
}

// predictV4 fills a 4x4 block from the top row.
func predictV4(dst []byte, stride int, above []byte) {
	for y := 0; y < 4; y++ {
		copy(dst[y*stride:y*stride+4], above[:4])
	}
}

// predictH4 fills a 4x4 block from the left column.
func predictH4(dst []byte, stride int, left []byte) {
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			dst[y*stride+x] = left[y]
		}
	}
}

// predictTM4 fills a 4x4 block using True Motion.
func predictTM4(dst []byte, stride int, above []byte, left []byte, topLeft byte) {
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			v := int(above[x]) + int(left[y]) - int(topLeft)
			dst[y*stride+x] = clampByte(v)
		}
	}
}

// choosePredMode4 evaluates modes for a 4x4 block (simplified: only DC, V, H, TM).
func choosePredMode4(src []byte, srcStride int, above []byte, left []byte, topLeft byte, haveAbove, haveLeft bool) int {
	pred := make([]byte, 4*4)
	type candidate struct {
		mode int
		sad  int
	}
	best := predDC4
	bestSAD := 1<<31 - 1

	modes := []int{predDC4, predV4, predH4, predTM4}
	for _, mode := range modes {
		if mode == predV4 && !haveAbove {
			continue
		}
		if mode == predH4 && !haveLeft {
			continue
		}
		if mode == predTM4 && (!haveAbove || !haveLeft) {
			continue
		}
		switch mode {
		case predDC4:
			predictDC4(pred, 4, above, left, haveAbove, haveLeft)
		case predV4:
			predictV4(pred, 4, above)
		case predH4:
			predictH4(pred, 4, left)
		case predTM4:
			predictTM4(pred, 4, above, left, topLeft)
		}
		s := 0
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				d := int(src[y*srcStride+x]) - int(pred[y*4+x])
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
