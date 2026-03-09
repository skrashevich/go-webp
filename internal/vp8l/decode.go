package vp8l

import (
	"errors"
	"image"
	"image/color"
)

const vp8lSignature = 0x2f

// DecodeVP8L decodes a VP8L lossless bitstream and returns an NRGBA image.
func DecodeVP8L(data []byte) (*image.NRGBA, error) {
	if len(data) < 5 {
		return nil, errors.New("vp8l: data too short")
	}
	if data[0] != vp8lSignature {
		return nil, errors.New("vp8l: invalid signature")
	}

	br := newBitReader(data[1:])

	// Read width (14 bits) and height (14 bits).
	wBits, err := br.readBits(14)
	if err != nil {
		return nil, err
	}
	width := int(wBits) + 1

	hBits, err := br.readBits(14)
	if err != nil {
		return nil, err
	}
	height := int(hBits) + 1

	// Alpha hint (1 bit, ignored for decoding).
	_, err = br.readBit()
	if err != nil {
		return nil, err
	}

	// Version (3 bits, must be 0).
	ver, err := br.readBits(3)
	if err != nil {
		return nil, err
	}
	if ver != 0 {
		return nil, errors.New("vp8l: unsupported version")
	}

	// Read transforms.
	var transforms []*transform
	decodeWidth := width

	for {
		hasTransform, err := br.readBit()
		if err != nil {
			return nil, err
		}
		if !hasTransform {
			break
		}
		t, newWidth, err := readTransform(br, decodeWidth, height)
		if err != nil {
			return nil, err
		}
		transforms = append(transforms, t)
		decodeWidth = newWidth
	}

	// Decode pixel data.
	pixels, err := decodeImageData(br, decodeWidth, height, width)
	if err != nil {
		return nil, err
	}

	// Apply inverse transforms.
	pixels = inverseTransformsWithWidths(pixels, transforms, decodeWidth, width, height)

	// Convert ARGB to NRGBA image.
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := pixels[y*width+x]
			a := uint8(p >> 24)
			r := uint8(p >> 16)
			g := uint8(p >> 8)
			b := uint8(p)
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}

	return img, nil
}

// inverseTransformsWithWidths applies inverse transforms tracking width changes.
// decodeWidth is the width used to decode the raw pixel data (after all forward transforms).
// origWidth is the final output width.
func inverseTransformsWithWidths(pixels []uint32, transforms []*transform, decodeWidth, origWidth, height int) []uint32 {
	if len(transforms) == 0 {
		return pixels
	}

	// Compute the width at each transform stage, going forward.
	// widths[i] = width before applying transforms[i] (i.e., the "real" width at that stage).
	// widths[len] = decodeWidth (width of encoded pixel data).
	widths := make([]int, len(transforms)+1)
	widths[0] = origWidth
	for i, t := range transforms {
		w := widths[i]
		switch t.kind {
		case transformColorIndexing:
			// Forward transform reduces width by packing.
			w = subSampleSize(w, t.bits)
		}
		widths[i+1] = w
	}
	// widths[len(transforms)] should equal decodeWidth.

	// Apply inverse transforms in reverse order.
	currentWidth := decodeWidth
	for i := len(transforms) - 1; i >= 0; i-- {
		t := transforms[i]
		targetWidth := widths[i]
		switch t.kind {
		case transformSubtractGreen:
			inverseSubtractGreen(pixels)
		case transformColor:
			inverseColorTransform(pixels, t, currentWidth, height)
		case transformPredictor:
			inversePredictorTransform(pixels, t, currentWidth, height)
		case transformColorIndexing:
			pixels = inverseColorIndexingFull(pixels, t, currentWidth, height, targetWidth)
		}
		currentWidth = targetWidth
	}
	return pixels
}

