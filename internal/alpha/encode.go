package alpha

import "errors"

// defaultQuantizeLevels is the number of quantization levels used when
// preprocessing is enabled (matches libwebp behaviour).
const defaultQuantizeLevels = 64

// EncodeALPHWithPreprocessing encodes an alpha channel with optional preprocessing.
// When preprocess is true, alpha values are quantized to 64 distinct levels
// before filtering and compression, improving compressibility at the cost of
// some alpha precision.
func EncodeALPHWithPreprocessing(alpha []byte, width, height int, method int, filter FilterType, preprocess bool) ([]byte, error) {
	if len(alpha) != width*height {
		return nil, errors.New("alpha: alpha plane size mismatch")
	}
	if method < 0 || method > 1 {
		return nil, errors.New("alpha: invalid method")
	}
	if filter < FilterNone || filter > FilterGradient {
		return nil, errors.New("alpha: invalid filter")
	}

	plane := alpha
	preprocessBits := byte(0)
	if preprocess {
		plane = QuantizeAlpha(alpha, defaultQuantizeLevels)
		preprocessBits = 1
	}

	filtered := ApplyFilter(plane, width, height, filter)

	compressed, err := Compress(filtered, width, height, method)
	if err != nil {
		return nil, err
	}

	flags := byte(method) | (byte(filter) << 2) | (preprocessBits << 4)
	out := make([]byte, 1+len(compressed))
	out[0] = flags
	copy(out[1:], compressed)
	return out, nil
}

// EncodeALPH encodes an alpha channel into ALPH chunk data (without the RIFF chunk header).
// The returned bytes begin with a flags byte followed by compressed data.
//
// Flags byte layout (per WebP spec):
//   bits 0-1: compression method (0=none, 1=VP8L)
//   bits 2-3: filter type (0=none, 1=horizontal, 2=vertical, 3=gradient)
//   bits 4-5: pre-processing (0=none) — always 0 here
//   bits 6-7: reserved (0)
func EncodeALPH(alpha []byte, width, height int, method int, filter FilterType) ([]byte, error) {
	if len(alpha) != width*height {
		return nil, errors.New("alpha: alpha plane size mismatch")
	}
	if method < 0 || method > 1 {
		return nil, errors.New("alpha: invalid method")
	}
	if filter < FilterNone || filter > FilterGradient {
		return nil, errors.New("alpha: invalid filter")
	}

	filtered := ApplyFilter(alpha, width, height, filter)

	compressed, err := Compress(filtered, width, height, method)
	if err != nil {
		return nil, err
	}

	flags := byte(method) | (byte(filter) << 2)
	out := make([]byte, 1+len(compressed))
	out[0] = flags
	copy(out[1:], compressed)
	return out, nil
}

// DecodeALPH decodes ALPH chunk data (flags byte + compressed payload) and returns
// the raw alpha plane (width*height bytes).
func DecodeALPH(data []byte, width, height int) ([]byte, error) {
	if len(data) < 1 {
		return nil, errors.New("alpha: ALPH chunk too short")
	}

	flags := data[0]
	method := int(flags & 0x03)
	filter := FilterType((flags >> 2) & 0x03)
	payload := data[1:]

	decompressed, err := Decompress(payload, width, height, method)
	if err != nil {
		return nil, err
	}

	if len(decompressed) != width*height {
		return nil, errors.New("alpha: decompressed size mismatch")
	}

	return ReverseFilter(decompressed, width, height, filter), nil
}
