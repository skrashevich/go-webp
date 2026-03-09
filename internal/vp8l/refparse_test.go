package vp8l

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	refvp8l "golang.org/x/image/vp8l"
)

func TestRefDecodeSmall(t *testing.T) {
	data, err := os.ReadFile("/tmp/test_small_lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:] // skip RIFF/WEBP/VP8L chunk header
	fmt.Printf("VP8L data: %x\n", vp8lData)
	img, err := refvp8l.Decode(bytes.NewReader(vp8lData))
	if err != nil {
		t.Fatalf("ref decode: %v", err)
	}
	b := img.Bounds()
	fmt.Printf("Decoded OK: %dx%d\n", b.Max.X, b.Max.Y)
}

func TestRefDecodeReal(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	img, err := refvp8l.Decode(bytes.NewReader(vp8lData))
	if err != nil {
		t.Fatalf("ref decode: %v", err)
	}
	b := img.Bounds()
	t.Logf("Ref decoded OK: %dx%d", b.Max.X, b.Max.Y)
}

func TestOurDecodeReal(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	img, err := DecodeVP8L(vp8lData)
	if err != nil {
		t.Fatalf("our decode: %v", err)
	}
	b := img.Bounds()
	t.Logf("Our decoded OK: %dx%d", b.Max.X, b.Max.Y)
}

func TestDebugReal(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	w, _ := br.readBits(14); w++
	h, _ := br.readBits(14); h++
	br.readBit(); br.readBits(3)
	t.Logf("Image %dx%d at bit %d", w, h, br.bitsRead)

	// transforms
	for {
		hasTr, _ := br.readBit()
		if !hasTr { break }
		trType, _ := br.readBits(2)
		t.Logf("Transform type=%d at bit %d", trType, br.bitsRead)
		switch trType {
		case 3: // color indexing
			ps, _ := br.readBits(8)
			palSize := int(ps) + 1
			t.Logf("  palette size=%d, decoding at bit %d", palSize, br.bitsRead)
			palData, err := decodeEntropyImageLevel(br, palSize, 1, false)
			if err != nil {
				t.Fatalf("palette decode: %v at bit %d", err, br.bitsRead)
			}
			t.Logf("  palette decoded: %d entries at bit %d", len(palData), br.bitsRead)
		case 0, 1: // predictor, color
			bits, _ := br.readBits(3); bits += 2
			bw := subSampleSize(int(w), int(bits))
			bh := subSampleSize(int(h), int(bits))
			t.Logf("  transform data %dx%d at bit %d", bw, bh, br.bitsRead)
			data2, err := decodeEntropyImageLevel(br, bw, bh, false)
			if err != nil {
				t.Fatalf("transform data decode: %v at bit %d", err, br.bitsRead)
			}
			t.Logf("  transform data decoded: %d pixels at bit %d", len(data2), br.bitsRead)
		}
	}
	t.Logf("After transforms at bit %d", br.bitsRead)

	// main image color cache
	useCC, _ := br.readBit()
	var ccBits uint
	if useCC {
		b, _ := br.readBits(4); ccBits = uint(b)
	}
	t.Logf("use_cc=%v ccBits=%d at bit %d", useCC, ccBits, br.bitsRead)

	// use_meta
	useMeta, _ := br.readBit()
	t.Logf("use_meta=%v at bit %d", useMeta, br.bitsRead)
	if useMeta {
		mb, _ := br.readBits(3); mb += 2
		metaW := subSampleSize(int(w), int(mb))
		metaH := subSampleSize(int(h), int(mb))
		t.Logf("meta_bits=%d metaW=%d metaH=%d at bit %d", mb, metaW, metaH, br.bitsRead)
		metaData, err := decodeEntropyImageLevel(br, metaW, metaH, false)
		if err != nil {
			t.Fatalf("meta decode: %v at bit %d", err, br.bitsRead)
		}
		// count groups
		numGroups := 0
		for _, p := range metaData {
			g := int((p >> 8) & 0xffff)
			if g+1 > numGroups { numGroups = g+1 }
		}
		t.Logf("numGroups=%d at bit %d", numGroups, br.bitsRead)
	}
}

func TestDebugRealPalette(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	// Skip to bit 2490 (after transform type=3, palSize)
	// header 32, tr type2 bits=3, tr type0 bits=3+data=2479, tr type3 bits=5+palSize=8 = 2490
	// Read each piece:
	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3) // header 32
	br.readBit(); br.readBits(2) // transform type=2 (35)
	br.readBit(); br.readBits(2) // transform type=0 (38)
	br.readBits(3) // bits (41)
	// read predictor data: 23*44=1012 pixels via decodeEntropyImageLevel
	_, err = decodeEntropyImageLevel(br, 23, 44, false)
	if err != nil { t.Fatalf("pred: %v", err) }
	t.Logf("After predictor data at bit %d", br.bitsRead) // expect 2479
	br.readBit(); br.readBits(2) // transform type=3 (2482)
	ps, _ := br.readBits(8); palSize := int(ps)+1
	t.Logf("palSize=%d at bit %d", palSize, br.bitsRead) // expect 2490

	// Now decode palette manually: 157 pixels, non-topLevel
	// Read use_cc
	useCC2, _ := br.readBit()
	t.Logf("palette use_cc=%v at bit %d", useCC2, br.bitsRead)
	// Read 5 trees
	alphaSizes := []int{greenAlphabetSize(), numColorCodes, numColorCodes, numColorCodes, numDistanceCodes}
	for tr := 0; tr < 5; tr++ {
		start := br.bitsRead
		cls, err := readCodeLengths(br, alphaSizes[tr])
		if err != nil { t.Fatalf("tree[%d]: %v", tr, err) }
		nonZero := 0
		for _, l := range cls { if l > 0 { nonZero++ } }
		t.Logf("tree[%d] alphabetSize=%d nonZero=%d bits %d..%d", tr, alphaSizes[tr], nonZero, start, br.bitsRead)
	}
	t.Logf("After trees at bit %d", br.bitsRead)
	
	// Decode first few pixels
	// (need actual trees to decode)
}

