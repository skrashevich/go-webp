package vp8l

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// pixelEqual compares two NRGBA images pixel-by-pixel.
// Returns the first mismatch or empty string on success.
func pixelsIdentical(t *testing.T, want, got *image.NRGBA) bool {
	t.Helper()
	if want.Bounds() != got.Bounds() {
		t.Errorf("bounds mismatch: want %v, got %v", want.Bounds(), got.Bounds())
		return false
	}
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			w := want.NRGBAAt(x, y)
			g := got.NRGBAAt(x, y)
			if w != g {
				t.Errorf("pixel (%d,%d): want %v, got %v", x, y, w, g)
				return false
			}
		}
	}
	return true
}

// encodeDecodeRoundtrip encodes img and decodes the result.
func encodeDecodeRoundtrip(img image.Image) (*image.NRGBA, error) {
	data, err := EncodeVP8L(img)
	if err != nil {
		return nil, err
	}
	return DecodeVP8L(data)
}

// makeSolidImage creates an image of uniform color.
func makeSolidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// toNRGBA converts any image to *image.NRGBA by round-tripping through encode/decode.
func toNRGBADirect(img image.Image) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

// findFfmpeg returns path to ffmpeg or empty string.
func findFfmpeg() string {
	candidates := []string{
		"/opt/homebrew/opt/ffmpeg-full/bin/ffmpeg",
		"/opt/homebrew/bin/ffmpeg",
		"/usr/local/bin/ffmpeg",
		"ffmpeg",
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	return ""
}

// TestVP8LRoundtripPixelPerfect tests solid color images at various sizes.
// For lossless encoding, decoded pixels must be identical to original.
func TestVP8LRoundtripPixelPerfect(t *testing.T) {
	sizes := []struct{ w, h int }{
		{1, 1},
		{16, 16},
		{32, 32},
		{64, 64},
		{128, 128},
		{255, 255},
	}
	colors := []color.NRGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
		{R: 128, G: 64, B: 32, A: 255},
		{R: 0, G: 0, B: 0, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
	}

	for _, sz := range sizes {
		for _, c := range colors {
			t.Run("", func(t *testing.T) {
				orig := makeSolidImage(sz.w, sz.h, c)
				got, err := encodeDecodeRoundtrip(orig)
				if err != nil {
					t.Fatalf("%dx%d color=%v encode/decode: %v", sz.w, sz.h, c, err)
				}
				pixelsIdentical(t, orig, got)
			})
		}
	}
}

// TestVP8LRoundtripGradients tests gradient images (horizontal, vertical, diagonal).
func TestVP8LRoundtripGradients(t *testing.T) {
	tests := []struct {
		name string
		fn   func(x, y, w, h int) color.NRGBA
	}{
		{
			name: "horizontal",
			fn: func(x, y, w, h int) color.NRGBA {
				v := uint8(x * 255 / (w - 1))
				return color.NRGBA{R: v, G: v, B: v, A: 255}
			},
		},
		{
			name: "vertical",
			fn: func(x, y, w, h int) color.NRGBA {
				v := uint8(y * 255 / (h - 1))
				return color.NRGBA{R: v, G: v, B: v, A: 255}
			},
		},
		{
			name: "diagonal",
			fn: func(x, y, w, h int) color.NRGBA {
				v := uint8((x + y) * 255 / (w + h - 2))
				return color.NRGBA{R: v, G: uint8(255 - int(v)), B: 128, A: 255}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h := 64, 64
			orig := image.NewNRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					orig.SetNRGBA(x, y, tc.fn(x, y, w, h))
				}
			}
			got, err := encodeDecodeRoundtrip(orig)
			if err != nil {
				t.Fatalf("encode/decode: %v", err)
			}
			pixelsIdentical(t, orig, got)
		})
	}
}

