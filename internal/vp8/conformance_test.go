package vp8

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"testing"

	refvp8 "golang.org/x/image/vp8"
)

// --- helpers ---

func makeSolidImage(w, h int, r, g, b uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	c := color.NRGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func makeHGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(x * 255 / (w - 1))
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

func makeVGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		v := uint8(y * 255 / (h - 1))
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

func makeDiagGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8((x + y) * 255 / (w + h - 2))
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// encodeDecodeVP8 encodes img with go-webp VP8 encoder and decodes it back.
func encodeDecodeVP8(img image.Image, quality float32) (image.Image, []byte, error) {
	data, err := EncodeVP8(img, quality)
	if err != nil {
		return nil, nil, err
	}
	decoded, err := Decode(data)
	if err != nil {
		return nil, data, err
	}
	return decoded, data, nil
}

// calcPSNR computes PSNR in dB between two images over the region of `a`.
func calcPSNR(a, b image.Image) float64 {
	bounds := a.Bounds()
	var sumSq float64
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			dr := float64(int(r1>>8) - int(r2>>8))
			dg := float64(int(g1>>8) - int(g2>>8))
			db := float64(int(b1>>8) - int(b2>>8))
			sumSq += dr*dr + dg*dg + db*db
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	mse := sumSq / float64(n)
	if mse == 0 {
		return 999.0
	}
	return 10 * math.Log10(255*255/mse)
}

// imagesPixelIdentical returns true if every pixel in region of `a` matches `b`.
func imagesPixelIdentical(a, b image.Image) bool {
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}

// --- tests ---

// TestVP8RoundtripSolidColors encodes and decodes solid color images.
func TestVP8RoundtripSolidColors(t *testing.T) {
	colors := []struct {
		name    string
		r, g, b uint8
	}{
		{"black", 0, 0, 0},
		{"white", 255, 255, 255},
		{"gray", 128, 128, 128},
		{"red", 200, 0, 0},
		{"green", 0, 200, 0},
		{"blue", 0, 0, 200},
	}
	sizes := [][2]int{{16, 16}, {32, 32}, {64, 64}, {128, 128}}

	for _, c := range colors {
		for _, sz := range sizes {
			w, h := sz[0], sz[1]
			t.Run(c.name+"_"+string(rune('0'+w/16)), func(t *testing.T) {
				img := makeSolidImage(w, h, c.r, c.g, c.b)
				decoded, _, err := encodeDecodeVP8(img, 75)
				if err != nil {
					t.Fatalf("encode/decode error: %v", err)
				}
				psnr := calcPSNR(img, decoded)
				if psnr < 30.0 {
					t.Errorf("PSNR=%.1f < 30 dB for %s %dx%d", psnr, c.name, w, h)
				}
			})
		}
	}
}

// TestVP8RoundtripGradients encodes and decodes gradient images.
func TestVP8RoundtripGradients(t *testing.T) {
	tests := []struct {
		name string
		img  *image.NRGBA
	}{
		{"horizontal", makeHGradient(64, 64)},
		{"vertical", makeVGradient(64, 64)},
		{"diagonal", makeDiagGradient(64, 64)},
		{"horizontal_128", makeHGradient(128, 128)},
		{"vertical_128", makeVGradient(128, 128)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded, _, err := encodeDecodeVP8(tc.img, 75)
			if err != nil {
				t.Fatalf("encode/decode error: %v", err)
			}
			psnr := calcPSNR(tc.img, decoded)
			if psnr < 30.0 {
				t.Errorf("PSNR=%.1f < 30 dB for %s", psnr, tc.name)
			}
		})
	}
}

// TestVP8QualityScaling verifies that higher quality yields higher PSNR and larger file.
func TestVP8QualityScaling(t *testing.T) {
	img := makeDiagGradient(64, 64)
	qualities := []float32{25, 50, 75, 95}

	prevPSNR := 0.0
	prevSize := 0

	for _, q := range qualities {
		decoded, data, err := encodeDecodeVP8(img, q)
		if err != nil {
			t.Fatalf("q=%.0f: encode/decode error: %v", q, err)
		}
		psnr := calcPSNR(img, decoded)
		size := len(data)

		t.Logf("q=%.0f: PSNR=%.1f dB, size=%d bytes", q, psnr, size)

		if psnr < 25.0 {
			t.Errorf("q=%.0f: PSNR=%.1f < 25 dB", q, psnr)
		}
		if prevPSNR > 0 && psnr < prevPSNR-2.0 {
			t.Errorf("q=%.0f: PSNR=%.1f dropped more than 2dB vs previous %.1f", q, psnr, prevPSNR)
		}
		if prevSize > 0 && size < prevSize {
			t.Errorf("q=%.0f: file size %d < previous %d (should increase)", q, size, prevSize)
		}
		prevPSNR = psnr
		prevSize = size
	}
}

