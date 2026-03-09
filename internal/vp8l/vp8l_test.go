package vp8l

import (
	"image"
	"image/color"
	"testing"
)

// makeTestImage creates a simple NRGBA test image.
func makeTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 10 % 256),
				G: uint8(y * 10 % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	orig := makeTestImage(8, 8)

	data, err := EncodeVP8L(orig)
	if err != nil {
		t.Fatalf("EncodeVP8L: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("EncodeVP8L: empty output")
	}

	got, err := DecodeVP8L(data)
	if err != nil {
		t.Fatalf("DecodeVP8L: %v", err)
	}

	if got.Bounds() != orig.Bounds() {
		t.Fatalf("bounds mismatch: got %v, want %v", got.Bounds(), orig.Bounds())
	}

	// Lossless: pixels must match exactly.
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := orig.NRGBAAt(x, y)
			have := got.NRGBAAt(x, y)
			if want != have {
				t.Errorf("pixel (%d,%d): want %v, got %v", x, y, want, have)
			}
		}
	}
}

func TestDecodeConfig(t *testing.T) {
	orig := makeTestImage(16, 32)
	data, err := EncodeVP8L(orig)
	if err != nil {
		t.Fatalf("EncodeVP8L: %v", err)
	}

	dec := NewDecoder(data)
	cfg, err := dec.DecodeConfig()
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 16 || cfg.Height != 32 {
		t.Errorf("DecodeConfig: got %dx%d, want 16x32", cfg.Width, cfg.Height)
	}
}

func TestBitReaderWriter(t *testing.T) {
	bw := newBitWriter()
	bw.writeBits(0b1011, 4)
	bw.writeBits(0b110, 3)
	bw.writeBits(255, 8)
	data := bw.bytes()

	br := newBitReader(data)
	v1, _ := br.readBits(4)
	v2, _ := br.readBits(3)
	v3, _ := br.readBits(8)

	if v1 != 0b1011 {
		t.Errorf("readBits(4): got %b, want 1011", v1)
	}
	if v2 != 0b110 {
		t.Errorf("readBits(3): got %b, want 110", v2)
	}
	if v3 != 255 {
		t.Errorf("readBits(8): got %d, want 255", v3)
	}
}

func TestSubtractGreenRoundtrip(t *testing.T) {
	pixels := []uint32{
		0xff102030, 0xff405060, 0xff708090,
	}
	orig := make([]uint32, len(pixels))
	copy(orig, pixels)

	applySubtractGreen(pixels)
	inverseSubtractGreen(pixels)

	for i, p := range pixels {
		if p != orig[i] {
			t.Errorf("pixel %d: got %08x, want %08x", i, p, orig[i])
		}
	}
}
