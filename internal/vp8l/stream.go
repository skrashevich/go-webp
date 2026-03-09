package vp8l

import (
	"fmt"
	"image"
	"image/color"
)

const (
	maxHeaderDimension = 1 << 14
	minColorCacheBits  = 1
	maxColorCacheBits  = 11
)

type imageHeader struct {
	width    int
	height   int
	hasAlpha bool
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func validateStreamDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("vp8l: invalid image dimensions %dx%d", width, height)
	}
	if width > maxInt()/height {
		return fmt.Errorf("vp8l: image dimensions overflow %dx%d", width, height)
	}
	return nil
}

func validateHeaderDimensions(width, height int) error {
	if err := validateStreamDimensions(width, height); err != nil {
		return err
	}
	if width > maxHeaderDimension || height > maxHeaderDimension {
		return fmt.Errorf("vp8l: image dimensions %dx%d exceed 16384x16384 header limit", width, height)
	}
	return nil
}

func validateColorCacheBits(bits uint32) error {
	if bits < minColorCacheBits || bits > maxColorCacheBits {
		return fmt.Errorf("vp8l: invalid color cache bits %d", bits)
	}
	return nil
}

func readImageHeader(br *bitReader) (imageHeader, error) {
	wBits, err := br.readBits(14)
	if err != nil {
		return imageHeader{}, err
	}
	hBits, err := br.readBits(14)
	if err != nil {
		return imageHeader{}, err
	}
	hasAlpha, err := br.readBit()
	if err != nil {
		return imageHeader{}, err
	}
	ver, err := br.readBits(3)
	if err != nil {
		return imageHeader{}, err
	}
	if ver != 0 {
		return imageHeader{}, fmt.Errorf("vp8l: unsupported version %d", ver)
	}
	return imageHeader{
		width:    int(wBits) + 1,
		height:   int(hBits) + 1,
		hasAlpha: hasAlpha,
	}, nil
}

func decodeImageStream(br *bitReader, width, height int) ([]uint32, error) {
	if err := validateStreamDimensions(width, height); err != nil {
		return nil, err
	}

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

	pixels, err := decodeImageData(br, decodeWidth, height, width)
	if err != nil {
		return nil, err
	}
	return inverseTransformsWithWidths(pixels, transforms, decodeWidth, width, height), nil
}

func imageToPixels(img image.Image) ([]uint32, int, int, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if err := validateStreamDimensions(width, height); err != nil {
		return nil, 0, 0, err
	}

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
		return pixels, width, height, nil
	}

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
	return pixels, width, height, nil
}

func pixelsToNRGBA(pixels []uint32, width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := pixels[y*width+x]
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(p >> 16),
				G: uint8(p >> 8),
				B: uint8(p),
				A: uint8(p >> 24),
			})
		}
	}
	return img
}

func defaultEncodeOptions(width, height int) EncodeOptions {
	opts := EncodeOptions{
		SubtractGreen: true,
		Palette:       true,
	}
	if uint64(width)*uint64(height) > 32*32 {
		opts.Predictor = true
		opts.PredictorBits = 4
		opts.Color = true
		opts.ColorBits = 4
	}
	return opts
}

