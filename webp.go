// Package webp implements encoding and decoding of WebP images.
//
// WebP is an image format developed by Google that provides both lossy (VP8)
// and lossless (VP8L) compression.
//
// To decode a WebP image:
//
//	img, err := webp.Decode(r)
//
// To encode an image as WebP:
//
//	err := webp.Encode(w, img, &webp.Options{Lossy: false})
//
// The package registers itself with the standard image package so that
// image.Decode can decode WebP files automatically.
package webp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"

	"github.com/skrashevich/go-webp/internal/riff"
	"github.com/skrashevich/go-webp/internal/vp8"
	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Options controls WebP encoding behaviour.
type Options struct {
	// Lossy selects VP8 lossy encoding when true, VP8L lossless when false.
	Lossy bool
	// Quality is the encoding quality for lossy mode, in the range [0, 100].
	// Higher values produce better quality at the cost of larger files.
	// Ignored in lossless mode.
	Quality float32
}

func init() {
	// Register the WebP format with the standard image package.
	// The magic string matches "RIFF????WEBP" where ???? is any 4 bytes.
	image.RegisterFormat("webp", "RIFF????WEBP", Decode, DecodeConfig)
}

// Decode reads a WebP image from r and returns it as an image.Image.
func Decode(r io.Reader) (image.Image, error) {
	chunkType, data, err := readWebP(r)
	if err != nil {
		return nil, err
	}
	switch chunkType {
	case riff.ChunkVP8:
		dec := vp8.NewDecoder(data)
		return dec.Decode()
	case riff.ChunkVP8L:
		dec := vp8l.NewDecoder(data)
		return dec.Decode()
	case riff.ChunkVP8X:
		return nil, errors.New("webp: extended format (VP8X) not yet supported")
	default:
		return nil, fmt.Errorf("webp: unknown chunk type")
	}
}

// DecodeConfig returns the color model and dimensions of a WebP image without
// decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	chunkType, data, err := readWebP(r)
	if err != nil {
		return image.Config{}, err
	}
	switch chunkType {
	case riff.ChunkVP8:
		dec := vp8.NewDecoder(data)
		return dec.DecodeConfig()
	case riff.ChunkVP8L:
		dec := vp8l.NewDecoder(data)
		return dec.DecodeConfig()
	case riff.ChunkVP8X:
		return image.Config{}, errors.New("webp: extended format (VP8X) not yet supported")
	default:
		return image.Config{}, fmt.Errorf("webp: unknown chunk type")
	}
}

// Encode writes img to w in WebP format according to opts.
// If opts is nil, lossless encoding is used.
func Encode(w io.Writer, img image.Image, opts *Options) error {
	if opts == nil {
		opts = &Options{Lossy: false}
	}

	var (
		chunkID riff.FourCC
		payload []byte
		err     error
	)

	if opts.Lossy {
		enc := vp8.NewEncoder(opts.Quality)
		payload, err = enc.Encode(img)
		if err != nil {
			return fmt.Errorf("webp: VP8 encode: %w", err)
		}
		chunkID = riff.FourCCVP8
	} else {
		enc := vp8l.NewEncoder()
		payload, err = enc.Encode(img)
		if err != nil {
			return fmt.Errorf("webp: VP8L encode: %w", err)
		}
		chunkID = riff.FourCCVP8L
	}

	// RIFF file size = 4 ("WEBP") + chunk header (8) + chunk data (padded).
	chunkOnDisk := riff.ChunkSize(len(payload))
	fileSize := uint32(4 + chunkOnDisk)

	var buf bytes.Buffer
	if err := riff.WriteHeader(&buf, fileSize); err != nil {
		return err
	}
	if err := riff.WriteChunk(&buf, chunkID, payload); err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	return err
}

// readWebP reads the RIFF/WEBP header and the first image chunk from r.
// Returns the chunk type and raw chunk data.
func readWebP(r io.Reader) (riff.ChunkType, []byte, error) {
	if _, err := riff.ReadHeader(r); err != nil {
		return 0, nil, err
	}

	chunk, err := riff.ReadChunk(r)
	if err != nil {
		return 0, nil, fmt.Errorf("webp: reading first chunk: %w", err)
	}

	switch chunk.ID {
	case riff.FourCCVP8:
		return riff.ChunkVP8, chunk.Data, nil
	case riff.FourCCVP8L:
		return riff.ChunkVP8L, chunk.Data, nil
	case riff.FourCCVP8X:
		return riff.ChunkVP8X, chunk.Data, nil
	default:
		return 0, nil, fmt.Errorf("webp: unexpected first chunk: %s", chunk.ID)
	}
}
