package alpha

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// ---- GradientPredictor tests ----

func TestGradientPredictor(t *testing.T) {
	tests := []struct {
		a, b, c byte
		want    byte
	}{
		{100, 100, 50, 150},  // 100+100-50 = 150
		{0, 0, 0, 0},         // 0
		{255, 255, 255, 255}, // clamp: 255
		{0, 0, 255, 0},       // clamp low: 0+0-255 = -255 → 0
		{200, 200, 100, 255}, // 200+200-100 = 300 → clamp 255
		{128, 64, 32, 160},   // 128+64-32 = 160
	}
	for _, tt := range tests {
		got := GradientPredictor(tt.a, tt.b, tt.c)
		if got != tt.want {
			t.Errorf("GradientPredictor(%d,%d,%d) = %d, want %d", tt.a, tt.b, tt.c, got, tt.want)
		}
	}
}

// ---- Filter round-trip tests ----

func makeAlpha(width, height int, fillFn func(x, y int) byte) []byte {
	data := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			data[y*width+x] = fillFn(x, y)
		}
	}
	return data
}

func testFilterRoundTrip(t *testing.T, name string, alpha []byte, width, height int, filter FilterType) {
	t.Helper()
	filtered := ApplyFilter(alpha, width, height, filter)
	recovered := ReverseFilter(filtered, width, height, filter)
	if !bytes.Equal(recovered, alpha) {
		t.Errorf("%s filter=%d round-trip failed: got %v, want %v", name, filter, recovered[:min(len(recovered), 20)], alpha[:min(len(alpha), 20)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFilterNoneRoundTrip(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte(x*y % 256) })
	testFilterRoundTrip(t, "8x8", alpha, 8, 8, FilterNone)
}

func TestFilterHorizontalRoundTrip(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte((x + y*3) % 256) })
	testFilterRoundTrip(t, "8x8", alpha, 8, 8, FilterHorizontal)
}

func TestFilterVerticalRoundTrip(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte((x*2 + y) % 256) })
	testFilterRoundTrip(t, "8x8", alpha, 8, 8, FilterVertical)
}

func TestFilterGradientRoundTrip(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte((x*3 + y*5) % 256) })
	testFilterRoundTrip(t, "8x8", alpha, 8, 8, FilterGradient)
}

func TestFilterEdgeCases(t *testing.T) {
	filters := []FilterType{FilterNone, FilterHorizontal, FilterVertical, FilterGradient}

	// Single pixel
	for _, f := range filters {
		alpha := []byte{128}
		testFilterRoundTrip(t, "1x1", alpha, 1, 1, f)
	}

	// Single row
	for _, f := range filters {
		alpha := makeAlpha(10, 1, func(x, y int) byte { return byte(x * 25) })
		testFilterRoundTrip(t, "10x1", alpha, 10, 1, f)
	}

	// Single column
	for _, f := range filters {
		alpha := makeAlpha(1, 10, func(x, y int) byte { return byte(y * 25) })
		testFilterRoundTrip(t, "1x10", alpha, 1, 10, f)
	}

	// All zero
	for _, f := range filters {
		alpha := make([]byte, 16)
		testFilterRoundTrip(t, "all-zero 4x4", alpha, 4, 4, f)
	}

	// All 255
	for _, f := range filters {
		alpha := bytes.Repeat([]byte{255}, 16)
		testFilterRoundTrip(t, "all-255 4x4", alpha, 4, 4, f)
	}
}

func TestFilterNonePreservesData(t *testing.T) {
	alpha := makeAlpha(4, 4, func(x, y int) byte { return byte(x*64 + y*16) })
	filtered := ApplyFilter(alpha, 4, 4, FilterNone)
	if !bytes.Equal(filtered, alpha) {
		t.Error("FilterNone should not change data")
	}
}