// TestVP8LRoundtripRandomPixels tests random pixel patterns with fixed seed.
func TestVP8LRoundtripRandomPixels(t *testing.T) {
	sizes := []struct{ w, h int }{
		{16, 16},
		{64, 64},
		{100, 100},
	}
	for _, sz := range sizes {
		t.Run("", func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			orig := image.NewNRGBA(image.Rect(0, 0, sz.w, sz.h))
			for y := 0; y < sz.h; y++ {
				for x := 0; x < sz.w; x++ {
					orig.SetNRGBA(x, y, color.NRGBA{
						R: uint8(rng.Intn(256)),
						G: uint8(rng.Intn(256)),
						B: uint8(rng.Intn(256)),
						A: 255,
					})
				}
			}
			got, err := encodeDecodeRoundtrip(orig)
			if err != nil {
				t.Fatalf("%dx%d encode/decode: %v", sz.w, sz.h, err)
			}
			pixelsIdentical(t, orig, got)
		})
	}
}

// TestVP8LRoundtripAllBlack tests all-black images.
func TestVP8LRoundtripAllBlack(t *testing.T) {
	orig := makeSolidImage(64, 64, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	got, err := encodeDecodeRoundtrip(orig)
	if err != nil {
		t.Fatalf("encode/decode: %v", err)
	}
	pixelsIdentical(t, orig, got)
}

// TestVP8LRoundtripAllWhite tests all-white images.
func TestVP8LRoundtripAllWhite(t *testing.T) {
	orig := makeSolidImage(64, 64, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	got, err := encodeDecodeRoundtrip(orig)
	if err != nil {
		t.Fatalf("encode/decode: %v", err)
	}
	pixelsIdentical(t, orig, got)
}

// TestVP8LRoundtripTransparency tests images with alpha channel.
// The encoder reads pixels via image.Image.At().RGBA() which returns premultiplied
// alpha, so only fully-opaque (A=255) and fully-transparent (A=0) pixels are
// round-trip lossless at the NRGBA level. Semi-transparent pixels are tested for
// idempotency (encode→decode→encode produces identical bitstream).
func TestVP8LRoundtripTransparency(t *testing.T) {
	t.Run("fully_transparent", func(t *testing.T) {
		// All transparent pixels: alpha=0, RGB doesn't matter (zeroed by encoder).
		orig := makeSolidImage(32, 32, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
		got, err := encodeDecodeRoundtrip(orig)
		if err != nil {
			t.Fatalf("encode/decode: %v", err)
		}
		pixelsIdentical(t, orig, got)
	})

	t.Run("opaque_only", func(t *testing.T) {
		// Mix of fully-opaque colors — must be pixel-perfect.
		w, h := 32, 32
		orig := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				orig.SetNRGBA(x, y, color.NRGBA{
					R: uint8(x * 8),
					G: uint8(y * 8),
					B: 128,
					A: 255,
				})
			}
		}
		got, err := encodeDecodeRoundtrip(orig)
		if err != nil {
			t.Fatalf("encode/decode: %v", err)
		}
		pixelsIdentical(t, orig, got)
	})

	t.Run("checkerboard_opaque_transparent", func(t *testing.T) {
		// Checkerboard of fully-opaque and fully-transparent pixels.
		w, h := 32, 32
		orig := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if (x+y)%2 == 0 {
					orig.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
				} else {
					orig.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
				}
			}
		}
		got, err := encodeDecodeRoundtrip(orig)
		if err != nil {
			t.Fatalf("encode/decode: %v", err)
		}
		pixelsIdentical(t, orig, got)
	})

	t.Run("semi_transparent_idempotent", func(t *testing.T) {
		// Semi-transparent: verify encode→decode→encode produces identical bitstream.
		w, h := 16, 16
		orig := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				orig.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
			}
		}
		data1, err := EncodeVP8L(orig)
		if err != nil {
			t.Fatalf("first encode: %v", err)
		}
		decoded, err := DecodeVP8L(data1)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		data2, err := EncodeVP8L(decoded)
		if err != nil {
			t.Fatalf("second encode: %v", err)
		}
		if !bytes.Equal(data1, data2) {
			t.Errorf("encode not idempotent: len1=%d, len2=%d", len(data1), len(data2))
		}
	})
}

