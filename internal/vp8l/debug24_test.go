package vp8l

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	refvp8l "golang.org/x/image/vp8l"
)

// TestDebugFirstPalettePixel decodes all trees and reads the first palette pixel
// to compare vs reference.
func TestDebugFirstPalettePixel(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip("test file not available:", err)
	}
	vp8lData := data[20:]

	// Reference decode for comparison.
	refImg, err := refvp8l.Decode(bytes.NewReader(vp8lData))
	if err != nil {
		t.Fatalf("ref: %v", err)
	}
	_ = refImg

	br := newBitReader(vp8lData[1:])

	mustBits := func(n uint) uint32 {
		v, e := br.readBits(n)
		if e != nil {
			t.Fatalf("readBits(%d) at %d: %v", n, br.bitsRead, e)
		}
		return v
	}
	mustBit := func() bool {
		v, e := br.readBit()
		if e != nil {
			t.Fatalf("readBit at %d: %v", br.bitsRead, e)
		}
		return v
	}

	// Header
	w := int(mustBits(14)) + 1
	h := int(mustBits(14)) + 1
	mustBit()   // alpha
	mustBits(3) // version

	// Transform 0: subtract_green
	mustBit()   // has_transform
	mustBits(2) // type=2

	// Transform 1: predictor
	mustBit()   // has_transform
	mustBits(2) // type=0
	bits := int(mustBits(3)) + 2
	bw := subSampleSize(w, bits)
	bh := subSampleSize(h, bits)
	_, err = decodeEntropyImageLevel(br, bw, bh, false)
	if err != nil {
		t.Fatalf("predictor: %v", err)
	}

	// Transform 2: palette
	mustBit()   // has_transform
	mustBits(2) // type=3
	palSize := int(mustBits(8)) + 1
	fmt.Printf("palSize=%d, bit=%d\n", palSize, br.bitsRead)

	// Palette entropy image: use_cc=false + 5 trees.
	useCC, _ := br.readBit()
	var cc *colorCache
	var ccBits uint
	if useCC {
		cb, _ := br.readBits(4)
		ccBits = uint(cb)
		cc = newColorCache(ccBits)
	}

	// topLevel=false → no use_meta → single group
	alphaSizes := [5]int{greenAlphabetSize(), 256, 256, 256, 40}
	if cc != nil {
		alphaSizes[0] += 1 << ccBits
	}

	var trees [5]*huffTree
	for tIdx := 0; tIdx < 5; tIdx++ {
		clens, err2 := readCodeLengths(br, alphaSizes[tIdx])
		if err2 != nil {
			t.Fatalf("tree[%d] clens: %v", tIdx, err2)
		}
		tree, err2 := buildHuffTree(clens)
		if err2 != nil {
			t.Fatalf("tree[%d] build: %v", tIdx, err2)
		}
		trees[tIdx] = tree
		fmt.Printf("tree[%d] nodes=%d single=%v bit=%d\n", tIdx, len(tree.nodes), tree.single, br.bitsRead)
	}

	fmt.Printf("\nDecoding palette pixels from bit=%d\n", br.bitsRead)

	// Decode first 10 palette pixels.
	pixels := make([]uint32, palSize)
	for i := 0; i < palSize && i < 10; i++ {
		startBit := br.bitsRead
		sym, err2 := trees[0].readSymbol(br)
		if err2 != nil {
			t.Fatalf("pixel %d green: %v at bit %d", i, err2, br.bitsRead)
		}
		fmt.Printf("pixel[%d] green sym=%d (%s) startBit=%d consumed=%d\n",
			i, sym, symKind(sym), startBit, br.bitsRead-startBit)

		if sym < numLiteralCodes {
			r, _ := trees[1].readSymbol(br)
			b2, _ := trees[2].readSymbol(br)
			a, _ := trees[3].readSymbol(br)
			col := uint32(a)<<24 | uint32(r)<<16 | uint32(sym)<<8 | uint32(b2)
			pixels[i] = col
			fmt.Printf("  literal: r=%d g=%d b=%d a=%d col=0x%08x\n", r, sym, b2, a, col)
		} else if sym < numLiteralCodes+numLengthCodes {
			length, _ := readLength(br, sym)
			dist, _ := readDistance(br, trees[4], palSize)
			src := i - dist
			fmt.Printf("  LZ77: length=%d dist=%d src=%d (i=%d)\n", length, dist, src, i)
			if src < 0 {
				fmt.Printf("  ERROR: backward ref out of bounds!\n")
				break
			}
			for j := 0; j < length && i < palSize; j++ {
				pixels[i] = pixels[src+j]
				i++
			}
			i-- // loop will increment
		}
	}

	// Compare with what reference produces for palette.
	// Reference's palette is stored in the transform data after decode+delta.
	fmt.Printf("\nFirst few palette pixels (ours):\n")
	for i := 0; i < 5 && i < palSize; i++ {
		fmt.Printf("  palette[%d] = 0x%08x\n", i, pixels[i])
	}
}

func symKind(sym int) string {
	switch {
	case sym < numLiteralCodes:
		return "literal"
	case sym < numLiteralCodes+numLengthCodes:
		return "LZ77"
	default:
		return "cache"
	}
}
