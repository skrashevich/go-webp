package anim

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/color"
	"io"

	"github.com/skrashevich/go-webp/internal/riff"
	"github.com/skrashevich/go-webp/internal/vp8"
	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Encode writes an animated WebP to w.
// Each frame is encoded independently as a VP8 (lossy) or VP8L (lossless) keyframe.
// If opts is nil, lossless encoding with default settings is used.
func Encode(w io.Writer, anim *Animation, opts *AnimationOptions) error {
	if anim == nil {
		return fmt.Errorf("anim: animation is nil")
	}
	if opts == nil {
		opts = &AnimationOptions{}
	}
	if err := validateCanvasDimensions(anim.Width, anim.Height); err != nil {
		return fmt.Errorf("anim: %w", err)
	}
	if len(anim.Frames) == 0 {
		return fmt.Errorf("anim: animation has no frames")
	}
	if opts.Lossy && framesHaveAlpha(anim) {
		return fmt.Errorf("anim: lossy animation frames with alpha are not supported")
	}

	// Encode all frames first so we know total size.
	type encodedFrame struct {
		anmfData []byte // complete ANMF chunk payload
	}

	encoded := make([]encodedFrame, len(anim.Frames))
	for i, frame := range anim.Frames {
		if _, _, err := validateFrameGeometry(&frame, anim.Width, anim.Height); err != nil {
			return fmt.Errorf("anim: frame %d geometry: %w", i, err)
		}
		anmfPayload, err := encodeANMFPayload(&frame, opts)
		if err != nil {
			return fmt.Errorf("anim: encoding frame %d: %w", i, err)
		}
		encoded[i].anmfData = anmfPayload
	}

	// Build the full WEBP payload in a buffer so we can compute sizes.
	var body bytes.Buffer

	// VP8X chunk (10 bytes payload).
	vp8xFlags := riff.VP8XFlagAnimation
	// Check if any frame has alpha by trying to detect NRGBA with transparency.
	if framesHaveAlpha(anim) {
		vp8xFlags |= riff.VP8XFlagAlpha
	}
	vp8xPayload := makeVP8XPayload(vp8xFlags, anim.Width, anim.Height)
	if err := riff.WriteChunk(&body, riff.FourCCVP8X, vp8xPayload); err != nil {
		return fmt.Errorf("anim: writing VP8X: %w", err)
	}

	// ANIM chunk (6 bytes payload): background color (BGRA) + loop count.
	animPayload := makeANIMPayload(anim.BackgroundColor, anim.LoopCount)
	if err := riff.WriteChunk(&body, riff.FourCCANIM, animPayload); err != nil {
		return fmt.Errorf("anim: writing ANIM: %w", err)
	}

	// ANMF chunks for each frame.
	for i, ef := range encoded {
		if err := riff.WriteChunk(&body, riff.FourCCANMF, ef.anmfData); err != nil {
			return fmt.Errorf("anim: writing ANMF frame %d: %w", i, err)
		}
	}

	// RIFF file size = 4 ("WEBP") + body length.
	fileSize := uint32(4 + body.Len())

	var out bytes.Buffer
	if err := riff.WriteHeader(&out, fileSize); err != nil {
		return err
	}
	if _, err := out.Write(body.Bytes()); err != nil {
		return err
	}

	_, err := w.Write(out.Bytes())
	return err
}

