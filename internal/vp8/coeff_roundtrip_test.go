package vp8

import (
	"bytes"
	"testing"
)

func TestCoeffRoundTrip(t *testing.T) {
	// Test that encoding and decoding coefficients produces the same values.
	tests := []struct {
		name  string
		coeffs [16]int16
		plane  int
		first  int
	}{
		{
			"dc_only",
			[16]int16{-4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			1, 0,
		},
		{
			"gradient_ac",
			[16]int16{-4, -3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			0, 1, // Y with DC via WHT (skip first)
		},
		{
			"mixed_ac",
			[16]int16{10, -5, 3, -1, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			0, 1,
		},
		{
			"all_ones",
			[16]int16{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			0, 0,
		},
		{
			"large_values",
			[16]int16{100, -50, 30, -20, 10, -5, 3, -2, 1, 0, 0, 0, 0, 0, 0, 0},
			2, 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			var buf bytes.Buffer
			enc := newBoolEncoder(&buf)
			encodeCoefficientsFrom(enc, &tt.coeffs, tt.plane, 0, tt.first)
			enc.flush()

			encoded := buf.Bytes()
			t.Logf("Encoded %d bytes", len(encoded))

			// Decode
			bd, err := newBoolDecoder(encoded, 0)
			if err != nil {
				t.Fatalf("newBoolDecoder: %v", err)
			}

			var decoded [16]int16
			probs := &defaultCoeffProbs[tt.plane]
			skipFirst := tt.first > 0
			decodeResiduals4(bd, probs, 0, 1, 1, skipFirst, &decoded)

			// The encoder uses zigzag[i] to index coefficients.
			// The decoder uses zigzagDecode to place them.
			// We need to compare the decoded raster-order output with the input.
			//
			// The encoder reads coeffs[zigzag[i]] at scan position i.
			// The decoder writes coeffs[zigzagDecode[n-1]] at scan position n-1.
			// Since zigzag == zigzagDecode, this should be a round-trip.
			//
			// But wait - the decoder uses dequantization with dcQ=1, acQ=1,
			// so the values should pass through unchanged.

			t.Logf("Input:   %v", tt.coeffs)
			t.Logf("Decoded: %v", decoded)

			match := true
			for i := 0; i < 16; i++ {
				if i < tt.first {
					continue
				}
				if tt.coeffs[i] != decoded[i] {
					t.Errorf("coeffs[%d]: input=%d decoded=%d", i, tt.coeffs[i], decoded[i])
					match = false
				}
			}
			if match {
				t.Log("MATCH!")
			}
		})
	}
}
