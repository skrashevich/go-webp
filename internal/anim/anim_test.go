package anim

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/skrashevich/go-webp/internal/riff"
)

// makeTestImage returns a solid-color NRGBA image of the given size.
func makeTestImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// ---------- BlendPixel tests ----------

func TestBlendPixelFullyOpaqueSrc(t *testing.T) {
	dst := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	src := color.NRGBA{R: 200, G: 50, B: 10, A: 255}
	got := BlendPixel(dst, src)
	if got != src {
		t.Errorf("BlendPixel(dst, fully-opaque src) = %v, want %v", got, src)
	}
}

func TestBlendPixelFullyTransparentSrc(t *testing.T) {
	dst := color.NRGBA{R: 100, G: 100, B: 100, A: 200}
	src := color.NRGBA{R: 200, G: 50, B: 10, A: 0}
	got := BlendPixel(dst, src)
	if got != dst {
		t.Errorf("BlendPixel(dst, transparent src) = %v, want %v (dst unchanged)", got, dst)
	}
}

func TestBlendPixelBothTransparent(t *testing.T) {
	dst := color.NRGBA{R: 100, G: 100, B: 100, A: 0}
	src := color.NRGBA{R: 200, G: 50, B: 10, A: 0}
	got := BlendPixel(dst, src)
	want := color.NRGBA{}
	if got != want {
		t.Errorf("BlendPixel(transparent dst, transparent src) = %v, want %v", got, want)
	}
}

func TestBlendPixelFormula(t *testing.T) {
	// src over dst, both semi-transparent.
	// src.A=128, dst.A=255 → blend_alpha = 128 + 255*(255-128)/255 = 128 + 127 = 255
	// blend_R = (src.R*src.A + dst.R*dst.A*(255-src.A)/255) / blend_alpha
	//         = (200*128 + 100*127) / 255 = (25600+12700)/255 = 38300/255 ≈ 150
	dst := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	src := color.NRGBA{R: 200, G: 200, B: 200, A: 128}
	got := BlendPixel(dst, src)
	if got.A != 255 {
		t.Errorf("BlendPixel alpha = %d, want 255", got.A)
	}
	// Allow ±1 for integer rounding.
	wantR := uint8(38300 / 255) // =150
	if abs8(int(got.R)-int(wantR)) > 1 {
		t.Errorf("BlendPixel R = %d, want ~%d", got.R, wantR)
	}
}

func abs8(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---------- Canvas initialization test ----------

func TestCanvasInitTransparentBlack(t *testing.T) {
	canvas := newCanvas(10, 8)
	b := canvas.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			px := canvas.NRGBAAt(x, y)
			if px != (color.NRGBA{}) {
				t.Fatalf("canvas pixel at (%d,%d) = %v, want transparent black", x, y, px)
			}
		}
	}
}

// ---------- ComposeFrame tests ----------

func TestComposeFrameDisposeNone(t *testing.T) {
	// After rendering, frame pixels should remain on canvas.
	canvas := newCanvas(4, 4)
	red := color.NRGBA{R: 255, A: 255}
	frame := &Frame{
		Image:   makeTestImage(2, 2, red),
		X:       0, Y: 0,
		Dispose: DisposeNone,
		Blend:   BlendNone,
	}
	bg := color.NRGBA{}
	ComposeFrame(canvas, frame, bg)
	// After DisposeNone the canvas should still have the red pixels.
	px := canvas.NRGBAAt(0, 0)
	if px != red {
		t.Errorf("after DisposeNone: canvas(0,0) = %v, want %v", px, red)
	}
}

func TestComposeFrameDisposeBackground(t *testing.T) {
	// After rendering, frame area should be cleared to background.
	canvas := newCanvas(4, 4)
	red := color.NRGBA{R: 255, A: 255}
	bg := color.NRGBA{B: 100, A: 255}
	frame := &Frame{
		Image:   makeTestImage(2, 2, red),
		X:       0, Y: 0,
		Dispose: DisposeBackground,
		Blend:   BlendNone,
	}
	// The returned snapshot should show the red frame.
	snap := ComposeFrame(canvas, frame, bg)
	if snap.NRGBAAt(0, 0) != red {
		t.Errorf("snapshot pixel = %v, want red %v", snap.NRGBAAt(0, 0), red)
	}
	// After dispose, canvas should be cleared to bg.
	px := canvas.NRGBAAt(0, 0)
	if px != bg {
		t.Errorf("after DisposeBackground: canvas(0,0) = %v, want bg %v", px, bg)
	}
}

