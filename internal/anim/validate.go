package anim

import "fmt"

const maxStoredDimension = 1 << 24

func validateCanvasDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid canvas dimensions %dx%d", width, height)
	}
	if width > maxStoredDimension || height > maxStoredDimension {
		return fmt.Errorf("canvas dimensions %dx%d exceed uint24 limit", width, height)
	}
	if uint64(width)*uint64(height) > uint64(^uint32(0)) {
		return fmt.Errorf("canvas area %dx%d exceeds 2^32-1 pixels", width, height)
	}
	return nil
}

func validateFrameGeometry(frame *Frame, canvasWidth, canvasHeight int) (int, int, error) {
	if frame == nil || frame.Image == nil {
		return 0, 0, fmt.Errorf("frame image is nil")
	}
	if frame.X < 0 || frame.Y < 0 {
		return 0, 0, fmt.Errorf("frame offset (%d,%d) must be non-negative", frame.X, frame.Y)
	}
	if frame.X%2 != 0 || frame.Y%2 != 0 {
		return 0, 0, fmt.Errorf("frame offset (%d,%d) must be even", frame.X, frame.Y)
	}

	bounds := frame.Image.Bounds()
	frameWidth := bounds.Dx()
	frameHeight := bounds.Dy()
	if frameWidth <= 0 || frameHeight <= 0 {
		return 0, 0, fmt.Errorf("invalid frame dimensions %dx%d", frameWidth, frameHeight)
	}
	if frameWidth > maxStoredDimension || frameHeight > maxStoredDimension {
		return 0, 0, fmt.Errorf("frame dimensions %dx%d exceed uint24 limit", frameWidth, frameHeight)
	}

	right := uint64(frame.X) + uint64(frameWidth)
	bottom := uint64(frame.Y) + uint64(frameHeight)
	if right > uint64(canvasWidth) || bottom > uint64(canvasHeight) {
		return 0, 0, fmt.Errorf(
			"frame rect (%d,%d)-(%d,%d) exceeds canvas %dx%d",
			frame.X, frame.Y, right, bottom, canvasWidth, canvasHeight,
		)
	}
	return frameWidth, frameHeight, nil
}
