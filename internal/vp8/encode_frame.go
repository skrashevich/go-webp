package vp8

import (
	"encoding/binary"
	"io"
)

// encodeFrameHeader holds the uncompressed data chunk fields for a VP8 key frame.
type encodeFrameHeader struct {
	width           int
	height          int
	horizontalScale int
	verticalScale   int
}

// writeFrameHeader writes the 3-byte VP8 frame tag + 7-byte key-frame start code.
// partSize is the size of the first (and only, for our encoder) partition in bytes.
func writeFrameHeader(w io.Writer, partSize int, hdr encodeFrameHeader) error {
	// Frame tag: 3 bytes.
	// Bit 0: key frame (0 = key frame)
	// Bits 1-3: version (0)
	// Bit 4: show_frame (1)
	// Bits 5-18: first_part_size
	tag := uint32(0) | // key frame
		(0 << 1) | // version 0
		(1 << 4) | // show_frame
		(uint32(partSize) << 5)
	var tagBuf [3]byte
	tagBuf[0] = byte(tag)
	tagBuf[1] = byte(tag >> 8)
	tagBuf[2] = byte(tag >> 16)
	if _, err := w.Write(tagBuf[:]); err != nil {
		return err
	}

	// Start code: 0x9D 0x01 0x2A
	startCode := [3]byte{0x9D, 0x01, 0x2A}
	if _, err := w.Write(startCode[:]); err != nil {
		return err
	}

	// Width and height, each 14 bits + 2-bit scale.
	var dim [4]byte
	binary.LittleEndian.PutUint16(dim[0:2], uint16(hdr.width|(hdr.horizontalScale<<14)))
	binary.LittleEndian.PutUint16(dim[2:4], uint16(hdr.height|(hdr.verticalScale<<14)))
	if _, err := w.Write(dim[:]); err != nil {
		return err
	}

	return nil
}

// writeSegmentHeader writes the segment_header() syntax using the bool encoder.
// We use a single segment (no segmentation), so update_mb_segmentation_map = 0.
func writeSegmentHeader(enc *boolEncoder) {
	enc.writeBool(128, false) // update_segmentation = 0
}

// writeFilterHeader writes the loop_filter_adj_enable and related fields.
// We disable the loop filter for simplicity.
func writeFilterHeader(enc *boolEncoder) {
	// loop_filter_type = 0 (normal), loop_filter_level = 0, sharpness = 0
	enc.writeLiteral(1, 0)  // filter_type
	enc.writeLiteral(6, 0)  // loop_filter_level
	enc.writeLiteral(3, 0)  // sharpness_level
	enc.writeBool(128, false) // loop_filter_adj_enable = 0
}

// writeQuantHeader writes the quantisation indices.
func writeQuantHeader(enc *boolEncoder, qp int) {
	enc.writeLiteral(7, uint32(qp)) // y_ac_qi
	enc.writeBool(128, false)        // y_dc_delta_present = 0
	enc.writeBool(128, false)        // y2_dc_delta_present = 0
	enc.writeBool(128, false)        // y2_ac_delta_present = 0
	enc.writeBool(128, false)        // uv_dc_delta_present = 0
	enc.writeBool(128, false)        // uv_ac_delta_present = 0
}

// writePartitionCount writes mb_no_coeff_skip and the number of token partitions.
func writePartitionCount(enc *boolEncoder) {
	enc.writeBool(128, false) // mb_no_coeff_skip = 0
	enc.writeLiteral(2, 0)   // token_partition_count = 0 (1 partition)
}