// inverseColorIndexingFull expands palette-indexed pixels to full ARGB.
func inverseColorIndexingFull(pixels []uint32, t *transform, packedWidth, height, origWidth int) []uint32 {
	xbits := t.bits
	if xbits == 0 {
		// No packing, just replace index with palette color.
		result := make([]uint32, len(pixels))
		for i, p := range pixels {
			idx := int((p >> 8) & 0xff)
			if idx < len(t.palette) {
				result[i] = t.palette[idx]
			}
		}
		return result
	}

	pixPerUnit := 1 << uint(xbits)
	bitsPerIdx := 8 / pixPerUnit
	mask := uint32((1 << uint(bitsPerIdx)) - 1)

	result := make([]uint32, origWidth*height)
	for y := 0; y < height; y++ {
		for x := 0; x < packedWidth; x++ {
			packed := (pixels[y*packedWidth+x] >> 8) & 0xff
			for k := 0; k < pixPerUnit; k++ {
				ox := x*pixPerUnit + k
				if ox >= origWidth {
					break
				}
				idx := int((packed >> uint(k*bitsPerIdx)) & mask)
				if idx < len(t.palette) {
					result[y*origWidth+ox] = t.palette[idx]
				}
			}
		}
	}
	return result
}

// decodeImageData decodes the main pixel data using Huffman + LZ77.
func decodeImageData(br *bitReader, width, height, origWidth int) ([]uint32, error) {
	// Check for color cache.
	useColorCache, err := br.readBit()
	if err != nil {
		return nil, err
	}
	var cc *colorCache
	var ccBits uint
	if useColorCache {
		bits, err := br.readBits(4)
		if err != nil {
			return nil, err
		}
		ccBits = uint(bits)
		cc = newColorCache(ccBits)
	}

	// Read Huffman trees.
	// Check for meta-Huffman (multiple Huffman groups).
	useMetaHuffman, err := br.readBit()
	if err != nil {
		return nil, err
	}

	var groups []huffGroup
	var metaData []uint32
	var metaBits uint32

	if useMetaHuffman {
		// Read meta-Huffman data: encodes which group each block uses.
		metaBits, err = br.readBits(3)
		if err != nil {
			return nil, err
		}
		metaBits += 2
		metaW := subSampleSize(width, int(metaBits))
		metaH := subSampleSize(height, int(metaBits))
		metaData, err = decodeEntropyImageLevel(br, metaW, metaH, false)
		if err != nil {
			return nil, err
		}

		// Count Huffman groups: max group index + 1.
		numGroups := 0
		for _, p := range metaData {
			gIdx := int((p >> 8) & 0xffff)
			if gIdx+1 > numGroups {
				numGroups = gIdx + 1
			}
		}

		groups = make([]huffGroup, numGroups)
		for g := 0; g < numGroups; g++ {
			grp, err := readHuffGroup(br, cc, ccBits)
			if err != nil {
				return nil, err
			}
			groups[g] = grp
		}
	} else {
		grp, err := readHuffGroup(br, cc, ccBits)
		if err != nil {
			return nil, err
		}
		groups = []huffGroup{grp}
	}

	// Decode pixels.
	pixels := make([]uint32, width*height)
	i := 0
	total := width * height

	for i < total {
		x := i % width
		y := i / width

		// Select Huffman group.
		grp := &groups[0]
		if useMetaHuffman && len(metaData) > 0 {
			// Each block of (1<<metaBits) x (1<<metaBits) pixels uses one group.
			metaX := x >> metaBits
			metaY := y >> metaBits
			metaW := subSampleSize(width, int(metaBits))
			metaIdx := metaY*metaW + metaX
			if metaIdx < len(metaData) {
				gIdx := int((metaData[metaIdx] >> 8) & 0xffff)
				if gIdx < len(groups) {
					grp = &groups[gIdx]
				}
			}
		}

		sym, err := grp.trees[0].readSymbol(br) // green channel
		if err != nil {
			return nil, err
		}

		switch {
		case sym < numLiteralCodes:
			// Literal pixel: read red, blue, alpha (VP8L spec order).
			r, err := grp.trees[1].readSymbol(br)
			if err != nil {
				return nil, err
			}
			b, err := grp.trees[2].readSymbol(br)
			if err != nil {
				return nil, err
			}
			a, err := grp.trees[3].readSymbol(br)
			if err != nil {
				return nil, err
			}
			col := uint32(a)<<24 | uint32(r)<<16 | uint32(sym)<<8 | uint32(b)
			pixels[i] = col
			if cc != nil {
				cc.insert(col)
			}
			i++

		case sym < numLiteralCodes+numLengthCodes:
			// LZ77 backward reference.
			length, err := readLength(br, sym)
			if err != nil {
				return nil, err
			}
			dist, err := readDistance(br, grp.trees[4], width)
			if err != nil {
				return nil, err
			}
			if dist <= 0 {
				dist = 1
			}
			src := i - dist
			if src < 0 {
				return nil, errors.New("vp8l: invalid backward reference distance")
			}
			for j := 0; j < length && i < total; j++ {
				col := pixels[src+j]
				pixels[i] = col
				if cc != nil {
					cc.insert(col)
				}
				i++
			}

		default:
			// Color cache reference.
			if cc == nil {
				return nil, errors.New("vp8l: color cache reference without color cache")
			}
			cacheIdx := sym - numLiteralCodes - numLengthCodes
			col := cc.lookup(cacheIdx)
			pixels[i] = col
			i++
		}
	}

	return pixels, nil
}