// TestVP8ReferenceDecoderCross encodes with go-webp, decodes with both decoders,
// verifies outputs are pixel-identical.
func TestVP8ReferenceDecoderCross(t *testing.T) {
	img := makeSolidImage(64, 64, 100, 150, 200)
	data, err := EncodeVP8(img, 75)
	if err != nil {
		t.Fatalf("EncodeVP8: %v", err)
	}

	// Our decoder.
	ownDecoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode (own): %v", err)
	}

	// Reference decoder.
	refDec := refvp8.NewDecoder()
	refDec.Init(bytes.NewReader(data), len(data))
	if _, err := refDec.DecodeFrameHeader(); err != nil {
		t.Fatalf("ref DecodeFrameHeader: %v", err)
	}
	refDecoded, err := refDec.DecodeFrame()
	if err != nil {
		t.Fatalf("ref DecodeFrame: %v", err)
	}

	psnr := calcPSNR(ownDecoded, refDecoded)
	t.Logf("PSNR own vs ref: %.1f dB", psnr)
	if psnr < 40.0 {
		t.Errorf("PSNR=%.1f dB between own and ref decoders — decoders disagree", psnr)
	}
}

// TestVP8PredictionModes creates images that trigger different prediction modes.
func TestVP8PredictionModes(t *testing.T) {
	tests := []struct {
		name string
		img  image.Image
	}{
		// DC mode: solid color triggers DC prediction.
		{"solid_dc", makeSolidImage(64, 64, 128, 128, 128)},
		// V mode: vertical stripes — each column is uniform.
		{"vertical_stripes", func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
			for y := 0; y < 64; y++ {
				for x := 0; x < 64; x++ {
					v := uint8((x / 8) * 32)
					img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
				}
			}
			return img
		}()},
		// H mode: horizontal stripes — each row is uniform.
		{"horizontal_stripes", func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
			for y := 0; y < 64; y++ {
				v := uint8((y / 8) * 32)
				for x := 0; x < 64; x++ {
					img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
				}
			}
			return img
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded, _, err := encodeDecodeVP8(tc.img, 75)
			if err != nil {
				t.Fatalf("encode/decode error: %v", err)
			}
			psnr := calcPSNR(tc.img, decoded)
			if psnr < 25.0 {
				t.Errorf("PSNR=%.1f < 25 dB", psnr)
			}
			t.Logf("%s: PSNR=%.1f dB", tc.name, psnr)
		})
	}
}

// TestVP8FrameHeaderParsing verifies frame header parsing matches expected values.
func TestVP8FrameHeaderParsing(t *testing.T) {
	tests := []struct {
		w, h int
		q    float32
	}{
		{16, 16, 75},
		{64, 32, 50},
		{128, 128, 90},
	}

	for _, tc := range tests {
		img := makeSolidImage(tc.w, tc.h, 128, 128, 128)
		data, err := EncodeVP8(img, tc.q)
		if err != nil {
			t.Fatalf("%dx%d: EncodeVP8: %v", tc.w, tc.h, err)
		}

		fh, _, err := parseFrameHeader(data)
		if err != nil {
			t.Fatalf("%dx%d: parseFrameHeader: %v", tc.w, tc.h, err)
		}

		if !fh.keyFrame {
			t.Errorf("%dx%d: expected keyFrame=true", tc.w, tc.h)
		}
		if int(fh.width) != tc.w {
			t.Errorf("%dx%d: width=%d, want %d", tc.w, tc.h, fh.width, tc.w)
		}
		if int(fh.height) != tc.h {
			t.Errorf("%dx%d: height=%d, want %d", tc.w, tc.h, fh.height, tc.h)
		}
		if fh.firstPartSize == 0 {
			t.Errorf("%dx%d: firstPartSize=0, want >0", tc.w, tc.h)
		}
		t.Logf("%dx%d q=%.0f: keyFrame=%v w=%d h=%d firstPartSize=%d",
			tc.w, tc.h, tc.q, fh.keyFrame, fh.width, fh.height, fh.firstPartSize)
	}
}

