package riff_test

import (
	"bytes"
	"testing"

	"github.com/skrashevich/go-webp/internal/riff"
)

// --- VP8X ---

func TestParseVP8X_AllFlags(t *testing.T) {
	// flags = ICC(5) | alpha(4) | EXIF(3) | XMP(2) | animation(1)
	flags := riff.VP8XFlagICC | riff.VP8XFlagAlpha | riff.VP8XFlagExif | riff.VP8XFlagXMP | riff.VP8XFlagAnimation
	// width-1 = 999, height-1 = 599 stored as uint24 LE
	data := []byte{
		byte(flags), byte(flags >> 8), byte(flags >> 16), byte(flags >> 24),
		231, 3, 0, // 999 as uint24 LE (231 + 3*256 = 999)
		87, 2, 0,  // 599 as uint24 LE (87 + 2*256 = 599)
	}
	chunk, err := riff.ParseVP8X(data)
	if err != nil {
		t.Fatalf("ParseVP8X: %v", err)
	}
	if chunk.Flags != flags {
		t.Errorf("Flags: got %v, want %v", chunk.Flags, flags)
	}
	if chunk.Width != 999 {
		t.Errorf("Width: got %d, want 999", chunk.Width)
	}
	if chunk.Height != 599 {
		t.Errorf("Height: got %d, want 599", chunk.Height)
	}
}

func TestParseVP8X_ZeroDimensions(t *testing.T) {
	data := make([]byte, 10)
	chunk, err := riff.ParseVP8X(data)
	if err != nil {
		t.Fatalf("ParseVP8X zero dims: %v", err)
	}
	if chunk.Width != 0 || chunk.Height != 0 {
		t.Errorf("expected 0,0 got %d,%d", chunk.Width, chunk.Height)
	}
}

func TestParseVP8X_MaxDimensions(t *testing.T) {
	// max uint24 = 16777215
	data := []byte{
		0, 0, 0, 0, // no flags
		0xFF, 0xFF, 0xFF, // width-1 = 16777215
		0xFF, 0xFF, 0xFF, // height-1 = 16777215
	}
	chunk, err := riff.ParseVP8X(data)
	if err != nil {
		t.Fatalf("ParseVP8X max dims: %v", err)
	}
	if chunk.Width != 16777215 {
		t.Errorf("Width: got %d, want 16777215", chunk.Width)
	}
	if chunk.Height != 16777215 {
		t.Errorf("Height: got %d, want 16777215", chunk.Height)
	}
}

