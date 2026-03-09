# go-webp

[![Go Reference](https://pkg.go.dev/badge/github.com/skrashevich/go-webp.svg)](https://pkg.go.dev/github.com/skrashevich/go-webp)
[![DeepWiki](https://img.shields.io/badge/DeepWiki-skrashevich%2Fgo--webp-blue.svg?logo=data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjQiIGhlaWdodD0iMjQiIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0ibm9uZSIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cGF0aCBkPSJNNCAxOGgxNk00IDEyaDE2TTQgNmgxNiIgc3Ryb2tlPSIjZmZmIiBzdHJva2Utd2lkdGg9IjIiIHN0cm9rZS1saW5lY2FwPSJyb3VuZCIvPjwvc3ZnPg==)](https://deepwiki.com/skrashevich/go-webp)

Pure Go encoder and decoder for the [WebP](https://developers.google.com/speed/webp) image format. No CGO, no libwebp — just Go.

Supports **lossy (VP8)**, **lossless (VP8L)**, **extended format (VP8X)** with alpha channel in lossy mode, **animation**, and **metadata (ICC, EXIF, XMP)**.

363 tests across 8 packages.

## Install

```bash
go get github.com/skrashevich/go-webp
```

Requires Go 1.21+.

## Quick Start

### Decode

```go
package main

import (
	"image/png"
	"os"

	"github.com/skrashevich/go-webp"
)

func main() {
	f, _ := os.Open("photo.webp")
	defer f.Close()

	img, err := webp.Decode(f)
	if err != nil {
		panic(err)
	}

	out, _ := os.Create("photo.png")
	defer out.Close()
	png.Encode(out, img)
}
```

### Encode (lossless)

```go
img, _, _ := image.Decode(input)

f, _ := os.Create("output.webp")
defer f.Close()

err := webp.Encode(f, img, nil) // nil opts = lossless
```

### Encode (lossy)

```go
err := webp.Encode(f, img, &webp.Options{
	Lossy:   true,
	Quality: 80, // 0–100
})
```

### Lossy with alpha

When encoding in lossy mode with an image that has transparency, the encoder
automatically uses the VP8X extended format with a separate ALPH chunk:

```go
// Transparent NRGBA image → lossy+alpha WebP (VP8X + ALPH + VP8)
err := webp.Encode(f, nrgbaImg, &webp.Options{
	Lossy:   true,
	Quality: 80,
})
```

### Metadata (ICC, EXIF, XMP)

```go
// Encode with metadata
meta := &webp.Metadata{
	ICCProfile: iccBytes,
	EXIF:       exifBytes,
	XMP:        xmpBytes,
}
err := webp.EncodeWithMetadata(f, img, nil, meta) // nil opts = lossless

// Decode with metadata
img, meta, err := webp.DecodeWithMetadata(f)
if meta != nil {
	// meta.ICCProfile, meta.EXIF, meta.XMP — raw bytes
}
```

### Animation

```go
import "github.com/skrashevich/go-webp/internal/anim"

// Encode
animation := &anim.Animation{
	Width:  320,
	Height: 240,
	LoopCount: 0, // 0 = infinite
	Frames: []anim.Frame{
		{Image: frame1, Duration: 100, Dispose: anim.DisposeNone, Blend: anim.BlendNone},
		{Image: frame2, Duration: 100, Dispose: anim.DisposeBackground, Blend: anim.BlendAlpha},
	},
}
err := webp.EncodeAnimation(f, animation, &anim.AnimationOptions{
	Lossy:   false, // VP8L lossless frames
})

// Decode
f, _ := os.Open("animated.webp")
animation, err := webp.DecodeAnimation(f)
// animation.Frames contains individual frames
// Use anim.Compose(animation) to get fully composited frame images
```

### Auto-detection via `image.Decode`

The package registers itself with Go's `image` package on import, so standard decoding works out of the box:

```go
import (
	"image"
	_ "github.com/skrashevich/go-webp" // register WebP format
)

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f) // WebP detected automatically
	return img, err
}
```

## API

### Types

```go
type Options struct {
	Lossy   bool    // true = VP8 lossy, false = VP8L lossless (default)
	Quality float32 // encoding quality for lossy mode [0, 100]; ignored for lossless
}
```

### Functions

| Function | Description |
|---|---|
| `Decode(r io.Reader) (image.Image, error)` | Decode a WebP image (VP8, VP8L, or VP8X). For animated files, returns the first composed frame. |
| `DecodeConfig(r io.Reader) (image.Config, error)` | Read dimensions and color model without decoding pixel data. |
| `Encode(w io.Writer, img image.Image, opts *Options) error` | Encode to WebP. Lossy images with alpha automatically use VP8X+ALPH. |
| `DecodeAnimation(r io.Reader) (*anim.Animation, error)` | Decode an animated WebP into individual frames with timing and compositing metadata. |
| `EncodeAnimation(w io.Writer, a *anim.Animation, opts *anim.AnimationOptions) error` | Encode an animated WebP with multiple frames. |
| `DecodeWithMetadata(r io.Reader) (image.Image, *Metadata, error)` | Decode a WebP image and return embedded ICC/EXIF/XMP metadata. |
| `EncodeWithMetadata(w io.Writer, img image.Image, opts *Options, meta *Metadata) error` | Encode to WebP with optional metadata chunks. |

## Spec Conformance

### WebP Container Format ([RIFF Container Spec](https://developers.google.com/speed/webp/docs/riff_container))

| Chunk | Encode | Decode | Notes | Tests |
|---|---|---|---|---|
| `VP8 ` (lossy) | Yes | Yes | Keyframe-only (intra-frame) | [conformance_test.go](internal/vp8/conformance_test.go), [crosscmp_test.go](internal/vp8/crosscmp_test.go) |
| `VP8L` (lossless) | Yes | Yes | Full ARGB with alpha | [conformance_test.go](internal/vp8l/conformance_test.go), [vp8l_test.go](internal/vp8l/vp8l_test.go) |
| `VP8X` (extended) | Yes | Yes | Flags: alpha, animation, ICC, EXIF, XMP | [vp8x_test.go](internal/riff/vp8x_test.go) |
| `ALPH` (alpha) | Yes | Yes | Filters: none, horizontal, vertical, gradient. Compression: raw or VP8L. | [alpha_test.go](internal/alpha/alpha_test.go) |
| `ANIM` (animation params) | Yes | Yes | Background color (BGRA), loop count | [vp8x_test.go](internal/riff/vp8x_test.go), [anim_test.go](internal/anim/anim_test.go) |
| `ANMF` (animation frame) | Yes | Yes | Offset, duration, dispose/blend flags | [vp8x_test.go](internal/riff/vp8x_test.go), [anim_test.go](internal/anim/anim_test.go) |
| `ICCP` (ICC profile) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) |
| `EXIF` (metadata) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) |
| `XMP ` (metadata) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) |

### VP8 Lossy Codec ([RFC 6386](https://datatracker.ietf.org/doc/html/rfc6386))

| Feature | Encode | Decode | Notes | Tests |
|---|---|---|---|---|
| Keyframe (I-frame) | Yes | Yes | Full intra-prediction, DCT, boolean arithmetic coding | [conformance_test.go](internal/vp8/conformance_test.go) |
| Inter-frame (P-frame) | — | — | Not used in WebP; each frame is an independent keyframe | — |
| YCbCr 4:2:0 color space | Yes | Yes | Standard WebP color subsampling | [conformance_test.go](internal/vp8/conformance_test.go) |
| Macroblock prediction modes | Yes | Yes | DC, V, H, TM, B_PRED (all 10 sub-modes) | [conformance_test.go](internal/vp8/conformance_test.go) |
| Loop filtering | Yes | Yes | Normal and simple modes | [crosscmp_nofilter_test.go](internal/vp8/crosscmp_nofilter_test.go), [crosscmp_gradient_test.go](internal/vp8/crosscmp_gradient_test.go) |
| Segmentation | Yes | Yes | Up to 4 segments with per-segment quantization | [crosscmp_seg_test.go](internal/vp8/crosscmp_seg_test.go) |
| DCT partitions | Yes | Yes | Up to 8 parallel decode partitions | [dct_roundtrip_test.go](internal/vp8/dct_roundtrip_test.go), [coeff_roundtrip_test.go](internal/vp8/coeff_roundtrip_test.go) |
| Boolean arithmetic coder | Yes | Yes | Full encode + decode | [bool_test.go](internal/vp8/bool_test.go) |
| Quality control | Yes | — | 0–100 range mapping to quantizer parameters | [conformance_test.go](internal/vp8/conformance_test.go) |

### VP8L Lossless Codec ([Lossless Bitstream Spec](https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification))

| Feature | Encode | Decode | Notes | Tests |
|---|---|---|---|---|
| ARGB pixel encoding | Yes | Yes | Full alpha channel support | [conformance_test.go](internal/vp8l/conformance_test.go) |
| Huffman coding | Yes | Yes | Package-merge length-limited codes | [conformance_test.go](internal/vp8l/conformance_test.go) |
| LZ77 back-references | Yes | Yes | Distance-to-plane mapping per spec | [encode_lz77_test.go](internal/vp8l/encode_lz77_test.go) |
| Color cache | — | Yes | Decode-only; encoder writes `use_color_cache=0` | [validation_test.go](internal/vp8l/validation_test.go) |
| Meta prefix codes (entropy image) | — | Yes | Decode-only; encoder uses single Huffman group | — |
| Predictor transform | Yes | Yes | All 14 spatial prediction modes | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) |
| Color (cross-color) transform | Yes | Yes | Channel correlation decorrelation | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) |
| Subtract green transform | Yes | Yes | Green channel subtraction from R and B | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) |
| Color indexing (palette) transform | Yes | Yes | 1–256 colors with pixel packing | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) |

