package vp8l

import (
	"fmt"
	"os"
	"testing"
)

func TestBitParse(t *testing.T) {
	data, _ := os.ReadFile("/tmp/test_small_lossless.webp")
	payload := data[20:]
	fmt.Printf("VP8L bytes: %x\n", payload)
	fmt.Printf("Total bits available: %d\n", len(payload)*8)
	
	br := newBitReader(payload[1:]) // skip signature
	
	// Header
	w, _ := br.readBits(14); w++
	h, _ := br.readBits(14); h++
	br.readBit(); br.readBits(3)
	fmt.Printf("Header: %dx%d at bit %d\n", w, h, br.bitsRead)
	
	// has_transform=1, type=3
	ht, _ := br.readBit()
	tt, _ := br.readBits(2)
	ps, _ := br.readBits(8)
	fmt.Printf("transform: has=%v type=%d palSize=%d at bit %d\n", ht, tt, ps+1, br.bitsRead)
	
	// decodeEntropyImage(1, 1) manually:
	// use_cc
	useCC, _ := br.readBit()
	fmt.Printf("[pal entropy] use_cc=%v at bit %d\n", useCC, br.bitsRead)
	
	// use_meta
	useMeta, _ := br.readBit()
	fmt.Printf("[pal entropy] use_meta=%v at bit %d\n", useMeta, br.bitsRead)
	
	// greenAlphabetSize() = 280, no CC
	// Read 5 trees
	alphaSizes := []int{greenAlphabetSize(), numColorCodes, numColorCodes, numColorCodes, numDistanceCodes}
	for tr := 0; tr < 5; tr++ {
		start := br.bitsRead
		// readCodeLengths manually:
		useMeta2, _ := br.readBit()
		fmt.Printf("  tree[%d] useMeta=%v at bit %d\n", tr, useMeta2, br.bitsRead)
		if useMeta2 { // simple code
			ns, _ := br.readBits(1); ns++
			isFirst8bit, _ := br.readBit()
			symBits := uint(1); if isFirst8bit { symBits = 8 }
			sym0, _ := br.readBits(symBits)
			fmt.Printf("    simple: ns=%d isFirst8bit=%v sym0=%d at bit %d\n", ns, isFirst8bit, sym0, br.bitsRead)
			if ns == 2 {
				sym1, _ := br.readBits(8)
				fmt.Printf("    sym1=%d\n", sym1)
			}
		} else { // complex code
			numCLC, _ := br.readBits(4); numCLC += 4
			fmt.Printf("    complex: numCLC=%d at bit %d\n", numCLC, br.bitsRead)
			clcOrder := []int{17,18,0,1,2,3,4,5,16,6,7,8,9,10,11,12,13,14,15}
			clcLens := make([]int, 19)
			for i := uint32(0); i < numCLC; i++ {
				v, _ := br.readBits(3)
				clcLens[clcOrder[i]] = int(v)
			}
			fmt.Printf("    clcLens=%v at bit %d\n", clcLens, br.bitsRead)
			// use_length
			useLen, _ := br.readBit()
			fmt.Printf("    use_length=%v at bit %d\n", useLen, br.bitsRead)
			// skip CLC body - just report
		}
		_ = start
		_ = alphaSizes[tr]
	}
	fmt.Printf("Remaining bits: %d (total used: %d)\n", int(br.nbits)+(len(payload[1:])-br.pos)*8, br.bitsRead)
}
