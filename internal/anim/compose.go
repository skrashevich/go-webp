package anim

import (
	"image"
	"image/color"
)

// BlendPixel alpha-blends src over dst using non-premultiplied alpha.
// Formula: blend_alpha = src.A + dst.A*(255-src.A)/255
// If blend_alpha == 0, result is transparent black.
// Otherwise: blend_C = (src.C*src.A + dst.C*dst.A*(255-src.A)/255) / blend_alpha
func BlendPixel(dst, src color.NRGBA) color.NRGBA {
	if src.A == 255 {
		return src
	}
	blendA := uint32(src.A) + uint32(dst.A)*(255-uint32(src.A))/255
	if blendA == 0 {
		return color.NRGBA{}
	}
	if src.A == 0 {
		return dst
	}
	dstContrib := uint32(dst.A) * (255 - uint32(src.A)) / 255
	blendR := (uint32(src.R)*uint32(src.A) + uint32(dst.R)*dstContrib) / blendA
	blendG := (uint32(src.G)*uint32(src.A) + uint32(dst.G)*dstContrib) / blendA
	blendB := (uint32(src.B)*uint32(src.A) + uint32(dst.B)*dstContrib) / blendA
	return color.NRGBA{
		R: uint8(blendR),
		G: uint8(blendG),
		B: uint8(blendB),
		A: uint8(blendA),
	}
}

// newCanvas creates a new canvas initialized to transparent black.
func newCanvas(width, height int) *image.NRGBA {
	return image.NewNRGBA(image.Rect(0, 0, width, height))
}

// copyCanvas returns a deep copy of an NRGBA image.
func copyCanvas(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// clearRect fills the rectangle r on canvas with the given color.
func clearRect(canvas *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	bounds := canvas.Bounds()
	r = r.Intersect(bounds)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			canvas.SetNRGBA(x, y, c)
		}
	}
}

// toNRGBA converts any image.Image to *image.NRGBA.
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, b_, a := img.At(x, y).RGBA()
			var c color.NRGBA
			if a == 0 {
				c = color.NRGBA{}
			} else {
				c = color.NRGBA{
					R: uint8(r >> 8),
					G: uint8(g >> 8),
					B: uint8(b_ >> 8),
					A: uint8(a >> 8),
				}
			}
			dst.SetNRGBA(x-b.Min.X, y-b.Min.Y, c)
		}
	}
	return dst
}

// ComposeFrame renders a single frame onto canvas.
// prevDisposed is the canvas state after the previous frame's dispose was applied
// (used when Blend == BlendAlpha, to blend over; it is also the base when Blend == BlendNone).
// The function modifies canvas in-place and returns a snapshot of the canvas
// before dispose is applied (i.e., the fully rendered frame as the viewer sees it).
// After the snapshot is taken, if Dispose == DisposeBackground, the frame area on
// canvas is cleared to transparent black (the animation's background).
func ComposeFrame(canvas *image.NRGBA, frame *Frame, bg color.NRGBA) *image.NRGBA {
	frameBounds := image.Rect(0, 0, 0, 0)
	if frame.Image != nil {
		fb := frame.Image.Bounds()
		frameBounds = image.Rect(
			frame.X,
			frame.Y,
			frame.X+fb.Dx(),
			frame.Y+fb.Dy(),
		)
	}

	// Render frame pixels onto canvas.
	if frame.Image != nil {
		src := toNRGBA(frame.Image)
		sb := frame.Image.Bounds()
		for y := sb.Min.Y; y < sb.Max.Y; y++ {
			for x := sb.Min.X; x < sb.Max.X; x++ {
				cx := frame.X + (x - sb.Min.X)
				cy := frame.Y + (y - sb.Min.Y)
				if !image.Pt(cx, cy).In(canvas.Bounds()) {
					continue
				}
				srcPx := src.NRGBAAt(x-sb.Min.X, y-sb.Min.Y)
				if frame.Blend == BlendAlpha {
					dstPx := canvas.NRGBAAt(cx, cy)
					canvas.SetNRGBA(cx, cy, BlendPixel(dstPx, srcPx))
				} else {
					canvas.SetNRGBA(cx, cy, srcPx)
				}
			}
		}
	}

	// Snapshot the canvas as the viewer sees this frame.
	result := copyCanvas(canvas)

	// Apply dispose method.
	if frame.Dispose == DisposeBackground {
		clearRect(canvas, frameBounds, bg)
	}
	// DisposeNone: leave canvas as-is.

	return result
}

// Compose renders all frames of an animation and returns the composed canvas
// state after each frame (what the viewer would see for each frame).
// Each returned image is the full canvas at display time for that frame.
func Compose(anim *Animation) []*image.NRGBA {
	canvas := newCanvas(anim.Width, anim.Height)
	// Initialize canvas to background color.
	clearRect(canvas, canvas.Bounds(), anim.BackgroundColor)

	results := make([]*image.NRGBA, len(anim.Frames))
	for i := range anim.Frames {
		results[i] = ComposeFrame(canvas, &anim.Frames[i], anim.BackgroundColor)
	}
	return results
}

// IsKeyframe reports whether the given frame index should be treated as a keyframe.
// Frame 0 is always a keyframe. Later frames are keyframes when they have
// BlendNone and DisposeBackground and cover the full canvas — meaning the
// composed result does not depend on any prior frame.
func IsKeyframe(anim *Animation, idx int) bool {
	if idx == 0 {
		return true
	}
	if idx >= len(anim.Frames) {
		return false
	}
	f := &anim.Frames[idx]
	if f.Blend != BlendNone || f.Dispose != DisposeBackground {
		return false
	}
	// Check if the frame covers the full canvas.
	if f.Image == nil {
		return false
	}
	fb := f.Image.Bounds()
	return fb.Dx() >= anim.Width && fb.Dy() >= anim.Height
}