func encodePixels(pixels []uint32, width, height int, opts EncodeOptions, includeHeader bool) ([]byte, error) {
	if includeHeader {
		if err := validateHeaderDimensions(width, height); err != nil {
			return nil, err
		}
	} else if err := validateStreamDimensions(width, height); err != nil {
		return nil, err
	}

	bw := newBitWriter()

	if includeHeader {
		bw.writeBits(vp8lSignature, 8)
		bw.writeBits(uint32(width-1), 14)
		bw.writeBits(uint32(height-1), 14)

		hasAlpha := false
		for _, p := range pixels {
			if (p >> 24) != 0xff {
				hasAlpha = true
				break
			}
		}
		bw.writeBit(hasAlpha)
		bw.writeBits(0, 3)
	}

	currentWidth := width

	paletteApplied := false
	var palette []uint32
	var xbits int
	if opts.Palette {
		var indexed []uint32
		var ok bool
		indexed, palette, ok = applyColorIndexing(pixels, width, height)
		if ok {
			sortPalette(palette)
			colorToIdx := make(map[uint32]int, len(palette))
			for i, c := range palette {
				colorToIdx[c] = i
			}
			for i, p := range pixels {
				indexed[i] = uint32(colorToIdx[p]) << 8
			}

			xbits = paletteXBits(len(palette))

			bw.writeBit(true)
			bw.writeBits(uint32(transformColorIndexing), 2)
			bw.writeBits(uint32(len(palette)-1), 8)

			palDeltas := make([]uint32, len(palette))
			palDeltas[0] = palette[0]
			for i := 1; i < len(palette); i++ {
				palDeltas[i] = subARGB(palette[i], palette[i-1])
			}
			writeImageDataSubImage(bw, palDeltas, len(palette), 1)

			if xbits > 0 {
				indexed = packColorIndices(indexed, width, height, xbits)
				currentWidth = subSampleSize(width, xbits)
			}

			pixels = indexed
			paletteApplied = true
		}
	}

	if opts.Predictor && !paletteApplied {
		bits := opts.PredictorBits
		if bits < 2 {
			bits = 4
		}
		if bits > 11 {
			bits = 11
		}

		residual, predImage := applyPredictorTransform(pixels, currentWidth, height, bits)

		bw.writeBit(true)
		bw.writeBits(uint32(transformPredictor), 2)
		bw.writeBits(uint32(bits-2), 3)

		predW := subSampleSize(currentWidth, bits)
		predH := subSampleSize(height, bits)
		writeImageDataSubImage(bw, predImage, predW, predH)

		pixels = residual
	}

	if opts.Color && !paletteApplied {
		bits := opts.ColorBits
		if bits < 2 {
			bits = 4
		}
		if bits > 11 {
			bits = 11
		}

		transformed, colorImage := applyColorTransform(pixels, currentWidth, height, bits)

		bw.writeBit(true)
		bw.writeBits(uint32(transformColor), 2)
		bw.writeBits(uint32(bits-2), 3)

		colorW := subSampleSize(currentWidth, bits)
		colorH := subSampleSize(height, bits)
		writeImageDataSubImage(bw, colorImage, colorW, colorH)

		pixels = transformed
	}

	if opts.SubtractGreen {
		applySubtractGreen(pixels)

		bw.writeBit(true)
		bw.writeBits(uint32(transformSubtractGreen), 2)
	}

	bw.writeBit(false)

	lz77Width := 0
	if opts.LZ77 {
		lz77Width = currentWidth
	}
	writeImageData(bw, pixels, lz77Width, height)

	return bw.bytes(), nil
}

// EncodeAlphaPlane encodes a green-channel alpha plane as a headerless VP8L image stream.
func EncodeAlphaPlane(alpha []byte, width, height int) ([]byte, error) {
	if err := validateStreamDimensions(width, height); err != nil {
		return nil, err
	}
	if len(alpha) != width*height {
		return nil, fmt.Errorf("vp8l: alpha plane size mismatch: got %d, want %d", len(alpha), width*height)
	}

	pixels := make([]uint32, len(alpha))
	for i, v := range alpha {
		pixels[i] = 0xff000000 | uint32(v)<<8
	}
	return encodePixels(pixels, width, height, defaultEncodeOptions(width, height), false)
}

// DecodeAlphaPlane decodes a headerless VP8L image stream and extracts the green channel.
func DecodeAlphaPlane(data []byte, width, height int) ([]byte, error) {
	pixels, err := decodeImageStream(newBitReader(data), width, height)
	if err != nil {
		return nil, err
	}

	alpha := make([]byte, len(pixels))
	for i, p := range pixels {
		alpha[i] = byte((p >> 8) & 0xff)
	}
	return alpha, nil
}