// encodeANMFPayload encodes a single frame into an ANMF chunk payload.
// ANMF payload layout (per WebP spec):
//   - 3 bytes: X / 2  (24-bit LE)
//   - 3 bytes: Y / 2  (24-bit LE)
//   - 3 bytes: width - 1  (24-bit LE)
//   - 3 bytes: height - 1 (24-bit LE)
//   - 3 bytes: duration in ms (24-bit LE)
//   - 1 byte:  flags (bit0=dispose_background, bit1=no_blend)
//   - N bytes: frame bitstream (VP8/VP8L chunk)
func encodeANMFPayload(frame *Frame, opts *AnimationOptions) ([]byte, error) {
	if frame.Image == nil {
		return nil, fmt.Errorf("anim: frame image is nil")
	}
	bounds := frame.Image.Bounds()
	fw := bounds.Dx()
	fh := bounds.Dy()

	// Encode the frame image.
	var frameChunkID riff.FourCC
	var frameBitstream []byte
	var err error

	if opts.Lossy {
		enc := vp8.NewEncoder(opts.Quality)
		frameBitstream, err = enc.Encode(frame.Image)
		if err != nil {
			return nil, fmt.Errorf("anim: VP8 encode: %w", err)
		}
		frameChunkID = riff.FourCCVP8
	} else {
		enc := vp8l.NewEncoder()
		frameBitstream, err = enc.Encode(frame.Image)
		if err != nil {
			return nil, fmt.Errorf("anim: VP8L encode: %w", err)
		}
		frameChunkID = riff.FourCCVP8L
	}

	// Build the ANMF payload.
	var buf bytes.Buffer

	// X/2, Y/2, width-1, height-1 as 24-bit LE values.
	writeUint24LE(&buf, uint32(frame.X/2))
	writeUint24LE(&buf, uint32(frame.Y/2))
	writeUint24LE(&buf, uint32(fw-1))
	writeUint24LE(&buf, uint32(fh-1))

	// Duration as 24-bit LE.
	dur := frame.Duration
	if dur < 0 {
		dur = 0
	}
	if dur > 0xFFFFFF {
		dur = 0xFFFFFF
	}
	writeUint24LE(&buf, uint32(dur))

	// Flags byte: bit0 = dispose_background, bit1 = no_blend.
	var flags byte
	if frame.Dispose == DisposeBackground {
		flags |= 0x01
	}
	if frame.Blend == BlendNone {
		flags |= 0x02
	}
	buf.WriteByte(flags)

	// Write the frame bitstream as a VP8 or VP8L chunk inside the ANMF payload.
	if err := riff.WriteChunk(&buf, frameChunkID, frameBitstream); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// makeVP8XPayload builds the 10-byte VP8X chunk payload.
func makeVP8XPayload(flags riff.VP8XFlags, width, height int) []byte {
	b := make([]byte, 10)
	binary.LittleEndian.PutUint32(b[0:4], uint32(flags))
	// Width and height stored as canvas_width - 1 and canvas_height - 1, 3 bytes each.
	putUint24LE(b[4:7], uint32(width-1))
	putUint24LE(b[7:10], uint32(height-1))
	return b
}

// makeANIMPayload builds the 6-byte ANIM chunk payload.
// Background color is stored as BGRA (4 bytes) + loop count (2 bytes LE).
func makeANIMPayload(bg color.NRGBA, loopCount int) []byte {
	b := make([]byte, 6)
	// Background color stored as BGRA on the wire.
	b[0] = bg.B
	b[1] = bg.G
	b[2] = bg.R
	b[3] = bg.A
	lc := loopCount
	if lc < 0 {
		lc = 0
	}
	if lc > 0xFFFF {
		lc = 0xFFFF
	}
	binary.LittleEndian.PutUint16(b[4:6], uint16(lc))
	return b
}

// framesHaveAlpha reports whether any frame in the animation has an alpha channel.
func framesHaveAlpha(anim *Animation) bool {
	for _, frame := range anim.Frames {
		if frame.Image == nil {
			continue
		}
		b := frame.Image.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				_, _, _, a := frame.Image.At(x, y).RGBA()
				if a != 0xffff {
					return true
				}
			}
		}
	}
	return false
}

// writeUint24LE writes a 24-bit little-endian value to buf.
func writeUint24LE(buf *bytes.Buffer, v uint32) {
	buf.WriteByte(byte(v))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v >> 16))
}

// putUint24LE writes a 24-bit little-endian value into b (must be at least 3 bytes).
func putUint24LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}
