package vp8l

import "errors"

// transformType enumerates VP8L transform types.
type transformType int

const (
	transformPredictor    transformType = 0
	transformColor        transformType = 1
	transformSubtractGreen transformType = 2
	transformColorIndexing transformType = 3
)

// transform holds a decoded transform.
type transform struct {
	kind transformType
	// For predictor and color transforms:
	bits  int      // block size bits (block = 1 << bits)
	data  []uint32 // transform data (predictor modes / color multipliers)
	// For color indexing:
	palette     []uint32
	paletteSize int
}

// readTransform reads a single transform from the bitstream.
func readTransform(br *bitReader, width, height int) (*transform, int, error) {
	kindBits, err := br.readBits(2)
	if err != nil {
		return nil, width, err
	}
	t := &transform{kind: transformType(kindBits)}

	switch t.kind {
	case transformPredictor, transformColor:
		bits, err := br.readBits(3)
		if err != nil {
			return nil, width, err
		}
		t.bits = int(bits) + 2
		bw := subSampleSize(width, t.bits)
		bh := subSampleSize(height, t.bits)
		data, err := decodeEntropyImageLevel(br, bw, bh, false)
		if err != nil {
			return nil, width, err
		}
		t.data = data

	case transformSubtractGreen:
		// No extra data needed.

	case transformColorIndexing:
		palSize, err := br.readBits(8)
		if err != nil {
			return nil, width, err
		}
		t.paletteSize = int(palSize) + 1
		// Decode palette as a 1×paletteSize image.
		palData, err := decodeEntropyImageLevel(br, t.paletteSize, 1, false)
		if err != nil {
			return nil, width, err
		}
		// Palette colors are stored as deltas; decode.
		t.palette = make([]uint32, t.paletteSize)
		t.palette[0] = palData[0]
		for i := 1; i < t.paletteSize; i++ {
			t.palette[i] = addARGB(t.palette[i-1], palData[i])
		}
		// Compute bits needed to hold palette indices.
		xbits := 0
		if t.paletteSize <= 2 {
			xbits = 3
		} else if t.paletteSize <= 4 {
			xbits = 2
		} else if t.paletteSize <= 16 {
			xbits = 1
		}
		// New effective width after packing.
		width = subSampleSize(width, xbits)
		t.bits = xbits

	default:
		return nil, width, errors.New("vp8l: unknown transform type")
	}

	return t, width, nil
}

// subSampleSize computes ceil(size / (1 << bits)).
func subSampleSize(size, bits int) int {
	if bits == 0 {
		return size
	}
	return (size + (1<<uint(bits) - 1)) >> uint(bits)
}

// addARGB adds two ARGB values component-wise (mod 256).
func addARGB(a, b uint32) uint32 {
	return uint32((a+b)&0xff) |
		uint32(((a>>8)+(b>>8))&0xff)<<8 |
		uint32(((a>>16)+(b>>16))&0xff)<<16 |
		uint32(((a>>24)+(b>>24))&0xff)<<24
}

// --- Inverse transforms ---

// inverseTransforms applies all transforms in reverse order.
func inverseTransforms(pixels []uint32, transforms []*transform, width, height int) []uint32 {
	for i := len(transforms) - 1; i >= 0; i-- {
		t := transforms[i]
		switch t.kind {
		case transformSubtractGreen:
			inverseSubtractGreen(pixels)
		case transformColor:
			inverseColorTransform(pixels, t, width, height)
		case transformPredictor:
			inversePredictorTransform(pixels, t, width, height)
		case transformColorIndexing:
			pixels = inverseColorIndexing(pixels, t, width, height)
			width = t.paletteSize // not quite, but after unpacking we have original width
		}
	}
	return pixels
}

// inverseSubtractGreen adds the green channel back to red and blue.
func inverseSubtractGreen(pixels []uint32) {
	for i, p := range pixels {
		g := (p >> 8) & 0xff
		r := (p>>16)&0xff + g
		b := p&0xff + g
		pixels[i] = (p & 0xff00ff00) | ((r & 0xff) << 16) | (b & 0xff)
	}
}

