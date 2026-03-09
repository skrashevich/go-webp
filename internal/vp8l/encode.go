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
	// Color applies the cross-color (channel correlation) transform.
	Color bool
	// ColorBits is the block-size exponent for the color transform (block = 1<<ColorBits).
	// Valid range 2-11; 0 defaults to 4 (block size 16).
	ColorBits int
	// LZ77 enables LZ77 back-reference matching during pixel encoding.
	LZ77 bool
}

// EncodeVP8L encodes an image as a VP8L lossless bitstream with default transforms:
// subtract-green always; palette when ≤256 colors; predictor for larger images.
func EncodeVP8L(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	return EncodeVP8LWithOptions(img, defaultEncodeOptions(bounds.Dx(), bounds.Dy()))
}

// EncodeVP8LWithOptions encodes an image with explicit transform options.
func EncodeVP8LWithOptions(img image.Image, opts EncodeOptions) ([]byte, error) {
	pixels, width, height, err := imageToPixels(img)
	if err != nil {
		return nil, err
	}
	return encodePixels(pixels, width, height, opts, true)
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

// lz77Token represents a single unit of LZ77-encoded output: either a literal
// pixel or a back-reference (length, distance) pair.
type lz77Token struct {
	isBackRef bool
	pixel     uint32 // for literals
	length    int    // for back-references
	planeDist int    // for back-references: VP8L plane distance (input to getDistanceCode)
}

// writeImageData writes the main pixel data (top-level image).
// Format: use_color_cache(0) + use_meta_huffman(0) + huffman trees + pixels.
// This matches decodeEntropyImageLevel(topLevel=true) / decodeImageData.
func writeImageData(bw *bitWriter, pixels []uint32, width, height int) {
	// No color cache.
	bw.writeBit(false)
	// No meta-Huffman (top-level flag).
	bw.writeBit(false)
	writeHuffmanPixelsWidth(bw, pixels, width)
}

// writeImageDataSubImage writes a sub-image used as transform data (palette, predictor, color).
// Format: use_color_cache(0) + huffman trees + pixels.
// This matches decodeEntropyImageLevel(topLevel=false) which reads no use_meta_huffman bit.
func writeImageDataSubImage(bw *bitWriter, pixels []uint32, width, height int) {
	// No color cache.
	bw.writeBit(false)
	// Note: topLevel=false decoder does NOT read a use_meta_huffman bit.
	writeHuffmanPixelsWidth(bw, pixels, width)
}

// writeHuffmanPixels writes the huffman trees and encoded pixels.
// Used by both writeImageData and writeImageDataSubImage.
func writeHuffmanPixels(bw *bitWriter, pixels []uint32) {
	writeHuffmanPixelsWidth(bw, pixels, 0)
}

// writeHuffmanPixelsWidth writes huffman trees and encoded pixels.
// width is used for VP8L plane-distance conversion when width > 0 (enables LZ77).
// width == 0 disables LZ77 matching (used for sub-images where LZ77 is not beneficial).
func writeHuffmanPixelsWidth(bw *bitWriter, pixels []uint32, width int) {
	// Pass 1: build token list.
	// LZ77 is only used for the main image (width > 0).
	var tokens []lz77Token
	if width > 0 {
		h := newLZ77Hash()
		pos := 0
		for pos < len(pixels) {
			length, linearDist := findMatch(pixels, pos, h)
			if length >= minMatchLen {
				// Convert linear pixel distance to VP8L plane distance.
				planeDist := linearToPlaneDistance(linearDist, width)
				tokens = append(tokens, lz77Token{
					isBackRef: true,
					length:    length,
					planeDist: planeDist,
				})
				// Hash all positions covered by the match (except the first,
				// which findMatch already inserted into the hash table).
				for k := 1; k < length; k++ {
					if pos+k < len(pixels) {
						v := pixels[pos+k]
						key := lz77Hash32(v)
						h.prev[(pos+k)%maxDist] = h.head[key]
						h.head[key] = int32(pos + k)
					}
				}
				pos += length
			} else {
				tokens = append(tokens, lz77Token{
					isBackRef: false,
					pixel:     pixels[pos],
				})
				pos++
			}
		}
	} else {
		tokens = make([]lz77Token, len(pixels))
		for i, p := range pixels {
			tokens[i] = lz77Token{isBackRef: false, pixel: p}
		}
	}

	// Pass 2: count frequencies.
	greenFreqs := make([]int, greenAlphabetSize())
	redFreqs := make([]int, numColorCodes)
	blueFreqs := make([]int, numColorCodes)
	alphaFreqs := make([]int, numColorCodes)
	distFreqs := make([]int, numDistanceCodes)

	for _, tok := range tokens {
		if tok.isBackRef {
			lenCode, _, _ := getLengthCode(tok.length)
			greenFreqs[lenCode]++
			distCode, _, _ := getDistanceCode(tok.planeDist)
			distFreqs[distCode]++
		} else {
			p := tok.pixel
			g := int((p >> 8) & 0xff)
			r := int((p >> 16) & 0xff)
			b := int(p & 0xff)
			a := int((p >> 24) & 0xff)
			greenFreqs[g]++
			redFreqs[r]++
			blueFreqs[b]++
			alphaFreqs[a]++
		}
	}
	// Ensure distance has at least one symbol (required for valid tree).
	if distFreqs[0] == 0 {
		distFreqs[0]++
	}

	// Build Huffman codes.
	greenCodes, greenLengths := buildHuffCodes(greenFreqs, 15)
	redCodes, redLengths := buildHuffCodes(redFreqs, 15)
	blueCodes, blueLengths := buildHuffCodes(blueFreqs, 15)
	alphaCodes, alphaLengths := buildHuffCodes(alphaFreqs, 15)
	distCodes, distLengths := buildHuffCodes(distFreqs, 15)

	// Write code lengths for 5 trees.
	writeCodeLengths(bw, greenLengths)
	writeCodeLengths(bw, redLengths)
	writeCodeLengths(bw, blueLengths)
	writeCodeLengths(bw, alphaLengths)
	writeCodeLengths(bw, distLengths)

	// Pass 3: write tokens.
	for _, tok := range tokens {
		if tok.isBackRef {
			// Write length prefix code via green tree.
			lenCode, lenExtraBits, lenExtra := getLengthCode(tok.length)
			gc := greenCodes[lenCode]
			bw.writeBits(gc.code, gc.nbits)
			if lenExtraBits > 0 {
				bw.writeBits(uint32(lenExtra), lenExtraBits)
			}
			// Write distance code.
			distCode, distExtraBitsCount, distExtra := getDistanceCode(tok.planeDist)
			dc := distCodes[distCode]
			bw.writeBits(dc.code, dc.nbits)
			if distExtraBitsCount > 0 {
				bw.writeBits(uint32(distExtra), distExtraBitsCount)
			}
		} else {
			p := tok.pixel
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
