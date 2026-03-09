package vp8l

import (
	"image"
	"image/color"
	"testing"
)

// makeRepeatingImage creates an image with a repeating tile pattern.
func makeRepeatingImage(w, h, tileW, tileH int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			tx := x % tileW
			ty := y % tileH
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(tx * 40 % 256),
				G: uint8(ty * 40 % 256),
				B: uint8((tx + ty) * 20 % 256),
				A: 255,
			})
		}
	}
	return img
}

// TestLZ77BasicRoundTrip encodes with LZ77, decodes, and verifies pixel-perfect output.
func TestLZ77BasicRoundTrip(t *testing.T) {
	img := makeGradientImage(16, 16)
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77BasicRoundTripLarger tests LZ77 on a larger image.
func TestLZ77BasicRoundTripLarger(t *testing.T) {
	img := makeGradientImage(64, 64)
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77RepeatingPattern tests that images with repeating patterns encode correctly.
func TestLZ77RepeatingPattern(t *testing.T) {
	img := makeRepeatingImage(32, 32, 4, 4)
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77RepeatingPatternLarge tests a large image with a repeating tile.
func TestLZ77RepeatingPatternLarge(t *testing.T) {
	img := makeRepeatingImage(64, 64, 8, 8)
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77CompressionImprovement verifies that LZ77 on repeating patterns produces smaller output.
func TestLZ77CompressionImprovement(t *testing.T) {
	img := makeRepeatingImage(64, 64, 4, 4)

	noLZ77, err := EncodeVP8LWithOptions(img, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode without LZ77: %v", err)
	}
	withLZ77, err := EncodeVP8LWithOptions(img, EncodeOptions{LZ77: true})
	if err != nil {
		t.Fatalf("encode with LZ77: %v", err)
	}

	t.Logf("without LZ77: %d bytes, with LZ77: %d bytes", len(noLZ77), len(withLZ77))
	if len(withLZ77) >= len(noLZ77) {
		t.Errorf("expected LZ77 to produce smaller output: %d >= %d", len(withLZ77), len(noLZ77))
	}
}

// TestLZ77WithTransforms tests LZ77 combined with subtract-green and predictor transforms.
func TestLZ77WithTransforms(t *testing.T) {
	img := makeGradientImage(32, 32)
	opts := EncodeOptions{
		LZ77:          true,
		SubtractGreen: true,
		Predictor:     true,
		PredictorBits: 3,
	}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77SolidColor verifies that a solid color image encodes and decodes correctly.
func TestLZ77SolidColor(t *testing.T) {
	colors := []color.NRGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
		{R: 128, G: 64, B: 32, A: 255},
	}
	for _, c := range colors {
		img := makeSolidImageT(32, 32, c)
		opts := EncodeOptions{LZ77: true}
		data, err := EncodeVP8LWithOptions(img, opts)
		if err != nil {
			t.Fatalf("encode color=%v: %v", c, err)
		}
		got, err := DecodeVP8L(data)
		if err != nil {
			t.Fatalf("decode color=%v: %v", c, err)
		}
		pixelsMatch(t, img, got)
	}
}

// TestLZ77WithAlpha verifies that images with alpha channels encode and decode correctly.
func TestLZ77WithAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: 128,
				A: uint8(x*y%200 + 55),
			})
		}
	}
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

// TestLZ77DefaultEncoder verifies that the default encoder (with LZ77 enabled) round-trips correctly.
func TestLZ77DefaultEncoder(t *testing.T) {
	sizes := [][2]int{{8, 8}, {16, 16}, {32, 32}, {64, 64}, {128, 128}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		img := makeGradientImage(w, h)
		data, err := EncodeVP8L(img)
		if err != nil {
			t.Fatalf("encode %dx%d: %v", w, h, err)
		}
		got, err := DecodeVP8L(data)
		if err != nil {
			t.Fatalf("decode %dx%d: %v", w, h, err)
		}
		pixelsMatch(t, img, got)
	}
}

// TestLZ77LongMatch verifies that images requiring matches longer than 255 pixels work correctly.
func TestLZ77LongMatch(t *testing.T) {
	// A 100x100 solid image will have a match of ~9999 pixels.
	img := makeSolidImageT(100, 100, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	opts := EncodeOptions{LZ77: true}
	data, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}
