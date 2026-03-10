// Package webp implements encoding and decoding of WebP images.
//
// WebP is an image format developed by Google that provides both lossy (VP8)
// and lossless (VP8L) compression, including the VP8X extended format with
// alpha channel support and animation.
//
// To decode a WebP image:
//
//	img, err := webp.Decode(r)
//
// To encode an image as WebP:
//
//	err := webp.Encode(w, img, &webp.Options{Lossy: false})
//
// To encode an animated WebP:
//
//	err := webp.EncodeAnimation(w, anim, &anim.AnimationOptions{})
//
// The package registers itself with the standard image package so that
// image.Decode can decode WebP files automatically.
package webp

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/skrashevich/go-webp/internal/alpha"
	"github.com/skrashevich/go-webp/internal/anim"
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
// For animated WebP files, use DecodeAnimation instead.
func Decode(r io.Reader) (image.Image, error) {
	chunkType, data, rest, err := readWebP(r)
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
		return decodeVP8X(data, rest)
	default:
		return nil, fmt.Errorf("webp: unknown chunk type")
	}
}

// DecodeConfig returns the color model and dimensions of a WebP image without
// decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	chunkType, data, rest, err := readWebP(r)
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
		return decodeConfigVP8X(data, rest)
	default:
		return image.Config{}, fmt.Errorf("webp: unknown chunk type")
	}
}

// DecodeAnimation reads an animated WebP from r and returns the animation.
// For non-animated WebP files, use Decode instead.
func DecodeAnimation(r io.Reader) (*anim.Animation, error) {
	chunkType, data, rest, err := readWebP(r)
	if err != nil {
		return nil, err
	}
	if chunkType != riff.ChunkVP8X {
		return nil, fmt.Errorf("webp: not an animated WebP (chunk type: %d)", chunkType)
	}
	vp8x, err := riff.ParseVP8X(data)
	if err != nil {
		return nil, fmt.Errorf("webp: %w", err)
	}
	if vp8x.Flags&riff.VP8XFlagAnimation == 0 {
		return nil, fmt.Errorf("webp: VP8X file is not animated")
	}
	info := anim.VP8XInfo{
		Flags:  uint32(vp8x.Flags),
		Width:  vp8x.Width,
		Height: vp8x.Height,
	}
	return anim.Decode(rest, info)
}

// EncodeAnimation writes an animated WebP to w.
func EncodeAnimation(w io.Writer, a *anim.Animation, opts *anim.AnimationOptions) error {
	return anim.Encode(w, a, opts)
}

// Encode writes img to w in WebP format according to opts.
// If opts is nil, lossless encoding is used.
// When Lossy is true and the image has an alpha channel, the VP8X extended
// format is used with a separate ALPH chunk.
func Encode(w io.Writer, img image.Image, opts *Options) error {
	if opts == nil {
		opts = &Options{Lossy: false}
	}

	if opts.Lossy && imageHasAlpha(img) {
		return encodeLossyWithAlpha(w, img, opts.Quality)
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

// readWebP reads the RIFF/WEBP header and the first chunk from r.
// Returns the chunk type, the first chunk's raw data, and the remaining reader
// (needed for VP8X which has additional chunks after the VP8X header).
func readWebP(r io.Reader) (riff.ChunkType, []byte, io.Reader, error) {
	if _, err := riff.ReadHeader(r); err != nil {
		return 0, nil, nil, err
	}

	chunk, err := riff.ReadChunk(r)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("webp: reading first chunk: %w", err)
	}

	switch chunk.ID {
	case riff.FourCCVP8:
		return riff.ChunkVP8, chunk.Data, nil, nil
	case riff.FourCCVP8L:
		return riff.ChunkVP8L, chunk.Data, nil, nil
	case riff.FourCCVP8X:
		return riff.ChunkVP8X, chunk.Data, r, nil
	default:
		return 0, nil, nil, fmt.Errorf("webp: unexpected first chunk: %s", chunk.ID)
	}
}

// decodeVP8X handles decoding of VP8X extended format images.
func decodeVP8X(vp8xData []byte, r io.Reader) (image.Image, error) {
	vp8x, err := riff.ParseVP8X(vp8xData)
	if err != nil {
		return nil, fmt.Errorf("webp: %w", err)
	}

	if vp8x.Flags&riff.VP8XFlagAnimation != 0 {
		// For animated WebP, return the first composed frame.
		info := anim.VP8XInfo{
			Flags:  uint32(vp8x.Flags),
			Width:  vp8x.Width,
			Height: vp8x.Height,
		}
		a, err := anim.Decode(r, info)
		if err != nil {
			return nil, fmt.Errorf("webp: animation: %w", err)
		}
		if len(a.Frames) == 0 {
			return nil, fmt.Errorf("webp: animated WebP has no frames")
		}
		composed := anim.Compose(a)
		return composed[0], nil
	}

	// Read subsequent chunks to find ALPH (optional) and VP8/VP8L.
	chunks, err := riff.ReadAllChunks(r)
	if err != nil {
		return nil, fmt.Errorf("webp: reading VP8X chunks: %w", err)
	}

	var alphData []byte
	for _, c := range chunks {
		if c.ID == riff.FourCCALPH {
			alphData = c.Data
		}
	}

	for _, c := range chunks {
		switch c.ID {
		case riff.FourCCVP8:
			dec := vp8.NewDecoder(c.Data)
			img, err := dec.Decode()
			if err != nil {
				return nil, err
			}
			if alphData != nil {
				return applyAlphaToVP8(img, alphData)
			}
			return img, nil
		case riff.FourCCVP8L:
			dec := vp8l.NewDecoder(c.Data)
			return dec.Decode()
		}
	}
	return nil, fmt.Errorf("webp: VP8X: no image bitstream found")
}

// applyAlphaToVP8 decodes the ALPH chunk and applies it to a VP8 image.
func applyAlphaToVP8(img image.Image, alphData []byte) (image.Image, error) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	alphaPlane, err := alpha.DecodeALPH(alphData, w, h)
	if err != nil {
		return nil, fmt.Errorf("webp: decoding alpha: %w", err)
	}
	// Try to get YCbCr for efficient conversion.
	if ycbcr, ok := img.(*image.YCbCr); ok {
		return alpha.ApplyAlpha(ycbcr, alphaPlane), nil
	}
	// Fallback: convert any image to NRGBA with alpha.
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			a := alphaPlane[y*w+x]
			out.SetNRGBA(x, y, color.NRGBA{
				R: byte(r >> 8),
				G: byte(g >> 8),
				B: byte(b >> 8),
				A: a,
			})
		}
	}
	return out, nil
}