// readHuffGroup reads 5 Huffman trees for one group.
func readHuffGroup(br *bitReader, cc *colorCache, ccBits uint) (huffGroup, error) {
	var grp huffGroup

	// Alphabet sizes for each tree.
	alphaSizes := [5]int{
		greenAlphabetSize(), // green: literals + lengths + cache
		numColorCodes,       // red
		numColorCodes,       // blue
		numColorCodes,       // alpha
		numDistanceCodes,    // distance
	}

	// Add color cache codes to green alphabet.
	if cc != nil {
		alphaSizes[0] += 1 << ccBits
	}

	for t := 0; t < 5; t++ {
		cls, err := readCodeLengths(br, alphaSizes[t])
		if err != nil {
			return grp, err
		}
		tree, err := buildHuffTree(cls)
		if err != nil {
			return grp, err
		}
		grp.trees[t] = tree
	}

	return grp, nil
}

// decodeEntropyImage decodes a sub-image used for transform data and meta-Huffman.
// topLevel=true: reads use_meta_huffman flag (for palette and main image sub-images).
// topLevel=false: no use_meta flag; just reads one Huffman group (for meta-meta images).
// This matches the reference VP8L decoder's decodePix(topLevel) behavior.
func decodeEntropyImage(br *bitReader, width, height int) ([]uint32, error) {
	return decodeEntropyImageLevel(br, width, height, true)
}

