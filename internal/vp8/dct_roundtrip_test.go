package vp8

import (
	"fmt"
	"testing"
)

func TestDCTRoundTrip(t *testing.T) {
	// Test DCT round-trip with a gradient-like block (no quantization)
	src := [16]int16{
		-30, -20, -10, 0,
		-30, -20, -10, 0,
		-30, -20, -10, 0,
		-30, -20, -10, 0,
	}

	var dct [16]int16
	fdct4x4(&src, &dct)
	t.Logf("Source: %v", src)
	t.Logf("DCT:   %v", dct)

	// Inverse DCT with prediction=128
	pred := make([]byte, 16)
	for i := range pred {
		pred[i] = 128
	}
	dst := make([]byte, 16)
	idct4x4(&dct, pred, 4, dst, 4)

	t.Logf("Recon:    %v", dst)
	expected := make([]byte, 16)
	for i := 0; i < 16; i++ {
		v := int(pred[i]) + int(src[i])
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		expected[i] = byte(v)
	}
	t.Logf("Expected: %v", expected)

	maxErr := 0
	for i := 0; i < 16; i++ {
		d := int(dst[i]) - int(expected[i])
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
	}
	t.Logf("Max error (no quant): %d", maxErr)
	if maxErr > 1 {
		t.Errorf("DCT round-trip error too large: %d", maxErr)
	}

	// Test with quantization (Q75)
	var dctQ [16]int16
	fdct4x4(&src, &dctQ)
	dcQ := int16(26)
	acQ := int16(27)
	t.Logf("\nDCT before quant: %v", dctQ)
	// Quantize
	dctQ[0] /= dcQ
	for i := 1; i < 16; i++ {
		dctQ[i] /= acQ
	}
	t.Logf("DCT quantized:    %v", dctQ)
	// Dequantize
	dctQ[0] *= dcQ
	for i := 1; i < 16; i++ {
		dctQ[i] *= acQ
	}
	t.Logf("DCT dequantized:  %v", dctQ)

	dst2 := make([]byte, 16)
	idct4x4(&dctQ, pred, 4, dst2, 4)
	fmt.Printf("Recon (Q75): %v\n", dst2)
	fmt.Printf("Expected:    %v\n", expected)
}

func TestEncodeDecodeGradientBlock(t *testing.T) {
	// Simulate what happens for a gradient block through the full pipeline
	// Source pixels: horizontal gradient 98..128 in the first 4x4 block
	var src [16]int16
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src[y*4+x] = int16(98 + x*10) // 98, 108, 118, 128
		}
	}
	t.Logf("Source residuals: %v", src)

	// With DC prediction = 128, residuals are:
	pred := byte(128)
	var residual [16]int16
	for i := 0; i < 16; i++ {
		residual[i] = src[i] - int16(pred)
	}
	t.Logf("Residual:         %v", residual)

	// Forward DCT
	var dct [16]int16
	fdct4x4(&residual, &dct)
	t.Logf("DCT coeffs:       %v", dct)

	// Without quantization - direct IDCT
	predBuf := make([]byte, 16)
	for i := range predBuf {
		predBuf[i] = pred
	}
	dst := make([]byte, 16)
	idct4x4(&dct, predBuf, 4, dst, 4)
	t.Logf("Recon (no quant):  %v", dst)

	// With quantization
	var dctQ [16]int16
	copy(dctQ[:], dct[:])
	quantizeBlock(&dctQ, 26, 27)
	t.Logf("Quantized DCT:     %v", dctQ)

	// Check which coefficients survive quantization
	nonZero := 0
	for i, c := range dctQ {
		if c != 0 {
			nonZero++
			t.Logf("  dctQ[%d] = %d (spatial idx via zigzag)", i, c)
		}
	}
	t.Logf("Non-zero after quant: %d", nonZero)
}