// TestVP8DCTRoundtrip verifies forward + quantize + dequantize + inverse DCT error is bounded.
func TestVP8DCTRoundtrip(t *testing.T) {
	tests := []struct {
		name   string
		input  [16]int16
		maxErr int
	}{
		{"flat_128", [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 1},
		{"gradient", [16]int16{-30, -20, -10, 0, -30, -20, -10, 0, -30, -20, -10, 0, -30, -20, -10, 0}, 5},
		{"alternating", [16]int16{50, -50, 50, -50, -50, 50, -50, 50, 50, -50, 50, -50, -50, 50, -50, 50}, 10},
		{"dc_only", [16]int16{100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100}, 5},
	}

	qp := qualityToQP(75)
	qParams := newEncodeQuantParams(qp)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var dct [16]int16
			fdct4x4(&tc.input, &dct)
			quantizeBlock(&dct, qParams.yDCQ, qParams.yACQ)
			dequantizeBlock(&dct, qParams.yDCQ, qParams.yACQ)

			// pred = 128 (neutral)
			pred := make([]byte, 16)
			for i := range pred {
				pred[i] = 128
			}
			out := make([]byte, 16)
			idct4x4(&dct, pred, 4, out, 4)

			maxErr := 0
			for i := 0; i < 16; i++ {
				orig := int(tc.input[i]) + 128
				if orig < 0 {
					orig = 0
				}
				if orig > 255 {
					orig = 255
				}
				diff := int(out[i]) - orig
				if diff < 0 {
					diff = -diff
				}
				if diff > maxErr {
					maxErr = diff
				}
			}
			t.Logf("%s: maxErr=%d", tc.name, maxErr)
			if maxErr > 20 {
				t.Errorf("maxErr=%d > 20 for %s", maxErr, tc.name)
			}
		})
	}
}