func decodeEntropyImageLevel(br *bitReader, width, height int, topLevel bool) ([]uint32, error) {
	// Check for color cache.
	useColorCache, err := br.readBit()
	if err != nil {
		return nil, err
	}
	var cc *colorCache
	var ccBits uint
	if useColorCache {
		bits, err := br.readBits(4)
		if err != nil {
			return nil, err
		}
		ccBits = uint(bits)
		cc = newColorCache(ccBits)
	}

	var groups []huffGroup
	var metaData []uint32
	var metaBits uint32

	// Only top-level images have a use_meta_huffman flag.
	// Nested meta-images (topLevel=false) always use a single Huffman group.
	useMetaHuffman := false
	if topLevel {
		useMeta, err := br.readBit()
		if err != nil {
			return nil, err
		}
		useMetaHuffman = useMeta
	}

	if useMetaHuffman {
		metaBits, err = br.readBits(3)
		if err != nil {
			return nil, err
		}
		metaBits += 2
		metaW := subSampleSize(width, int(metaBits))
		metaH := subSampleSize(height, int(metaBits))
		// Decode the meta image recursively (non-top-level: no use_meta flag).
		metaData, err = decodeEntropyImageLevel(br, metaW, metaH, false)
		if err != nil {
			return nil, err
		}
		numGroups := 0
		for _, p := range metaData {
			gIdx := int((p >> 8) & 0xffff)
			if gIdx+1 > numGroups {
				numGroups = gIdx + 1
			}
		}
		groups = make([]huffGroup, numGroups)
		for g := 0; g < numGroups; g++ {
			grp, err := readHuffGroup(br, cc, ccBits)
			if err != nil {
				return nil, err
			}
			groups[g] = grp
		}
	} else {
		grp, err := readHuffGroup(br, cc, ccBits)
		if err != nil {
			return nil, err
		}
		groups = []huffGroup{grp}
	}

	pixels := make([]uint32, width*height)
	i := 0
	total := width * height

	for i < total {
		x := i % width
		y := i / width
		grp := &groups[0]
		if useMetaHuffman && len(metaData) > 0 {
			metaX := x >> metaBits
			metaY := y >> metaBits
			metaW := subSampleSize(width, int(metaBits))
			metaIdx := metaY*metaW + metaX
			if metaIdx < len(metaData) {
				gIdx := int((metaData[metaIdx] >> 8) & 0xffff)
				if gIdx < len(groups) {
					grp = &groups[gIdx]
				}
			}
		}
		sym, err := grp.trees[0].readSymbol(br)
		if err != nil {
			return nil, err
		}

		switch {
		case sym < numLiteralCodes:
			r, err := grp.trees[1].readSymbol(br)
			if err != nil {
				return nil, err
			}
			b, err := grp.trees[2].readSymbol(br)
			if err != nil {
				return nil, err
			}
			a, err := grp.trees[3].readSymbol(br)
			if err != nil {
				return nil, err
			}
			col := uint32(a)<<24 | uint32(r)<<16 | uint32(sym)<<8 | uint32(b)
			pixels[i] = col
			if cc != nil {
				cc.insert(col)
			}
			i++

		case sym < numLiteralCodes+numLengthCodes:
			length, err := readLength(br, sym)
			if err != nil {
				return nil, err
			}
			dist, err := readDistance(br, grp.trees[4], width)
			if err != nil {
				return nil, err
			}
			if dist <= 0 {
				dist = 1
			}
			src := i - dist
			if src < 0 {
				return nil, errors.New("vp8l: invalid backward reference")
			}
			for j := 0; j < length && i < total; j++ {
				col := pixels[src+j]
				pixels[i] = col
				if cc != nil {
					cc.insert(col)
				}
				i++
			}

		default:
			if cc == nil {
				return nil, errors.New("vp8l: color cache reference without cache")
			}
			cacheIdx := sym - numLiteralCodes - numLengthCodes
			col := cc.lookup(cacheIdx)
			pixels[i] = col
			i++
		}
	}

	return pixels, nil
}

// decodeEntropyImageFlat decodes a meta-Huffman sub-image.
// Supports one level of meta-Huffman nesting (for meta images of meta images).
func decodeEntropyImageFlat(br *bitReader, width, height int) ([]uint32, error) {
	useColorCache, err := br.readBit()
	if err != nil {
		return nil, err
	}
	var cc *colorCache
	var ccBits uint
	if useColorCache {
		bits, err := br.readBits(4)
		if err != nil {
			return nil, err
		}
		ccBits = uint(bits)
		cc = newColorCache(ccBits)
	}

	useMetaHuffman, err := br.readBit()
	if err != nil {
		return nil, err
	}

	var groups []huffGroup
	var metaData []uint32
	var metaBits uint32

	if useMetaHuffman {
		metaBits, err = br.readBits(3)
		if err != nil {
			return nil, err
		}
		metaBits += 2
		metaW := subSampleSize(width, int(metaBits))
		metaH := subSampleSize(height, int(metaBits))
		metaData, err = decodeEntropyImageLevel(br, metaW, metaH, false)
		if err != nil {
			return nil, err
		}
		numGroups := 0
		for _, p := range metaData {
			gIdx := int((p >> 8) & 0xffff)
			if gIdx+1 > numGroups {
				numGroups = gIdx + 1
			}
		}
		groups = make([]huffGroup, numGroups)
		for g := 0; g < numGroups; g++ {
			grp, err := readHuffGroup(br, cc, ccBits)
			if err != nil {
				return nil, err
			}
			groups[g] = grp
		}
	} else {
		grp, err := readHuffGroup(br, cc, ccBits)
		if err != nil {
			return nil, err
		}
		groups = []huffGroup{grp}
	}

	return decodePixels(br, width, height, groups, metaData, metaBits, cc)
}