### Animation

| Feature | Encode | Decode | Notes | Tests |
|---|---|---|---|---|
| Frame encoding (VP8/VP8L per frame) | Yes | Yes | Each frame is an independent keyframe | [anim_test.go](internal/anim/anim_test.go) |
| Canvas compositing | Yes | Yes | Full canvas state tracking | [anim_test.go](internal/anim/anim_test.go) |
| Dispose methods | Yes | Yes | DisposeNone, DisposeBackground | [anim_test.go](internal/anim/anim_test.go) |
| Blend methods | Yes | Yes | BlendAlpha (over), BlendNone (replace) | [anim_test.go](internal/anim/anim_test.go) |
| Frame offsets | Yes | Yes | Even-pixel aligned X/Y offsets | [anim_test.go](internal/anim/anim_test.go) |
| Loop count | Yes | Yes | 0 = infinite | [vp8x_test.go](internal/riff/vp8x_test.go) |
| Background color | Yes | Yes | BGRA encoding | [vp8x_test.go](internal/riff/vp8x_test.go) |

### Alpha Channel (ALPH)

| Feature | Encode | Decode | Notes | Tests |
|---|---|---|---|---|
| No compression (raw) | Yes | Yes | Method 0 | [alpha_test.go](internal/alpha/alpha_test.go) |
| VP8L lossless compression | Yes | Yes | Method 1 (green channel encoding) | [alpha_test.go](internal/alpha/alpha_test.go) |
| Filter: none | Yes | Yes | | [alpha_test.go](internal/alpha/alpha_test.go) |
| Filter: horizontal | Yes | Yes | Predictive: left pixel | [alpha_test.go](internal/alpha/alpha_test.go) |
| Filter: vertical | Yes | Yes | Predictive: top pixel | [alpha_test.go](internal/alpha/alpha_test.go) |
| Filter: gradient | Yes | Yes | Predictive: clamp(left + top − topleft) | [alpha_test.go](internal/alpha/alpha_test.go) |
| Preprocessing (level reduction) | Yes | Yes | Quantization to 64 levels | [alpha_test.go](internal/alpha/alpha_test.go) |