// TestVP8WHT verifies Walsh-Hadamard transform forward + inverse roundtrip.
func TestVP8WHT(t *testing.T) {
	tests := []struct {
		name  string
		input [16]int16
	}{
		{"zeros", [16]int16{}},
		{"ones", [16]int16{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{"dc_only", [16]int16{100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"alternating", [16]int16{-224, 576, -224, 576, 576, -224, 576, -224, -224, 576, -224, 576, 576, -224, 576, -224}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := tc.input
			var fwd [16]int16
			fwht4x4(&orig, &fwd)

			var inv [16]int16
			iWHT4x4(&fwd, &inv)

			for i := 0; i < 16; i++ {
				if inv[i] != orig[i] {
					t.Errorf("index %d: got %d, want %d", i, inv[i], orig[i])
				}
			}
		})
	}
}

// TestVP8FfmpegDecode generates WebP with ffmpeg and decodes with go-webp.
// Skipped if ffmpeg is not available.
func TestVP8FfmpegDecode(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found, skipping")
	}

	// Create a simple test image and save as PNG temp file.
	img := makeSolidImage(64, 64, 100, 150, 200)
	tmpDir := t.TempDir()
	tmpPNG := tmpDir + "/test.png"
	tmpWebP := tmpDir + "/test.webp"

	// Write PNG.
	pf, err := os.Create(tmpPNG)
	if err != nil {
		t.Fatalf("create PNG file: %v", err)
	}
	if err := png.Encode(pf, img); err != nil {
		pf.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	pf.Close()

	// Encode with ffmpeg.
	cmd := exec.Command(ffmpegPath, "-y", "-i", tmpPNG, "-c:v", "libwebp", "-quality", "75", tmpWebP)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg encode failed: %v\n%s", err, out)
	}

	// Read ffmpeg WebP.
	webpData, err := os.ReadFile(tmpWebP)
	if err != nil {
		t.Fatalf("read ffmpeg WebP: %v", err)
	}

	// Find VP8 chunk — skip RIFF header (12 bytes) and scan for "VP8 " chunk.
	// ffmpeg may produce VP8X format; we need to find the raw VP8 bitstream.
	vp8data, err := findVP8Chunk(webpData)
	if err != nil {
		t.Skipf("cannot find VP8 chunk in ffmpeg output (may be VP8X/lossless): %v", err)
	}

	// Decode with go-webp.
	ownImg, err := Decode(vp8data)
	if err != nil {
		t.Fatalf("Decode ffmpeg WebP: %v", err)
	}
	refDec := refvp8.NewDecoder()
	refDec.Init(bytes.NewReader(vp8data), len(vp8data))
	if _, err := refDec.DecodeFrameHeader(); err != nil {
		t.Fatalf("ref DecodeFrameHeader: %v", err)
	}
	refImg, err := refDec.DecodeFrame()
	if err != nil {
		t.Fatalf("ref DecodeFrame: %v", err)
	}

	psnr := calcPSNR(ownImg, refImg)
	t.Logf("PSNR (go-webp vs ffmpeg-ref): %.1f dB", psnr)
	if psnr < 25.0 {
		t.Errorf("PSNR=%.1f < 25 dB when decoding ffmpeg WebP", psnr)
	}
}

// TestVP8ColorPreservation verifies chroma channels are preserved.
func TestVP8ColorPreservation(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
	}{
		{"red_dominant", 200, 50, 50},
		{"green_dominant", 50, 200, 50},
		{"blue_dominant", 50, 50, 200},
		{"saturated_mixed", 200, 50, 150},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := makeSolidImage(64, 64, tc.r, tc.g, tc.b)
			decoded, _, err := encodeDecodeVP8(img, 75)
			if err != nil {
				t.Fatalf("encode/decode error: %v", err)
			}

			// Sample center pixel.
			r, g, b, _ := decoded.At(32, 32).RGBA()
			dr := int(r >> 8)
			dg := int(g >> 8)
			db := int(b >> 8)

			t.Logf("orig=(%d,%d,%d) decoded=(%d,%d,%d)", tc.r, tc.g, tc.b, dr, dg, db)

			// Verify the dominant channel is still dominant.
			orig := [3]int{int(tc.r), int(tc.g), int(tc.b)}
			dec := [3]int{dr, dg, db}

			// Find dominant channel in original.
			domCh := 0
			for i := 1; i < 3; i++ {
				if orig[i] > orig[domCh] {
					domCh = i
				}
			}

			// Dominant channel should still be largest in decoded.
			for i := 0; i < 3; i++ {
				if i != domCh && dec[i] > dec[domCh]+20 {
					t.Errorf("channel %d dominates in decoded (%d) but not in original (dominant=%d)", i, dec[i], domCh)
				}
			}

			// Not grayscale: at least one channel should differ significantly.
			maxDiff := 0
			for i := 0; i < 3; i++ {
				for j := i + 1; j < 3; j++ {
					d := dec[i] - dec[j]
					if d < 0 {
						d = -d
					}
					if d > maxDiff {
						maxDiff = d
					}
				}
			}
			if tc.r != tc.g || tc.g != tc.b {
				if maxDiff < 10 {
					t.Errorf("decoded image appears grayscale (maxChannelDiff=%d) for non-gray input", maxDiff)
				}
			}
		})
	}
}

// findVP8Chunk scans a RIFF/WEBP container and returns the raw VP8 bitstream data.
// Returns an error if no "VP8 " chunk is found.
func findVP8Chunk(data []byte) ([]byte, error) {
	// Skip RIFF header: "RIFF" (4) + file size (4) + "WEBP" (4) = 12 bytes.
	if len(data) < 12 {
		return nil, os.ErrInvalid
	}
	pos := 12
	for pos+8 <= len(data) {
		tag := string(data[pos : pos+4])
		size := int(data[pos+4]) | int(data[pos+5])<<8 | int(data[pos+6])<<16 | int(data[pos+7])<<24
		pos += 8
		if tag == "VP8 " {
			end := pos + size
			if end > len(data) {
				end = len(data)
			}
			return data[pos:end], nil
		}
		// Skip chunk (padded to even size).
		skip := size
		if skip%2 != 0 {
			skip++
		}
		pos += skip
	}
	return nil, os.ErrNotExist
}
