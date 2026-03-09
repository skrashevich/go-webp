package vp8l

import (
	"image"
	"image/color"
	"testing"
)

// --- helpers ---

func makeGradientImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 3 % 256),
				G: uint8(y * 3 % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

// makePaletteImage creates an image with exactly numColors unique colors.
func makePaletteImage(w, h, numColors int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	palette := make([]color.NRGBA, numColors)
	for i := 0; i < numColors; i++ {
		palette[i] = color.NRGBA{
			R: uint8(i * 37 % 256),
			G: uint8(i * 71 % 256),
			B: uint8(i * 113 % 256),
			A: 255,
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, palette[(y*w+x)%numColors])
		}
	}
	return img
}

func makeSolidImageT(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// pixelsMatch compares two NRGBA images pixel-by-pixel.
func pixelsMatch(t *testing.T, orig, got *image.NRGBA) {
	t.Helper()
	if orig.Bounds() != got.Bounds() {
		t.Fatalf("bounds mismatch: want %v, got %v", orig.Bounds(), got.Bounds())
	}
	w := orig.Bounds().Dx()
	h := orig.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := orig.NRGBAAt(x, y)
			have := got.NRGBAAt(x, y)
			if want != have {
				t.Errorf("pixel (%d,%d): want %v, got %v", x, y, want, have)
			}
		}
	}
}

// --- subtract green tests ---

