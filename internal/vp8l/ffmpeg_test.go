package vp8l

import (
	"os"
	"testing"
)

func TestDecodeFFmpegSmall(t *testing.T) {
	data, err := os.ReadFile("/tmp/test_small_lossless.webp")
	if err != nil {
		t.Skipf("test file not found: %v", err)
	}
	// Skip RIFF+WEBP header (12) + VP8L chunk tag+size (8) = 20 bytes
	vp8lData := data[20:]
	img, err := DecodeVP8L(vp8lData)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	t.Logf("Decoded: %v", img.Bounds())
}

func TestDecodeFFmpegReal(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skipf("test file not found: %v", err)
	}
	vp8lData := data[20:]
	img, err := DecodeVP8L(vp8lData)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	t.Logf("Decoded: %v", img.Bounds())
}

func TestDecode64x64(t *testing.T) {
	data, err := os.ReadFile("/tmp/test_64x64.webp")
	if err != nil {
		t.Skipf("not found: %v", err)
	}
	img, err := DecodeVP8L(data[20:])
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	t.Logf("Decoded: %v", img.Bounds())
}
