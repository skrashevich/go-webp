package vp8l

import (
	"fmt"
	"os"
	"testing"
)

// TestDebugCLCDecode traces the exact CLC tree and code lengths for palette green tree.
func TestDebugCLCDecode(t *testing.T) {
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

	// Skip to palette.
	mustBits(14); mustBits(14); mustBit(); mustBits(3) // header
	mustBit(); mustBits(2)                              // tr[0] subtract_green
	mustBit(); mustBits(2)                              // tr[1] type
	bits := int(mustBits(3)) + 2
	bw := subSampleSize(736, bits)
	bh := subSampleSize(1408, bits)
	decodeEntropyImageLevel(br, bw, bh, false)  // predictor
	mustBit(); mustBits(2)                              // tr[2] type
	mustBits(8)                                         // palSize-1

	// Now at palette entropy image.
	fmt.Printf("Palette entropy starts at bit=%d\n", br.bitsRead)

	// use_cc
	useCC, _ := br.readBit()
	fmt.Printf("use_cc=%v bit=%d\n", useCC, br.bitsRead)

	// Read green tree (tree[0], alphabetSize=280) step by step.
	// readCodeLengths reads: useMeta(1), then COMPLEX: numCLC(4), clcLens[19*3], useLen(1), then symbols.
	fmt.Printf("\n=== Reading green tree (alphabet=280) ===\n")

	useSimple, _ := br.readBit()
	fmt.Printf("useMeta/useSimple=%v bit=%d\n", useSimple, br.bitsRead)

	if useSimple {
		fmt.Println("SIMPLE code - not expected")
		return
	}

	numCLC, _ := br.readBits(4)
	numCLC += 4
	fmt.Printf("numCLC=%d bit=%d\n", numCLC, br.bitsRead)

	clcOrder := []int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	clcLens := make([]int, 19)
	for i := 0; i < int(numCLC); i++ {
		v, _ := br.readBits(3)
		clcLens[clcOrder[i]] = int(v)
	}
	fmt.Printf("clcLens=%v bit=%d\n", clcLens, br.bitsRead)

	// Compute Kraft for CLC.
	kraft := 0.0
	for _, l := range clcLens {
		if l > 0 {
			k := 1.0
			for i := 0; i < l; i++ { k /= 2 }
			kraft += k
		}
	}
	fmt.Printf("CLC Kraft=%.4f\n", kraft)

	// Build CLC tree with our algorithm.
	clcTree, _ := buildHuffTree(clcLens)
	fmt.Printf("CLC tree: single=%v nodes=%d\n", clcTree.single, len(clcTree.nodes))
	if !clcTree.single {
		for i, nd := range clcTree.nodes {
			fmt.Printf("  clc_node[%d]: sym=%d children=%d\n", i, nd.symbol, nd.children)
		}
	}

	// Read useLength and maxSymbol.
	useLen, _ := br.readBit()
	maxSym := 280
	fmt.Printf("useLen=%v bit=%d\n", useLen, br.bitsRead)
	if useLen {
		n, _ := br.readBits(3)
		msBits := 2 + 2*uint(n)
		ms, _ := br.readBits(msBits)
		maxSym = int(ms) + 2
		if maxSym > 280 { maxSym = 280 }
		fmt.Printf("maxSym=%d bit=%d\n", maxSym, br.bitsRead)
	}

	// Decode code lengths using CLC tree, showing each symbol.
	fmt.Printf("\n=== Decoding 280 code lengths (maxSym=%d) ===\n", maxSym)
	codeLengths := make([]int, 280)
	prevLen := 8
	i := 0
	clcSymCount := 0
	for i < 280 && maxSym > 0 {
		maxSym--
		startBit := br.bitsRead
		sym, err2 := clcTree.readSymbol(br)
		clcSymCount++
		if err2 != nil {
			fmt.Printf("  clc readSymbol FAILED at bit %d: %v\n", br.bitsRead, err2)
			break
		}
		consumed := br.bitsRead - startBit

		switch sym {
		case 16:
			rep, _ := br.readBits(2)
			rep += 3
			for j := uint32(0); j < rep && i < 280; j++ {
				codeLengths[i] = prevLen
				i++
			}
			fmt.Printf("  sym16 rep=%d→i=%d (bits %d..%d, %d bits)\n", rep, i, startBit, br.bitsRead, consumed+2)
		case 17:
			rep, _ := br.readBits(3)
			rep += 3
			for j := uint32(0); j < rep && i < 280; j++ {
				codeLengths[i] = 0
				i++
			}
			prevLen = 0
			fmt.Printf("  sym17 rep=%d→i=%d (bits %d..%d)\n", rep, i, startBit, br.bitsRead)
		case 18:
			rep, _ := br.readBits(7)
			rep += 11
			for j := uint32(0); j < rep && i < 280; j++ {
				codeLengths[i] = 0
				i++
			}
			prevLen = 0
			fmt.Printf("  sym18 rep=%d→i=%d (bits %d..%d)\n", rep, i, startBit, br.bitsRead)
		default:
			codeLengths[i] = sym
			if sym != 0 { prevLen = sym }
			fmt.Printf("  sym%2d→codeLengths[%d]=%d (bits %d..%d, %d bits)\n",
				sym, i, sym, startBit, br.bitsRead, consumed)
			i++
		}
		if i >= 10 && clcSymCount < 5 {
			fmt.Printf("  ... (showing first 10 lengths then stopping)\n")
			break
		}
	}
	fmt.Printf("After %d clc reads: i=%d bit=%d\n", clcSymCount, i, br.bitsRead)
	fmt.Printf("First 20 code lengths: %v\n", codeLengths[:20])

	// Count nonzero.
	nonzero := 0
	for _, l := range codeLengths { if l > 0 { nonzero++ } }
	fmt.Printf("Nonzero code lengths: %d\n", nonzero)
}