func TestDebugRealPalette2(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	// Skip to palette trees (bit 2490)
	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3)
	br.readBit(); br.readBits(2) // type=2
	br.readBit(); br.readBits(2) // type=0
	br.readBits(3)
	decodeEntropyImageLevel(br, 23, 44, false)
	br.readBit(); br.readBits(2) // type=3
	ps, _ := br.readBits(8); palSize := int(ps)+1

	// palette: non-topLevel
	br.readBit() // use_cc=false
	
	// Read 5 trees
	alphaSizes := []int{greenAlphabetSize(), numColorCodes, numColorCodes, numColorCodes, numDistanceCodes}
	trees := make([]*huffTree, 5)
	for tr := 0; tr < 5; tr++ {
		cls, _ := readCodeLengths(br, alphaSizes[tr])
		trees[tr], _ = buildHuffTree(cls)
	}
	t.Logf("Palette trees built, now decoding %d pixels at bit %d", palSize, br.bitsRead)
	
	// Decode pixels with logging
	pixels := make([]uint32, palSize)
	for i := 0; i < palSize; i++ {
		sym, err := trees[0].readSymbol(br)
		if err != nil {
			t.Fatalf("pixel %d green: %v at bit %d", i, err, br.bitsRead)
		}
		if sym < numLiteralCodes {
			r, _ := trees[1].readSymbol(br)
			b2, _ := trees[2].readSymbol(br)
			a, _ := trees[3].readSymbol(br)
			pixels[i] = uint32(a)<<24 | uint32(r)<<16 | uint32(sym)<<8 | uint32(b2)
		} else if sym < numLiteralCodes+numLengthCodes {
			length, _ := readLength(br, sym)
			distSym, _ := trees[4].readSymbol(br)
			eb := distanceExtraBits[distSym]
			extra, _ := br.readBits(eb)
			rawDist := distanceBase[distSym] + int(extra)
			planeDist := vp8lDistanceToPlane(rawDist, palSize)
			src := i - planeDist
			t.Logf("  pixel %d: backref len=%d distSym=%d rawDist=%d planeDist=%d src=%d", 
				i, length, distSym, rawDist, planeDist, src)
			if src < 0 {
				t.Fatalf("  INVALID: src=%d < 0 at pixel %d", src, i)
			}
			for j := 0; j < length && i < palSize; j++ {
				pixels[i] = pixels[src+j]
				i++
			}
			i-- // loop will increment
		}
	}
	t.Logf("Palette decoded OK at bit %d", br.bitsRead)
}

