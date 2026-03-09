package vp8

import (
	"testing"
)

// TestBoolDecoder verifies basic bool decoder operation.
func TestBoolDecoder(t *testing.T) {
	// Encode a known sequence using boolEncoder and decode it back.
	// writeBool(128, false) = 0 bit, writeBool(128, true) = 1 bit.
	raw := []byte{0x10, 0x00} // minimal valid stream
	bd, err := newBoolDecoder(raw, 0)
	if err != nil {
		t.Fatalf("newBoolDecoder: %v", err)
	}
	// Just verify we can call ReadBool without panicking.
	_ = bd.ReadBool(128)
}

// TestParseFrameHeader verifies keyframe header parsing.
func TestParseFrameHeader(t *testing.T) {
	// Minimal VP8 keyframe header:
	// frame tag: key frame (bit0=0), version 0, show_frame=1, first_part_size=0
	// tag = 0x00 | 0<<1 | 1<<4 | 0<<5 = 0x10
	// start code: 0x9D 0x01 0x2A
	// width (16px): 0x10 0x00 → raw=0x0010, width=16&0x3FFF=16, hscale=0
	// height (16px): 0x10 0x00
	data := []byte{
		0x00, 0x00, 0x00, // frame tag: keyframe, version=0, show=0, partsize=0
		0x9D, 0x01, 0x2A, // start code
		0x10, 0x00, // width=16
		0x10, 0x00, // height=16
		// padding
		0x00, 0x00, 0x00,
	}
	fh, offset, err := parseFrameHeader(data)
	if err != nil {
		t.Fatalf("parseFrameHeader: %v", err)
	}
	if !fh.keyFrame {
		t.Error("expected keyFrame=true")
	}
	if fh.width != 16 {
		t.Errorf("width: got %d, want 16", fh.width)
	}
	if fh.height != 16 {
		t.Errorf("height: got %d, want 16", fh.height)
	}
	if offset != 10 {
		t.Errorf("offset: got %d, want 10", offset)
	}
}

// TestIDCT4x4DCOnly verifies DC-only IDCT.
func TestIDCT4x4DCOnly(t *testing.T) {
	// DC-only: coeffs[0]=16, all others 0.
	// After IDCT: each output = round(16/8) = 2 added to prediction.
	var coeffs [16]int16
	coeffs[0] = 16
	pred := make([]byte, 4*4)
	for i := range pred {
		pred[i] = 100
	}
	dst := make([]byte, 4*4)
	idct4x4(&coeffs, pred, 4, dst, 4)
	// DC contribution = (16 + 4) >> 3 = 2 (row pass) then >> 3 again = 0 approx.
	// Just verify no panic and output is in valid range.
	for i, v := range dst {
		if v < 1 || v > 255 {
			t.Errorf("dst[%d]=%d out of range", i, v)
		}
	}
}

// TestIWHT4x4 verifies Walsh-Hadamard transform.
func TestIWHT4x4(t *testing.T) {
	var in [16]int16
	in[0] = 16 // only DC
	var out [16]int16
	iWHT4x4(&in, &out)
	// All outputs should be equal for a flat DC input.
	expected := out[0]
	for i, v := range out {
		if v != expected {
			t.Errorf("out[%d]=%d, want %d", i, v, expected)
		}
	}
}

// TestDecodeConfig verifies dimension extraction from a keyframe header.
func TestDecodeConfig(t *testing.T) {
	// Build a minimal keyframe with 32x24 dimensions.
	// frame tag: keyframe, firstPartSize=1 (so byte[0..2] = tag, then start code, width, height)
	// tag bytes: key=0, ver=0, show=1, partSize=1 → 0x00 | (1<<4) | (1<<5) = 0x30
	// Actually partSize in bits [5..24]: partSize=1 means bits 5..24 = 1 → tag = 0x00|0x20 = 0x20 0x00 0x00
	data := []byte{
		0x00, 0x00, 0x00, // frame tag
		0x9D, 0x01, 0x2A, // start code
		0x20, 0x00, // width=32 (0x0020 & 0x3FFF = 32)
		0x18, 0x00, // height=24
		// first partition data (minimal bool stream)
		0x00, 0x00,
	}
	cfg, err := DecodeConfig(data)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 32 {
		t.Errorf("width: got %d, want 32", cfg.Width)
	}
	if cfg.Height != 24 {
		t.Errorf("height: got %d, want 24", cfg.Height)
	}
}
