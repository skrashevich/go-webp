// Package anim implements WebP animation encoding and decoding.
//
// WebP animations use the VP8X extended format with ANIM and ANMF chunks.
// Each frame is an independent VP8 (lossy) or VP8L (lossless) keyframe.
package anim

import (
	"image"
	"image/color"
)

// DisposeMethod determines how the frame area is treated after rendering.
type DisposeMethod int

const (
	DisposeNone       DisposeMethod = 0 // Leave frame as-is after rendering
	DisposeBackground DisposeMethod = 1 // Clear frame area to background color after rendering
)

// BlendMethod determines how the frame is composited onto the canvas.
type BlendMethod int

const (
	BlendAlpha BlendMethod = 0 // Alpha-blend over previous canvas content
	BlendNone  BlendMethod = 1 // Replace (no blending)
)

// Animation represents a decoded WebP animation.
type Animation struct {
	Width           int         // Canvas width
	Height          int         // Canvas height
	LoopCount       int         // 0 = infinite loop
	BackgroundColor color.NRGBA // Background color (from ANIM chunk, BGRA order on wire)
	Frames          []Frame
}

// Frame represents a single animation frame.
type Frame struct {
	Image    image.Image   // Decoded frame image
	X        int           // Frame X offset on canvas (must be even)
	Y        int           // Frame Y offset on canvas (must be even)
	Duration int           // Duration in milliseconds (0–16777215)
	Dispose  DisposeMethod // How to treat canvas after this frame is shown
	Blend    BlendMethod   // How to composite this frame onto the canvas
}

// AnimationOptions controls animation encoding.
type AnimationOptions struct {
	LoopCount       int
	BackgroundColor color.NRGBA
	Lossy           bool    // Use VP8 lossy encoding for frames (VP8L lossless if false)
	Quality         float32 // Quality for lossy encoding [0, 100]
}

// IsAnimation reports whether the VP8X flags indicate an animated WebP.
func IsAnimation(vp8xFlags uint32) bool {
	const animationFlag = 1 << 1
	return vp8xFlags&animationFlag != 0
}

// VP8XInfo carries the canvas dimensions and flags parsed from a VP8X chunk.
// This mirrors riff.VP8XChunk but is defined here to avoid import cycles.
type VP8XInfo struct {
	Flags  uint32
	Width  uint32 // canvas width - 1 (as stored in the chunk)
	Height uint32 // canvas height - 1 (as stored in the chunk)
}