// decodeConfigVP8X returns the image config for a VP8X extended format image.
func decodeConfigVP8X(vp8xData []byte, r io.Reader) (image.Config, error) {
	vp8x, err := riff.ParseVP8X(vp8xData)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: %w", err)
	}

	if vp8x.Flags&riff.VP8XFlagAnimation != 0 {
		return image.Config{
			ColorModel: color.NRGBAModel,
			Width:      int(vp8x.Width) + 1,
			Height:     int(vp8x.Height) + 1,
		}, nil
	}

	// For VP8X with alpha, return NRGBA model.
	if vp8x.Flags&riff.VP8XFlagAlpha != 0 {
		return image.Config{
			ColorModel: color.NRGBAModel,
			Width:      int(vp8x.Width) + 1,
			Height:     int(vp8x.Height) + 1,
		}, nil
	}

	// Read subsequent chunks to find VP8/VP8L.
	chunks, err := riff.ReadAllChunks(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: reading VP8X chunks: %w", err)
	}

	for _, c := range chunks {
		switch c.ID {
		case riff.FourCCVP8:
			dec := vp8.NewDecoder(c.Data)
			return dec.DecodeConfig()
		case riff.FourCCVP8L:
			dec := vp8l.NewDecoder(c.Data)
			return dec.DecodeConfig()
		}
	}
	return image.Config{}, fmt.Errorf("webp: VP8X: no image bitstream found")
}

// encodeLossyWithAlpha encodes a lossy image with alpha using VP8X + ALPH + VP8.
func encodeLossyWithAlpha(w io.Writer, img image.Image, quality float32) error {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	// Extract and encode alpha channel.
	alphaPlane := alpha.ExtractAlpha(img)
	alphChunk, err := alpha.EncodeALPH(alphaPlane, imgW, imgH, 1, alpha.FilterNone)
	if err != nil {
		return fmt.Errorf("webp: encoding alpha: %w", err)
	}

	// Encode VP8 bitstream (without alpha).
	enc := vp8.NewEncoder(quality)
	vp8Payload, err := enc.Encode(img)
	if err != nil {
		return fmt.Errorf("webp: VP8 encode: %w", err)
	}

	// Build body: VP8X + ALPH + VP8
	var body bytes.Buffer
	if err := riff.WriteVP8X(&body, riff.VP8XFlagAlpha, imgW, imgH); err != nil {
		return err
	}
	if err := riff.WriteChunk(&body, riff.FourCCALPH, alphChunk); err != nil {
		return err
	}
	if err := riff.WriteChunk(&body, riff.FourCCVP8, vp8Payload); err != nil {
		return err
	}

	fileSize := uint32(4 + body.Len())
	var out bytes.Buffer
	if err := riff.WriteHeader(&out, fileSize); err != nil {
		return err
	}
	if _, err := out.Write(body.Bytes()); err != nil {
		return err
	}
	_, err = w.Write(out.Bytes())
	return err
}

// imageHasAlpha reports whether the image has any non-opaque pixels.
func imageHasAlpha(img image.Image) bool {
	if nrgba, ok := img.(*image.NRGBA); ok {
		for i := 3; i < len(nrgba.Pix); i += 4 {
			if nrgba.Pix[i] != 0xff {
				return true
			}
		}
		return false
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}