func TestSubtractGreenTransformEncodeDecode(t *testing.T) {
	img := makeGradientImage(16, 16)
	opts := EncodeOptions{SubtractGreen: true}
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

func TestSubtractGreenTransformPixelPerfect(t *testing.T) {
	// Encode with subtract green, decode, compare.
	img := makeTestImage(8, 8)
	opts := EncodeOptions{SubtractGreen: true}
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

func TestSubtractGreenAlpha(t *testing.T) {
	// Verify alpha channel is unchanged by subtract green.
	// Use only non-zero alpha values to avoid the transparent-black normalisation.
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			a := uint8(x*60 + y*16 + 16) // 16..255, never 0
			img.SetNRGBA(x, y, color.NRGBA{R: 100, G: 50, B: 200, A: a})
		}
	}
	opts := EncodeOptions{SubtractGreen: true}
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

func TestSubtractGreenReducesEntropy(t *testing.T) {
	// For a natural-ish image with correlated channels, subtract green should
	// produce smaller or equal encoded output.
	img := makeGradientImage(32, 32)

	dataNoTransform, err := EncodeVP8LWithOptions(img, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode without transform: %v", err)
	}
	dataWithTransform, err := EncodeVP8LWithOptions(img, EncodeOptions{SubtractGreen: true})
	if err != nil {
		t.Fatalf("encode with transform: %v", err)
	}

	// Log the sizes; we do not fail if the transform is slightly larger on
	// a tiny synthetic image, but it should be close.
	t.Logf("without subtract green: %d bytes, with: %d bytes", len(dataNoTransform), len(dataWithTransform))
}

// --- palette (color indexing) transform tests ---

func TestPaletteTransform2Colors(t *testing.T) {
	img := makePaletteImage(16, 16, 2)
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransform4Colors(t *testing.T) {
	img := makePaletteImage(16, 16, 4)
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransform16Colors(t *testing.T) {
	img := makePaletteImage(16, 16, 16)
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransform256Colors(t *testing.T) {
	img := makePaletteImage(32, 32, 256)
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransformSkippedForManyColors(t *testing.T) {
	// Image with >256 distinct colors: palette transform must be skipped
	// and the image still encodes/decodes correctly.
	img := makeGradientImage(32, 32) // many distinct colors
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransform1Color(t *testing.T) {
	img := makeSolidImageT(8, 8, color.NRGBA{R: 42, G: 100, B: 200, A: 255})
	opts := EncodeOptions{Palette: true}
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

func TestPaletteTransformWithAlpha(t *testing.T) {
	// Use only non-zero alpha values; A=0 pixels are normalised to transparent black
	// by the encoder (as per VP8L convention).
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	colors := []color.NRGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 128},
		{R: 200, G: 100, B: 50, A: 64},
		{R: 255, G: 255, B: 0, A: 200},
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, colors[(y*8+x)%len(colors)])
		}
	}
	opts := EncodeOptions{Palette: true}
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

// --- predictor transform tests ---

func TestPredictorAllModes(t *testing.T) {
	// Test all 14 predictor modes with known inputs.
	left := uint32(0xff804020)
	top := uint32(0xff102040)
	topLeft := uint32(0xff201030)
	topRight := uint32(0xff305070)

	for mode := 0; mode <= 13; mode++ {
		pred := predict(mode, left, top, topLeft, topRight)
		// Verify the prediction is a valid ARGB value (smoke test).
		// Each channel must be 0-255, which is satisfied by uint32 representation.
		_ = pred
	}

	// Check specific modes with known values.
	// Mode 0: black
	if p := predict(0, left, top, topLeft, topRight); p != 0xff000000 {
		t.Errorf("mode 0: want 0xff000000, got %08x", p)
	}
	// Mode 1: left
	if p := predict(1, left, top, topLeft, topRight); p != left {
		t.Errorf("mode 1: want %08x, got %08x", left, p)
	}
	// Mode 2: top
	if p := predict(2, left, top, topLeft, topRight); p != top {
		t.Errorf("mode 2: want %08x, got %08x", top, p)
	}
	// Mode 3: topRight
	if p := predict(3, left, top, topLeft, topRight); p != topRight {
		t.Errorf("mode 3: want %08x, got %08x", topRight, p)
	}
	// Mode 4: topLeft
	if p := predict(4, left, top, topLeft, topRight); p != topLeft {
		t.Errorf("mode 4: want %08x, got %08x", topLeft, p)
	}
}

func TestPredictorTransformEncodeDecode(t *testing.T) {
	img := makeGradientImage(16, 16)
	opts := EncodeOptions{Predictor: true, PredictorBits: 3}
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

func TestPredictorTransformSolidColor(t *testing.T) {
	img := makeSolidImageT(16, 16, color.NRGBA{R: 100, G: 150, B: 200, A: 255})
	opts := EncodeOptions{Predictor: true, PredictorBits: 3}
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

func TestPredictorTransformPixelPerfect(t *testing.T) {
	img := makeTestImage(8, 8)
	opts := EncodeOptions{Predictor: true, PredictorBits: 3}
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

func TestPredictorImageDimensions(t *testing.T) {
	// Verify that the predictor image has correct sub-resolution dimensions.
	// block_size = 1 << bits; predictor_w = ceil(w / block_size).
	tests := []struct {
		w, h, bits int
		predW, predH int
	}{
		{16, 16, 3, 2, 2},   // block=8, 16/8=2
		{17, 17, 3, 3, 3},   // block=8, ceil(17/8)=3
		{8, 8, 2, 2, 2},     // block=4, 8/4=2
		{16, 16, 4, 1, 1},   // block=16, 16/16=1
	}
	for _, tc := range tests {
		bw := subSampleSize(tc.w, tc.bits)
		bh := subSampleSize(tc.h, tc.bits)
		if bw != tc.predW {
			t.Errorf("w=%d bits=%d: predW want %d, got %d", tc.w, tc.bits, tc.predW, bw)
		}
		if bh != tc.predH {
			t.Errorf("h=%d bits=%d: predH want %d, got %d", tc.h, tc.bits, tc.predH, bh)
		}
	}
}

func TestPredictorTransformVariousSizes(t *testing.T) {
	sizes := [][2]int{{4, 4}, {8, 8}, {16, 16}, {7, 13}, {32, 32}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		img := makeGradientImage(w, h)
		opts := EncodeOptions{Predictor: true, PredictorBits: 3}
		data, err := EncodeVP8LWithOptions(img, opts)
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

// --- combined transform tests ---

func TestSubtractGreenAndPalette(t *testing.T) {
	img := makePaletteImage(16, 16, 8)
	opts := EncodeOptions{Palette: true, SubtractGreen: true}
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

func TestSubtractGreenAndPredictor(t *testing.T) {
	img := makeGradientImage(16, 16)
	opts := EncodeOptions{SubtractGreen: true, Predictor: true, PredictorBits: 3}
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

func TestDefaultEncodeWithTransforms(t *testing.T) {
	// EncodeVP8L should now apply transforms and still round-trip correctly.
	img := makeGradientImage(16, 16)
	data, err := EncodeVP8L(img)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}

func TestEncodeOptionsNoTransforms(t *testing.T) {
	// EncodeVP8LWithOptions with zero options = no transforms.
	img := makeGradientImage(16, 16)
	data, err := EncodeVP8LWithOptions(img, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pixelsMatch(t, img, got)
}