// Row 0 and column 0 should be stored as-is for horizontal and gradient.
func TestFilterHorizontalRow0Col0(t *testing.T) {
	alpha := makeAlpha(4, 4, func(x, y int) byte { return byte(x*10 + y*40) })
	filtered := ApplyFilter(alpha, 4, 4, FilterHorizontal)
	// Row 0 should be unchanged.
	for x := 0; x < 4; x++ {
		if filtered[x] != alpha[x] {
			t.Errorf("horizontal: row0 col%d filtered=%d want=%d", x, filtered[x], alpha[x])
		}
	}
	// Column 0 of each row should be unchanged.
	for y := 0; y < 4; y++ {
		if filtered[y*4] != alpha[y*4] {
			t.Errorf("horizontal: row%d col0 filtered=%d want=%d", y, filtered[y*4], alpha[y*4])
		}
	}
}

func TestFilterVerticalRow0(t *testing.T) {
	alpha := makeAlpha(4, 4, func(x, y int) byte { return byte(x*10 + y*40) })
	filtered := ApplyFilter(alpha, 4, 4, FilterVertical)
	// Row 0 should be unchanged.
	for x := 0; x < 4; x++ {
		if filtered[x] != alpha[x] {
			t.Errorf("vertical: row0 col%d filtered=%d want=%d", x, filtered[x], alpha[x])
		}
	}
}

// Real-world-like patterns.
func TestFilterWithGradientPattern(t *testing.T) {
	// Linear gradient from 0 to 255 across width.
	alpha := makeAlpha(16, 16, func(x, y int) byte { return byte(x * 255 / 15) })
	for _, f := range []FilterType{FilterNone, FilterHorizontal, FilterVertical, FilterGradient} {
		testFilterRoundTrip(t, "gradient-pattern", alpha, 16, 16, f)
	}
}

func TestFilterWithSharpEdge(t *testing.T) {
	// Sharp edge: left half=0, right half=255.
	alpha := makeAlpha(16, 8, func(x, y int) byte {
		if x < 8 {
			return 0
		}
		return 255
	})
	for _, f := range []FilterType{FilterNone, FilterHorizontal, FilterVertical, FilterGradient} {
		testFilterRoundTrip(t, "sharp-edge", alpha, 16, 8, f)
	}
}

// ---- Compress/Decompress round-trip tests ----

func TestCompressDecompressRawMethod0(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte((x + y*8) % 256) })
	compressed, err := Compress(alpha, 8, 8, 0)
	if err != nil {
		t.Fatalf("Compress method=0: %v", err)
	}
	if !bytes.Equal(compressed, alpha) {
		t.Error("method=0: compressed should equal raw input")
	}
	recovered, err := Decompress(compressed, 8, 8, 0)
	if err != nil {
		t.Fatalf("Decompress method=0: %v", err)
	}
	if !bytes.Equal(recovered, alpha) {
		t.Error("method=0 round-trip failed")
	}
}

func TestCompressDecompressVP8LMethod1(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte((x + y*8) % 256) })
	compressed, err := Compress(alpha, 8, 8, 1)
	if err != nil {
		t.Fatalf("Compress method=1: %v", err)
	}
	recovered, err := Decompress(compressed, 8, 8, 1)
	if err != nil {
		t.Fatalf("Decompress method=1: %v", err)
	}
	if !bytes.Equal(recovered, alpha) {
		t.Error("method=1 round-trip failed")
	}
}

func TestCompressDecompressUniformAlpha(t *testing.T) {
	// All 255 — uniform alpha.
	alpha := bytes.Repeat([]byte{255}, 16*16)
	for _, method := range []int{0, 1} {
		compressed, err := Compress(alpha, 16, 16, method)
		if err != nil {
			t.Fatalf("Compress method=%d: %v", method, err)
		}
		recovered, err := Decompress(compressed, 16, 16, method)
		if err != nil {
			t.Fatalf("Decompress method=%d: %v", method, err)
		}
		if !bytes.Equal(recovered, alpha) {
			t.Errorf("uniform-255 method=%d round-trip failed", method)
		}
	}
}

