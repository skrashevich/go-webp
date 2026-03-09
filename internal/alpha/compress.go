package alpha

import (
	"errors"

	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Compress compresses an alpha plane using the specified method.
// method=0: no compression (raw bytes returned as-is).
// method=1: headerless VP8L lossless image stream via green channel.
func Compress(alpha []byte, width, height int, method int) ([]byte, error) {
	switch method {
	case 0:
		out := make([]byte, len(alpha))
		copy(out, alpha)
		return out, nil
	case 1:
		return vp8l.EncodeAlphaPlane(alpha, width, height)
	default:
		return nil, errors.New("alpha: unsupported compression method")
	}
}

// Decompress decompresses an alpha plane from the specified method.
// method=0: no compression (raw bytes returned as-is).
// method=1: headerless VP8L lossless image stream, green channel extracted.
func Decompress(data []byte, width, height int, method int) ([]byte, error) {
	switch method {
	case 0:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	case 1:
		return vp8l.DecodeAlphaPlane(data, width, height)
	default:
		return nil, errors.New("alpha: unsupported compression method")
	}
}
