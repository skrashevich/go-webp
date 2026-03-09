package vp8l

import (
	"image"
	"sort"
)

// EncodeOptions controls which transforms are applied during encoding.
type EncodeOptions struct {
	// SubtractGreen applies the subtract-green transform (nearly free, often helps).
	SubtractGreen bool
	// Palette applies the color-indexing transform when the image has ≤256 unique colors.
	Palette bool
	// Predictor applies the spatial predictor transform.
	Predictor bool
	// PredictorBits is the block-size exponent (block = 1<<PredictorBits).
	// Valid range 2-11; 0 defaults to 4 (block size 16).
	PredictorBits int
}

// EncodeVP8L encodes an image as a VP8L lossless bitstream with default transforms:
// subtract-green always; palette when ≤256 colors; predictor for larger images.
func EncodeVP8L(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	opts := EncodeOptions{
		SubtractGreen: true,
		Palette:       true,
	}
	// Enable predictor for images larger than 32x32.
	if w*h > 32*32 {
		opts.Predictor = true
		opts.PredictorBits = 4
	}
	return EncodeVP8LWithOptions(img, opts)
}

// EncodeVP8LWithOptions encodes an image with explicit transform options.
func EncodeVP8LWithOptions(img image.Image, opts EncodeOptions) ([]byte, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Convert image to ARGB pixels.
	pixels := make([]uint32, width*height)
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := nrgba.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				r8 := uint32(nrgba.Pix[off+0])
				g8 := uint32(nrgba.Pix[off+1])
				b8 := uint32(nrgba.Pix[off+2])
				a8 := uint32(nrgba.Pix[off+3])
				if a8 == 0 {
					pixels[y*width+x] = 0
				} else {
					pixels[y*width+x] = a8<<24 | r8<<16 | g8<<8 | b8
				}
			}
		}
	} else {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				var r8, g8, b8, a8 uint32
				if a == 0 {
					r8, g8, b8, a8 = 0, 0, 0, 0
				} else {
					r8 = r >> 8
					g8 = g >> 8
					b8 = b >> 8
					a8 = a >> 8
				}
				pixels[y*width+x] = a8<<24 | r8<<16 | g8<<8 | b8
			}
		}
	}

	bw := newBitWriter()

	// VP8L signature byte.
	bw.writeBits(vp8lSignature, 8)

	// Width and height (14 bits each, value - 1).
	bw.writeBits(uint32(width-1), 14)
	bw.writeBits(uint32(height-1), 14)

	// Alpha hint.
	hasAlpha := false
	for _, p := range pixels {
		if (p >> 24) != 0xff {
			hasAlpha = true
			break
		}
	}
	bw.writeBit(hasAlpha)

	// Version bits (3 bits, must be 0).
	bw.writeBits(0, 3)

	// Decide which transforms to apply and in what order.
	// Transforms are written in the bitstream in the order they are applied
	// to the original pixels. The decoder inverts them in reverse order.
	//
	// Application order (encoder):
	//   1. Palette (color indexing) — may reduce width
	//   2. Predictor
	//   3. Subtract green
	//
	// This means in the bitstream we write: palette, predictor, subtract_green
	// and the decoder inverts: subtract_green first, then predictor, then palette.

	currentWidth := width

	// --- Palette transform ---
	paletteApplied := false
	var palette []uint32
	var xbits int
	if opts.Palette {
		var indexed []uint32
		var ok bool
		indexed, palette, ok = applyColorIndexing(pixels, width, height)
		if ok {
			// Sort palette for better delta-coding compression.
			sortPalette(palette)
			// Remap indices to the sorted palette.
			colorToIdx := make(map[uint32]int, len(palette))
			for i, c := range palette {
				colorToIdx[c] = i
			}
			for i, p := range pixels {
				indexed[i] = uint32(colorToIdx[p]) << 8
			}

			// Compute xbits: packing factor for small palettes.
			xbits = paletteXBits(len(palette))

			// Write palette transform header.
			bw.writeBit(true)                              // has_transform
			bw.writeBits(uint32(transformColorIndexing), 2) // transform type
			bw.writeBits(uint32(len(palette)-1), 8)        // palette_size - 1

			// Encode palette as a 1×paletteSize image using delta coding.
			// Sub-images use topLevel=false format (no use_meta_huffman bit).
			palDeltas := make([]uint32, len(palette))
			palDeltas[0] = palette[0]
			for i := 1; i < len(palette); i++ {
				palDeltas[i] = subARGB(palette[i], palette[i-1])
			}
			writeImageDataSubImage(bw, palDeltas, len(palette), 1)

			// Pack palette indices if palette is small.
			if xbits > 0 {
				indexed = packColorIndices(indexed, width, height, xbits)
				currentWidth = subSampleSize(width, xbits)
			}

			pixels = indexed
			paletteApplied = true
		}
	}

	// --- Predictor transform ---
	predictorApplied := false
	if opts.Predictor && !paletteApplied {
		bits := opts.PredictorBits
		if bits < 2 {
			bits = 4
		}
		if bits > 11 {
			bits = 11
		}

		residual, predImage := applyPredictorTransform(pixels, currentWidth, height, bits)

		// Write predictor transform header.
		bw.writeBit(true)                            // has_transform
		bw.writeBits(uint32(transformPredictor), 2)  // transform type
		bw.writeBits(uint32(bits-2), 3)              // bits - 2

		predW := subSampleSize(currentWidth, bits)
		predH := subSampleSize(height, bits)
		writeImageDataSubImage(bw, predImage, predW, predH)

		pixels = residual
		predictorApplied = true
	}
	_ = predictorApplied

	// --- Subtract green transform ---
	if opts.SubtractGreen {
		applySubtractGreen(pixels)

		bw.writeBit(true)                                // has_transform
		bw.writeBits(uint32(transformSubtractGreen), 2) // transform type
	}

	// No more transforms.
	bw.writeBit(false)

	// Write image data.
	writeImageData(bw, pixels, currentWidth, height)

	return bw.bytes(), nil
}

