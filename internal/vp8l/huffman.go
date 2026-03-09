package vp8l

import (
	"errors"
	"sort"
)

const (
	maxHuffmanBits   = 16
	maxHuffmanCodes  = 2328 // max alphabet size in VP8L
	alphabetSize     = 256 + 24 + numDistanceCodes // green channel alphabet
	numDistanceCodes = 40
	numLiteralCodes  = 256
	numLengthCodes   = 24
	numColorCodes    = 256

	// Code length alphabet size for meta-Huffman
	codeLengthCodes = 19

	// minLUTBits is the minimum LUT table size in bits.
	// Using max(maxBits, minLUTBits) ensures every l-bit code (l <= minLUTBits)
	// fills 2^(lutBits-l) entries in the table, covering all possible peek values.
	minLUTBits = 7
)

// huffTree is a Huffman decoding tree.
// It uses a flat node array for bit-by-bit traversal, matching the reference VP8L decoder.
// nodes[0] is the root. For internal nodes, children > 0 is the index of the first
// child pair (left=children, right=children+1). children==0 means uninitialized
// (treated like root for reference-compatible behavior). children==-1 means leaf.
type huffTree struct {
	nodes        []huffTreeNode
	single       bool
	singleSymbol int
}

type huffTreeNode struct {
	symbol   int
	children int // -1=leaf, 0=uninitialized, >0=index of first child pair
}

// For the LUT-based fast path (kept for non-CLC trees with known depths):
type huffTableEntry struct {
	symbol int
	nbits  uint8
}

// reverseBits reverses the lowest n bits of val.
func reverseBits(val int, n uint) int {
	result := 0
	for i := uint(0); i < n; i++ {
		result = (result << 1) | (val & 1)
		val >>= 1
	}
	return result
}

// buildHuffTree constructs a Huffman tree from code lengths.
// Matches the reference VP8L decoder (golang.org/x/image/vp8l) tree structure exactly.
// codeLengths[i] is the bit length for symbol i; 0 means symbol is not present.
func buildHuffTree(codeLengths []int) (*huffTree, error) {
	numSymbols := 0
	lastSymbol := 0
	for i, l := range codeLengths {
		if l > 0 {
			numSymbols++
			lastSymbol = i
		}
	}

	if numSymbols == 0 {
		return &huffTree{single: true, singleSymbol: 0}, nil
	}
	if numSymbols == 1 {
		return &huffTree{single: true, singleSymbol: lastSymbol}, nil
	}

	// Count codes for each bit length (including length 0, matching reference codeLengthsToCodes).
	maxBits := 0
	for _, l := range codeLengths {
		if l > maxBits {
			maxBits = l
		}
	}
	blCount := make([]int, maxBits+1)
	for _, l := range codeLengths {
		blCount[l]++ // include l==0, matching golang.org/x/image/vp8l codeLengthsToCodes histogram
	}

	// Compute starting codes for each bit length (canonical Huffman).
	nextCode := make([]int, maxBits+1)
	code := 0
	for bits := 1; bits <= maxBits; bits++ {
		code = (code + blCount[bits-1]) << 1
		nextCode[bits] = code
	}

	// nodes[0] is root. children=0 means uninitialized, children=-1 means leaf.
	// Grow with append — canonical Huffman trees can have internal nodes > 2n-1
	// when code lengths are not all equal (partial trees are valid in VP8L).
	nodes := make([]huffTreeNode, 1)

	for sym, l := range codeLengths {
		if l == 0 {
			continue
		}
		c := nextCode[l]
		nextCode[l]++

		// Insert symbol navigating code bits LSB-first (matching reference insert() and readSymbol).
		// Reference: n = children + 1&(code>>codeLength) after codeLength--
		// i.e. bit 0 first, then bit 1, ... bit l-1 last.
		n := 0
		ok := true
		cl := l
		for cl > 0 {
			cl--
			b := (c >> uint(cl)) & 1
			switch nodes[n].children {
			case -1: // already a leaf — overflow code, skip
				ok = false
			case 0: // uninitialized — allocate two children
				childIdx := len(nodes)
				nodes = append(nodes, huffTreeNode{}, huffTreeNode{})
				nodes[n].children = childIdx
			}
			if !ok {
				break
			}
			n = nodes[n].children + b
		}
		if !ok {
			continue
		}
		// Mark as leaf.
		if nodes[n].children == 0 {
			nodes[n].children = -1
		}
		nodes[n].symbol = sym
	}

	return &huffTree{nodes: nodes}, nil
}