// TestVP8LHuffmanTableValidity encodes images and verifies ffmpeg can decode them.
func TestVP8LHuffmanTableValidity(t *testing.T) {
	ffmpeg := findFfmpeg()
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}

	tests := []struct {
		name string
		img  *image.NRGBA
	}{
		{"solid_red", makeSolidImage(32, 32, color.NRGBA{255, 0, 0, 255})},
		{"solid_white", makeSolidImage(32, 32, color.NRGBA{255, 255, 255, 255})},
		{"gradient", func() *image.NRGBA {
			img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					img.SetNRGBA(x, y, color.NRGBA{uint8(x * 8), uint8(y * 8), 128, 255})
				}
			}
			return img
		}()},
	}

	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := EncodeVP8L(tc.img)
			if err != nil {
				t.Fatalf("EncodeVP8L: %v", err)
			}

			// Wrap in RIFF/WebP container.
			webpData := wrapInRIFF(data)
			path := filepath.Join(dir, tc.name+".webp")
			if err := os.WriteFile(path, webpData, 0644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			// Verify ffmpeg can decode without error.
			cmd := exec.Command(ffmpeg, "-i", path, "-f", "null", "-")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("ffmpeg failed to decode %s:\n%s", tc.name, out)
			}
		})
	}
}

// TestVP8LKraftInequality verifies that Huffman codes satisfy sum(2^(-len_i)) == 1.0.
func TestVP8LKraftInequality(t *testing.T) {
	tests := []struct {
		name  string
		freqs []int
	}{
		{"uniform_256", func() []int {
			f := make([]int, 256)
			for i := range f {
				f[i] = 1
			}
			return f
		}()},
		{"skewed", []int{100, 50, 25, 10, 5, 1}},
		{"two_symbols", []int{10, 20}},
		{"power_of_two", []int{8, 4, 2, 1, 1}},
		{"random", func() []int {
			rng := rand.New(rand.NewSource(123))
			f := make([]int, 64)
			for i := range f {
				f[i] = rng.Intn(100) + 1
			}
			return f
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, lengths := buildHuffCodes(tc.freqs, 15)

			// Compute Kraft sum = sum(2^(-len_i)) for active symbols.
			kraft := 0.0
			for i, freq := range tc.freqs {
				if freq > 0 && lengths[i] > 0 {
					kraft += math.Pow(2, -float64(lengths[i]))
				}
			}

			// For a valid complete prefix code, Kraft sum must equal 1.0.
			// Allow tiny floating-point tolerance.
			if math.Abs(kraft-1.0) > 1e-9 {
				t.Errorf("Kraft inequality not satisfied: sum=%.10f (want 1.0)", kraft)
			}
		})
	}
}

// TestVP8LFfmpegDecode generates lossless WebP with ffmpeg and decodes with go-webp.
func TestVP8LFfmpegDecode(t *testing.T) {
	ffmpeg := findFfmpeg()
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}

	// Use existing reference file if available.
	refFile := "/tmp/webp-test/ffmpeg/ref_lossless.webp"
	data, err := os.ReadFile(refFile)
	if err != nil {
		t.Skipf("reference file not found: %v", err)
	}

	if len(data) < 20 {
		t.Fatalf("file too short: %d bytes", len(data))
	}

	// Extract VP8L data: skip RIFF header (12) + chunk tag+size (8).
	vp8lData := data[20:]
	img, err := DecodeVP8L(vp8lData)
	if err != nil {
		t.Fatalf("DecodeVP8L: %v", err)
	}

	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("decoded image has invalid bounds: %v", b)
	}
	t.Logf("Decoded ffmpeg lossless WebP: %dx%d", b.Dx(), b.Dy())
}