func TestComposeFrameBlendAlpha(t *testing.T) {
	// Semi-transparent red over opaque white → pinkish.
	canvas := newCanvas(4, 4)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	// Fill canvas with white.
	clearRect(canvas, canvas.Bounds(), white)

	semiRed := color.NRGBA{R: 255, A: 128}
	frame := &Frame{
		Image:   makeTestImage(2, 2, semiRed),
		X:       0, Y: 0,
		Dispose: DisposeNone,
		Blend:   BlendAlpha,
	}
	snap := ComposeFrame(canvas, frame, color.NRGBA{})
	px := snap.NRGBAAt(0, 0)
	// Alpha should be 255 (128 + 255*(127)/255 = 128+127=255).
	if px.A != 255 {
		t.Errorf("blended pixel A = %d, want 255", px.A)
	}
	// R should be between 128 and 255.
	if px.R < 128 {
		t.Errorf("blended pixel R = %d, expected > 128", px.R)
	}
}

func TestComposeFrameBlendNone(t *testing.T) {
	// BlendNone replaces canvas content.
	canvas := newCanvas(4, 4)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	clearRect(canvas, canvas.Bounds(), white)

	blue := color.NRGBA{B: 255, A: 255}
	frame := &Frame{
		Image:   makeTestImage(2, 2, blue),
		X:       0, Y: 0,
		Dispose: DisposeNone,
		Blend:   BlendNone,
	}
	snap := ComposeFrame(canvas, frame, color.NRGBA{})
	px := snap.NRGBAAt(0, 0)
	if px != blue {
		t.Errorf("BlendNone: pixel = %v, want blue %v", px, blue)
	}
}

func TestComposeFrameOffset(t *testing.T) {
	// Frame at offset (2,2) should only affect that region.
	canvas := newCanvas(6, 6)
	green := color.NRGBA{G: 255, A: 255}
	frame := &Frame{
		Image:   makeTestImage(2, 2, green),
		X:       2, Y: 2,
		Dispose: DisposeNone,
		Blend:   BlendNone,
	}
	snap := ComposeFrame(canvas, frame, color.NRGBA{})
	// (0,0) should be transparent.
	if snap.NRGBAAt(0, 0) != (color.NRGBA{}) {
		t.Errorf("pixel (0,0) should be transparent, got %v", snap.NRGBAAt(0, 0))
	}
	// (2,2) should be green.
	if snap.NRGBAAt(2, 2) != green {
		t.Errorf("pixel (2,2) = %v, want green %v", snap.NRGBAAt(2, 2), green)
	}
}

// ---------- IsKeyframe tests ----------

func TestIsKeyframeFirstFrame(t *testing.T) {
	anim := &Animation{
		Width: 4, Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(4, 4, color.NRGBA{R: 255, A: 255})},
		},
	}
	if !IsKeyframe(anim, 0) {
		t.Error("frame 0 should always be a keyframe")
	}
}

func TestIsKeyframeLaterFrameNotKeyframe(t *testing.T) {
	anim := &Animation{
		Width: 4, Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(4, 4, color.NRGBA{R: 255, A: 255})},
			{Image: makeTestImage(2, 2, color.NRGBA{G: 255, A: 255}), Blend: BlendAlpha, Dispose: DisposeBackground},
		},
	}
	// BlendAlpha means it depends on previous frame — not a keyframe.
	if IsKeyframe(anim, 1) {
		t.Error("frame 1 with BlendAlpha should not be a keyframe")
	}
}

func TestIsKeyframeLaterFrameFullCanvas(t *testing.T) {
	anim := &Animation{
		Width: 4, Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(4, 4, color.NRGBA{R: 255, A: 255})},
			{
				Image:   makeTestImage(4, 4, color.NRGBA{G: 255, A: 255}),
				Blend:   BlendNone,
				Dispose: DisposeBackground,
			},
		},
	}
	// BlendNone + DisposeBackground + full canvas → keyframe.
	if !IsKeyframe(anim, 1) {
		t.Error("frame 1 with BlendNone + DisposeBackground + full canvas should be a keyframe")
	}
}

// ---------- Compose multi-frame test ----------

