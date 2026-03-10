package webp_test

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	webp "github.com/skrashevich/go-webp"
)

// newTestImage creates a small NRGBA image for use in tests.
func newTestImage(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	return img
}

// roundTrip encodes img with opts+meta, then decodes it and returns the decoded
// image and metadata.
func roundTrip(t *testing.T, img image.Image, opts *webp.Options, meta *webp.Metadata) (image.Image, *webp.Metadata) {
	t.Helper()
	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, opts, meta); err != nil {
		t.Fatalf("EncodeWithMetadata: %v", err)
	}
	got, gotMeta, err := webp.DecodeWithMetadata(&buf)
	if err != nil {
		t.Fatalf("DecodeWithMetadata: %v", err)
	}
	return got, gotMeta
}

// TestRoundTripICCProfile verifies ICC profile data survives encode->decode.
func TestRoundTripICCProfile(t *testing.T) {
	icc := []byte("fake-icc-profile-data")
	meta := &webp.Metadata{ICCProfile: icc}
	img := newTestImage(8, 8)

	_, gotMeta := roundTrip(t, img, nil, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata, want non-nil")
	}
	if !bytes.Equal(gotMeta.ICCProfile, icc) {
		t.Errorf("ICCProfile mismatch: got %q, want %q", gotMeta.ICCProfile, icc)
	}
}

// TestRoundTripEXIF verifies EXIF data survives encode->decode.
func TestRoundTripEXIF(t *testing.T) {
	exif := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00} // minimal TIFF/EXIF header
	meta := &webp.Metadata{EXIF: exif}
	img := newTestImage(8, 8)

	_, gotMeta := roundTrip(t, img, nil, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata, want non-nil")
	}
	if !bytes.Equal(gotMeta.EXIF, exif) {
		t.Errorf("EXIF mismatch: got %v, want %v", gotMeta.EXIF, exif)
	}
}

// TestRoundTripXMP verifies XMP data survives encode->decode.
func TestRoundTripXMP(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"/><?xpacket end="w"?>`)
	meta := &webp.Metadata{XMP: xmp}
	img := newTestImage(8, 8)

	_, gotMeta := roundTrip(t, img, nil, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata, want non-nil")
	}
	if !bytes.Equal(gotMeta.XMP, xmp) {
		t.Errorf("XMP mismatch: got %q, want %q", gotMeta.XMP, xmp)
	}
}

// TestRoundTripAllMetadata verifies all three metadata types survive together.
func TestRoundTripAllMetadata(t *testing.T) {
	meta := &webp.Metadata{
		ICCProfile: []byte("icc-data"),
		EXIF:       []byte("exif-data"),
		XMP:        []byte("xmp-data"),
	}
	img := newTestImage(16, 16)

	_, gotMeta := roundTrip(t, img, nil, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata, want non-nil")
	}
	if !bytes.Equal(gotMeta.ICCProfile, meta.ICCProfile) {
		t.Errorf("ICCProfile: got %q, want %q", gotMeta.ICCProfile, meta.ICCProfile)
	}
	if !bytes.Equal(gotMeta.EXIF, meta.EXIF) {
		t.Errorf("EXIF: got %q, want %q", gotMeta.EXIF, meta.EXIF)
	}
	if !bytes.Equal(gotMeta.XMP, meta.XMP) {
		t.Errorf("XMP: got %q, want %q", gotMeta.XMP, meta.XMP)
	}
}

// TestEncodeWithoutMetadataBackwardCompat verifies nil metadata produces a
// valid simple VP8L file decodable by the standard Decode function.
func TestEncodeWithoutMetadataBackwardCompat(t *testing.T) {
	img := newTestImage(8, 8)

	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, nil, nil); err != nil {
		t.Fatalf("EncodeWithMetadata(nil meta): %v", err)
	}

	// Must also be decodable by Decode.
	decoded, err := webp.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Errorf("bounds: got %v, want %v", decoded.Bounds(), img.Bounds())
	}
}

// TestEncodeEmptyMetadataBackwardCompat verifies an empty Metadata struct (all
// nil fields) behaves the same as nil metadata.
func TestEncodeEmptyMetadataBackwardCompat(t *testing.T) {
	img := newTestImage(8, 8)

	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, nil, &webp.Metadata{}); err != nil {
		t.Fatalf("EncodeWithMetadata(empty meta): %v", err)
	}

	decoded, err := webp.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Errorf("bounds: got %v, want %v", decoded.Bounds(), img.Bounds())
	}
}

// TestVP8XFlagsOnlyICC verifies the VP8X chunk flags reflect only the ICC flag
// when only an ICC profile is present.
func TestVP8XFlagsOnlyICC(t *testing.T) {
	meta := &webp.Metadata{ICCProfile: []byte("icc")}
	img := newTestImage(4, 4)

	_, gotMeta := roundTrip(t, img, nil, meta)
	if gotMeta == nil {
		t.Fatal("got nil metadata")
	}
	if len(gotMeta.ICCProfile) == 0 {
		t.Error("ICCProfile not preserved")
	}
	if len(gotMeta.EXIF) != 0 {
		t.Errorf("unexpected EXIF data: %v", gotMeta.EXIF)
	}
	if len(gotMeta.XMP) != 0 {
		t.Errorf("unexpected XMP data: %v", gotMeta.XMP)
	}
}