func TestDebugRealPalette3(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	// Skip to palette trees
	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3)
	br.readBit(); br.readBits(2)
	br.readBit(); br.readBits(2)
	br.readBits(3)
	decodeEntropyImageLevel(br, 23, 44, false)
	br.readBit(); br.readBits(2)
	ps, _ := br.readBits(8); palSize := int(ps)+1
	br.readBit() // use_cc

	alphaSizes := []int{greenAlphabetSize(), numColorCodes, numColorCodes, numColorCodes, numDistanceCodes}
	trees := make([]*huffTree, 5)
	for tr := 0; tr < 5; tr++ {
		cls, _ := readCodeLengths(br, alphaSizes[tr])
		trees[tr], _ = buildHuffTree(cls)
	}
	t.Logf("Palette %d pixels, trees at bit %d", palSize, br.bitsRead)
	_ = palSize
	
	for i := 0; i < 6; i++ {
		startBit := br.bitsRead
		sym, err := trees[0].readSymbol(br)
		t.Logf("pixel %d: green sym=%d (consumed %d bits from bit %d)", i, sym, br.bitsRead-startBit, startBit)
		if err != nil { t.Fatalf("err: %v", err) }
		if sym < numLiteralCodes {
			r, _ := trees[1].readSymbol(br)
			b2, _ := trees[2].readSymbol(br)
			a, _ := trees[3].readSymbol(br)
			t.Logf("  literal: r=%d b=%d a=%d at bit %d", r, b2, a, br.bitsRead)
		} else {
			t.Logf("  NON-LITERAL sym=%d (length or cache)", sym)
			break
		}
	}
}

func TestDebugRealPaletteRef(t *testing.T) {
	// Compare raw bits at position 3636 (where pixel 3 green sym starts)
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	
	// Bit 3636 from start of vp8l data (after signature byte)
	// = byte offset (3636+8)/8 = 455+1=456 from start of data[20:]
	// Actually: bit 3636 in br (which starts after signature byte)
	// = bit 3636 in data[21:]
	byteOffset := (3636) / 8
	bitInByte := uint(3636 % 8)
	t.Logf("Pixel 3 green starts at byte %d bit %d of vp8l payload (after sig)", byteOffset, bitInByte)
	
	// Show raw bytes around that position
	if byteOffset+4 < len(vp8lData)-1 {
		raw := vp8lData[1+byteOffset : 1+byteOffset+8]
		t.Logf("Raw bytes: %08b %08b %08b %08b %08b %08b %08b %08b",
			raw[0], raw[1], raw[2], raw[3], raw[4], raw[5], raw[6], raw[7])
		// Extract 3 bits starting at bitInByte (LSB-first)
		val := uint64(0)
		for i := 0; i < 8; i++ {
			val |= uint64(raw[i]) << (uint(i)*8)
		}
		val >>= bitInByte
		t.Logf("bits from position: %064b", val&0xffffffff)
		// green sym=262 should be consumed in 3 bits -> then dist sym
		t.Logf("  first 3 bits (green): %03b = %d", val&7, val&7)
		t.Logf("  next bits (dist tree): %b", (val>>3)&0x3f)
	}
}

func TestDebugTree0(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3)
	br.readBit(); br.readBits(2)
	br.readBit(); br.readBits(2)
	br.readBits(3)
	decodeEntropyImageLevel(br, 23, 44, false)
	br.readBit(); br.readBits(2)
	ps, _ := br.readBits(8); _ = ps
	br.readBit() // use_cc

	// Read only tree[0]
	cls, _ := readCodeLengths(br, greenAlphabetSize())
	tree, _ := buildHuffTree(cls)
	
	// Check for uninitialized nodes
	uninit := 0
	for i, n := range tree.nodes {
		if n.children == 0 {
			uninit++
			if uninit <= 5 {
				t.Logf("  uninit node %d", i)
			}
		}
	}
	t.Logf("Tree[0]: %d nodes, %d uninitialized", len(tree.nodes), uninit)
	
	// Verify code lengths stats
	nonZero := 0
	maxLen := 0
	for _, l := range cls {
		if l > 0 { nonZero++ }
		if l > maxLen { maxLen = l }
	}
	t.Logf("Code lengths: nonZero=%d maxLen=%d", nonZero, maxLen)
	
	// Check Kraft inequality
	kraft := 0.0
	for _, l := range cls {
		if l > 0 {
			v := 1.0
			for i := 0; i < l; i++ { v /= 2 }
			kraft += v
		}
	}
	t.Logf("Kraft sum = %.6f (should be <= 1.0)", kraft)
}