// encTransform holds an encoder-side transform (kept for API compatibility).
type encTransform struct {
	kind    transformType
	bits    int
	data    []uint32
	palette []uint32
}

// subARGB subtracts two ARGB values component-wise (mod 256).
func subARGB(a, b uint32) uint32 {
	return uint32((a-b)&0xff) |
		uint32(((a>>8)-(b>>8))&0xff)<<8 |
		uint32(((a>>16)-(b>>16))&0xff)<<16 |
		uint32(((a>>24)-(b>>24))&0xff)<<24
}

// packColorIndices packs multiple palette indices into each pixel.
func packColorIndices(pixels []uint32, width, height, xbits int) []uint32 {
	pixPerUnit := 1 << uint(xbits)
	bitsPerIdx := 8 / pixPerUnit
	newWidth := subSampleSize(width, xbits)
	result := make([]uint32, newWidth*height)

	for y := 0; y < height; y++ {
		for x := 0; x < newWidth; x++ {
			var packed uint32
			for k := 0; k < pixPerUnit; k++ {
				ox := x*pixPerUnit + k
				if ox >= width {
					break
				}
				idx := (pixels[y*width+ox] >> 8) & 0xff
				packed |= (idx & uint32((1<<uint(bitsPerIdx))-1)) << uint(k*bitsPerIdx)
			}
			result[y*newWidth+x] = packed << 8
		}
	}
	return result
}

// paletteXBits returns the packing exponent for a palette of the given size.
// xbits=3: 8 pixels per uint32 (1-bit indices), 2 colors
// xbits=2: 4 pixels per uint32 (2-bit indices), 3-4 colors
// xbits=1: 2 pixels per uint32 (4-bit indices), 5-16 colors
// xbits=0: 1 pixel per uint32 (8-bit indices), 17-256 colors
func paletteXBits(paletteSize int) int {
	switch {
	case paletteSize <= 2:
		return 3
	case paletteSize <= 4:
		return 2
	case paletteSize <= 16:
		return 1
	default:
		return 0
	}
}

