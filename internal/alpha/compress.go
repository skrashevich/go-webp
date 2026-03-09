package alpha

import (
	"errors"
	"image"
	"image/color"

	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Compress compresses an alpha plane using the specified method.
// method=0: no compression (raw bytes returned as-is).
// method=1: VP8L lossless compression via green channel (signature byte stripped).
func Compress(alpha []byte, width, height int, method int) ([]byte, error) {
	switch method {
	case 0:
		out := make([]byte, len(alpha))
		copy(out, alpha)
		return out, nil
	case 1:
		// Build a grayscale NRGBA image where green = alpha value, others = 0.
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				v := alpha[y*width+x]
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: v, B: 0, A: 0xff})
			}
		}
		enc := vp8l.NewEncoder()
		bs, err := enc.Encode(img)
		if err != nil {
			return nil, err
		}
		// Strip the VP8L signature byte (0x2f) — ALPH stream omits it.
		if len(bs) == 0 {
			return nil, errors.New("alpha: vp8l encoder returned empty bitstream")
		}
		if bs[0] != vp8l.Signature {
			return nil, errors.New("alpha: vp8l encoder returned unexpected signature")
		}
		return bs[1:], nil
	default:
		return nil, errors.New("alpha: unsupported compression method")
	}
}

// Decompress decompresses an alpha plane from the specified method.
// method=0: no compression (raw bytes returned as-is).
// method=1: VP8L lossless decompression, green channel extracted.
func Decompress(data []byte, width, height int, method int) ([]byte, error) {
	switch method {
	case 0:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	case 1:
		// Prepend VP8L signature byte before decoding.
		bs := make([]byte, 1+len(data))
		bs[0] = vp8l.Signature
		copy(bs[1:], data)
		dec := vp8l.NewDecoder(bs)
		img, err := dec.Decode()
		if err != nil {
			return nil, err
		}
		nrgba, ok := img.(*image.NRGBA)
		if !ok {
			return nil, errors.New("alpha: vp8l decoder returned non-NRGBA image")
		}
		bounds := nrgba.Bounds()
		w := bounds.Max.X - bounds.Min.X
		h := bounds.Max.Y - bounds.Min.Y
		if w != width || h != height {
			return nil, errors.New("alpha: decoded image dimensions mismatch")
		}
		alpha := make([]byte, width*height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := nrgba.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				alpha[y*width+x] = nrgba.Pix[off+1] // green channel
			}
		}
		return alpha, nil
	default:
		return nil, errors.New("alpha: unsupported compression method")
	}
}
