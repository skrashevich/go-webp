package vp8

import (
	"errors"
	"fmt"
)

// frameHeader holds the parsed VP8 frame header (RFC 6386 §9.1).
type frameHeader struct {
	keyFrame      bool
	version       uint8
	showFrame     bool
	firstPartSize uint32 // size of first partition (compressed header)
	width         uint16
	height        uint16
	hScale        uint8
	vScale        uint8
	colorSpace    uint8
	clampType     uint8
}

// parseFrameHeader parses the uncompressed data chunk at the start of a VP8 frame.
// Returns the header and the offset of the first partition start.
func parseFrameHeader(data []byte) (*frameHeader, int, error) {
	if len(data) < 3 {
		return nil, 0, errors.New("vp8: frame too short")
	}

	// First 3 bytes: frame tag.
	tag := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16

	h := &frameHeader{}
	h.keyFrame = (tag & 1) == 0
	h.version = uint8((tag >> 1) & 7)
	h.showFrame = (tag>>4)&1 == 1
	h.firstPartSize = tag >> 5

	offset := 3

	if h.keyFrame {
		// Key frame: 3 bytes start code + 4 bytes width/height.
		if len(data) < offset+7 {
			return nil, 0, errors.New("vp8: keyframe header too short")
		}
		// Start code: 0x9D 0x01 0x2A
		if data[offset] != 0x9D || data[offset+1] != 0x01 || data[offset+2] != 0x2A {
			return nil, 0, fmt.Errorf("vp8: invalid keyframe start code: %02x %02x %02x",
				data[offset], data[offset+1], data[offset+2])
		}
		offset += 3

		hw := uint16(data[offset]) | uint16(data[offset+1])<<8
		h.width = hw & 0x3FFF
		h.hScale = uint8(hw >> 14)
		offset += 2

		hh := uint16(data[offset]) | uint16(data[offset+1])<<8
		h.height = hh & 0x3FFF
		h.vScale = uint8(hh >> 14)
		offset += 2
	}

	return h, offset, nil
}

// segmentHeader holds VP8 segmentation information (RFC 6386 §9.3).
type segmentHeader struct {
	enabled         bool
	updateMap       bool
	updateData      bool
	absoluteValues  bool
	quantDelta      [4]int8
	filterDelta     [4]int8
	// segment probabilities (for MB map decoding)
	prob [3]uint8
}

// loopFilterHeader holds loop filter parameters (RFC 6386 §9.4).
type loopFilterHeader struct {
	filterType int // 0=normal, 1=simple
	level      int
	sharpness  int
	deltasEnabled bool
	refDelta   [4]int8
	modeDelta  [4]int8
}

// quantHeader holds per-segment quantizer indices (RFC 6386 §9.6).
type quantHeader struct {
	yACQI     int // base quantizer index for Y AC
	yDCDelta  int
	y2DCDelta int
	y2ACDelta int
	uvDCDelta int
	uvACDelta int
}

// parsedHeader holds all header fields decoded from the first partition.
type parsedHeader struct {
	frame       *frameHeader
	segment     segmentHeader
	filter      loopFilterHeader
	quant       quantHeader
	numParts    int  // number of DCT partitions (1, 2, 4, or 8)
	mbWidth     int  // macroblock columns
	mbHeight    int  // macroblock rows
	useSkipProb bool
	skipProb    uint8
}