// TestVP8LBitstreamHeader verifies VP8L signature, width/height encoding, version.
func TestVP8LBitstreamHeader(t *testing.T) {
	sizes := []struct{ w, h int }{
		{1, 1},
		{100, 200},
		{16383, 1}, // max dimension (14 bits: 16383+1 = 16384)
	}

	for _, sz := range sizes {
		t.Run("", func(t *testing.T) {
			orig := makeSolidImage(sz.w, sz.h, color.NRGBA{128, 64, 32, 255})
			data, err := EncodeVP8L(orig)
			if err != nil {
				t.Fatalf("%dx%d EncodeVP8L: %v", sz.w, sz.h, err)
			}

			if len(data) < 5 {
				t.Fatalf("bitstream too short: %d bytes", len(data))
			}

			// Check VP8L signature byte.
			if data[0] != Signature {
				t.Errorf("signature: got 0x%02x, want 0x%02x", data[0], Signature)
			}

			// Decode header fields.
			dec := NewDecoder(data)
			cfg, err := dec.DecodeConfig()
			if err != nil {
				t.Fatalf("DecodeConfig: %v", err)
			}
			if cfg.Width != sz.w {
				t.Errorf("width: got %d, want %d", cfg.Width, sz.w)
			}
			if cfg.Height != sz.h {
				t.Errorf("height: got %d, want %d", cfg.Height, sz.h)
			}

			// Version bits must be 0 (verified by successful decode).
			_, err = DecodeVP8L(data)
			if err != nil {
				t.Errorf("version check failed: decode returned error: %v", err)
			}
		})
	}
}

// TestVP8LPaletteImages tests images with ≤256 unique colors (color indexing transform).
func TestVP8LPaletteImages(t *testing.T) {
	tests := []struct {
		name   string
		colors int
		fn     func(x, y, numColors int) color.NRGBA
	}{
		{
			name:   "2_colors",
			colors: 2,
			fn: func(x, y, n int) color.NRGBA {
				if (x+y)%2 == 0 {
					return color.NRGBA{255, 0, 0, 255}
				}
				return color.NRGBA{0, 0, 255, 255}
			},
		},
		{
			name:   "16_colors",
			colors: 16,
			fn: func(x, y, n int) color.NRGBA {
				idx := (x + y*4) % n
				return color.NRGBA{
					R: uint8(idx * 16),
					G: uint8((n - idx) * 16),
					B: uint8(128),
					A: 255,
				}
			},
		},
		{
			name:   "256_colors",
			colors: 256,
			fn: func(x, y, n int) color.NRGBA {
				idx := (x + y) % n
				return color.NRGBA{
					R: uint8(idx),
					G: uint8(255 - idx),
					B: uint8(idx / 2),
					A: 255,
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h := 64, 64
			orig := image.NewNRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					orig.SetNRGBA(x, y, tc.fn(x, y, tc.colors))
				}
			}
			got, err := encodeDecodeRoundtrip(orig)
			if err != nil {
				t.Fatalf("%s encode/decode: %v", tc.name, err)
			}
			pixelsIdentical(t, orig, got)
		})
	}
}

// wrapInRIFF wraps a VP8L bitstream in a RIFF/WebP container.
func wrapInRIFF(vp8lData []byte) []byte {
	// VP8L chunk: "VP8L" + 4-byte size + data
	chunkSize := uint32(len(vp8lData))
	totalSize := 4 + 4 + chunkSize // "WEBP" + chunk header + data
	if totalSize%2 != 0 {
		totalSize++ // RIFF chunks are padded to even size
	}

	var buf bytes.Buffer
	// RIFF header
	buf.WriteString("RIFF")
	writeUint32LE(&buf, 4+4+4+chunkSize) // "WEBP" + "VP8L" + size + data
	buf.WriteString("WEBP")
	// VP8L chunk
	buf.WriteString("VP8L")
	writeUint32LE(&buf, chunkSize)
	buf.Write(vp8lData)
	if chunkSize%2 != 0 {
		buf.WriteByte(0) // padding
	}
	_ = totalSize
	return buf.Bytes()
}

func writeUint32LE(buf *bytes.Buffer, v uint32) {
	buf.WriteByte(byte(v))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 24))
}