func TestParseVP8X_TooShort(t *testing.T) {
	_, err := riff.ParseVP8X([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short VP8X data")
	}
}

func TestVP8XEncode_RoundTrip(t *testing.T) {
	orig := &riff.VP8XChunk{
		Flags:  riff.VP8XFlagAlpha | riff.VP8XFlagICC,
		Width:  1279,  // stored as width-1=1279 means canvas=1280
		Height: 719,
	}
	encoded := orig.Encode()
	if len(encoded) != 10 {
		t.Fatalf("Encode len: got %d, want 10", len(encoded))
	}
	parsed, err := riff.ParseVP8X(encoded)
	if err != nil {
		t.Fatalf("ParseVP8X after encode: %v", err)
	}
	if parsed.Flags != orig.Flags {
		t.Errorf("Flags mismatch: got %v, want %v", parsed.Flags, orig.Flags)
	}
	if parsed.Width != orig.Width {
		t.Errorf("Width mismatch: got %d, want %d", parsed.Width, orig.Width)
	}
	if parsed.Height != orig.Height {
		t.Errorf("Height mismatch: got %d, want %d", parsed.Height, orig.Height)
	}
}

func TestWriteVP8X_ChunkOnDisk(t *testing.T) {
	var buf bytes.Buffer
	err := riff.WriteVP8X(&buf, riff.VP8XFlagAlpha, 800, 600)
	if err != nil {
		t.Fatalf("WriteVP8X: %v", err)
	}
	// 8 header + 10 payload = 18 bytes (even, no padding needed)
	if buf.Len() != 18 {
		t.Fatalf("WriteVP8X: got %d bytes, want 18", buf.Len())
	}
	chunk, err := riff.ReadChunk(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	if chunk.ID != riff.FourCCVP8X {
		t.Errorf("ID: got %s, want VP8X", chunk.ID)
	}
	vp8x, err := riff.ParseVP8X(chunk.Data)
	if err != nil {
		t.Fatalf("ParseVP8X: %v", err)
	}
	if vp8x.Flags != riff.VP8XFlagAlpha {
		t.Errorf("Flags: got %v, want alpha", vp8x.Flags)
	}
	// WriteVP8X stores width-1 and height-1
	if vp8x.Width != 799 {
		t.Errorf("Width: got %d, want 799", vp8x.Width)
	}
	if vp8x.Height != 599 {
		t.Errorf("Height: got %d, want 599", vp8x.Height)
	}
}

// --- ALPH ---

func TestParseALPHHeader_NoCompression(t *testing.T) {
	h := riff.ParseALPHHeader(0x00)
	if h.Method != 0 {
		t.Errorf("Method: got %d, want 0", h.Method)
	}
	if h.Filter != 0 {
		t.Errorf("Filter: got %d, want 0", h.Filter)
	}
	if h.Preprocessing != 0 {
		t.Errorf("Preprocessing: got %d, want 0", h.Preprocessing)
	}
}

func TestParseALPHHeader_AllFields(t *testing.T) {
	// method=1 (bits 0-1), filter=3 (bits 2-3), preprocessing=1 (bits 4-5)
	b := byte(1 | (3 << 2) | (1 << 4))
	h := riff.ParseALPHHeader(b)
	if h.Method != 1 {
		t.Errorf("Method: got %d, want 1", h.Method)
	}
	if h.Filter != 3 {
		t.Errorf("Filter: got %d, want 3", h.Filter)
	}
	if h.Preprocessing != 1 {
		t.Errorf("Preprocessing: got %d, want 1", h.Preprocessing)
	}
}

func TestALPHHeader_Encode_RoundTrip(t *testing.T) {
	cases := []riff.ALPHHeader{
		{Method: 0, Filter: 0, Preprocessing: 0},
		{Method: 1, Filter: 1, Preprocessing: 0},
		{Method: 0, Filter: 2, Preprocessing: 1},
		{Method: 1, Filter: 3, Preprocessing: 1},
	}
	for _, orig := range cases {
		b := orig.Encode()
		got := riff.ParseALPHHeader(b)
		if got != orig {
			t.Errorf("round-trip %+v: got %+v", orig, got)
		}
	}
}

// --- ANIM ---

func TestParseANIM_Basic(t *testing.T) {
	// Background BGRA = 0x00FF00FF (blue=0, green=255, red=0, alpha=255)
	// Loop count = 3
	data := []byte{0xFF, 0x00, 0xFF, 0x00, 3, 0}
	a, err := riff.ParseANIM(data)
	if err != nil {
		t.Fatalf("ParseANIM: %v", err)
	}
	if a.BackgroundColor != 0x00FF00FF {
		t.Errorf("BackgroundColor: got 0x%08X, want 0x00FF00FF", a.BackgroundColor)
	}
	if a.LoopCount != 3 {
		t.Errorf("LoopCount: got %d, want 3", a.LoopCount)
	}
}

func TestParseANIM_InfiniteLoop(t *testing.T) {
	data := []byte{0, 0, 0, 0, 0, 0}
	a, err := riff.ParseANIM(data)
	if err != nil {
		t.Fatalf("ParseANIM: %v", err)
	}
	if a.LoopCount != 0 {
		t.Errorf("LoopCount: got %d, want 0 (infinite)", a.LoopCount)
	}
}

func TestParseANIM_TooShort(t *testing.T) {
	_, err := riff.ParseANIM([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short ANIM data")
	}
}

func TestANIM_Encode_RoundTrip(t *testing.T) {
	orig := riff.ANIMChunk{BackgroundColor: 0xDEADBEEF, LoopCount: 42}
	enc := orig.Encode()
	if len(enc) != 6 {
		t.Fatalf("Encode len: got %d, want 6", len(enc))
	}
	got, err := riff.ParseANIM(enc)
	if err != nil {
		t.Fatalf("ParseANIM: %v", err)
	}
	if got != orig {
		t.Errorf("round-trip: got %+v, want %+v", got, orig)
	}
}

// --- ANMF ---

func TestParseANMF_Basic(t *testing.T) {
	// X/2=10 => stored 10, Y/2=20 => stored 20
	// width-1=99, height-1=49, duration=100ms, flags=dispose|no_blend
	data := make([]byte, 16)
	// Frame X/2 = 10 (uint24 LE)
	data[0], data[1], data[2] = 10, 0, 0
	// Frame Y/2 = 20 (uint24 LE)
	data[3], data[4], data[5] = 20, 0, 0
	// Width-1 = 99 (uint24 LE)
	data[6], data[7], data[8] = 99, 0, 0
	// Height-1 = 49 (uint24 LE)
	data[9], data[10], data[11] = 49, 0, 0
	// Duration = 100 (uint24 LE)
	data[12], data[13], data[14] = 100, 0, 0
	// Flags: dispose=1 (bit 0), blend=1 (bit 1, means no_blend)
	data[15] = 0x03
	// No frame bitstream after header
	h, bs, err := riff.ParseANMF(data)
	if err != nil {
		t.Fatalf("ParseANMF: %v", err)
	}
	if h.X != 20 {
		t.Errorf("X: got %d, want 20 (10*2)", h.X)
	}
	if h.Y != 40 {
		t.Errorf("Y: got %d, want 40 (20*2)", h.Y)
	}
	if h.Width != 100 {
		t.Errorf("Width: got %d, want 100 (99+1)", h.Width)
	}
	if h.Height != 50 {
		t.Errorf("Height: got %d, want 50 (49+1)", h.Height)
	}
	if h.Duration != 100 {
		t.Errorf("Duration: got %d, want 100", h.Duration)
	}
	if !h.Dispose {
		t.Error("Dispose: want true")
	}
	if !h.Blend {
		t.Error("Blend (no_blend flag): want true")
	}
	if len(bs) != 0 {
		t.Errorf("bitstream: got %d bytes, want 0", len(bs))
	}
}

func TestParseANMF_WithBitstream(t *testing.T) {
	header := make([]byte, 16)
	bs := []byte{0x01, 0x02, 0x03}
	data := append(header, bs...)
	_, got, err := riff.ParseANMF(data)
	if err != nil {
		t.Fatalf("ParseANMF: %v", err)
	}
	if !bytes.Equal(got, bs) {
		t.Errorf("bitstream: got %v, want %v", got, bs)
	}
}

func TestParseANMF_TooShort(t *testing.T) {
	_, _, err := riff.ParseANMF([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short ANMF data")
	}
}

func TestANMF_Encode_RoundTrip(t *testing.T) {
	orig := riff.ANMFHeader{
		X:        100,
		Y:        200,
		Width:    320,
		Height:   240,
		Duration: 33,
		Dispose:  true,
		Blend:    false,
	}
	enc := orig.Encode()
	if len(enc) != 16 {
		t.Fatalf("Encode len: got %d, want 16", len(enc))
	}
	got, _, err := riff.ParseANMF(enc)
	if err != nil {
		t.Fatalf("ParseANMF: %v", err)
	}
	if got != orig {
		t.Errorf("round-trip: got %+v, want %+v", got, orig)
	}
}

// --- Chunk ordering / WriteVP8X integration ---

func TestWriteVP8X_FlagsOnly(t *testing.T) {
	var buf bytes.Buffer
	err := riff.WriteVP8X(&buf, riff.VP8XFlagAnimation|riff.VP8XFlagAlpha, 1920, 1080)
	if err != nil {
		t.Fatalf("WriteVP8X: %v", err)
	}
	chunk, err := riff.ReadChunk(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	vp8x, err := riff.ParseVP8X(chunk.Data)
	if err != nil {
		t.Fatalf("ParseVP8X: %v", err)
	}
	want := riff.VP8XFlagAnimation | riff.VP8XFlagAlpha
	if vp8x.Flags != want {
		t.Errorf("Flags: got %v, want %v", vp8x.Flags, want)
	}
	if vp8x.Width != 1919 {
		t.Errorf("Width: got %d, want 1919", vp8x.Width)
	}
	if vp8x.Height != 1079 {
		t.Errorf("Height: got %d, want 1079", vp8x.Height)
	}
}