// inverseColorTransform reverses the color transform.
func inverseColorTransform(pixels []uint32, t *transform, width, height int) {
	blockSize := 1 << uint(t.bits)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bx := x >> uint(t.bits)
			by := y >> uint(t.bits)
			bw := subSampleSize(width, t.bits)
			m := t.data[by*bw+bx]
			p := pixels[y*width+x]
			g := int32((p >> 8) & 0xff)
			r := int32((p >> 16) & 0xff)
			b := int32(p & 0xff)
			a := int32((p >> 24) & 0xff)

			gToR := int32(int8((m >> 16) & 0xff))
			gToB := int32(int8(m & 0xff))
			rToB := int32(int8((m >> 8) & 0xff))

			r += (gToR * g) >> 5
			b += (gToB * g) >> 5
			b += (rToB * r) >> 5

			pixels[y*width+x] = uint32(a)<<24 | uint32(r&0xff)<<16 | uint32(g&0xff)<<8 | uint32(b&0xff)
		}
	}
	_ = blockSize
}

// inversePredictorTransform reverses the predictor transform.
func inversePredictorTransform(pixels []uint32, t *transform, width, height int) {
	// The first pixel (top-left) is stored as-is.
	// Left column: predicted from pixel above (mode 0 = 0xff000000).
	// Top row: predicted from left (mode 0).
	blockSize := 1 << uint(t.bits)
	bw := subSampleSize(width, t.bits)
	_ = blockSize

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if y == 0 && x == 0 {
				continue
			}
			var pred uint32
			if y == 0 {
				pred = pixels[y*width+x-1]
			} else if x == 0 {
				pred = pixels[(y-1)*width]
			} else {
				bx := x >> uint(t.bits)
				by := y >> uint(t.bits)
				mode := int((t.data[by*bw+bx] >> 8) & 0xff)
				left := pixels[y*width+x-1]
				top := pixels[(y-1)*width+x]
				topLeft := pixels[(y-1)*width+x-1]
				topRight := uint32(0)
				if x+1 < width {
					topRight = pixels[(y-1)*width+x+1]
				} else {
					topRight = pixels[(y-1)*width+x]
				}
				pred = predict(mode, left, top, topLeft, topRight)
			}
			pixels[y*width+x] = addARGB(pixels[y*width+x], pred)
		}
	}
}

// predict computes the predicted pixel value for the given predictor mode.
func predict(mode int, left, top, topLeft, topRight uint32) uint32 {
	switch mode {
	case 0:
		return 0xff000000
	case 1:
		return left
	case 2:
		return top
	case 3:
		return topRight
	case 4:
		return topLeft
	case 5:
		return average2(average2(left, topRight), top)
	case 6:
		return average2(left, topLeft)
	case 7:
		return average2(left, top)
	case 8:
		return average2(topLeft, top)
	case 9:
		return average2(top, topRight)
	case 10:
		return average2(average2(left, topLeft), average2(top, topRight))
	case 11:
		return select_(left, top, topLeft)
	case 12:
		return clampedAddSubtractFull(left, top, topLeft)
	case 13:
		return clampedAddSubtractHalf(average2(left, top), topLeft)
	default:
		return left
	}
}

func average2(a, b uint32) uint32 {
	return (a>>1)&0x7f7f7f7f + (b>>1)&0x7f7f7f7f + (a & b & 0x01010101)
}

func select_(left, top, topLeft uint32) uint32 {
	// Select left or top based on which is closer to topLeft.
	dL := colorDist(topLeft, left)
	dT := colorDist(topLeft, top)
	if dL < dT {
		return left
	}
	return top
}