func TestDebugTree0Lengths(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3)
	br.readBit(); br.readBits(2)
	br.readBit(); br.readBits(2)
	br.readBits(3)
	decodeEntropyImageLevel(br, 23, 44, false)
	br.readBit(); br.readBits(2)
	ps, _ := br.readBits(8); _ = ps
	br.readBit() // use_cc

	// Manually read tree[0] structure
	// readCodeLengths: bit=0 → complex
	useMeta, _ := br.readBit()
	t.Logf("tree[0] useMeta=%v at bit %d", useMeta, br.bitsRead)
	
	numCLC, _ := br.readBits(4); numCLC += 4
	t.Logf("numCLC=%d at bit %d", numCLC, br.bitsRead)
	
	clcOrder := []int{17,18,0,1,2,3,4,5,16,6,7,8,9,10,11,12,13,14,15}
	clcLens := make([]int, 19)
	for i := uint32(0); i < numCLC; i++ {
		v, _ := br.readBits(3)
		clcLens[clcOrder[i]] = int(v)
	}
	t.Logf("clcLens=%v at bit %d", clcLens, br.bitsRead)
	
	useLen, _ := br.readBit()
	t.Logf("use_length=%v at bit %d", useLen, br.bitsRead)
	
	maxSym := greenAlphabetSize()
	if useLen {
		n, _ := br.readBits(3)
		msBits := 2 + 2*uint(n)
		ms, _ := br.readBits(msBits)
		maxSym = int(ms) + 2
		t.Logf("  n=%d msBits=%d ms=%d maxSymbol=%d at bit %d", n, msBits, ms, maxSym, br.bitsRead)
	}
	
	// Build CLC tree and read first 20 symbols
	clTree, _ := buildHuffTree(clcLens)
	t.Logf("CLC tree: %d nodes at bit %d", len(clTree.nodes), br.bitsRead)
	
	// Read first 30 CLC symbols
	symCount := 0
	i := 0
	prevLen := 8
	for i < greenAlphabetSize() && symCount < maxSym && symCount < 30 {
		symCount++
		sym, err := clTree.readSymbol(br)
		if err != nil {
			t.Fatalf("CLC sym error at symCount=%d i=%d: %v", symCount, i, err)
		}
		switch sym {
		case 16:
			rep, _ := br.readBits(2); rep += 3
			t.Logf("  sym[%d]=16 rep=%d (fills %d..%d with prevLen=%d)", symCount, rep, i, i+int(rep)-1, prevLen)
			for j := uint32(0); j < rep && i < greenAlphabetSize(); j++ { i++ }
		case 17:
			rep, _ := br.readBits(3); rep += 3
			t.Logf("  sym[%d]=17 rep=%d (fills %d..%d with 0)", symCount, rep, i, i+int(rep)-1)
			for j := uint32(0); j < rep && i < greenAlphabetSize(); j++ { i++ }
			prevLen = 0
		case 18:
			rep, _ := br.readBits(7); rep += 11
			t.Logf("  sym[%d]=18 rep=%d (fills %d..%d with 0)", symCount, rep, i, i+int(rep)-1)
			for j := uint32(0); j < rep && i < greenAlphabetSize(); j++ { i++ }
			prevLen = 0
		default:
			t.Logf("  sym[%d]=%d at index %d", symCount, sym, i)
			if sym != 0 { prevLen = sym }
			i++
		}
	}
	t.Logf("After %d CLC syms: i=%d at bit %d", symCount, i, br.bitsRead)
	_ = prevLen
}