// sortPalette sorts palette entries to improve delta coding.
// Uses a nearest-neighbor approach: start with the darkest color, then
// repeatedly pick the color closest (by ARGB distance) to the last picked.
func sortPalette(palette []uint32) {
	if len(palette) <= 1 {
		return
	}
	// Find the entry with the smallest sum of channels (darkest).
	startIdx := 0
	minSum := channelSum(palette[0])
	for i, c := range palette {
		if s := channelSum(c); s < minSum {
			minSum = s
			startIdx = i
		}
	}
	palette[0], palette[startIdx] = palette[startIdx], palette[0]

	// Nearest-neighbor sort.
	for i := 1; i < len(palette); i++ {
		best := i
		bestDist := colorDist(palette[i-1], palette[i])
		for j := i + 1; j < len(palette); j++ {
			if d := colorDist(palette[i-1], palette[j]); d < bestDist {
				bestDist = d
				best = j
			}
		}
		palette[i], palette[best] = palette[best], palette[i]
	}
}

func channelSum(c uint32) int {
	return int(c&0xff) + int((c>>8)&0xff) + int((c>>16)&0xff) + int((c>>24)&0xff)
}

// applyPredictorTransform applies the spatial predictor transform.
// Returns the residual pixel array and the predictor-mode sub-image.
func applyPredictorTransform(pixels []uint32, width, height, bits int) (residual []uint32, predImage []uint32) {
	blockSize := 1 << uint(bits)
	predW := subSampleSize(width, bits)
	predH := subSampleSize(height, bits)

	predImage = make([]uint32, predW*predH)
	residual = make([]uint32, width*height)

	// Choose predictor mode per block by trying all 14 modes and picking
	// the one that minimises the sum of absolute residuals.
	for by := 0; by < predH; by++ {
		for bx := 0; bx < predW; bx++ {
			// Determine pixel range for this block.
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

			bestMode := 0
			bestCost := int64(-1)

			for mode := 0; mode <= 13; mode++ {
				cost := int64(0)
				for y := y0; y < y1; y++ {
					for x := x0; x < x1; x++ {
						if y == 0 && x == 0 {
							continue
						}
						var pred uint32
						if y == 0 {
							pred = pixels[y*width+x-1]
						} else if x == 0 {
							pred = pixels[(y-1)*width]
						} else {
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
						res := subARGB(pixels[y*width+x], pred)
						// Cost: sum of absolute values of each channel residual.
						cost += int64(residualCost(res))
					}
				}
				if bestCost < 0 || cost < bestCost {
					bestCost = cost
					bestMode = mode
				}
			}

			// Store best mode in green channel of predictor image pixel.
			predImage[by*predW+bx] = uint32(bestMode) << 8
		}
	}

	// Compute residuals using chosen predictor modes.
	// Neighbors must come from the original pixels array (not from residuals),
	// because the decoder reconstructs pixels sequentially using decoded (= original)
	// neighbor values. The encoder must mirror the same neighbor access pattern.
	residual = make([]uint32, width*height)
	copy(residual, pixels) // first pixel is stored as-is
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if y == 0 && x == 0 {
				// Top-left pixel stored as-is (residual = original).
				continue
			}
			var pred uint32
			if y == 0 {
				// Top row: predict from left original pixel.
				pred = pixels[y*width+x-1]
			} else if x == 0 {
				// Left column: predict from pixel above (top of first column).
				pred = pixels[(y-1)*width]
			} else {
				bx := x >> uint(bits)
				by := y >> uint(bits)
				mode := int((predImage[by*predW+bx] >> 8) & 0xff)
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
			residual[y*width+x] = subARGB(pixels[y*width+x], pred)
		}
	}

	return residual, predImage
}

// residualCost returns a cost metric for one residual pixel (sum of absolute channel values).
func residualCost(r uint32) int {
	a := int(r >> 24)
	if a > 127 {
		a = 256 - a
	}
	g := int((r >> 8) & 0xff)
	if g > 127 {
		g = 256 - g
	}
	re := int((r >> 16) & 0xff)
	if re > 127 {
		re = 256 - re
	}
	b := int(r & 0xff)
	if b > 127 {
		b = 256 - b
	}
	return a + g + re + b
}

