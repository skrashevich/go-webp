package vp8l

import (
	"image"
)

// EncodeVP8L encodes an image as a VP8L lossless bitstream.
// Uses Huffman coding of literals without transforms for simplicity and correctness.
func EncodeVP8L(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	// Convert image to ARGB pixels.
	// Use direct pixel access for NRGBA to avoid premultiplication round-trip losses.
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

	// No transforms.
	bw.writeBit(false)

	// Write image data as Huffman-coded literals.
	writeImageData(bw, pixels, width, height)

	return bw.bytes(), nil
}

// encTransform holds an encoder-side transform (unused in simplified encoder).
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

// writeImageData writes pixel data using Huffman coding (literals only, no LZ77).
func writeImageData(bw *bitWriter, pixels []uint32, width, height int) {
	// No color cache.
	bw.writeBit(false)

	// No meta-Huffman.
	bw.writeBit(false)

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
