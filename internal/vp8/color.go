package vp8

import (
	"image"
	"image/color"
)

// yuvPlanes holds YCbCr 4:2:0 planes for one frame.
type yuvPlanes struct {
	y          []byte
	cb, cr     []byte
	yStride    int
	cStride    int
	width      int
	height     int
}

// rgbaToYUV converts an image.Image to YCbCr 4:2:0 planes.
// Luma (Y) is full-resolution; Cb and Cr are half-resolution in each dimension.
func rgbaToYUV(img image.Image) *yuvPlanes {
	b := img.Bounds()
	w := b.Max.X - b.Min.X
	h := b.Max.Y - b.Min.Y

	// Pad to multiples of 16 for macroblock alignment.
	padW := (w + 15) &^ 15
	padH := (h + 15) &^ 15

	p := &yuvPlanes{
		y:       make([]byte, padW*padH),
		cb:      make([]byte, (padW/2)*(padH/2)),
		cr:      make([]byte, (padW/2)*(padH/2)),
		yStride: padW,
		cStride: padW / 2,
		width:   w,
		height:  h,
	}

	for y := 0; y < padH; y++ {
		for x := 0; x < padW; x++ {
			// Clamp to image bounds for padding.
			sx := x
			if sx >= w {
				sx = w - 1
			}
			sy := y
			if sy >= h {
				sy = h - 1
			}
			r32, g32, b32, _ := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
			r8 := uint8(r32 >> 8)
			g8 := uint8(g32 >> 8)
			b8 := uint8(b32 >> 8)

			// Use Go's standard BT.601 full-range conversion (JFIF/VP8).
			yv, cbu, cru := color.RGBToYCbCr(r8, g8, b8)
			p.y[y*p.yStride+x] = yv

			// Chroma subsampling: use top-left sample of each 2x2 block.
			if x%2 == 0 && y%2 == 0 {
				cx := x / 2
				cy := y / 2
				p.cb[cy*p.cStride+cx] = cbu
				p.cr[cy*p.cStride+cx] = cru
			}
		}
	}
	return p
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
