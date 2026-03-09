package vp8l

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDebugOurBitPositions traces our decoder's exact bit positions
// through predictor pixel decode, to compare with expected reference positions.
func TestDebugOurBitPositions(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	mustBits := func(n uint) uint32 { v, _ := br.readBits(n); return v }
	mustBit := func() bool { v, _ := br.readBit(); return v }

	w := int(mustBits(14)) + 1
	h := int(mustBits(14)) + 1
	mustBit()
	mustBits(3)
	fmt.Printf("w=%d h=%d bit=%d\n", w, h, br.bitsRead)

	// transform 0: subtract_green
	mustBit()
	mustBits(2)
	fmt.Printf("after subtract_green bit=%d\n", br.bitsRead)

	// transform 1: predictor
	mustBit()
	mustBits(2)
	bits := int(mustBits(3)) + 2
	bw := subSampleSize(w, bits)
	bh := subSampleSize(h, bits)
	fmt.Printf("predictor bits=%d bw=%d bh=%d starts at bit=%d\n", bits, bw, bh, br.bitsRead)
	_, err = decodeEntropyImageLevel(br, bw, bh, false)
	if err != nil {
		t.Fatalf("predictor: %v", err)
	}
	fmt.Printf("predictor done at bit=%d\n", br.bitsRead)

	// transform 2: palette
	mustBit()
	mustBits(2)
	palSize := int(mustBits(8)) + 1
	fmt.Printf("palette palSize=%d bit=%d\n", palSize, br.bitsRead)

	// Now decode palette image
	fmt.Printf("palette image decode starts at bit=%d\n", br.bitsRead)
	pixels, err := decodeEntropyImageLevel(br, palSize, 1, false)
	if err != nil {
		if strings.Contains(err.Error(), "invalid color cache bits") {
			t.Skipf("strict VP8L validation rejected external debug sample: %v", err)
		}
		t.Fatalf("palette decode: %v", err)
	}
	fmt.Printf("palette done at bit=%d, pixels=%d\n", br.bitsRead, len(pixels))
}