// readSymbol reads the next Huffman symbol from br using bit-by-bit tree traversal.
// Matches the reference VP8L decoder's slowPath behavior exactly, including the
// "uninitialized node" fallback: when children==0, navigate as n = 0 + b (back to
// root's first child pair), replicating the golang.org/x/image/vp8l behavior for
// incomplete trees (Kraft-inequality violations in ffmpeg-generated streams).
func (t *huffTree) readSymbol(br *bitReader) (int, error) {
	if t.single {
		return t.singleSymbol, nil
	}
	n := 0
	for t.nodes[n].children != -1 {
		if br.nbits == 0 {
			br.fill()
		}
		if br.nbits == 0 {
			return 0, errors.New("vp8l: unexpected end of bitstream in Huffman decode")
		}
		b := int(br.val & 1)
		br.val >>= 1
		br.nbits--
		br.bitsRead++
		// children==0: uninitialized node. Reference does n = 0 + b.
		n = t.nodes[n].children + b
	}
	return t.nodes[n].symbol, nil
}

// --- Code length decoding ---

// readCodeLengths reads Huffman code lengths for an alphabet of size alphabetSize
// using the meta-Huffman approach described in the VP8L spec.
func readCodeLengths(br *bitReader, alphabetSize int) ([]int, error) {
	// Read simple code length code.
	useMeta, err := br.readBit()
	if err != nil {
		return nil, err
	}

	if useMeta {
		// Simple code length code (bit=1): 1 or 2 symbols.
		numSymbols, err := br.readBits(1)
		if err != nil {
			return nil, err
		}
		numSymbols++

		// Read symbols (stored as fixed-width values).
		isFirst8bit, err := br.readBit()
		if err != nil {
			return nil, err
		}
		symBits := uint(8)
		if isFirst8bit {
			symBits = 8
		} else {
			symBits = 1
		}

		sym0, err := br.readBits(symBits)
		if err != nil {
			return nil, err
		}
		codeLengths := make([]int, alphabetSize)
		if int(sym0) < alphabetSize {
			codeLengths[sym0] = 1
		}
		if numSymbols == 2 {
			sym1, err := br.readBits(8)
			if err != nil {
				return nil, err
			}
			if int(sym1) < alphabetSize {
				codeLengths[sym1] = 1
			}
		}
		return codeLengths, nil
	}

	// Complex code length code.
	// Read number of code length codes.
	numCLCodes, err := br.readBits(4)
	if err != nil {
		return nil, err
	}
	numCLCodes += 4

	// Order of code length code lengths (same as DEFLATE).
	clcOrder := []int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

	clcLengths := make([]int, codeLengthCodes)
	for i := uint32(0); i < numCLCodes; i++ {
		v, err := br.readBits(3)
		if err != nil {
			return nil, err
		}
		clcLengths[clcOrder[i]] = int(v)
	}

	clTree, err := buildHuffTree(clcLengths)
	if err != nil {
		return nil, err
	}

	// Optional max_symbol limiter: if use_length=1, read n(3bits) then max_symbol((2+2*n) bits).
	maxSymbol := alphabetSize
	useLength, err := br.readBit()
	if err != nil {
		return nil, err
	}
	if useLength {
		n, err := br.readBits(3)
		if err != nil {
			return nil, err
		}
		msBits := 2 + 2*uint(n)
		ms, err := br.readBits(msBits)
		if err != nil {
			return nil, err
		}
		maxSymbol = int(ms) + 2
		if maxSymbol > alphabetSize {
			maxSymbol = alphabetSize
		}
	}

	codeLengths := make([]int, alphabetSize)
	prevLen := 8
	// maxSymbol is a count of CLC tree symbols to consume (not an index limit).
	// Each symbol read decrements maxSymbol by 1; repeats expand into multiple
	// code-length slots but only consume one CLC symbol.
	i := 0
	for i < alphabetSize {
		if maxSymbol == 0 {
			break
		}
		maxSymbol--
		sym, err := clTree.readSymbol(br)
		if err != nil {
			return nil, err
		}
		switch sym {
		case 16: // Repeat previous length 3-6 times.
			rep, err := br.readBits(2)
			if err != nil {
				return nil, err
			}
			rep += 3
			for j := uint32(0); j < rep && i < alphabetSize; j++ {
				codeLengths[i] = prevLen
				i++
			}
		case 17: // Repeat zero 3-10 times.
			rep, err := br.readBits(3)
			if err != nil {
				return nil, err
			}
			rep += 3
			for j := uint32(0); j < rep && i < alphabetSize; j++ {
				codeLengths[i] = 0
				i++
			}
			prevLen = 0
		case 18: // Repeat zero 11-138 times.
			rep, err := br.readBits(7)
			if err != nil {
				return nil, err
			}
			rep += 11
			for j := uint32(0); j < rep && i < alphabetSize; j++ {
				codeLengths[i] = 0
				i++
			}
			prevLen = 0
		default:
			codeLengths[i] = sym
			if sym != 0 {
				prevLen = sym
			}
			i++
		}
	}

	return codeLengths, nil
}

