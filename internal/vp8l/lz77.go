package vp8l

// VP8L LZ77 distance codes and color cache.

const (
	// maxColorCacheSize is the maximum color cache size (2^10 = 1024).
	maxColorCacheSize = 1 << 10
)

// distanceCodeInfo maps a distance code to (prefix, extra bits count, extra bits base).
// VP8L uses 40 distance codes: codes 0-3 are literals 1-4,
// codes 4+ use extra bits.
var distanceExtraBits = [numDistanceCodes]uint{
	0, 0, 0, 0,
	1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6,
	7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12,
	13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18,
}

var distanceBase = [numDistanceCodes]int{
	1, 2, 3, 4,
	5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193,
	257, 385, 513, 769, 1025, 1537, 2049, 3073,
	4097, 6145, 8193, 12289, 16385, 24577, 32769, 49153,
	65537, 98305, 131073, 196609, 262145, 393217, 524289, 786433,
}

// lengthExtraBits[code-256] gives extra bits count for length codes.
var lengthExtraBits = [numLengthCodes]uint{
	0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10,
}

var lengthBase = [numLengthCodes]int{
	1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073,
}

// distanceMapTable is the look-up table for vp8lDistanceToPlane.
// Matches golang.org/x/image/vp8l distanceMapTable exactly.
var distanceMapTable = [120]uint8{
	0x18, 0x07, 0x17, 0x19, 0x28, 0x06, 0x27, 0x29, 0x16, 0x1a,
	0x26, 0x2a, 0x38, 0x05, 0x37, 0x39, 0x15, 0x1b, 0x36, 0x3a,
	0x25, 0x2b, 0x48, 0x04, 0x47, 0x49, 0x14, 0x1c, 0x35, 0x3b,
	0x46, 0x4a, 0x24, 0x2c, 0x58, 0x45, 0x4b, 0x34, 0x3c, 0x03,
	0x57, 0x59, 0x13, 0x1d, 0x56, 0x5a, 0x23, 0x2d, 0x44, 0x4c,
	0x55, 0x5b, 0x33, 0x3d, 0x68, 0x02, 0x67, 0x69, 0x12, 0x1e,
	0x66, 0x6a, 0x22, 0x2e, 0x54, 0x5c, 0x43, 0x4d, 0x65, 0x6b,
	0x32, 0x3e, 0x78, 0x01, 0x77, 0x79, 0x53, 0x5d, 0x11, 0x1f,
	0x64, 0x6c, 0x42, 0x4e, 0x76, 0x7a, 0x21, 0x2f, 0x75, 0x7b,
	0x31, 0x3f, 0x63, 0x6d, 0x52, 0x5e, 0x00, 0x74, 0x7c, 0x41,
	0x4f, 0x10, 0x20, 0x62, 0x6e, 0x30, 0x73, 0x7d, 0x51, 0x5f,
	0x40, 0x72, 0x7e, 0x61, 0x6f, 0x50, 0x71, 0x7f, 0x60, 0x70,
}

// vp8lDistanceToPlane maps a VP8L distance code to a linear pixel offset.
// VP8L spec section 4.2.2: codes 1..120 use the distanceMapTable lookup,
// codes > 120 map to code-120 directly.
func vp8lDistanceToPlane(dist int, width int) int {
	if dist > 120 {
		return dist - 120
	}
	distCode := int(distanceMapTable[dist-1])
	yOffset := distCode >> 4
	xOffset := 8 - distCode&0xf
	if d := yOffset*width + xOffset; d >= 1 {
		return d
	}
	return 1
}

// colorCache is a hash-based palette for recently seen pixels.
type colorCache struct {
	colors []uint32
	bits   uint // cache size = 1 << bits
}

func newColorCache(bits uint) *colorCache {
	return &colorCache{
		colors: make([]uint32, 1<<bits),
		bits:   bits,
	}
}

func (cc *colorCache) insert(color uint32) {
	hash := colorHash(color, cc.bits)
	cc.colors[hash] = color
}

func (cc *colorCache) lookup(idx int) uint32 {
	return cc.colors[idx]
}

// colorHash computes the hash index for a color.
func colorHash(color uint32, bits uint) uint32 {
	return (color * 0x1e35a7bd) >> (32 - bits)
}

// getDistanceCode encodes a linear distance as a VP8L distance code + extra bits.
func getDistanceCode(dist int) (code int, extraBits uint, extra int) {
	for i := numDistanceCodes - 1; i >= 0; i-- {
		if dist >= distanceBase[i] {
			extra = dist - distanceBase[i]
			return i, distanceExtraBits[i], extra
		}
	}
	return 0, 0, dist - 1
}

