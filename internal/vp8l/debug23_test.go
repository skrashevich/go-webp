package vp8l

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	refvp8l "golang.org/x/image/vp8l"
)

// TestDebugRefVsOursDecode runs both reference and our decoder on the same data
// to compare their behavior step by step.
func TestDebugRefVsOursDecode(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]

	// Reference decode.
	refImg, err := refvp8l.Decode(bytes.NewReader(vp8lData))
	if err != nil {
		t.Fatalf("reference decode failed: %v", err)
	}
	fmt.Printf("Reference OK: %v\n", refImg.Bounds())

	// Our decode.
	ourImg, err := DecodeVP8L(vp8lData)
	if err != nil {
		fmt.Printf("Our decode FAILED: %v\n", err)

		// Try to decode just the palette sub-image to understand what's wrong.
		t.Logf("Attempting palette-only decode for diagnostics...")
		debugPaletteDecode(t, vp8lData)
		t.Fatalf("decode failed: %v", err)
	} else {
		fmt.Printf("Our decode OK: %v\n", ourImg.Bounds())
	}
}

// debugPaletteDecode manually decodes up to the palette and checks it.
func debugPaletteDecode(t *testing.T, vp8lData []byte) {
	t.Helper()
	br := newBitReader(vp8lData[1:]) // skip 0x2f

	mustBits := func(n uint) uint32 {
		v, e := br.readBits(n)
		if e != nil { t.Fatalf("readBits(%d) at %d: %v", n, br.bitsRead, e) }
		return v
	}
	mustBit := func() bool {
		v, e := br.readBit()
		if e != nil { t.Fatalf("readBit at %d: %v", br.bitsRead, e) }
		return v
	}

	w := int(mustBits(14)) + 1
	h := int(mustBits(14)) + 1
	mustBit()     // alpha
	mustBits(3)   // version
	t.Logf("image: %dx%d bit=%d", w, h, br.bitsRead)

	// Skip subtract_green transform (type=2, no data).
	if !mustBit() { t.Fatal("expected has_transform=1") }
	trType := int(mustBits(2))
	t.Logf("transform[0] type=%d bit=%d", trType, br.bitsRead)
	if trType != 2 { t.Fatalf("expected subtract_green") }

	// Decode predictor transform.
	if !mustBit() { t.Fatal("expected has_transform=1") }
	trType = int(mustBits(2))
	t.Logf("transform[1] type=%d bit=%d", trType, br.bitsRead)
	if trType != 0 { t.Fatalf("expected predictor") }
	bits := int(mustBits(3)) + 2
	bw := subSampleSize(w, bits)
	bh := subSampleSize(h, bits)
	t.Logf("predictor bits=%d bw=%d bh=%d start=%d", bits, bw, bh, br.bitsRead)
	_, err := decodeEntropyImageLevel(br, bw, bh, false)
	if err != nil { t.Fatalf("predictor decode: %v at %d", err, br.bitsRead) }
	t.Logf("predictor done at bit=%d", br.bitsRead)

	// Read palette transform header.
	if !mustBit() { t.Fatal("expected has_transform=1 for palette") }
	trType = int(mustBits(2))
	t.Logf("transform[2] type=%d bit=%d", trType, br.bitsRead)
	if trType != 3 { t.Fatalf("expected color indexing (palette)") }
	palSize := int(mustBits(8)) + 1
	t.Logf("palette palSize=%d bit=%d", palSize, br.bitsRead)

	// Now decode palette entropy image step by step.
	t.Logf("=== Palette entropy image starts at bit=%d ===", br.bitsRead)

	// use_color_cache
	useCC, _ := br.readBit()
	t.Logf("use_cc=%v bit=%d", useCC, br.bitsRead)
	if useCC {
		ccBits, _ := br.readBits(4)
		t.Logf("cc_bits=%d bit=%d", ccBits, br.bitsRead)
	}

	// topLevel=false → no use_meta. Read single group = 5 trees.
	// Tree[0]: green alphabet = 280
	alphaSizes := [5]int{280, 256, 256, 256, 40}

	for tIdx := 0; tIdx < 5; tIdx++ {
		start := br.bitsRead
		clens, err2 := readCodeLengths(br, alphaSizes[tIdx])
		if err2 != nil {
			t.Logf("tree[%d] readCodeLengths FAILED at %d: %v", tIdx, br.bitsRead, err2)
			return
		}
		nonzero := 0
		for _, l := range clens { if l > 0 { nonzero++ } }
		t.Logf("tree[%d] alphaSz=%d nonzero=%d bits %d..%d", tIdx, alphaSizes[tIdx], nonzero, start, br.bitsRead)

		tree, err2 := buildHuffTree(clens)
		if err2 != nil {
			t.Logf("tree[%d] buildHuffTree FAILED: %v", tIdx, err2)
			return
		}
		t.Logf("tree[%d] built: single=%v nodes=%d", tIdx, tree.single, len(tree.nodes))
	}

	t.Logf("All 5 trees read, bit=%d. Now decoding %d palette pixels...", br.bitsRead, palSize)
}