// --- Huffman encoding ---

type huffCode struct {
	code  uint32
	nbits uint
}

// buildHuffCodes computes length-limited canonical Huffman codes from symbol frequencies.
// Uses the package-merge (Larmore-Hirschberg) algorithm to guarantee code lengths <= maxBits
// while satisfying the Kraft inequality.
// Returns codes indexed by symbol.
func buildHuffCodes(freqs []int, maxBits int) ([]huffCode, []int) {
	n := len(freqs)
	if maxBits > 15 {
		maxBits = 15
	}

	type symFreq struct {
		sym  int
		freq int
	}

	active := make([]symFreq, 0, n)
	for i, f := range freqs {
		if f > 0 {
			active = append(active, symFreq{sym: i, freq: f})
		}
	}

	codeLengths := make([]int, n)

	if len(active) == 0 {
		return make([]huffCode, n), codeLengths
	}
	if len(active) == 1 {
		// Single symbol: code length = 1 but encoder writes 0 bits.
		codeLengths[active[0].sym] = 1
		codes := make([]huffCode, n)
		codes[active[0].sym] = huffCode{code: 0, nbits: 0}
		return codes, codeLengths
	}

	// Sort by frequency ascending, break ties by symbol index.
	sort.Slice(active, func(i, j int) bool {
		if active[i].freq != active[j].freq {
			return active[i].freq < active[j].freq
		}
		return active[i].sym < active[j].sym
	})

	// Package-merge algorithm (Larmore & Hirschberg 1990).
	// Produces optimal length-limited Huffman code lengths.
	numSyms := len(active)

	// Each "package" has a weight (sum of freqs) and a list of leaf symbols it covers.
	type pkg struct {
		weight  int
		symbols []int // leaf symbol indices into active[]
	}

	// Run maxBits rounds of package-merge.
	// prev holds the packages from the previous round.
	var prev []pkg

	for round := 0; round < maxBits; round++ {
		// Start with all leaves as single-symbol packages.
		cur := make([]pkg, numSyms)
		for i, sf := range active {
			cur[i] = pkg{weight: sf.freq, symbols: []int{i}}
		}

		// Merge prev packages pairwise and insert into cur (sorted merge).
		merged := make([]pkg, 0, len(prev)/2)
		for i := 0; i+1 < len(prev); i += 2 {
			syms := make([]int, len(prev[i].symbols)+len(prev[i+1].symbols))
			copy(syms, prev[i].symbols)
			copy(syms[len(prev[i].symbols):], prev[i+1].symbols)
			merged = append(merged, pkg{
				weight:  prev[i].weight + prev[i+1].weight,
				symbols: syms,
			})
		}

		// Merge cur and merged into a single sorted list.
		combined := make([]pkg, 0, len(cur)+len(merged))
		ci, mi := 0, 0
		for ci < len(cur) && mi < len(merged) {
			if cur[ci].weight <= merged[mi].weight {
				combined = append(combined, cur[ci])
				ci++
			} else {
				combined = append(combined, merged[mi])
				mi++
			}
		}
		combined = append(combined, cur[ci:]...)
		combined = append(combined, merged[mi:]...)

		prev = combined
	}

	// Select the 2*(numSyms-1) lightest packages from the last round.
	// Each selected package contributes 1 to the code length of each of its leaf symbols.
	numSelected := 2 * (numSyms - 1)
	if numSelected > len(prev) {
		numSelected = len(prev)
	}

	symLengths := make([]int, numSyms)
	for i := 0; i < numSelected; i++ {
		for _, si := range prev[i].symbols {
			symLengths[si]++
		}
	}

	// Map back to original symbol indices.
	for i, sf := range active {
		l := symLengths[i]
		if l == 0 {
			l = 1 // safety: every used symbol must have a code
		}
		if l > maxBits {
			l = maxBits
		}
		codeLengths[sf.sym] = l
	}

	// Assign canonical codes.
	codes := assignCanonicalCodes(codeLengths)
	return codes, codeLengths
}