func TestComposeMultiFrame(t *testing.T) {
	// 3-frame animation with different dispose/blend combinations.
	red := color.NRGBA{R: 255, A: 255}
	green := color.NRGBA{G: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	bg := color.NRGBA{R: 10, G: 10, B: 10, A: 255}

	anim := &Animation{
		Width: 4, Height: 4,
		BackgroundColor: bg,
		Frames: []Frame{
			// Frame 0: red, full canvas, BlendNone, DisposeNone.
			{Image: makeTestImage(4, 4, red), Dispose: DisposeNone, Blend: BlendNone},
			// Frame 1: green 2x2 at (0,0), BlendNone, DisposeBackground.
			{Image: makeTestImage(2, 2, green), Dispose: DisposeBackground, Blend: BlendNone},
			// Frame 2: blue 2x2 at (2,2), BlendNone, DisposeNone.
			{Image: makeTestImage(2, 2, blue), X: 2, Y: 2, Dispose: DisposeNone, Blend: BlendNone},
		},
	}

	results := Compose(anim)
	if len(results) != 3 {
		t.Fatalf("Compose returned %d results, want 3", len(results))
	}

	// Frame 0: entire canvas is red.
	if results[0].NRGBAAt(0, 0) != red {
		t.Errorf("frame0 (0,0) = %v, want red", results[0].NRGBAAt(0, 0))
	}

	// Frame 1: (0,0) is green (frame painted), (2,2) is red (from frame0, DisposeNone).
	if results[1].NRGBAAt(0, 0) != green {
		t.Errorf("frame1 (0,0) = %v, want green", results[1].NRGBAAt(0, 0))
	}
	if results[1].NRGBAAt(2, 2) != red {
		t.Errorf("frame1 (2,2) = %v, want red (persisted)", results[1].NRGBAAt(2, 2))
	}

	// After frame1 DisposeBackground, (0..1, 0..1) is cleared to bg.
	// Frame 2: blue at (2,2). (0,0) should be bg (cleared), (2,2) should be blue.
	if results[2].NRGBAAt(2, 2) != blue {
		t.Errorf("frame2 (2,2) = %v, want blue", results[2].NRGBAAt(2, 2))
	}
	if results[2].NRGBAAt(0, 0) != bg {
		t.Errorf("frame2 (0,0) = %v, want bg %v (disposed)", results[2].NRGBAAt(0, 0), bg)
	}
}

// ---------- Encode/Decode round-trip tests ----------

func TestEncodeDecodeRoundTrip(t *testing.T) {
	red := color.NRGBA{R: 200, G: 100, B: 50, A: 255}
	blue := color.NRGBA{R: 50, G: 100, B: 200, A: 255}

	original := &Animation{
		Width: 4, Height: 4,
		LoopCount:       3,
		BackgroundColor: color.NRGBA{R: 10, G: 20, B: 30, A: 255},
		Frames: []Frame{
			{Image: makeTestImage(4, 4, red), Duration: 100, Dispose: DisposeNone, Blend: BlendNone},
			{Image: makeTestImage(4, 4, blue), Duration: 200, Dispose: DisposeBackground, Blend: BlendNone},
		},
	}

	var buf bytes.Buffer
	opts := &AnimationOptions{Lossy: false}
	if err := Encode(&buf, original, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := decodeFromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Width != original.Width {
		t.Errorf("Width = %d, want %d", decoded.Width, original.Width)
	}
	if decoded.Height != original.Height {
		t.Errorf("Height = %d, want %d", decoded.Height, original.Height)
	}
	if decoded.LoopCount != original.LoopCount {
		t.Errorf("LoopCount = %d, want %d", decoded.LoopCount, original.LoopCount)
	}
	if decoded.BackgroundColor != original.BackgroundColor {
		t.Errorf("BackgroundColor = %v, want %v", decoded.BackgroundColor, original.BackgroundColor)
	}
	if len(decoded.Frames) != len(original.Frames) {
		t.Fatalf("frame count = %d, want %d", len(decoded.Frames), len(original.Frames))
	}

	for i, f := range decoded.Frames {
		orig := original.Frames[i]
		if f.Duration != orig.Duration {
			t.Errorf("frame %d Duration = %d, want %d", i, f.Duration, orig.Duration)
		}
		if f.Dispose != orig.Dispose {
			t.Errorf("frame %d Dispose = %v, want %v", i, f.Dispose, orig.Dispose)
		}
		if f.Blend != orig.Blend {
			t.Errorf("frame %d Blend = %v, want %v", i, f.Blend, orig.Blend)
		}
		if f.Image == nil {
			t.Errorf("frame %d Image is nil", i)
			continue
		}
		b := f.Image.Bounds()
		if b.Dx() != orig.Image.Bounds().Dx() || b.Dy() != orig.Image.Bounds().Dy() {
			t.Errorf("frame %d size = %dx%d, want %dx%d", i, b.Dx(), b.Dy(),
				orig.Image.Bounds().Dx(), orig.Image.Bounds().Dy())
		}
	}
}

func TestEncodeDecodeWithOffsets(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	original := &Animation{
		Width: 8, Height: 8,
		Frames: []Frame{
			{
				Image:    makeTestImage(4, 4, red),
				X:        2, Y: 4,
				Duration: 50,
				Dispose:  DisposeNone,
				Blend:    BlendNone,
			},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, original, nil); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := decodeFromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Frames) != 1 {
		t.Fatalf("frame count = %d, want 1", len(decoded.Frames))
	}
	f := decoded.Frames[0]
	if f.X != 2 || f.Y != 4 {
		t.Errorf("frame offset = (%d,%d), want (2,4)", f.X, f.Y)
	}
}

func TestEncodeDecodeDisposeBlendFlags(t *testing.T) {
	img := makeTestImage(4, 4, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	cases := []struct {
		dispose DisposeMethod
		blend   BlendMethod
	}{
		{DisposeNone, BlendAlpha},
		{DisposeNone, BlendNone},
		{DisposeBackground, BlendAlpha},
		{DisposeBackground, BlendNone},
	}
	for _, tc := range cases {
		original := &Animation{
			Width: 4, Height: 4,
			Frames: []Frame{
				{Image: img, Duration: 100, Dispose: tc.dispose, Blend: tc.blend},
			},
		}
		var buf bytes.Buffer
		if err := Encode(&buf, original, nil); err != nil {
			t.Fatalf("Encode (dispose=%v blend=%v): %v", tc.dispose, tc.blend, err)
		}
		decoded, err := decodeFromBytes(buf.Bytes())
		if err != nil {
			t.Fatalf("Decode (dispose=%v blend=%v): %v", tc.dispose, tc.blend, err)
		}
		if decoded.Frames[0].Dispose != tc.dispose {
			t.Errorf("dispose mismatch: got %v, want %v", decoded.Frames[0].Dispose, tc.dispose)
		}
		if decoded.Frames[0].Blend != tc.blend {
			t.Errorf("blend mismatch: got %v, want %v", decoded.Frames[0].Blend, tc.blend)
		}
	}
}

func TestRoundTripFrameCount(t *testing.T) {
	img := makeTestImage(4, 4, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
	anim := &Animation{Width: 4, Height: 4}
	for i := 0; i < 5; i++ {
		anim.Frames = append(anim.Frames, Frame{
			Image: img, Duration: (i + 1) * 33, Dispose: DisposeNone, Blend: BlendNone,
		})
	}
	var buf bytes.Buffer
	if err := Encode(&buf, anim, nil); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := decodeFromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Frames) != 5 {
		t.Errorf("frame count = %d, want 5", len(decoded.Frames))
	}
	for i, f := range decoded.Frames {
		want := (i + 1) * 33
		if f.Duration != want {
			t.Errorf("frame %d duration = %d, want %d", i, f.Duration, want)
		}
	}
}

// ---------- Decode from raw bytes helper ----------

// decodeFromBytes parses a complete RIFF/WEBP animated byte stream.
func decodeFromBytes(data []byte) (*Animation, error) {
	r := bytes.NewReader(data)

	// Read RIFF/WEBP header (12 bytes).
	if _, err := riff.ReadHeader(r); err != nil {
		return nil, err
	}

	// Read VP8X chunk.
	chunk, err := riff.ReadChunk(r)
	if err != nil {
		return nil, err
	}
	if chunk.ID != riff.FourCCVP8X {
		return nil, fmt.Errorf("expected VP8X chunk, got %s", chunk.ID)
	}
	vp8xChunk, err := riff.ParseVP8X(chunk.Data)
	if err != nil {
		return nil, err
	}

	info := VP8XInfo{
		Flags:  uint32(vp8xChunk.Flags),
		Width:  vp8xChunk.Width,
		Height: vp8xChunk.Height,
	}

	return Decode(r, info)
}

// ---------- IsAnimation test ----------

func TestIsAnimation(t *testing.T) {
	const animFlag = uint32(1 << 1)
	if !IsAnimation(animFlag) {
		t.Error("IsAnimation should return true when animation bit is set")
	}
	if IsAnimation(0) {
		t.Error("IsAnimation should return false when no flags set")
	}
	if IsAnimation(uint32(riff.VP8XFlagAlpha)) {
		t.Error("IsAnimation should return false for alpha-only flag")
	}
}

