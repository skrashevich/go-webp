# go-webp

Pure Go encoder and decoder for the [WebP](https://developers.google.com/speed/webp) image format. No CGO, no libwebp — just Go.

Supports **lossy (VP8)**, **lossless (VP8L)**, **extended format (VP8X)** with alpha channel in lossy mode, **animation**, and **metadata (ICC, EXIF, XMP)**.

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
| `ICCP` (ICC profile) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) `TestRoundTripICCProfile`, `TestVP8XFlagsOnlyICC` |
| `EXIF` (metadata) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) `TestRoundTripEXIF`, `TestChunkOrderingEXIFAfterImage` |
| `XMP ` (metadata) | Yes | Yes | Raw bytes round-trip | [metadata_test.go](metadata_test.go) `TestRoundTripXMP` |

### VP8 Lossy Codec ([RFC 6386](https://datatracker.ietf.org/doc/html/rfc6386))

| Feature | Status | Notes | Tests |
|---|---|---|---|
| Keyframe (I-frame) encoding/decoding | Yes | Full intra-prediction, DCT, arithmetic coding | [conformance_test.go](internal/vp8/conformance_test.go) `TestVP8RoundtripSolidColors`, `TestVP8RoundtripGradients` |
| Inter-frame (P-frame) | No | Not used in WebP — each frame is an independent keyframe | — |
| YCbCr 4:2:0 color space | Yes | | [conformance_test.go](internal/vp8/conformance_test.go) `TestVP8ColorPreservation` |
| Macroblock prediction modes | Yes | DC, V, H, TM, B_PRED (all 10 sub-modes) | [conformance_test.go](internal/vp8/conformance_test.go) `TestVP8PredictionModes` |
| Loop filtering | Yes | Normal and simple modes | [crosscmp_nofilter_test.go](internal/vp8/crosscmp_nofilter_test.go), [crosscmp_gradient_test.go](internal/vp8/crosscmp_gradient_test.go) |
| Segmentation | Yes | Up to 4 segments with per-segment quantization | [crosscmp_seg_test.go](internal/vp8/crosscmp_seg_test.go) |
| DCT partitions | Yes | Up to 8 parallel decode partitions | [dct_roundtrip_test.go](internal/vp8/dct_roundtrip_test.go), [coeff_roundtrip_test.go](internal/vp8/coeff_roundtrip_test.go) |
| Boolean arithmetic coder | Yes | Encode + decode | [bool_test.go](internal/vp8/bool_test.go) |
| Quality control | Yes | 0–100 range | [conformance_test.go](internal/vp8/conformance_test.go) `TestVP8QualityScaling` |

### VP8L Lossless Codec ([Lossless Bitstream Spec](https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification))

| Feature | Status | Notes | Tests |
|---|---|---|---|
| ARGB pixel encoding | Yes | Full alpha channel support | [conformance_test.go](internal/vp8l/conformance_test.go) `TestVP8LRoundtripTransparency` |
| Huffman coding | Yes | Package-merge length-limited codes | [conformance_test.go](internal/vp8l/conformance_test.go) `TestVP8LHuffmanTableValidity`, `TestVP8LKraftInequality` |
| LZ77 back-references | Yes | Encode + decode | [encode_lz77_test.go](internal/vp8l/encode_lz77_test.go) `TestLZ77BasicRoundTrip`, `TestLZ77CompressionImprovement` |
| Predictor transform | Yes | Encode + decode (14 modes) | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) `TestPredictorTransformEncodeDecode`, `TestPredictorAllModes` |
| Color (cross-color) transform | Yes | Encode + decode | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) `TestColorTransformRoundTrip`, `TestColorTransformCoefficients` |
| Subtract green transform | Yes | Encode + decode | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) `TestSubtractGreenTransformEncodeDecode` |
| Color indexing (palette) transform | Yes | Encode + decode (1–256 colors) | [encode_transforms_test.go](internal/vp8l/encode_transforms_test.go) `TestPaletteTransform2Colors`, `TestPaletteTransform256Colors` |

### Animation

| Feature | Status | Notes | Tests |
|---|---|---|---|
| Frame encoding (VP8/VP8L per frame) | Yes | Each frame is an independent keyframe | [anim_test.go](internal/anim/anim_test.go) `TestEncodeDecodeRoundTrip` |
| Canvas compositing | Yes | Full canvas state tracking | [anim_test.go](internal/anim/anim_test.go) `TestComposeMultiFrame` |
| Dispose methods | Yes | DisposeNone, DisposeBackground | [anim_test.go](internal/anim/anim_test.go) `TestComposeFrameDisposeNone`, `TestComposeFrameDisposeBackground` |
| Blend methods | Yes | BlendAlpha (over), BlendNone (replace) | [anim_test.go](internal/anim/anim_test.go) `TestComposeFrameBlendAlpha`, `TestComposeFrameBlendNone` |
| Frame offsets | Yes | Even-pixel aligned X/Y offsets | [anim_test.go](internal/anim/anim_test.go) `TestComposeFrameOffset`, `TestEncodeDecodeWithOffsets` |
| Loop count | Yes | 0 = infinite | [vp8x_test.go](internal/riff/vp8x_test.go) `TestParseANIM_InfiniteLoop` |
| Background color | Yes | BGRA encoding | [vp8x_test.go](internal/riff/vp8x_test.go) `TestParseANIM_Basic`, `TestANIM_Encode_RoundTrip` |

### Alpha Channel (ALPH)

| Feature | Status | Notes | Tests |
|---|---|---|---|
| No compression (raw) | Yes | Method 0 | [alpha_test.go](internal/alpha/alpha_test.go) `TestCompressDecompressRawMethod0` |
| VP8L lossless compression | Yes | Method 1 (green channel encoding) | [alpha_test.go](internal/alpha/alpha_test.go) `TestCompressDecompressVP8LMethod1` |
| Filter: none | Yes | | [alpha_test.go](internal/alpha/alpha_test.go) `TestFilterNoneRoundTrip` |
| Filter: horizontal | Yes | Predictive: left pixel | [alpha_test.go](internal/alpha/alpha_test.go) `TestFilterHorizontalRoundTrip` |
| Filter: vertical | Yes | Predictive: top pixel | [alpha_test.go](internal/alpha/alpha_test.go) `TestFilterVerticalRoundTrip` |
| Filter: gradient | Yes | Predictive: clamp(left + top - topleft) | [alpha_test.go](internal/alpha/alpha_test.go) `TestFilterGradientRoundTrip` |
| Preprocessing (level reduction) | Yes | Quantization to 64 levels | [alpha_test.go](internal/alpha/alpha_test.go) `TestEncodeALPHWithPreprocessing_RoundTrip`, `TestQuantizeAlpha_KnownValues` |

## Codec Details

| | VP8 (Lossy) | VP8L (Lossless) |
|---|---|---|
| **Spec** | RFC 6386 | WebP Lossless Bitstream Spec |
| **Color space** | YCbCr 4:2:0 | ARGB |
| **Output type** | `*image.YCbCr` (or `*image.NRGBA` with ALPH) | `*image.NRGBA` |
| **Quality param** | 0–100 (default 75) | N/A |
| **Alpha** | Yes (via ALPH chunk) | Yes (native) |
| **Compression** | DCT + arithmetic coding | Huffman + LZ77 |

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

- **Metadata chunks are stored as raw bytes.** ICC profiles, EXIF, and XMP are round-tripped but not parsed or interpreted.

## License

Apache 2.0 — see [LICENSE](LICENSE).