// TestDebugCLCOverfull tests what happens when our buildHuffTree handles the
// actual CLC from the palette: [4,4,4,3,4,1,0,2,4,1,0,0,0,0,0,0,2,5,7]
func TestDebugCLCOverfull(t *testing.T) {
	// CLC from bit 2491 for palette green tree
	clcLens := []int{4, 4, 4, 3, 4, 1, 0, 2, 4, 1, 0, 0, 0, 0, 0, 0, 2, 5, 7}

	nSym := 0
	for _, l := range clcLens { if l > 0 { nSym++ } }
	fmt.Printf("nSym=%d Kraft=", nSym)
	kraft := 0.0
	for _, l := range clcLens {
		if l > 0 {
			k := 1.0
			for i := 0; i < l; i++ { k /= 2 }
			kraft += k
		}
	}
	fmt.Printf("%.4f\n", kraft)

	// Our algorithm: blCount WITHOUT hist[0]
	maxBits := 0
	for _, l := range clcLens { if l > maxBits { maxBits = l } }
	blCount := make([]int, maxBits+1)
	for _, l := range clcLens { if l > 0 { blCount[l]++ } }
	nextCode := make([]int, maxBits+1)
	code := 0
	for bits := 1; bits <= maxBits; bits++ {
		code = (code + blCount[bits-1]) << 1
		nextCode[bits] = code
	}
	fmt.Printf("Our blCount=%v nextCode=%v\n", blCount, nextCode)

	// Reference algorithm: includes hist[0]
	hist := make([]int, maxBits+1)
	for _, l := range clcLens { hist[l]++ }
	nextCodeRef := make([]int, maxBits+1)
	codeRef := 0
	for bits := 1; bits <= maxBits; bits++ {
		codeRef = (codeRef + hist[bits-1]) << 1
		nextCodeRef[bits] = codeRef
	}
	fmt.Printf("Ref hist=%v nextCode=%v\n", hist, nextCodeRef)

	// Build with our algorithm and show result
	tree, err := buildHuffTree(clcLens)
	if err != nil {
		t.Logf("buildHuffTree error: %v", err)
	} else {
		fmt.Printf("Our tree: single=%v nodes=%d\n", tree.single, len(tree.nodes))
		for i, nd := range tree.nodes {
			fmt.Printf("  node[%d]: sym=%d children=%d\n", i, nd.symbol, nd.children)
		}
	}

	// Show what symbols our tree would decode for various bit patterns
	fmt.Println("\nOur tree symbol lookup (1-bit patterns):")
	for bits := uint64(0); bits < 256; bits++ {
		// Feed this as bitstream
		if tree.single {
			fmt.Printf("  single sym=%d\n", tree.singleSymbol)
			break
		}
		n := 0
		for tree.nodes[n].children != -1 {
			b := int(bits & 1)
			bits >>= 1
			n = tree.nodes[n].children + b
			if n >= len(tree.nodes) || n < 0 {
				fmt.Printf("  INVALID node %d\n", n)
				break
			}
		}
		fmt.Printf("  bits→sym=%d\n", tree.nodes[n].symbol)
		break // just one example
	}
}
