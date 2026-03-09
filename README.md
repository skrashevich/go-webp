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

| Chunk | Encode | Decode | Notes |
|---|---|---|---|
| `VP8 ` (lossy) | Yes | Yes | Keyframe-only (intra-frame) |
| `VP8L` (lossless) | Yes | Yes | Full ARGB with alpha |
| `VP8X` (extended) | Yes | Yes | Flags: alpha, animation, ICC, EXIF, XMP |
| `ALPH` (alpha) | Yes | Yes | Filters: none, horizontal, vertical, gradient. Compression: raw or VP8L. |
| `ANIM` (animation params) | Yes | Yes | Background color (BGRA), loop count |
| `ANMF` (animation frame) | Yes | Yes | Offset, duration, dispose/blend flags |
| `ICCP` (ICC profile) | Yes | Yes | Raw bytes round-trip via `EncodeWithMetadata`/`DecodeWithMetadata` |
| `EXIF` (metadata) | Yes | Yes | Raw bytes round-trip via `EncodeWithMetadata`/`DecodeWithMetadata` |
| `XMP ` (metadata) | Yes | Yes | Raw bytes round-trip via `EncodeWithMetadata`/`DecodeWithMetadata` |

### VP8 Lossy Codec ([RFC 6386](https://datatracker.ietf.org/doc/html/rfc6386))

| Feature | Status | Notes |
|---|---|---|
| Keyframe (I-frame) encoding/decoding | Yes | Full intra-prediction, DCT, arithmetic coding |
| Inter-frame (P-frame) | No | Not used in WebP — each frame is an independent keyframe |
| YCbCr 4:2:0 color space | Yes | |
| Macroblock prediction modes | Yes | DC, V, H, TM, B_PRED (all 10 sub-modes) |
| Loop filtering | Yes | Normal and simple modes |
| Segmentation | Yes | Up to 4 segments with per-segment quantization |
| DCT partitions | Yes | Up to 8 parallel decode partitions |
| Quality control | Yes | 0–100 range |

### VP8L Lossless Codec ([Lossless Bitstream Spec](https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification))

| Feature | Status | Notes |
|---|---|---|
| ARGB pixel encoding | Yes | Full alpha channel support |
| Huffman coding | Yes | |
| LZ77 back-references | Yes | |
| Predictor transform | Yes | Decode only |
| Color (cross-color) transform | Yes | Decode only |
| Subtract green transform | Yes | Decode only |
| Color indexing (palette) transform | Yes | Decode only |

### Animation

| Feature | Status | Notes |
|---|---|---|
| Frame encoding (VP8/VP8L per frame) | Yes | Each frame is an independent keyframe |
| Canvas compositing | Yes | Full canvas state tracking |
| Dispose methods | Yes | DisposeNone, DisposeBackground |
| Blend methods | Yes | BlendAlpha (over), BlendNone (replace) |
| Frame offsets | Yes | Even-pixel aligned X/Y offsets |
| Loop count | Yes | 0 = infinite |
| Background color | Yes | BGRA encoding |

### Alpha Channel (ALPH)

| Feature | Status | Notes |
|---|---|---|
| No compression (raw) | Yes | Method 0 |
| VP8L lossless compression | Yes | Method 1 (green channel encoding) |
| Filter: none | Yes | |
| Filter: horizontal | Yes | Predictive: left pixel |
| Filter: vertical | Yes | Predictive: top pixel |
| Filter: gradient | Yes | Predictive: clamp(left + top - topleft) |
| Preprocessing (level reduction) | Yes | Quantization to 64 levels via `EncodeALPHWithPreprocessing` |

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
- **VP8L transforms are decode-only.** The lossless encoder uses a simplified path without predictor/color/palette transforms.

## License

Apache 2.0 — see [LICENSE](LICENSE).
