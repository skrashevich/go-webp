package anim

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// --- validateCanvasDimensions tests ---

func TestValidateCanvasDimensions_Valid(t *testing.T) {
	if err := validateCanvasDimensions(100, 200); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCanvasDimensions_ZeroWidth(t *testing.T) {
	if err := validateCanvasDimensions(0, 10); err == nil {
		t.Fatal("accepted zero width")
	}
}

func TestValidateCanvasDimensions_NegativeHeight(t *testing.T) {
	if err := validateCanvasDimensions(10, -1); err == nil {
		t.Fatal("accepted negative height")
	}
}

func TestValidateCanvasDimensions_ExceedsUint24(t *testing.T) {
	if err := validateCanvasDimensions(1<<24+1, 1); err == nil {
		t.Fatal("accepted dimension exceeding uint24 limit")
	}
}

func TestValidateCanvasDimensions_MaxValid(t *testing.T) {
	if err := validateCanvasDimensions(1<<24, 1); err != nil {
		t.Fatalf("rejected max valid dimension: %v", err)
	}
}

// --- validateFrameGeometry tests ---

func TestValidateFrameGeometry_Valid(t *testing.T) {
	frame := &Frame{Image: makeTestImage(4, 4, color.NRGBA{R: 255, A: 255}), X: 0, Y: 0}
	w, h, err := validateFrameGeometry(frame, 4, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 4 || h != 4 {
		t.Errorf("got %dx%d, want 4x4", w, h)
	}
}

func TestValidateFrameGeometry_NilFrame(t *testing.T) {
	if _, _, err := validateFrameGeometry(nil, 4, 4); err == nil {
		t.Fatal("accepted nil frame")
	}
}

func TestValidateFrameGeometry_NilImage(t *testing.T) {
	frame := &Frame{Image: nil}
	if _, _, err := validateFrameGeometry(frame, 4, 4); err == nil {
		t.Fatal("accepted nil image")
	}
}

func TestValidateFrameGeometry_NegativeOffset(t *testing.T) {
	frame := &Frame{Image: makeTestImage(2, 2, color.NRGBA{A: 255}), X: -2, Y: 0}
	if _, _, err := validateFrameGeometry(frame, 4, 4); err == nil {
		t.Fatal("accepted negative offset")
	}
}

func TestValidateFrameGeometry_OddOffset(t *testing.T) {
	frame := &Frame{Image: makeTestImage(2, 2, color.NRGBA{A: 255}), X: 1, Y: 0}
	if _, _, err := validateFrameGeometry(frame, 4, 4); err == nil {
		t.Fatal("accepted odd offset")
	}
}

func TestValidateFrameGeometry_ExceedsCanvas(t *testing.T) {
	frame := &Frame{Image: makeTestImage(3, 3, color.NRGBA{A: 255}), X: 2, Y: 2}
	if _, _, err := validateFrameGeometry(frame, 4, 4); err == nil {
		t.Fatal("accepted frame exceeding canvas")
	}
}

func TestValidateFrameGeometry_WithOffset(t *testing.T) {
	frame := &Frame{Image: makeTestImage(2, 2, color.NRGBA{A: 255}), X: 2, Y: 2}
	w, h, err := validateFrameGeometry(frame, 4, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 2 || h != 2 {
		t.Errorf("got %dx%d, want 2x2", w, h)
	}
}

func TestValidateFrameGeometry_ZeroSizedImage(t *testing.T) {
	frame := &Frame{Image: image.NewNRGBA(image.Rect(0, 0, 0, 0))}
	if _, _, err := validateFrameGeometry(frame, 4, 4); err == nil {
		t.Fatal("accepted zero-sized image")
	}
}

// --- Encode integration validation tests ---

func TestEncodeRejectsNilAnimation(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, nil, nil); err == nil {
		t.Fatal("Encode accepted nil animation")
	}
}

func TestEncodeRejectsNoFrames(t *testing.T) {
	a := &Animation{Width: 4, Height: 4}
	var buf bytes.Buffer
	if err := Encode(&buf, a, nil); err == nil {
		t.Fatal("Encode accepted animation with no frames")
	}
}

func TestEncodeRejectsOddFrameOffset(t *testing.T) {
	a := &Animation{
		Width:  4,
		Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(2, 2, color.NRGBA{R: 255, A: 255}), X: 1},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, a, nil); err == nil {
		t.Fatal("Encode accepted odd frame offset")
	}
}

func TestEncodeRejectsFrameOutsideCanvas(t *testing.T) {
	a := &Animation{
		Width:  4,
		Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(3, 3, color.NRGBA{G: 255, A: 255}), X: 2, Y: 2},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, a, nil); err == nil {
		t.Fatal("Encode accepted frame outside canvas")
	}
}

func TestEncodeRejectsLossyAlphaFrames(t *testing.T) {
	a := &Animation{
		Width:  2,
		Height: 2,
		Frames: []Frame{
			{Image: makeTestImage(2, 2, color.NRGBA{R: 255, A: 128})},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, a, &AnimationOptions{Lossy: true, Quality: 75}); err == nil {
		t.Fatal("Encode accepted lossy animation frame with alpha")
	}
}

func TestEncodeRejectsZeroCanvasDimensions(t *testing.T) {
	a := &Animation{
		Width: 0, Height: 4,
		Frames: []Frame{{Image: makeTestImage(2, 2, color.NRGBA{A: 255})}},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, a, nil); err == nil {
		t.Fatal("Encode accepted zero canvas width")
	}
}

func TestEncodeAcceptsValidAnimation(t *testing.T) {
	a := &Animation{
		Width:  4,
		Height: 4,
		Frames: []Frame{
			{Image: makeTestImage(4, 4, color.NRGBA{R: 255, A: 255})},
		},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, a, nil); err != nil {
		t.Fatalf("Encode rejected valid animation: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("Encode produced empty output")
	}
}

// --- DecodeFrame validation tests ---

func TestDecodeFrameRejectsOutOfCanvas(t *testing.T) {
	payload, err := encodeANMFPayload(&Frame{
		Image: makeTestImage(2, 2, color.NRGBA{R: 255, A: 255}),
	}, &AnimationOptions{})
	if err != nil {
		t.Fatalf("encodeANMFPayload: %v", err)
	}
	payload[0] = 2 // x/2 = 2 -> x = 4
	if _, err := DecodeFrame(payload, 4, 4); err == nil {
		t.Fatal("DecodeFrame accepted frame outside canvas")
	}
}

func TestDecodeFrameRejectsHeaderSizeMismatch(t *testing.T) {
	payload, err := encodeANMFPayload(&Frame{
		Image: makeTestImage(2, 2, color.NRGBA{B: 255, A: 255}),
	}, &AnimationOptions{})
	if err != nil {
		t.Fatalf("encodeANMFPayload: %v", err)
	}
	payload[6] = 2 // width-1 = 2 -> width = 3
	if _, err := DecodeFrame(payload, 4, 4); err == nil {
		t.Fatal("DecodeFrame accepted mismatched ANMF header dimensions")
	}
}

func TestDecodeFrameRejectsTooShortPayload(t *testing.T) {
	if _, err := DecodeFrame(make([]byte, 10), 4, 4); err == nil {
		t.Fatal("DecodeFrame accepted too-short payload")
	}
}