// writeImageData writes the main pixel data (top-level image).
// Format: use_color_cache(0) + use_meta_huffman(0) + huffman trees + pixels.
// This matches decodeEntropyImageLevel(topLevel=true) / decodeImageData.
func writeImageData(bw *bitWriter, pixels []uint32, width, height int) {
	// No color cache.
	bw.writeBit(false)

	// No meta-Huffman (top-level flag).
	bw.writeBit(false)

	writeHuffmanPixels(bw, pixels)
}

// writeImageDataSubImage writes a sub-image used as transform data (palette, predictor, color).
// Format: use_color_cache(0) + huffman trees + pixels.
// This matches decodeEntropyImageLevel(topLevel=false) which reads no use_meta_huffman bit.
func writeImageDataSubImage(bw *bitWriter, pixels []uint32, width, height int) {
	// No color cache.
	bw.writeBit(false)

	// Note: topLevel=false decoder does NOT read a use_meta_huffman bit.
	// We write only the single huffman group directly.
	writeHuffmanPixels(bw, pixels)
}

// writeHuffmanPixels writes the huffman trees and encoded pixels.
// Used by both writeImageData and writeImageDataSubImage.
func writeHuffmanPixels(bw *bitWriter, pixels []uint32) {
	// Compute frequency tables for 5 channels.
	greenFreqs := make([]int, greenAlphabetSize())
	redFreqs := make([]int, numColorCodes)
	blueFreqs := make([]int, numColorCodes)
	alphaFreqs := make([]int, numColorCodes)
	distFreqs := make([]int, numDistanceCodes)

	for _, p := range pixels {
		g := int((p >> 8) & 0xff)
		r := int((p >> 16) & 0xff)
		b := int(p & 0xff)
		a := int((p >> 24) & 0xff)
		greenFreqs[g]++
		redFreqs[r]++
		blueFreqs[b]++
		alphaFreqs[a]++
	}
	// Ensure distance has at least one symbol (required for valid tree).
	distFreqs[0]++

	// Build Huffman codes.
	greenCodes, greenLengths := buildHuffCodes(greenFreqs, 15)
	redCodes, redLengths := buildHuffCodes(redFreqs, 15)
	blueCodes, blueLengths := buildHuffCodes(blueFreqs, 15)
	alphaCodes, alphaLengths := buildHuffCodes(alphaFreqs, 15)
	_, distLengths := buildHuffCodes(distFreqs, 15)

	// Write code lengths for 5 trees.
	writeCodeLengths(bw, greenLengths)
	writeCodeLengths(bw, redLengths)
	writeCodeLengths(bw, blueLengths)
	writeCodeLengths(bw, alphaLengths)
	writeCodeLengths(bw, distLengths)

	// Write pixel data as literals only.
	for _, p := range pixels {
		g := int((p >> 8) & 0xff)
		r := int((p >> 16) & 0xff)
		b := int(p & 0xff)
		a := int((p >> 24) & 0xff)

		gc := greenCodes[g]
		bw.writeBits(gc.code, gc.nbits)
		rc := redCodes[r]
		bw.writeBits(rc.code, rc.nbits)
		bc := blueCodes[b]
		bw.writeBits(bc.code, bc.nbits)
		ac := alphaCodes[a]
		bw.writeBits(ac.code, ac.nbits)
	}
}

// --- applyColorIndexing is in transform.go; sortPalette helpers below ---

// buildSortedPalette collects unique colors, sorts them, and returns palette + index map.
// This is used internally by the encoder.
func buildSortedPalette(pixels []uint32) (palette []uint32, colorToIdx map[uint32]int, ok bool) {
	seen := make(map[uint32]struct{})
	for _, p := range pixels {
		seen[p] = struct{}{}
		if len(seen) > 256 {
			return nil, nil, false
		}
	}

	palette = make([]uint32, 0, len(seen))
	for c := range seen {
		palette = append(palette, c)
	}
	// Stable sort by value for determinism.
	sort.Slice(palette, func(i, j int) bool { return palette[i] < palette[j] })
	sortPalette(palette)

	colorToIdx = make(map[uint32]int, len(palette))
	for i, c := range palette {
		colorToIdx[c] = i
	}
	return palette, colorToIdx, true
}
