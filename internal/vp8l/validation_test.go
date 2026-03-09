package vp8l

import (
	"image"
	"image/color"
	"testing"
)

func makeHeaderBitstream(width, height int, hasAlpha bool, version uint32) []byte {
	bw := newBitWriter()
	bw.writeBits(vp8lSignature, 8)
	bw.writeBits(uint32(width-1), 14)
	bw.writeBits(uint32(height-1), 14)
	bw.writeBit(hasAlpha)
	bw.writeBits(version, 3)
	return bw.bytes()
}

func makeBitstreamWithColorCacheBits(bits uint32) []byte {
	bw := newBitWriter()
	bw.writeBits(vp8lSignature, 8)
	bw.writeBits(0, 14)   // width = 1
	bw.writeBits(0, 14)   // height = 1
	bw.writeBit(false)    // alpha hint
	bw.writeBits(0, 3)    // version
	bw.writeBit(false)    // no transforms
	bw.writeBit(true)     // use color cache
	bw.writeBits(bits, 4) // invalid cache bits
	return bw.bytes()
}

// --- validateStreamDimensions tests ---

func TestValidateStreamDimensions_Valid(t *testing.T) {
	if err := validateStreamDimensions(100, 200); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStreamDimensions_ZeroWidth(t *testing.T) {
	if err := validateStreamDimensions(0, 10); err == nil {
		t.Fatal("accepted zero width")
	}
}

func TestValidateStreamDimensions_NegativeHeight(t *testing.T) {
	if err := validateStreamDimensions(10, -1); err == nil {
		t.Fatal("accepted negative height")
	}
}

func TestValidateStreamDimensions_Overflow(t *testing.T) {
	// maxInt()/2 + 1 ensures width*height overflows int
	half := maxInt()/2 + 1
	if err := validateStreamDimensions(half, 2); err == nil {
		t.Fatal("accepted overflowing dimensions")
	}
}

// --- validateHeaderDimensions tests ---

func TestValidateHeaderDimensions_Valid(t *testing.T) {
	if err := validateHeaderDimensions(16384, 16384); err != nil {
		t.Fatalf("rejected max valid header dimensions: %v", err)
	}
}

func TestValidateHeaderDimensions_ExceedsLimit(t *testing.T) {
	if err := validateHeaderDimensions(16385, 1); err == nil {
		t.Fatal("accepted dimension exceeding 16384 limit")
	}
}

func TestValidateHeaderDimensions_HeightExceedsLimit(t *testing.T) {
	if err := validateHeaderDimensions(1, 16385); err == nil {
		t.Fatal("accepted height exceeding 16384 limit")
	}
}

// --- validateColorCacheBits tests ---

func TestValidateColorCacheBits_Valid(t *testing.T) {
	for bits := uint32(1); bits <= 11; bits++ {
		if err := validateColorCacheBits(bits); err != nil {
			t.Errorf("rejected valid color cache bits=%d: %v", bits, err)
		}
	}
}

func TestValidateColorCacheBits_Zero(t *testing.T) {
	if err := validateColorCacheBits(0); err == nil {
		t.Fatal("accepted color cache bits=0")
	}
}

func TestValidateColorCacheBits_TooLarge(t *testing.T) {
	if err := validateColorCacheBits(12); err == nil {
		t.Fatal("accepted color cache bits=12")
	}
}

// --- readImageHeader tests ---

func TestReadImageHeader_Valid(t *testing.T) {
	data := makeHeaderBitstream(100, 200, true, 0)
	br := newBitReader(data[1:]) // skip signature byte
	hdr, err := readImageHeader(br)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdr.width != 100 || hdr.height != 200 {
		t.Errorf("got %dx%d, want 100x200", hdr.width, hdr.height)
	}
	if !hdr.hasAlpha {
		t.Error("expected hasAlpha=true")
	}
}

func TestReadImageHeader_NoAlpha(t *testing.T) {
	data := makeHeaderBitstream(1, 1, false, 0)
	br := newBitReader(data[1:])
	hdr, err := readImageHeader(br)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdr.hasAlpha {
		t.Error("expected hasAlpha=false")
	}
}

func TestReadImageHeader_InvalidVersion(t *testing.T) {
	data := makeHeaderBitstream(1, 1, false, 3)
	br := newBitReader(data[1:])
	if _, err := readImageHeader(br); err == nil {
		t.Fatal("accepted invalid version")
	}
}

func TestReadImageHeader_EmptyData(t *testing.T) {
	br := newBitReader(nil)
	if _, err := readImageHeader(br); err == nil {
		t.Fatal("accepted empty data")
	}
}

// --- imageToPixels / pixelsToNRGBA round-trip tests ---

func TestImageToPixelsRoundTrip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 60),
				G: uint8(y * 60),
				B: 128,
				A: 255,
			})
		}
	}
	pixels, w, h, err := imageToPixels(img)
	if err != nil {
		t.Fatalf("imageToPixels: %v", err)
	}
	if w != 4 || h != 4 {
		t.Fatalf("got %dx%d, want 4x4", w, h)
	}
	out := pixelsToNRGBA(pixels, w, h)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			want := img.NRGBAAt(x, y)
			got := out.NRGBAAt(x, y)
			if got != want {
				t.Errorf("pixel (%d,%d): got %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestImageToPixels_TransparentPixelsZeroed(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 128, B: 64, A: 0})
	pixels, _, _, err := imageToPixels(img)
	if err != nil {
		t.Fatalf("imageToPixels: %v", err)
	}
	if pixels[0] != 0 {
		t.Errorf("transparent pixel not zeroed: got 0x%08x", pixels[0])
	}
}

func TestImageToPixels_ZeroSize(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	if _, _, _, err := imageToPixels(img); err == nil {
		t.Fatal("accepted zero-sized image")
	}
}

func TestImageToPixels_GenericImage(t *testing.T) {
	// Test with a non-NRGBA image type (image.RGBA)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetRGBA(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	pixels, w, h, err := imageToPixels(img)
	if err != nil {
		t.Fatalf("imageToPixels: %v", err)
	}
	if w != 2 || h != 2 {
		t.Fatalf("got %dx%d, want 2x2", w, h)
	}
	// Check first pixel is red
	r := (pixels[0] >> 16) & 0xff
	g := (pixels[0] >> 8) & 0xff
	if r != 255 || g != 0 {
		t.Errorf("pixel (0,0): got r=%d g=%d, want r=255 g=0", r, g)
	}
}

// --- defaultEncodeOptions tests ---

func TestDefaultEncodeOptions_SmallImage(t *testing.T) {
	opts := defaultEncodeOptions(4, 4)
	if !opts.SubtractGreen {
		t.Error("expected SubtractGreen for small image")
	}
	if !opts.Palette {
		t.Error("expected Palette for small image")
	}
	if opts.Predictor {
		t.Error("unexpected Predictor for small image")
	}
	if opts.Color {
		t.Error("unexpected Color for small image")
	}
}

func TestDefaultEncodeOptions_LargeImage(t *testing.T) {
	opts := defaultEncodeOptions(100, 100)
	if !opts.SubtractGreen {
		t.Error("expected SubtractGreen")
	}
	if !opts.Palette {
		t.Error("expected Palette")
	}
	if !opts.Predictor {
		t.Error("expected Predictor for large image")
	}
	if !opts.Color {
		t.Error("expected Color for large image")
	}
	if opts.PredictorBits != 4 {
		t.Errorf("PredictorBits: got %d, want 4", opts.PredictorBits)
	}
	if opts.ColorBits != 4 {
		t.Errorf("ColorBits: got %d, want 4", opts.ColorBits)
	}
}

// --- EncodeAlphaPlane / DecodeAlphaPlane round-trip tests ---

func TestAlphaPlaneRoundTrip(t *testing.T) {
	alpha := make([]byte, 16*16)
	for i := range alpha {
		alpha[i] = byte(i)
	}
	encoded, err := EncodeAlphaPlane(alpha, 16, 16)
	if err != nil {
		t.Fatalf("EncodeAlphaPlane: %v", err)
	}
	decoded, err := DecodeAlphaPlane(encoded, 16, 16)
	if err != nil {
		t.Fatalf("DecodeAlphaPlane: %v", err)
	}
	for i := range alpha {
		if decoded[i] != alpha[i] {
			t.Errorf("alpha[%d]: got %d, want %d", i, decoded[i], alpha[i])
			break
		}
	}
}

func TestAlphaPlaneRoundTrip_Uniform(t *testing.T) {
	alpha := make([]byte, 8*8)
	for i := range alpha {
		alpha[i] = 200
	}
	encoded, err := EncodeAlphaPlane(alpha, 8, 8)
	if err != nil {
		t.Fatalf("EncodeAlphaPlane: %v", err)
	}
	decoded, err := DecodeAlphaPlane(encoded, 8, 8)
	if err != nil {
		t.Fatalf("DecodeAlphaPlane: %v", err)
	}
	for i := range alpha {
		if decoded[i] != 200 {
			t.Errorf("alpha[%d]: got %d, want 200", i, decoded[i])
			break
		}
	}
}

func TestEncodeAlphaPlane_SizeMismatch(t *testing.T) {
	if _, err := EncodeAlphaPlane(make([]byte, 10), 4, 4); err == nil {
		t.Fatal("accepted mismatched alpha plane size")
	}
}

func TestEncodeAlphaPlane_ZeroDimensions(t *testing.T) {
	if _, err := EncodeAlphaPlane(nil, 0, 0); err == nil {
		t.Fatal("accepted zero dimensions")
	}
}

// --- Encode/Decode integration validation tests ---

func TestEncodeVP8LRejectsZeroSizedImage(t *testing.T) {
	if _, err := EncodeVP8L(image.NewNRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Fatal("EncodeVP8L accepted zero-sized image")
	}
}

func TestEncodeVP8LRejectsOversizedHeaderDimensions(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16385, 1))
	if _, err := EncodeVP8L(img); err == nil {
		t.Fatal("EncodeVP8L accepted width that exceeds 14-bit header limit")
	}
}

