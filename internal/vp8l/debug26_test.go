package vp8l

import (
	"fmt"
	"testing"

	refvp8l "golang.org/x/image/vp8l"
	"bytes"
	"os"
)

// TestDebugRefCLCBuild tests what exactly the reference decoder does with
// the overfull CLC clcLens=[4,4,4,3,4,1,0,2,4,1,0,0,0,0,0,0,2,5,7].
// The reference includes hist[0] in nextCode computation, producing large codes.
// We need to understand if that causes cap overflow or not.
func TestDebugRefCLCBuild(t *testing.T) {
	// Simulate codeLengthsToCodes with the actual CLC lengths
	codeLengths := []uint32{4, 4, 4, 3, 4, 1, 0, 2, 4, 1, 0, 0, 0, 0, 0, 0, 2, 5, 7}

	maxCL := uint32(0)
	for _, cl := range codeLengths {
		if cl > maxCL { maxCL = cl }
	}

	histogram := [16]uint32{}
	for _, cl := range codeLengths {
		histogram[cl]++
	}
	fmt.Printf("histogram=%v\n", histogram[:maxCL+1])

	currCode := uint32(0)
	nextCodes := [16]uint32{}
	for cl := 1; cl <= int(maxCL); cl++ {
		currCode = (currCode + histogram[cl-1]) << 1
		nextCodes[cl] = currCode
	}
	fmt.Printf("nextCodes=%v\n", nextCodes[:maxCL+1])

	// Count symbols
	nSymbols := 0
	for _, cl := range codeLengths { if cl > 0 { nSymbols++ } }
	cap23 := 2*nSymbols - 1
	fmt.Printf("nSymbols=%d cap=%d\n", nSymbols, cap23)

	// Simulate the insert() calls
	// insert(symbol, code, codeLength) navigates codeLength bits MSB-first:
	//   1&(code>>codeLength) after decrementing codeLength each step
	//
	// nodes[n].children: 0=uninitialized, leafNode=-1, >0=first child
	// When uninitialized: if len==cap → error; else allocate 2 children
	// Each node: children field. 0=uninitialized, -1=leaf(leafNode), >0=index of first child pair
	type hNode struct {
		children int32
		symbol   uint32
	}
	hnodes := make([]hNode, 1, cap23)
	hnodes[0] = hNode{}

	nc := nextCodes
	insertOK := true
	for sym, cl := range codeLengths {
		if cl == 0 { continue }
		code := nc[cl]
		nc[cl]++
		cl32 := cl

		// Walk the tree MSB-first (reference insert behavior)
		n := uint32(0)
		ok := true
		for cl32 > 0 {
			cl32--
			if int(n) >= len(hnodes) {
				fmt.Printf("  sym=%2d: node %d out of bounds\n", sym, n)
				ok = false; break
			}
			switch hnodes[n].children {
			case -1: // leaf hit → error
				fmt.Printf("  sym=%2d: leaf hit at node %d\n", sym, n)
				ok = false
			case 0: // uninitialized → allocate children
				if len(hnodes) == cap(hnodes) {
					fmt.Printf("  sym=%2d: cap exceeded at node %d (len=%d cap=%d)\n", sym, n, len(hnodes), cap(hnodes))
					ok = false
				} else {
					childIdx := int32(len(hnodes))
					hnodes[n].children = childIdx
					hnodes = append(hnodes, hNode{}, hNode{})
				}
			}
			if !ok { break }
			b := uint32(1) & (code >> cl32)
			n = uint32(hnodes[n].children) + b
		}
		if !ok {
			insertOK = false
			fmt.Printf("  sym=%2d l=%d code=%b (orig=%d) → INSERT FAILED\n", sym, codeLengths[sym], code, code)
			continue
		}
		// Mark leaf
		switch hnodes[n].children {
		case -1:
			fmt.Printf("  sym=%2d l=%d code=%b → already leaf!\n", sym, codeLengths[sym], code)
			insertOK = false
		case 0:
			hnodes[n].children = -1
			hnodes[n].symbol = uint32(sym)
			fmt.Printf("  sym=%2d l=%d code=%b → OK (nodes=%d/%d)\n", sym, codeLengths[sym], code, len(hnodes), cap(hnodes))
		default:
			fmt.Printf("  sym=%2d l=%d code=%b → internal node!\n", sym, codeLengths[sym], code)
			insertOK = false
		}
	}
	fmt.Printf("insertOK=%v, final nodes=%d/%d\n", insertOK, len(hnodes), cap(hnodes))
}

// TestDebugRefDecodeFile verifies the reference decoder can decode our file.
func TestDebugRefDecodeFile(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]
	_, err = refvp8l.Decode(bytes.NewReader(vp8lData))
	if err != nil {
		t.Fatalf("reference failed: %v", err)
	}
	fmt.Println("Reference decode OK")
}
