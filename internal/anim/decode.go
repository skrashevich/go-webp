package anim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"

	"github.com/skrashevich/go-webp/internal/riff"
	"github.com/skrashevich/go-webp/internal/vp8"
	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Decode reads an animated WebP from r.
// r must be positioned after the RIFF/WEBP header (i.e., at the first chunk).
// vp8xInfo carries the canvas dimensions and flags already parsed from VP8X.
func Decode(r io.Reader, vp8xInfo VP8XInfo) (*Animation, error) {
	anim := &Animation{
		Width:  int(vp8xInfo.Width) + 1,
		Height: int(vp8xInfo.Height) + 1,
	}

	chunks, err := riff.ReadAllChunks(r)
	if err != nil {
		return nil, fmt.Errorf("anim: reading chunks: %w", err)
	}

	animSeen := false
	for _, chunk := range chunks {
		switch chunk.ID {
		case riff.FourCCANIM:
			if err := parseANIM(chunk.Data, anim); err != nil {
				return nil, fmt.Errorf("anim: ANIM chunk: %w", err)
			}
			animSeen = true

		case riff.FourCCANMF:
			if !animSeen {
				return nil, errors.New("anim: ANMF chunk before ANIM chunk")
			}
			frame, err := DecodeFrame(chunk.Data)
			if err != nil {
				return nil, fmt.Errorf("anim: ANMF frame %d: %w", len(anim.Frames), err)
			}
			anim.Frames = append(anim.Frames, *frame)
		}
		// Other chunks (ICCP, EXIF, XMP) are silently skipped.
	}

	if !animSeen {
		return nil, errors.New("anim: no ANIM chunk found")
	}

	return anim, nil
}

// parseANIM parses the ANIM chunk payload into anim.
// Layout: 4 bytes BGRA background color + 2 bytes loop count (LE).
func parseANIM(data []byte, anim *Animation) error {
	if len(data) < 6 {
		return errors.New("anim: ANIM chunk too short")
	}
	// Background color is stored as BGRA.
	anim.BackgroundColor.B = data[0]
	anim.BackgroundColor.G = data[1]
	anim.BackgroundColor.R = data[2]
	anim.BackgroundColor.A = data[3]
	anim.LoopCount = int(binary.LittleEndian.Uint16(data[4:6]))
	return nil
}

// DecodeFrame decodes a single ANMF chunk payload into a Frame.
// ANMF payload layout:
//   - 3 bytes: X / 2       (24-bit LE)
//   - 3 bytes: Y / 2       (24-bit LE)
//   - 3 bytes: width - 1   (24-bit LE)
//   - 3 bytes: height - 1  (24-bit LE)
//   - 3 bytes: duration ms (24-bit LE)
//   - 1 byte:  flags (bit0=dispose_bg, bit1=no_blend)
//   - N bytes: VP8/VP8L chunk (frame bitstream)
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < 16 {
		return nil, errors.New("anim: ANMF payload too short")
	}

	x := int(readUint24LE(data[0:3])) * 2
	y := int(readUint24LE(data[3:6])) * 2
	// fw and fh are encoded as (value - 1).
	_ = int(readUint24LE(data[6:9])) + 1  // fw — used implicitly by the decoder
	_ = int(readUint24LE(data[9:12])) + 1 // fh — used implicitly by the decoder
	duration := int(readUint24LE(data[12:15]))
	flags := data[15]

	frame := &Frame{
		X:        x,
		Y:        y,
		Duration: duration,
	}
	if flags&0x01 != 0 {
		frame.Dispose = DisposeBackground
	} else {
		frame.Dispose = DisposeNone
	}
	if flags&0x02 != 0 {
		frame.Blend = BlendNone
	} else {
		frame.Blend = BlendAlpha
	}

	// Remaining bytes are the frame bitstream: one VP8/VP8L chunk.
	img, err := decodeANMFImage(data[16:])
	if err != nil {
		return nil, fmt.Errorf("anim: frame bitstream: %w", err)
	}
	frame.Image = img

	return frame, nil
}

// decodeANMFImage reads the first VP8 or VP8L chunk from data and decodes it.
func decodeANMFImage(data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	chunk, err := riff.ReadChunk(r)
	if err != nil {
		return nil, fmt.Errorf("anim: reading frame chunk: %w", err)
	}

	switch chunk.ID {
	case riff.FourCCVP8:
		dec := vp8.NewDecoder(chunk.Data)
		return dec.Decode()

	case riff.FourCCVP8L:
		dec := vp8l.NewDecoder(chunk.Data)
		return dec.Decode()

	default:
		return nil, fmt.Errorf("anim: unsupported frame chunk type: %s", chunk.ID)
	}
}

// readUint24LE reads a 24-bit little-endian value from 3 bytes.
func readUint24LE(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}