func TestCompressDecompressGradientAlpha(t *testing.T) {
	alpha := makeAlpha(16, 16, func(x, y int) byte { return byte(x * 16) })
	for _, method := range []int{0, 1} {
		compressed, err := Compress(alpha, 16, 16, method)
		if err != nil {
			t.Fatalf("Compress method=%d: %v", method, err)
		}
		recovered, err := Decompress(compressed, 16, 16, method)
		if err != nil {
			t.Fatalf("Decompress method=%d: %v", method, err)
		}
		if !bytes.Equal(recovered, alpha) {
			t.Errorf("gradient method=%d round-trip failed", method)
		}
	}
}

// ---- EncodeALPH / DecodeALPH integration tests ----

func TestEncodeDecodeALPHAllCombinations(t *testing.T) {
	alpha := makeAlpha(12, 10, func(x, y int) byte { return byte((x*17 + y*31) % 256) })
	filters := []FilterType{FilterNone, FilterHorizontal, FilterVertical, FilterGradient}
	methods := []int{0, 1}

	for _, f := range filters {
		for _, m := range methods {
			chunk, err := EncodeALPH(alpha, 12, 10, m, f)
			if err != nil {
				t.Errorf("EncodeALPH method=%d filter=%d: %v", m, f, err)
				continue
			}
			recovered, err := DecodeALPH(chunk, 12, 10)
			if err != nil {
				t.Errorf("DecodeALPH method=%d filter=%d: %v", m, f, err)
				continue
			}
			if !bytes.Equal(recovered, alpha) {
				t.Errorf("ALPH round-trip failed method=%d filter=%d", m, f)
			}
		}
	}
}

func TestEncodeDecodeALPHFlagsEncoding(t *testing.T) {
	alpha := makeAlpha(4, 4, func(x, y int) byte { return byte(x * 64) })
	chunk, err := EncodeALPH(alpha, 4, 4, 1, FilterVertical)
	if err != nil {
		t.Fatalf("EncodeALPH: %v", err)
	}
	// Flags byte: method=1, filter=2 → 1 | (2<<2) = 0x09
	wantFlags := byte(1 | (2 << 2))
	if chunk[0] != wantFlags {
		t.Errorf("flags byte = 0x%02x, want 0x%02x", chunk[0], wantFlags)
	}
}

func TestEncodeDecodeALPHUniformOpaque(t *testing.T) {
	alpha := bytes.Repeat([]byte{255}, 20*20)
	chunk, err := EncodeALPH(alpha, 20, 20, 1, FilterNone)
	if err != nil {
		t.Fatalf("EncodeALPH: %v", err)
	}
	recovered, err := DecodeALPH(chunk, 20, 20)
	if err != nil {
		t.Fatalf("DecodeALPH: %v", err)
	}
	if !bytes.Equal(recovered, alpha) {
		t.Error("uniform opaque round-trip failed")
	}
}

func TestEncodeDecodeALPHWithNRGBAImage(t *testing.T) {
	// Build an NRGBA image with varying alpha.
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			a := byte((x + y*8) % 256)
			img.SetNRGBA(x, y, color.NRGBA{R: 100, G: 150, B: 200, A: a})
		}
	}
	extracted := ExtractAlpha(img)
	chunk, err := EncodeALPH(extracted, 8, 8, 1, FilterGradient)
	if err != nil {
		t.Fatalf("EncodeALPH: %v", err)
	}
	recovered, err := DecodeALPH(chunk, 8, 8)
	if err != nil {
		t.Fatalf("DecodeALPH: %v", err)
	}
	if !bytes.Equal(recovered, extracted) {
		t.Error("NRGBA extracted alpha round-trip failed")
	}
}

// ---- ExtractAlpha / ApplyAlpha tests ----

