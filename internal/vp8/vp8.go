// Package vp8 provides VP8 lossy codec interfaces and stubs.
// The actual encoding/decoding is implemented separately.
package vp8

import (
	"errors"
	"image"
)

// ErrNotImplemented is returned when VP8 codec is not yet implemented.
var ErrNotImplemented = errors.New("vp8: not implemented")

// Decoder decodes a VP8 bitstream into an image.
type Decoder struct {
	data []byte
}

// NewDecoder creates a new VP8 Decoder for the given bitstream data.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data}
}

// Decode decodes the VP8 bitstream and returns the decoded image.
func (d *Decoder) Decode() (image.Image, error) {
	return Decode(d.data)
}

// DecodeConfig returns the image dimensions without fully decoding.
func (d *Decoder) DecodeConfig() (image.Config, error) {
	return DecodeConfig(d.data)
}

// Encoder encodes an image into a VP8 bitstream.
type Encoder struct {
	quality float32
}

// NewEncoder creates a new VP8 Encoder with the given quality (0-100).
func NewEncoder(quality float32) *Encoder {
	return &Encoder{quality: quality}
}

// Encode encodes img into a VP8 bitstream and returns the bytes.
func (e *Encoder) Encode(img image.Image) ([]byte, error) {
	return EncodeVP8(img, e.quality)
}