func colorDist(a, b uint32) int {
	diff := func(x, y uint32) int {
		d := int(x&0xff) - int(y&0xff)
		if d < 0 {
			d = -d
		}
		return d
	}
	return diff(a>>24, b>>24) + diff(a>>16, b>>16) + diff(a>>8, b>>8) + diff(a, b)
}

func clampByte(v int32) uint32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint32(v)
}

func clampedAddSubtractFull(a, b, c uint32) uint32 {
	comp := func(x, y, z uint32, shift uint) uint32 {
		xv := int32((x >> shift) & 0xff)
		yv := int32((y >> shift) & 0xff)
		zv := int32((z >> shift) & 0xff)
		return clampByte(xv + yv - zv)
	}
	return comp(a, b, c, 24)<<24 | comp(a, b, c, 16)<<16 | comp(a, b, c, 8)<<8 | comp(a, b, c, 0)
}

func clampedAddSubtractHalf(a, b uint32) uint32 {
	comp := func(x, y uint32, shift uint) uint32 {
		xv := int32((x >> shift) & 0xff)
		yv := int32((y >> shift) & 0xff)
		return clampByte(xv + (xv-yv)/2)
	}
	return comp(a, b, 24)<<24 | comp(a, b, 16)<<16 | comp(a, b, 8)<<8 | comp(a, b, 0)
}

// inverseColorIndexing expands palette-indexed pixels to ARGB.
func inverseColorIndexing(pixels []uint32, t *transform, width, height int) []uint32 {
	// Each pixel in `pixels` contains packed palette indices.
	// xbits tells how many indices are packed per uint32.
	xbits := t.bits
	pixPerUnit := 1 << uint(xbits)
	mask := uint32((1 << (8 / uint(pixPerUnit))) - 1)
	_ = mask

	origWidth := width << uint(xbits)
	result := make([]uint32, origWidth*height)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			packed := pixels[y*width+x]
			for k := 0; k < pixPerUnit; k++ {
				ox := x*pixPerUnit + k
				if ox >= origWidth {
					break
				}
				shift := uint(k) * (8 / uint(pixPerUnit))
				idx := int((packed >> (8 * shift)) & uint32((1<<(8/uint(pixPerUnit)))-1))
				if idx < len(t.palette) {
					result[y*origWidth+ox] = t.palette[idx]
				}
			}
		}
	}
	return result
}

// --- Forward transforms (encoder side) ---

// applySubtractGreen applies the subtract green transform.
func applySubtractGreen(pixels []uint32) {
	for i, p := range pixels {
		g := (p >> 8) & 0xff
		r := ((p >> 16) & 0xff) - g
		b := (p & 0xff) - g
		pixels[i] = (p & 0xff00ff00) | ((r & 0xff) << 16) | (b & 0xff)
	}
}