func TestExtractAlphaFromNRGBA(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: byte(x*64 + y*4)})
		}
	}
	alpha := ExtractAlpha(img)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			want := byte(x*64 + y*4)
			got := alpha[y*4+x]
			if got != want {
				t.Errorf("ExtractAlpha[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

// ---- QuantizeAlpha tests ----

func TestQuantizeAlpha_KnownValues(t *testing.T) {
	input := []byte{0, 63, 64, 127, 128, 191, 192, 255}
	got := QuantizeAlpha(input, 4)
	expected := []byte{32, 32, 96, 96, 160, 160, 224, 224}
	for i, v := range got {
		if v != expected[i] {
			t.Errorf("QuantizeAlpha[%d]: got %d, want %d (input=%d)", i, v, expected[i], input[i])
		}
	}
}

func TestQuantizeAlpha_NoOp(t *testing.T) {
	input := []byte{10, 50, 100, 200}
	for _, levels := range []int{0, 256} {
		got := QuantizeAlpha(input, levels)
		for i, v := range got {
			if v != input[i] {
				t.Errorf("QuantizeAlpha levels=%d[%d]: got %d, want %d", levels, i, v, input[i])
			}
		}
	}
}

func TestQuantizeAlpha_ReducesDistinctValues(t *testing.T) {
	input := make([]byte, 256)
	for i := range input {
		input[i] = byte(i)
	}
	got := QuantizeAlpha(input, 16)
	distinct := make(map[byte]bool)
	for _, v := range got {
		distinct[v] = true
	}
	if len(distinct) > 16 {
		t.Errorf("QuantizeAlpha with 16 levels produced %d distinct values, want ≤16", len(distinct))
	}
}

// ---- EncodeALPHWithPreprocessing tests ----

func TestEncodeALPHWithPreprocessing_RoundTrip(t *testing.T) {
	alpha := makeAlpha(16, 16, func(x, y int) byte { return byte((x + y) * 255 / 30) })
	payload, err := EncodeALPHWithPreprocessing(alpha, 16, 16, 0, FilterNone, true)
	if err != nil {
		t.Fatalf("EncodeALPHWithPreprocessing: %v", err)
	}
	// Check preprocessing bits are set.
	if payload[0]>>4&0x03 != 1 {
		t.Errorf("flags byte: preprocessing bits = %d, want 1", payload[0]>>4&0x03)
	}
	got, err := DecodeALPH(payload, 16, 16)
	if err != nil {
		t.Fatalf("DecodeALPH: %v", err)
	}
	step := 256.0 / 64.0
	maxErr := int(step)
	for i, v := range got {
		diff := int(v) - int(alpha[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > maxErr {
			t.Errorf("pixel %d: original=%d decoded=%d diff=%d > maxErr=%d", i, alpha[i], v, diff, maxErr)
		}
	}
}

func TestEncodeALPHWithPreprocessing_BackwardCompat(t *testing.T) {
	alpha := makeAlpha(8, 8, func(x, y int) byte { return byte(x * 32) })
	payload, err := EncodeALPHWithPreprocessing(alpha, 8, 8, 0, FilterNone, false)
	if err != nil {
		t.Fatalf("EncodeALPHWithPreprocessing: %v", err)
	}
	if payload[0]>>4&0x03 != 0 {
		t.Errorf("flags byte: preprocessing bits = %d, want 0", payload[0]>>4&0x03)
	}
}

func TestApplyAlphaToYCbCr(t *testing.T) {
	// Build a plain YCbCr image (all white).
	ycbcr := image.NewYCbCr(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio420)
	for i := range ycbcr.Y {
		ycbcr.Y[i] = 235 // Y=235 ≈ white luma
	}
	for i := range ycbcr.Cb {
		ycbcr.Cb[i] = 128
	}
	for i := range ycbcr.Cr {
		ycbcr.Cr[i] = 128
	}

	alphaPlane := makeAlpha(4, 4, func(x, y int) byte { return byte(x * 64) })
	out := ApplyAlpha(ycbcr, alphaPlane)

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			_, _, _, a := out.At(x, y).RGBA()
			wantA := uint32(byte(x * 64))
			gotA := a >> 8
			if gotA != wantA {
				t.Errorf("ApplyAlpha[%d,%d] alpha=%d, want %d", x, y, gotA, wantA)
			}
		}
	}
}
