package vp8l

import (
	"fmt"
	"os"
	"testing"
)

// TestDebugBitCompare reads the same bit positions that our decoder reads
// and compares what the reference decoder would read, to find where they diverge.
func TestDebugBitCompare(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	mustBits := func(n uint) uint32 {
		v, _ := br.readBits(n)
		return v
	}
	mustBit := func() bool {
		v, _ := br.readBit()
		return v
	}

	// Header
	w := int(mustBits(14)) + 1
	h := int(mustBits(14)) + 1
	mustBit()   // alpha
	mustBits(3) // version
	fmt.Printf("w=%d h=%d bit=%d\n", w, h, br.bitsRead)

	// transform 0: subtract_green
	mustBit()   // has_transform
	mustBits(2) // type=2
	fmt.Printf("after tr0 bit=%d\n", br.bitsRead)

	// transform 1: predictor
	mustBit()   // has_transform
	mustBits(2) // type=0
	bits := int(mustBits(3)) + 2
	bw := subSampleSize(w, bits)
	bh := subSampleSize(h, bits)
	decodeEntropyImageLevel(br, bw, bh, false)
	fmt.Printf("after predictor bit=%d\n", br.bitsRead)

	// transform 2: palette
	mustBit()   // has_transform
	mustBits(2) // type=3
	palSize := int(mustBits(8)) + 1
	fmt.Printf("palSize=%d bit=%d\n", palSize, br.bitsRead)

	// Now decode palette image step by step
	// use_cc
	useCC := mustBit()
	fmt.Printf("use_cc=%v bit=%d\n", useCC, br.bitsRead)

	// useMeta (readHuffGroup reads useMeta=0 for topLevel=false, so NO useMeta bit here)
	// Actually decodeEntropyImageLevel(topLevel=false) → readHuffGroup (no useMeta)
	// readHuffGroup reads 5 trees via readCodeLengths
	// readCodeLengths reads useMeta(1 bit) FIRST

	// Tree[0] green, alphabetSize=280
	fmt.Printf("\n=== Tree[0] green alphabet=280 starts at bit=%d ===\n", br.bitsRead)

	// readCodeLengths: useMeta
	useMeta := mustBit()
	fmt.Printf("useMeta=%v bit=%d\n", useMeta, br.bitsRead)

	if useMeta {
		fmt.Println("SIMPLE tree")
		return
	}

	// Complex: numCLC (4 bits)
	numCLC := mustBits(4) + 4
	fmt.Printf("numCLC=%d bit=%d\n", numCLC, br.bitsRead)

	// CLC lengths (3 bits each, numCLC of them)
	clcOrder := []int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	clcLens := make([]int, 19)
	for i := 0; i < int(numCLC); i++ {
		v := mustBits(3)
		clcLens[clcOrder[i]] = int(v)
	}
	fmt.Printf("clcLens=%v bit=%d\n", clcLens, br.bitsRead)

	// Print raw bits BEFORE CLC tree read to see what we have
	fmt.Printf("\nRaw bits from position %d:\n", br.bitsRead)
	// Save position and peek at next 64 bits
	savedPos := br.bitsRead
	var rawBits [64]bool
	for i := 0; i < 64; i++ {
		b, err2 := br.readBit()
		if err2 != nil { break }
		rawBits[i] = b
	}
	fmt.Printf("bits %d..%d: ", savedPos, savedPos+64)
	for i := 0; i < 64; i++ {
		if rawBits[i] { fmt.Print("1") } else { fmt.Print("0") }
		if (i+1)%8 == 0 { fmt.Print(" ") }
	}
	fmt.Println()
}

// TestDebugRawBitsAtCLC prints raw bits at the CLC start position to
// understand what symbol the CLC tree actually decodes.
func TestDebugRawBitsAtCLC(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]

	// The CLC tree decoding starts at bit 2536 (as confirmed by TestDebugCLCDecode).
	// Let's print the raw bytes around byte 316 (=2536/8=317th byte = index 316)
	byteIdx := 2536 / 8
	bitOff := 2536 % 8
	fmt.Printf("CLC starts at bit=2536, byte=%d, bitOffset=%d\n", byteIdx, bitOff)
	fmt.Printf("Bytes around position: ")
	for i := byteIdx; i < byteIdx+10 && i+1 < len(vp8lData); i++ {
		fmt.Printf("%08b ", vp8lData[i+1]) // +1 for the signature byte we skip
	}
	fmt.Println()

	// What does the reference decoder see as first CLC symbol?
	// CLC tree: sym=5 has l=1. In our new code (hist[0] included), nextCode[1]=14=1110b.
	// The LSB of 14 is 0. So sym=5 code reads bit=0 from stream.
	//
	// What bit is at position 2536?
	br := newBitReader(vp8lData[1:])
	for i := 0; i < 2536; i++ {
		br.readBit()
	}
	b0, _ := br.readBit()
	b1, _ := br.readBit()
	b2, _ := br.readBit()
	b3, _ := br.readBit()
	fmt.Printf("Bits 2536..2539: %v %v %v %v\n", b0, b1, b2, b3)

	// OLD algorithm: nextCode[1]=0, so sym=5 code is 0, reads bit=0.
	// NEW algorithm: nextCode[1]=14, so sym=5 code is 14 (LSB=0), reads bit=0.
	// Both read the same first bit! So the CLC traversal bit pattern is the same
	// for sym=5 (1-bit code, both directions read same LSB).
	//
	// The DIFFERENCE comes when multiple symbols compete for same path.
}