## Comparison with libwebp

This implementation covers all WebP features needed for correct encoding and decoding. The table below compares with Google's [libwebp](https://chromium.googlesource.com/webm/libwebp) reference implementation:

| Feature | go-webp | libwebp | Notes |
|---|---|---|---|
| VP8 lossy encode/decode | Yes | Yes | |
| VP8L lossless encode/decode | Yes | Yes | |
| VP8X extended format | Yes | Yes | |
| Alpha channel (ALPH) | Yes | Yes | All 4 filter types + VP8L compression |
| Animation (ANIM/ANMF) | Yes | Yes | Encode + decode + compositing |
| Metadata (ICC/EXIF/XMP) | Yes | Yes | Round-trip as raw bytes |
| VP8L transforms (all 4) | Yes | Yes | Predictor, color, subtract-green, palette |
| LZ77 back-references | Yes | Yes | |
| Color cache (VP8L) | Decode | Yes | Encoder does not use color cache yet |
| Meta prefix codes (VP8L) | Decode | Yes | Encoder uses single Huffman group |
| Near-lossless preprocessing | No | Yes | libwebp-specific quality/size optimization |
| Advanced rate control | No | Yes | libwebp multi-pass RD optimization |
| Incremental decoding | No | Yes | |
| CGO dependency | No | Yes (C) | go-webp is pure Go |

## Codec Details

| | VP8 (Lossy) | VP8L (Lossless) |
|---|---|---|
| **Spec** | [RFC 6386](https://datatracker.ietf.org/doc/html/rfc6386) | [WebP Lossless Bitstream Spec](https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification) |
| **Color space** | YCbCr 4:2:0 | ARGB |
| **Output type** | `*image.YCbCr` (or `*image.NRGBA` with ALPH) | `*image.NRGBA` |
| **Quality param** | 0–100 (default 75) | N/A |
| **Alpha** | Yes (via ALPH chunk) | Yes (native) |
| **Compression** | DCT + boolean arithmetic coding | Huffman + LZ77 |

## CLI Tool

The repository includes `webpconv`, a command-line converter between WebP, PNG, and JPEG.

```bash
go install github.com/skrashevich/go-webp/cmd/webpconv@latest
```

```bash
# PNG → WebP (lossless)
webpconv -o output.webp input.png

# PNG → WebP (lossy, quality 80)
webpconv -lossy -quality 80 -o output.webp input.png

# WebP → PNG
webpconv -o output.png input.webp
```

## Limitations

- **Color cache and meta prefix codes** are decoded but not yet used in the encoder. This does not affect correctness — output files are valid per spec — but means lossless compression ratios are not yet optimal compared to libwebp.
- **Metadata chunks are stored as raw bytes.** ICC profiles, EXIF, and XMP are round-tripped but not parsed or interpreted.
- **No near-lossless mode.** libwebp offers a near-lossless preprocessing step that is not part of the WebP spec; this implementation does not replicate it.

## License

Apache 2.0 — see [LICENSE](LICENSE).