// applyColorTransform applies the forward color (cross-color) transform.
// For each block it estimates per-block correlation coefficients gToR, gToB, rToB
// and subtracts the predicted channel contributions.
// Returns the transformed pixel array and the color-transform sub-image.
// The sub-image pixel format (per the WebP spec) is:
//
//	A=255, R=gToR (signed 8-bit as uint8), G=rToB, B=gToB
func applyColorTransform(pixels []uint32, width, height, bits int) ([]uint32, []uint32) {
	blockSize := 1 << uint(bits)
	colorW := subSampleSize(width, bits)
	colorH := subSampleSize(height, bits)

	colorImage := make([]uint32, colorW*colorH)
	result := make([]uint32, len(pixels))
	copy(result, pixels)

	for by := 0; by < colorH; by++ {
		for bx := 0; bx < colorW; bx++ {
			x0 := bx * blockSize
			y0 := by * blockSize
			x1 := x0 + blockSize
			y1 := y0 + blockSize
			if x1 > width {
				x1 = width
			}
			if y1 > height {
				y1 = height
			}

			// Accumulate sums for least-squares coefficient estimation.
			// gToR: minimize sum((r - gToR*g/32)^2) => gToR = 32*sum(g*r)/sum(g*g)
			// gToB: minimize sum((b - gToB*g/32)^2) => gToB = 32*sum(g*b)/sum(g*g)
			// rToB: uses r' (post gToR transform) to minimize sum((b' - rToB*r'/32)^2)
			var sumGG, sumGR, sumGB int64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					p := pixels[y*width+x]
					g := int64((p >> 8) & 0xff)
					r := int64((p >> 16) & 0xff)
					b := int64(p & 0xff)
					sumGG += g * g
					sumGR += g * r
					sumGB += g * b
				}
			}

			var gToR, gToB int8
			if sumGG > 0 {
				gToR = clampToInt8(32 * sumGR / sumGG)
				gToB = clampToInt8(32 * sumGB / sumGG)
			}

			// Compute rToB using rDec values, which mirror what the decoder sees:
			// rDec = int32(uint8(r - (gToR*g)>>5)) + (gToR*g)>>5
			var sumRR, sumRB int64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					p := pixels[y*width+x]
					g := int64((p >> 8) & 0xff)
					r := int64((p >> 16) & 0xff)
					b := int64(p & 0xff)
					gCorr := (int64(gToR) * g) >> 5
					rDec := int64(int32(uint8(r-gCorr))) + gCorr
					bResid := b - (int64(gToB)*g)>>5
					sumRR += rDec * rDec
					sumRB += rDec * bResid
				}
			}

			var rToB int8
			if sumRR > 0 {
				rToB = clampToInt8(32 * sumRB / sumRR)
			}

			// Pack coefficients into ARGB: A=255, R=gToR, G=rToB, B=gToB
			colorImage[by*colorW+bx] = 0xff000000 |
				uint32(uint8(gToR))<<16 |
				uint32(uint8(rToB))<<8 |
				uint32(uint8(gToB))

			// Apply forward transform to all pixels in this block.
			// The decoder reads rStored as an unsigned 8-bit value, adds (gToR*g)>>5
			// without masking, then uses that unmasked value for the rToB correction.
			// We must mirror that arithmetic exactly in the forward direction.
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					p := pixels[y*width+x]
					g := int32((p >> 8) & 0xff)
					r := int32((p >> 16) & 0xff)
					b := int32(p & 0xff)
					a := int32((p >> 24) & 0xff)

					// rStored is what will be written (masked to 8 bits).
					rStored := int32(uint8(r - (int32(gToR)*g)>>5))
					// rDec mirrors what the decoder computes: stored uint8 + gToR correction.
					rDec := rStored + (int32(gToR)*g)>>5
					bPrime := b - (int32(gToB)*g)>>5 - (int32(rToB)*rDec)>>5

					result[y*width+x] = uint32(a)<<24 |
						uint32(rStored&0xff)<<16 |
						uint32(g&0xff)<<8 |
						uint32(bPrime&0xff)
				}
			}
		}
	}

	return result, colorImage
}

// clampToInt8 clamps a value to the signed 8-bit range [-128, 127].
func clampToInt8(v int64) int8 {
	if v < -128 {
		return -128
	}
	if v > 127 {
		return 127
	}
	return int8(v)
}

// applyColorIndexing applies the color indexing transform (if <= 256 unique colors).
// Returns transformed pixels, palette, and success flag.
func applyColorIndexing(pixels []uint32, width, height int) ([]uint32, []uint32, bool) {
	// Collect unique colors.
	seen := make(map[uint32]int)
	for _, p := range pixels {
		if _, ok := seen[p]; !ok {
			if len(seen) > 256 {
				return nil, nil, false
			}
			seen[p] = len(seen)
		}
	}
	if len(seen) > 256 {
		return nil, nil, false
	}

	palette := make([]uint32, len(seen))
	for col, idx := range seen {
		palette[idx] = col
	}

	// Replace pixels with palette indices (in green channel).
	indexed := make([]uint32, len(pixels))
	for i, p := range pixels {
		indexed[i] = uint32(seen[p]) << 8
	}

	return indexed, palette, true
}