func TestDecodeConfigRejectsInvalidVersion(t *testing.T) {
	data := makeHeaderBitstream(1, 1, false, 1)
	if _, err := NewDecoder(data).DecodeConfig(); err == nil {
		t.Fatal("DecodeConfig accepted invalid VP8L version")
	}
}

func TestDecodeRejectsInvalidColorCacheBits(t *testing.T) {
	for _, bits := range []uint32{0, 12, 15} {
		data := makeBitstreamWithColorCacheBits(bits)
		if _, err := DecodeVP8L(data); err == nil {
			t.Fatalf("DecodeVP8L accepted invalid color cache bits=%d", bits)
		}
	}
}

func TestEncodeVP8LMaxHeaderDimension(t *testing.T) {
	// 16384 is max valid — should not error on dimensions check
	// (may be slow to allocate, so use a 1-pixel tall image)
	img := image.NewNRGBA(image.Rect(0, 0, 16384, 1))
	if _, err := EncodeVP8L(img); err != nil {
		t.Fatalf("EncodeVP8L rejected max valid dimension: %v", err)
	}
}

func TestEncodeVP8LWithOptions_NoTransforms(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	opts := EncodeOptions{} // all transforms disabled
	encoded, err := EncodeVP8LWithOptions(img, opts)
	if err != nil {
		t.Fatalf("EncodeVP8LWithOptions: %v", err)
	}
	decoded, err := DecodeVP8L(encoded)
	if err != nil {
		t.Fatalf("DecodeVP8L: %v", err)
	}
	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 4 {
		t.Errorf("got %v, want 4x4", decoded.Bounds())
	}
}
