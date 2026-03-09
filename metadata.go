package webp

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"github.com/skrashevich/go-webp/internal/alpha"
	"github.com/skrashevich/go-webp/internal/riff"
	"github.com/skrashevich/go-webp/internal/vp8"
	"github.com/skrashevich/go-webp/internal/vp8l"
)

// Metadata holds optional metadata chunks from a VP8X WebP file.
// All fields are raw bytes; the package does not interpret their content.
type Metadata struct {
	ICCProfile []byte // Raw ICC profile data (ICCP chunk)
	EXIF       []byte // Raw EXIF data (EXIF chunk)
	XMP        []byte // Raw XMP data (XMP  chunk)
}

// hasAny reports whether any metadata field is non-empty.
func (m *Metadata) hasAny() bool {
	return m != nil && (len(m.ICCProfile) > 0 || len(m.EXIF) > 0 || len(m.XMP) > 0)
}

// DecodeWithMetadata reads a WebP image from r and returns the decoded image
// together with any embedded metadata. For simple VP8/VP8L files (no VP8X
// wrapper) the returned *Metadata is nil.
func DecodeWithMetadata(r io.Reader) (image.Image, *Metadata, error) {
	if _, err := riff.ReadHeader(r); err != nil {
		return nil, nil, err
	}

	firstChunk, err := riff.ReadChunk(r)
	if err != nil {
		return nil, nil, fmt.Errorf("webp: reading first chunk: %w", err)
	}

	switch firstChunk.ID {
	case riff.FourCCVP8:
		img, err := vp8.NewDecoder(firstChunk.Data).Decode()
		return img, nil, err

	case riff.FourCCVP8L:
		img, err := vp8l.NewDecoder(firstChunk.Data).Decode()
		return img, nil, err

	case riff.FourCCVP8X:
		return decodeVP8XWithMetadata(firstChunk.Data, r)

	default:
		return nil, nil, fmt.Errorf("webp: unexpected first chunk: %s", firstChunk.ID)
	}
}

// decodeVP8XWithMetadata processes VP8X chunks, collecting metadata and
// decoding the image payload including alpha channel support.
func decodeVP8XWithMetadata(vp8xData []byte, r io.Reader) (image.Image, *Metadata, error) {
	if _, err := riff.ParseVP8X(vp8xData); err != nil {
		return nil, nil, fmt.Errorf("webp: VP8X: %w", err)
	}

	meta := &Metadata{}
	var img image.Image

	chunks, err := riff.ReadAllChunks(r)
	if err != nil {
		return nil, nil, fmt.Errorf("webp: reading VP8X chunks: %w", err)
	}

	var alphData []byte

	for _, c := range chunks {
		switch c.ID {
		case riff.FourCCICCP:
			meta.ICCProfile = c.Data
		case riff.FourCCEXIF:
			meta.EXIF = c.Data
		case riff.FourCCXMP:
			meta.XMP = c.Data
		case riff.FourCCALPH:
			alphData = c.Data
		case riff.FourCCVP8:
			decoded, decErr := vp8.NewDecoder(c.Data).Decode()
			if decErr != nil {
				return nil, nil, fmt.Errorf("webp: VP8 decode in VP8X: %w", decErr)
			}
			if alphData != nil {
				img, err = applyAlphaToVP8(decoded, alphData)
				if err != nil {
					return nil, nil, err
				}
			} else {
				img = decoded
			}
		case riff.FourCCVP8L:
			img, err = vp8l.NewDecoder(c.Data).Decode()
			if err != nil {
				return nil, nil, fmt.Errorf("webp: VP8L decode in VP8X: %w", err)
			}
		}
	}

	if img == nil {
		return nil, nil, fmt.Errorf("webp: VP8X container has no image chunk")
	}

	if !meta.hasAny() {
		meta = nil
	}

	return img, meta, nil
}

// EncodeWithMetadata writes img to w in WebP format. When meta is non-nil and
// contains at least one non-empty field the file is wrapped in a VP8X
// container with the appropriate flags set. Chunk order follows the WebP spec:
// VP8X -> ICCP -> (ALPH+)VP8 or VP8L -> EXIF -> XMP.
func EncodeWithMetadata(w io.Writer, img image.Image, opts *Options, meta *Metadata) error {
	if opts == nil {
		opts = &Options{Lossy: false}
	}

	// If no metadata, fall back to simple encoding.
	if !meta.hasAny() {
		return Encode(w, img, opts)
	}

	// Encode the image payload.
	var (
		imageChunkID riff.FourCC
		imagePayload []byte
		alphPayload  []byte
		encErr       error
	)
	if opts.Lossy {
		enc := vp8.NewEncoder(opts.Quality)
		imagePayload, encErr = enc.Encode(img)
		if encErr != nil {
			return fmt.Errorf("webp: VP8 encode: %w", encErr)
		}
		imageChunkID = riff.FourCCVP8

		// Handle alpha for lossy.
		if imageHasAlpha(img) {
			bounds := img.Bounds()
			alphaPlane := alpha.ExtractAlpha(img)
			alphPayload, encErr = alpha.EncodeALPH(alphaPlane, bounds.Dx(), bounds.Dy(), 1, alpha.FilterNone)
			if encErr != nil {
				return fmt.Errorf("webp: encoding alpha: %w", encErr)
			}
		}
	} else {
		enc := vp8l.NewEncoder()
		imagePayload, encErr = enc.Encode(img)
		if encErr != nil {
			return fmt.Errorf("webp: VP8L encode: %w", encErr)
		}
		imageChunkID = riff.FourCCVP8L
	}

	// Build VP8X flags.
	bounds := img.Bounds()
	var flags riff.VP8XFlags
	if len(meta.ICCProfile) > 0 {
		flags |= riff.VP8XFlagICC
	}
	if len(meta.EXIF) > 0 {
		flags |= riff.VP8XFlagExif
	}
	if len(meta.XMP) > 0 {
		flags |= riff.VP8XFlagXMP
	}
	if alphPayload != nil {
		flags |= riff.VP8XFlagAlpha
	}

	vp8xChunk := &riff.VP8XChunk{
		Flags:  flags,
		Width:  uint32(bounds.Dx() - 1),
		Height: uint32(bounds.Dy() - 1),
	}

	// Accumulate all chunks into a buffer so we can compute the RIFF file size.
	var body bytes.Buffer

	// VP8X chunk (always first after WEBP).
	if err := riff.WriteChunk(&body, riff.FourCCVP8X, vp8xChunk.Encode()); err != nil {
		return err
	}
	// ICCP before image data.
	if len(meta.ICCProfile) > 0 {
		if err := riff.WriteChunk(&body, riff.FourCCICCP, meta.ICCProfile); err != nil {
			return err
		}
	}
	// ALPH chunk before VP8 (if lossy with alpha).
	if alphPayload != nil {
		if err := riff.WriteChunk(&body, riff.FourCCALPH, alphPayload); err != nil {
			return err
		}
	}
	// Image chunk.
	if err := riff.WriteChunk(&body, imageChunkID, imagePayload); err != nil {
		return err
	}
	// EXIF and XMP after image data.
	if len(meta.EXIF) > 0 {
		if err := riff.WriteChunk(&body, riff.FourCCEXIF, meta.EXIF); err != nil {
			return err
		}
	}
	if len(meta.XMP) > 0 {
		if err := riff.WriteChunk(&body, riff.FourCCXMP, meta.XMP); err != nil {
			return err
		}
	}

	// RIFF file size = 4 ("WEBP") + body.
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