// TestChunkOrderingICCBeforeImage verifies the ICCP chunk appears before the
// image chunk in the encoded output.
func TestChunkOrderingICCBeforeImage(t *testing.T) {
	meta := &webp.Metadata{ICCProfile: []byte("icc-profile")}
	img := newTestImage(4, 4)

	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, nil, meta); err != nil {
		t.Fatalf("EncodeWithMetadata: %v", err)
	}

	data := buf.Bytes()
	iccPos := bytes.Index(data, []byte("ICCP"))
	vp8lPos := bytes.Index(data, []byte("VP8L"))
	if iccPos < 0 {
		t.Fatal("ICCP chunk not found")
	}
	if vp8lPos < 0 {
		t.Fatal("VP8L chunk not found")
	}
	if iccPos > vp8lPos {
		t.Errorf("ICCP (%d) appears after VP8L (%d); want ICCP before VP8L", iccPos, vp8lPos)
	}
}

// TestChunkOrderingEXIFAfterImage verifies the EXIF chunk appears after the
// image chunk in the encoded output.
func TestChunkOrderingEXIFAfterImage(t *testing.T) {
	meta := &webp.Metadata{EXIF: []byte("exif-data")}
	img := newTestImage(4, 4)

	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, nil, meta); err != nil {
		t.Fatalf("EncodeWithMetadata: %v", err)
	}

	data := buf.Bytes()
	exifPos := bytes.Index(data, []byte("EXIF"))
	vp8lPos := bytes.Index(data, []byte("VP8L"))
	if exifPos < 0 {
		t.Fatal("EXIF chunk not found")
	}
	if vp8lPos < 0 {
		t.Fatal("VP8L chunk not found")
	}
	if exifPos < vp8lPos {
		t.Errorf("EXIF (%d) appears before VP8L (%d); want EXIF after VP8L", exifPos, vp8lPos)
	}
}

// TestMetadataWithLossyEncoding verifies metadata round-trip with VP8 lossy encoding.
func TestMetadataWithLossyEncoding(t *testing.T) {
	meta := &webp.Metadata{
		ICCProfile: []byte("icc-lossy"),
		EXIF:       []byte("exif-lossy"),
	}
	opts := &webp.Options{Lossy: true, Quality: 75}
	img := newTestImage(16, 16)

	_, gotMeta := roundTrip(t, img, opts, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata")
	}
	if !bytes.Equal(gotMeta.ICCProfile, meta.ICCProfile) {
		t.Errorf("ICCProfile: got %q, want %q", gotMeta.ICCProfile, meta.ICCProfile)
	}
	if !bytes.Equal(gotMeta.EXIF, meta.EXIF) {
		t.Errorf("EXIF: got %q, want %q", gotMeta.EXIF, meta.EXIF)
	}
}

// TestMetadataWithLosslessEncoding verifies metadata round-trip with VP8L lossless encoding.
func TestMetadataWithLosslessEncoding(t *testing.T) {
	meta := &webp.Metadata{
		XMP: []byte("<xmp>lossless</xmp>"),
	}
	opts := &webp.Options{Lossy: false}
	img := newTestImage(8, 8)

	_, gotMeta := roundTrip(t, img, opts, meta)

	if gotMeta == nil {
		t.Fatal("got nil metadata")
	}
	if !bytes.Equal(gotMeta.XMP, meta.XMP) {
		t.Errorf("XMP: got %q, want %q", gotMeta.XMP, meta.XMP)
	}
}

// TestDecodeWithMetadataSimpleVP8 verifies that DecodeWithMetadata on a plain
// VP8 file (no VP8X) returns nil metadata.
func TestDecodeWithMetadataSimpleVP8(t *testing.T) {
	img := newTestImage(8, 8)

	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, &webp.Options{Lossy: true, Quality: 80}); err != nil {
		t.Fatalf("Encode VP8: %v", err)
	}

	_, meta, err := webp.DecodeWithMetadata(&buf)
	if err != nil {
		t.Fatalf("DecodeWithMetadata: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil metadata for plain VP8 file, got %+v", meta)
	}
}

// TestDecodeWithMetadataSimpleVP8L verifies that DecodeWithMetadata on a plain
// VP8L file (no VP8X) returns nil metadata.
func TestDecodeWithMetadataSimpleVP8L(t *testing.T) {
	img := newTestImage(8, 8)

	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, &webp.Options{Lossy: false}); err != nil {
		t.Fatalf("Encode VP8L: %v", err)
	}

	_, meta, err := webp.DecodeWithMetadata(&buf)
	if err != nil {
		t.Fatalf("DecodeWithMetadata: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil metadata for plain VP8L file, got %+v", meta)
	}
}

// TestDecodeConfigVP8X verifies DecodeConfig works on a VP8X-wrapped file.
func TestDecodeConfigVP8X(t *testing.T) {
	meta := &webp.Metadata{ICCProfile: []byte("icc")}
	img := newTestImage(12, 10)

	var buf bytes.Buffer
	if err := webp.EncodeWithMetadata(&buf, img, nil, meta); err != nil {
		t.Fatalf("EncodeWithMetadata: %v", err)
	}

	cfg, err := webp.DecodeConfig(&buf)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width != 12 || cfg.Height != 10 {
		t.Errorf("DecodeConfig: got %dx%d, want 12x10", cfg.Width, cfg.Height)
	}
}