// assignCanonicalCodes converts code lengths to canonical Huffman codes.
func assignCanonicalCodes(codeLengths []int) []huffCode {
	maxBits := 0
	for _, l := range codeLengths {
		if l > maxBits {
			maxBits = l
		}
	}

	blCount := make([]int, maxBits+1)
	for _, l := range codeLengths {
		if l > 0 {
			blCount[l]++
		}
	}

	nextCode := make([]int, maxBits+1)
	code := 0
	for bits := 1; bits <= maxBits; bits++ {
		code = (code + blCount[bits-1]) << 1
		nextCode[bits] = code
	}

	codes := make([]huffCode, len(codeLengths))
	for sym, l := range codeLengths {
		if l == 0 {
			continue
		}
		c := nextCode[l]
		nextCode[l]++
		// VP8L Huffman codes are written MSB-first into the LSB-first stream.
		// To achieve this with LSB-first writeBits, we reverse the canonical code bits.
		rev := reverseBits(c, uint(l))
		codes[sym] = huffCode{code: uint32(rev), nbits: uint(l)}
	}
	return codes
}

// writeCodeLengths writes Huffman code lengths to the bitstream.
// Format matches readCodeLengths exactly.
func writeCodeLengths(bw *bitWriter, codeLengths []int) {
	// Count present symbols.
	symbols := []int{}
	for i, l := range codeLengths {
		if l > 0 {
			symbols = append(symbols, i)
		}
	}

	// Simple code is only usable when all symbols fit in 8 bits (0-255).
	// LZ77 length codes are symbols 256-279, which require complex encoding.
	canUseSimple := len(symbols) <= 2
	for _, s := range symbols {
		if s > 255 {
			canUseSimple = false
			break
		}
	}

	if canUseSimple {
		// Simple code: per VP8L spec, bit=1 means simple code.
		bw.writeBit(true)

		if len(symbols) == 0 {
			// 1 symbol (numSymbols-1 = 0), symbol = 0, 1-bit.
			bw.writeBits(0, 1) // numSymbols - 1
			bw.writeBit(false) // not 8-bit
			bw.writeBits(0, 1) // symbol 0
			return
		}

		if len(symbols) == 1 {
			bw.writeBits(0, 1) // numSymbols - 1 = 0
			sym := symbols[0]
			if sym > 1 {
				bw.writeBit(true) // 8-bit symbol
				bw.writeBits(uint32(sym), 8)
			} else {
				bw.writeBit(false) // 1-bit symbol
				bw.writeBits(uint32(sym), 1)
			}
			return
		}

		// 2 symbols, both <= 255.
		bw.writeBits(1, 1) // numSymbols - 1 = 1
		sym0 := symbols[0]
		if sym0 > 1 {
			bw.writeBit(true)
			bw.writeBits(uint32(sym0), 8)
		} else {
			bw.writeBit(false)
			bw.writeBits(uint32(sym0), 1)
		}
		bw.writeBits(uint32(symbols[1]), 8)
		return
	}

	// Complex code: per VP8L spec, bit=0 means normal (complex) code.
	bw.writeBit(false)

	// Run-length encode the code lengths to get symbol sequence.
	// Symbols 0-15: literal length values
	// Symbol 16: repeat previous non-zero length 3-6 times (2 extra bits)
	// Symbol 17: repeat zero 3-10 times (3 extra bits)
	// Symbol 18: repeat zero 11-138 times (7 extra bits)
	type clSym struct {
		sym   int
		extra int // extra bits value
	}
	var clSyms []clSym
	n := len(codeLengths)
	i := 0
	prevLen := 0
	for i < n {
		l := codeLengths[i]
		if l == 0 {
			// Count run of zeros.
			j := i
			for j < n && codeLengths[j] == 0 {
				j++
			}
			run := j - i
			for run > 0 {
				if run >= 11 {
					rep := run
					if rep > 138 {
						rep = 138
					}
					clSyms = append(clSyms, clSym{18, rep - 11})
					run -= rep
				} else if run >= 3 {
					rep := run
					if rep > 10 {
						rep = 10
					}
					clSyms = append(clSyms, clSym{17, rep - 3})
					run -= rep
				} else {
					clSyms = append(clSyms, clSym{0, 0})
					run--
				}
			}
			prevLen = 0
			i = j
		} else {
			// Count run of same non-zero length (only if >= 3 and same as prev).
			if l == prevLen {
				j := i
				for j < n && codeLengths[j] == l && j-i < 6 {
					j++
				}
				run := j - i
				if run >= 3 {
					clSyms = append(clSyms, clSym{16, run - 3})
					i = j
					continue
				}
			}
			clSyms = append(clSyms, clSym{l, 0})
			prevLen = l
			i++
		}
	}

	// Build frequency table for code-length alphabet (symbols 0..18).
	clFreqs := make([]int, codeLengthCodes)
	for _, cs := range clSyms {
		clFreqs[cs.sym]++
	}

	_, clLengths := buildHuffCodes(clFreqs, 7)

	// Ensure all needed symbols have a code (buildHuffCodes may skip zero-freq symbols).
	// Assign canonical codes.
	clCodes := assignCanonicalCodes(clLengths)

	// Write numCLCodes.
	clcOrder := []int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	numCLCodes := codeLengthCodes
	for numCLCodes > 4 && clLengths[clcOrder[numCLCodes-1]] == 0 {
		numCLCodes--
	}
	bw.writeBits(uint32(numCLCodes-4), 4)
	for i := 0; i < numCLCodes; i++ {
		bw.writeBits(uint32(clLengths[clcOrder[i]]), 3)
	}

	// use_length=0: no max_symbol limiter.
	bw.writeBit(false)

	// Write actual code lengths using meta-Huffman codes.
	for _, cs := range clSyms {
		c := clCodes[cs.sym]
		bw.writeBits(c.code, c.nbits)
		switch cs.sym {
		case 16:
			bw.writeBits(uint32(cs.extra), 2)
		case 17:
			bw.writeBits(uint32(cs.extra), 3)
		case 18:
			bw.writeBits(uint32(cs.extra), 7)
		}
	}
}

// huffGroup holds 5 Huffman trees for decoding one group of pixels.
type huffGroup struct {
	trees [5]*huffTree // green, red, blue, alpha, distance
}

// greenAlphabetSize returns the alphabet size for the green channel.
// Per VP8L spec: green = 256 literal values + 24 length codes.
// Distance codes (40) use a separate 5th Huffman tree.
func greenAlphabetSize() int {
	return numLiteralCodes + numLengthCodes
}

// readSymbolFromBits reads a symbol from pre-loaded bits (for testing).
func (t *huffTree) readSymbolFromBits(bits uint64, nbits uint) (int, error) {
	if t.single {
		return t.singleSymbol, nil
	}
	n := 0
	for t.nodes[n].children != -1 {
		if nbits == 0 {
			return -1, errors.New("EOF")
		}
		b := int(bits & 1)
		bits >>= 1; nbits--
		n = t.nodes[n].children + b
	}
	return t.nodes[n].symbol, nil
}