// getLengthCode encodes a match length as a VP8L length code + extra bits.
func getLengthCode(length int) (code int, extraBits uint, extra int) {
	for i := numLengthCodes - 1; i >= 0; i-- {
		if length >= lengthBase[i] {
			extra = length - lengthBase[i]
			return 256 + i, lengthExtraBits[i], extra
		}
	}
	return 256, 0, 0
}

// readLength decodes a match length from green symbol + bit reader.
func readLength(br *bitReader, greenSym int) (int, error) {
	idx := greenSym - 256
	if idx < 0 || idx >= numLengthCodes {
		return 0, nil
	}
	extra, err := br.readBits(lengthExtraBits[idx])
	if err != nil {
		return 0, err
	}
	return lengthBase[idx] + int(extra), nil
}

// readDistance decodes a match distance from distance symbol + bit reader.
func readDistance(br *bitReader, distTree *huffTree, width int) (int, error) {
	sym, err := distTree.readSymbol(br)
	if err != nil {
		return 0, err
	}
	if sym >= numDistanceCodes {
		return 0, nil
	}
	extra, err := br.readBits(distanceExtraBits[sym])
	if err != nil {
		return 0, err
	}
	dist := distanceBase[sym] + int(extra)
	return vp8lDistanceToPlane(dist, width), nil
}

// linearToPlaneDistance converts a linear pixel distance (as used in the decoded
// pixel array) to a VP8L "plane distance" value expected by getDistanceCode.
// The VP8L spec encodes distance as a plane-distance code; the decoder then
// converts it back via vp8lDistanceToPlane. We must invert that mapping.
//
// For plane values 1..120, vp8lDistanceToPlane does a table lookup.
// For plane values >120, vp8lDistanceToPlane returns planeVal-120.
//
// We try every entry of distanceMapTable first (O(120)), then fall back to
// the linear region.
func linearToPlaneDistance(linearDist, width int) int {
	if linearDist <= 0 {
		return 1
	}
	// Try the table entries (plane values 1..120).
	for i := 0; i < 120; i++ {
		planeVal := i + 1
		distCode := int(distanceMapTable[i])
		yOff := distCode >> 4
		xOff := 8 - (distCode & 0xf)
		d := yOff*width + xOff
		if d < 1 {
			d = 1
		}
		if d == linearDist {
			return planeVal
		}
	}
	// Fall back: for planeVal > 120, vp8lDistanceToPlane returns planeVal-120.
	return linearDist + 120
}

// --- LZ77 encoder ---

const (
	hashBits    = 18
	hashSize    = 1 << hashBits
	hashMult    = 0x1e35a7bd
	minMatchLen = 2
	// maxMatchLen is the maximum match length representable by VP8L length prefix codes.
	// lengthBase[23]=3073, lengthExtraBits[23]=10 → max = 3073 + (1<<10 - 1) = 4096.
	maxMatchLen = 4096
	maxDist     = 32768 * 8
)

type lz77Hash struct {
	head [hashSize]int32 // head[hash] = most recent position with that hash
	prev [maxDist]int32  // prev[pos % maxDist] = previous position with same hash
}

func newLZ77Hash() *lz77Hash {
	h := &lz77Hash{}
	for i := range h.head {
		h.head[i] = -1
	}
	return h
}

func lz77Hash32(v uint32) uint32 {
	return (v * hashMult) >> (32 - hashBits)
}

// findMatch finds the best LZ77 match for pixels[pos:].
func findMatch(pixels []uint32, pos int, h *lz77Hash) (length int, dist int) {
	if pos+minMatchLen > len(pixels) {
		return 0, 0
	}

	v := pixels[pos]
	key := lz77Hash32(v)

	bestLen := 0
	bestDist := 0

	cur := h.head[key]
	limit := 8 // limit search depth
	for cur >= 0 && limit > 0 {
		limit--
		d := pos - int(cur)
		if d > maxDist {
			break
		}
		// Compute match length.
		mlen := 0
		for pos+mlen < len(pixels) && mlen < maxMatchLen {
			if pixels[pos+mlen] != pixels[int(cur)+mlen] {
				break
			}
			mlen++
		}
		if mlen >= minMatchLen && mlen > bestLen {
			bestLen = mlen
			bestDist = d
		}
		prev := h.prev[int(cur)%maxDist]
		cur = prev
	}

	// Update hash table.
	h.prev[pos%maxDist] = h.head[key]
	h.head[key] = int32(pos)

	return bestLen, bestDist
}
