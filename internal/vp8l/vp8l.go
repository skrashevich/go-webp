// Package vp8l implements the VP8L lossless WebP codec.
package vp8l

import (
	"errors"
	"image"
	"image/color"
)

// ErrNotImplemented is kept for API compatibility.
var ErrNotImplemented = errors.New("vp8l: not implemented")

// Signature byte that starts every VP8L bitstream.
const Signature = 0x2F

// Decoder decodes a VP8L bitstream into an image.
type Decoder struct {
	data []byte
}

// NewDecoder creates a new VP8L Decoder for the given bitstream data.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data}
}

// Decode decodes the VP8L bitstream and returns the decoded image.
func (d *Decoder) Decode() (image.Image, error) {
	return DecodeVP8L(d.data)
}

// DecodeConfig returns the image dimensions without fully decoding.
func (d *Decoder) DecodeConfig() (image.Config, error) {
	if len(d.data) < 5 {
		return image.Config{}, errors.New("vp8l: data too short")
	}
	if d.data[0] != vp8lSignature {
		return image.Config{}, errors.New("vp8l: invalid signature")
	}
	br := newBitReader(d.data[1:])
	header, err := readImageHeader(br)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: color.NRGBAModel,
		Width:      header.width,
		Height:     header.height,
	}, nil
}

// Encoder encodes an image into a VP8L bitstream.
type Encoder struct{}

// NewEncoder creates a new VP8L Encoder.
func NewEncoder() *Encoder {
	return &Encoder{}
}

// Encode encodes img into a VP8L bitstream and returns the bytes.
func (e *Encoder) Encode(img image.Image) ([]byte, error) {
	return EncodeVP8L(img)
}