func TestDebugCLCKraft(t *testing.T) {
	// clcLens from test: [4 4 4 3 4 1 0 2 4 1 0 0 0 0 0 0 2 5 7]
	// These are for symbols 0..18 in the CLC alphabet
	clcLens := []int{4, 4, 4, 3, 4, 1, 0, 2, 4, 1, 0, 0, 0, 0, 0, 0, 2, 5, 7}
	
	kraft := 0.0
	nonZero := 0
	for i, l := range clcLens {
		if l > 0 {
			nonZero++
			v := 1.0
			for j := 0; j < l; j++ { v /= 2 }
			kraft += v
			t.Logf("  clc[%d] l=%d contribution=1/%d", i, l, 1<<l)
		}
	}
	t.Logf("CLC: nonZero=%d kraft=%.6f", nonZero, kraft)
	
	// Build tree and check
	tree, err := buildHuffTree(clcLens)
	if err != nil {
		t.Fatalf("buildHuffTree: %v", err)
	}
	uninit := 0
	for _, n := range tree.nodes {
		if n.children == 0 { uninit++ }
	}
	t.Logf("CLC tree: %d nodes, %d uninitialized", len(tree.nodes), uninit)
	
	// Try to decode all possible 7-bit patterns  
	unique := make(map[int]int)
	for bits := 0; bits < 128; bits++ {
		sym, _ := tree.readSymbolFromBits(uint64(bits), 7)
		unique[sym]++
	}
	t.Logf("Unique symbols decodable: %d", len(unique))
	for sym, count := range unique {
		t.Logf("  sym=%d count=%d", sym, count)
	}
}

func TestDebugBitPos(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]

	// Read bits one by one up to bit 2491, tracking exactly
	// Transform type=2 (subtract green): 1+2 = 3 bits
	// Transform type=0 (predictor): 1+2+3 = 6 bits + predictor data
	// Transform type=3 (color indexing): 1+2+8 = 11 bits
	// palette: use_cc=1 bit
	// tree[0]: use_meta + complex code...
	
	// Let's just print the exact byte and bit of position 2491 in the data
	byteOff := (2491) / 8  // bit 2491 in br = 2491 bits from start of vp8lData[1:]
	bitOff := uint(2491 % 8)
	t.Logf("Bit 2491 = byte %d, bit %d of vp8lData[1+%d]", byteOff, bitOff, byteOff)
	raw := vp8lData[1+byteOff:]
	t.Logf("Raw bytes from there: %08b %08b %08b %08b %08b", raw[0], raw[1], raw[2], raw[3], raw[4])
	
	// Show bit stream from position 2491 LSB-first
	val := uint64(raw[0]) | uint64(raw[1])<<8 | uint64(raw[2])<<16 | uint64(raw[3])<<24 | uint64(raw[4])<<32
	val >>= bitOff
	t.Logf("LSB-first bits from 2491: %064b", val)
	t.Logf("First 20 bits: %020b", val&0xfffff)
	
	// useMeta (bit 0) = should be 0 (complex code)
	// then numCLC (bits 1..4) = 13-4=9 → 1001 in binary
	t.Logf("useMeta=%d numCLC_raw=%d (numCLC=%d)", val&1, (val>>1)&0xf, ((val>>1)&0xf)+4)
}

func TestDebugGreenTreeCLS(t *testing.T) {
	data, err := os.ReadFile("/tmp/webp-test/ffmpeg/lossless.webp")
	if err != nil {
		t.Skip(err)
	}
	vp8lData := data[20:]
	br := newBitReader(vp8lData[1:])

	br.readBits(14); br.readBits(14); br.readBit(); br.readBits(3)
	br.readBit(); br.readBits(2)
	br.readBit(); br.readBits(2)
	br.readBits(3)
	decodeEntropyImageLevel(br, 23, 44, false)
	br.readBit(); br.readBits(2)
	ps, _ := br.readBits(8); _ = ps
	br.readBit() // use_cc

	// Read tree[0] code lengths
	cls, err := readCodeLengths(br, greenAlphabetSize())
	if err != nil {
		t.Fatalf("readCodeLengths: %v", err)
	}
	
	// Summarize: count by length
	byLen := make(map[int]int)
	for _, l := range cls {
		byLen[l]++
	}
	t.Logf("Code length distribution: %v", byLen)
	
	// Compute Kraft
	kraft := 0.0
	for _, l := range cls {
		if l > 0 {
			v := 1.0
			for i := 0; i < l; i++ { v /= 2 }
			kraft += v
		}
	}
	t.Logf("Kraft sum = %.6f", kraft)
	
	// Show first 20 non-zero lengths
	count := 0
	for i, l := range cls {
		if l > 0 && count < 20 {
			t.Logf("  cls[%d]=%d", i, l)
			count++
		}
	}
}