// parseCompressedHeader decodes the first partition (compressed header) using
// the bool decoder. Returns a parsedHeader and the per-partition DCT offsets.
func parseCompressedHeader(bd *boolDecoder, fh *frameHeader, coeffProbs *[4][8][3][11]uint8) (*parsedHeader, error) {
	ph := &parsedHeader{frame: fh}
	ph.mbWidth = (int(fh.width) + 15) / 16
	ph.mbHeight = (int(fh.height) + 15) / 16

	if fh.keyFrame {
		// color_space and clamping_type (RFC 6386 §9.2)
		fh.colorSpace = uint8(bd.ReadLiteral(1))
		fh.clampType = uint8(bd.ReadLiteral(1))
	}

	// Segmentation header (§9.3)
	if err := parseSegmentHeader(bd, &ph.segment); err != nil {
		return nil, err
	}

	// Loop filter header (§9.4)
	if err := parseLoopFilterHeader(bd, &ph.filter); err != nil {
		return nil, err
	}

	// Number of DCT partitions (§9.5)
	log2Parts := bd.ReadLiteral(2)
	ph.numParts = 1 << log2Parts

	// Quantization indices (§9.6)
	parseQuantHeader(bd, &ph.quant)

	if fh.keyFrame {
		// refresh_last_frame_buffer / refresh_entropy_probs (1 bit, §9.8/§9.9)
		_ = bd.ReadBool(128)
	}

	// Token probability updates (§13.4)
	parseTokenProbUpdates(bd, coeffProbs)

	// mb_no_coeff_skip (§9.11): if true, a per-MB skip flag is coded.
	ph.useSkipProb = bd.ReadBool(128)
	if ph.useSkipProb {
		ph.skipProb = uint8(bd.ReadLiteral(8))
	}

	return ph, nil
}

func parseSegmentHeader(bd *boolDecoder, seg *segmentHeader) error {
	seg.enabled = bd.ReadFlag()
	if !seg.enabled {
		return nil
	}
	seg.updateMap = bd.ReadFlag()
	seg.updateData = bd.ReadFlag()
	if seg.updateData {
		seg.absoluteValues = bd.ReadFlag()
		for i := 0; i < 4; i++ {
			if bd.ReadFlag() {
				seg.quantDelta[i] = int8(bd.ReadSignedLiteral(7))
			}
		}
		for i := 0; i < 4; i++ {
			if bd.ReadFlag() {
				seg.filterDelta[i] = int8(bd.ReadSignedLiteral(6))
			}
		}
	}
	if seg.updateMap {
		for i := 0; i < 3; i++ {
			if bd.ReadFlag() {
				seg.prob[i] = uint8(bd.ReadLiteral(8))
			} else {
				seg.prob[i] = 255
			}
		}
	}
	return nil
}

func parseLoopFilterHeader(bd *boolDecoder, lf *loopFilterHeader) error {
	lf.filterType = int(bd.ReadLiteral(1))
	lf.level = int(bd.ReadLiteral(6))
	lf.sharpness = int(bd.ReadLiteral(3))
	lf.deltasEnabled = bd.ReadFlag()
	if lf.deltasEnabled {
		if bd.ReadFlag() { // update_mb_segmentation_data
			for i := 0; i < 4; i++ {
				if bd.ReadFlag() {
					lf.refDelta[i] = int8(bd.ReadSignedLiteral(6))
				}
			}
			for i := 0; i < 4; i++ {
				if bd.ReadFlag() {
					lf.modeDelta[i] = int8(bd.ReadSignedLiteral(6))
				}
			}
		}
	}
	return nil
}

func parseQuantHeader(bd *boolDecoder, q *quantHeader) {
	q.yACQI = int(bd.ReadLiteral(7))
	readQuantDelta := func() int {
		if bd.ReadFlag() {
			return int(bd.ReadSignedLiteral(4))
		}
		return 0
	}
	q.yDCDelta = readQuantDelta()
	q.y2DCDelta = readQuantDelta()
	q.y2ACDelta = readQuantDelta()
	q.uvDCDelta = readQuantDelta()
	q.uvACDelta = readQuantDelta()
}

// parseTokenProbUpdates reads and applies token probability updates.
// RFC 6386 §13.4: 4 planes × 8 bands × 3 contexts × 11 probabilities.
func parseTokenProbUpdates(bd *boolDecoder, probs *[4][8][3][11]uint8) {
	for i := 0; i < 4; i++ {
		for j := 0; j < 8; j++ {
			for k := 0; k < 3; k++ {
				for l := 0; l < 11; l++ {
					if bd.ReadBool(coefUpdateProbs[i][j][k][l]) {
						probs[i][j][k][l] = uint8(bd.ReadLiteral(8))
					}
				}
			}
		}
	}
}