// decodeSingleGroupImage decodes an image with a single Huffman group.
// It reads the use_meta flag but always uses exactly one group.
func decodeSingleGroupImage(br *bitReader, width, height int) ([]uint32, error) {
	useColorCache, err := br.readBit()
	if err != nil {
		return nil, err
	}
	var cc *colorCache
	var ccBits uint
	if useColorCache {
		bits, err := br.readBits(4)
		if err != nil {
			return nil, err
		}
		ccBits = uint(bits)
		cc = newColorCache(ccBits)
	}

	// Read and ignore use_meta_huffman.
	_, err = br.readBit()
	if err != nil {
		return nil, err
	}

	grp, err := readHuffGroup(br, cc, ccBits)
	if err != nil {
		return nil, err
	}
	groups := []huffGroup{grp}

	return decodePixels(br, width, height, groups, nil, 0, cc)
}

// decodePixels decodes w*h pixels using the given Huffman groups.
func decodePixels(br *bitReader, width, height int, groups []huffGroup, metaData []uint32, metaBits uint32, cc *colorCache) ([]uint32, error) {
	pixels := make([]uint32, width*height)
	i := 0
	total := width * height
	useMetaHuffman := len(metaData) > 0

	for i < total {
		x := i % width
		y := i / width
		grp := &groups[0]
		if useMetaHuffman {
			metaX := x >> metaBits
			metaY := y >> metaBits
			metaW := subSampleSize(width, int(metaBits))
			metaIdx := metaY*metaW + metaX
			if metaIdx < len(metaData) {
				gIdx := int((metaData[metaIdx] >> 8) & 0xffff)
				if gIdx < len(groups) {
					grp = &groups[gIdx]
				}
			}
		}

		sym, err := grp.trees[0].readSymbol(br)
		if err != nil {
			return nil, err
		}

		switch {
		case sym < numLiteralCodes:
			r, err := grp.trees[1].readSymbol(br)
			if err != nil {
				return nil, err
			}
			b, err := grp.trees[2].readSymbol(br)
			if err != nil {
				return nil, err
			}
			a, err := grp.trees[3].readSymbol(br)
			if err != nil {
				return nil, err
			}
			col := uint32(a)<<24 | uint32(r)<<16 | uint32(sym)<<8 | uint32(b)
			pixels[i] = col
			if cc != nil {
				cc.insert(col)
			}
			i++
		case sym < numLiteralCodes+numLengthCodes:
			length, err := readLength(br, sym)
			if err != nil {
				return nil, err
			}
			dist, err := readDistance(br, grp.trees[4], width)
			if err != nil {
				return nil, err
			}
			if dist <= 0 {
				dist = 1
			}
			src := i - dist
			if src < 0 {
				return nil, errors.New("vp8l: invalid backward reference")
			}
			for j := 0; j < length && i < total; j++ {
				col := pixels[src+j]
				pixels[i] = col
				if cc != nil {
					cc.insert(col)
				}
				i++
			}
		default:
			if cc == nil {
				return nil, errors.New("vp8l: color cache reference without cache")
			}
			cacheIdx := sym - numLiteralCodes - numLengthCodes
			col := cc.lookup(cacheIdx)
			pixels[i] = col
			i++
		}
	}

	return pixels, nil
}
