// Package alpha implements alpha channel processing for WebP ALPH chunks.
package alpha

import (
	"image"
	"image/color"
)

// FilterType for alpha channel predictive filtering.
type FilterType int

const (
	FilterNone       FilterType = 0
	FilterHorizontal FilterType = 1
	FilterVertical   FilterType = 2
	FilterGradient   FilterType = 3
)

// GradientPredictor computes clamped gradient prediction: clamp(a+b-c, 0, 255).
// a = left, b = top, c = top-left
func GradientPredictor(a, b, c byte) byte {
	g := int(a) + int(b) - int(c)
	if g < 0 {
		return 0
	}
	if g > 255 {
		return 255
	}
	return byte(g)
}

// ApplyFilter applies a predictive filter to raw alpha data and returns the filtered data.
// Row 0 always uses no prediction. Column 0 uses no prediction for horizontal/gradient.
func ApplyFilter(alpha []byte, width, height int, filter FilterType) []byte {
	out := make([]byte, len(alpha))
	if filter == FilterNone {
		copy(out, alpha)
		return out
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			actual := alpha[i]
			switch filter {
			case FilterHorizontal:
				if y == 0 || x == 0 {
					out[i] = actual
				} else {
					out[i] = byte((int(actual) - int(alpha[i-1])) & 0xff)
				}
			case FilterVertical:
				if y == 0 {
					out[i] = actual
				} else {
					out[i] = byte((int(actual) - int(alpha[i-width])) & 0xff)
				}
			case FilterGradient:
				if y == 0 || x == 0 {
					out[i] = actual
				} else {
					left := alpha[i-1]
					top := alpha[i-width]
					topLeft := alpha[i-width-1]
					pred := GradientPredictor(left, top, topLeft)
					out[i] = byte((int(actual) - int(pred)) & 0xff)
				}
			}
		}
	}
	return out
}

// ReverseFilter reverses a predictive filter to recover raw alpha data.
func ReverseFilter(filtered []byte, width, height int, filter FilterType) []byte {
	out := make([]byte, len(filtered))
	if filter == FilterNone {
		copy(out, filtered)
		return out
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			v := filtered[i]
			switch filter {
			case FilterHorizontal:
				if y == 0 || x == 0 {
					out[i] = v
				} else {
					out[i] = byte((int(v) + int(out[i-1])) & 0xff)
				}
			case FilterVertical:
				if y == 0 {
					out[i] = v
				} else {
					out[i] = byte((int(v) + int(out[i-width])) & 0xff)
				}
			case FilterGradient:
				if y == 0 || x == 0 {
					out[i] = v
				} else {
					left := out[i-1]
					top := out[i-width]
					topLeft := out[i-width-1]
					pred := GradientPredictor(left, top, topLeft)
					out[i] = byte((int(v) + int(pred)) & 0xff)
				}
			}
		}
	}
	return out
}

// QuantizeAlpha quantizes alpha values to reduce the number of distinct levels.
// levels is the target number of quantization levels (e.g., 64).
// If levels <= 0 or >= 256, a copy is returned unchanged.
func QuantizeAlpha(alpha []byte, levels int) []byte {
	out := make([]byte, len(alpha))
	if levels <= 0 || levels >= 256 {
		copy(out, alpha)
		return out
	}
	step := 256.0 / float64(levels)
	for i, v := range alpha {
		level := int(float64(v) / step)
		if level >= levels {
			level = levels - 1
		}
		out[i] = byte(float64(level)*step + step/2)
	}
	return out
}

// ExtractAlpha extracts the alpha channel from an image as a flat byte slice (row-major).
func ExtractAlpha(img image.Image) []byte {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y
	alpha := make([]byte, width*height)
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := nrgba.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				alpha[y*width+x] = nrgba.Pix[off+3]
			}
		}
	} else {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				_, _, _, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				alpha[y*width+x] = byte(a >> 8)
			}
		}
	}
	return alpha
}

// ApplyAlpha applies an alpha channel to a decoded YCbCr image and returns an NRGBA image.
func ApplyAlpha(ycbcr *image.YCbCr, alphaPlane []byte) *image.NRGBA {
	bounds := ycbcr.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := ycbcr.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			a := byte(0xff)
			if i := y*width + x; i < len(alphaPlane) {
				a = alphaPlane[i]
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: byte(r >> 8),
				G: byte(g >> 8),
				B: byte(b >> 8),
				A: a,
			})
		}
	}
	return out
}
