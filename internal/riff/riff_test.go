package riff_test

import (
	"bytes"
	"testing"

	"github.com/skrashevich/go-webp/internal/riff"
)

func TestWriteReadHeader(t *testing.T) {
	var buf bytes.Buffer
	const fileSize = uint32(100)
	if err := riff.WriteHeader(&buf, fileSize); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if buf.Len() != 12 {
		t.Fatalf("header should be 12 bytes, got %d", buf.Len())
	}

	hdr, err := riff.ReadHeader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if hdr.FileSize != fileSize {
		t.Fatalf("FileSize: got %d, want %d", hdr.FileSize, fileSize)
	}
}

func TestWriteReadChunk(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	var buf bytes.Buffer
	if err := riff.WriteChunk(&buf, riff.FourCCVP8L, data); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	// 8 header + 5 data + 1 padding = 14
	if buf.Len() != 14 {
		t.Fatalf("chunk on disk should be 14 bytes, got %d", buf.Len())
	}

	chunk, err := riff.ReadChunk(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	if chunk.ID != riff.FourCCVP8L {
		t.Fatalf("ID: got %s, want VP8L", chunk.ID)
	}
	if !bytes.Equal(chunk.Data, data) {
		t.Fatalf("Data mismatch: got %v, want %v", chunk.Data, data)
	}
}

func TestInvalidRIFF(t *testing.T) {
	bad := bytes.Repeat([]byte("X"), 12)
	_, err := riff.ReadHeader(bytes.NewReader(bad))
	if err == nil {
		t.Fatal("expected error for invalid RIFF header")
	}
}

func TestChunkSize(t *testing.T) {
	tests := []struct{ data, want int }{
		{0, 8},
		{4, 12},
		{5, 14}, // padded
		{6, 14},
	}
	for _, tt := range tests {
		got := riff.ChunkSize(tt.data)
		if got != tt.want {
			t.Errorf("ChunkSize(%d) = %d, want %d", tt.data, got, tt.want)
		}
	}
}
