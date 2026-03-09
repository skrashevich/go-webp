package bitreader_test

import (
	"bytes"
	"testing"

	"github.com/skrashevich/go-webp/internal/bitreader"
)

func TestReadWriteBits(t *testing.T) {
	// Write 16 bits then read them back.
	var buf bytes.Buffer
	w := bitreader.NewWriter(&buf)

	// Write 0b10110100_11001010 = 0xB4CA
	if err := w.WriteBits(0xB4CA, 16); err != nil {
		t.Fatalf("WriteBits: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	r := bitreader.NewReader(bytes.NewReader(buf.Bytes()))
	val, err := r.ReadBits(16)
	if err != nil {
		t.Fatalf("ReadBits: %v", err)
	}
	if val != 0xB4CA {
		t.Fatalf("ReadBits: got 0x%X, want 0xB4CA", val)
	}
}

func TestReadWriteBitsLSB(t *testing.T) {
	var buf bytes.Buffer
	w := bitreader.NewWriter(&buf)

	const val = uint32(0b10110011)
	if err := w.WriteBitsLSB(val, 8); err != nil {
		t.Fatalf("WriteBitsLSB: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	r := bitreader.NewReader(bytes.NewReader(buf.Bytes()))
	got, err := r.ReadBitsLSB(8)
	if err != nil {
		t.Fatalf("ReadBitsLSB: %v", err)
	}
	if got != val {
		t.Fatalf("ReadBitsLSB: got 0b%08b, want 0b%08b", got, val)
	}
}

func TestReadBool(t *testing.T) {
	var buf bytes.Buffer
	w := bitreader.NewWriter(&buf)
	_ = w.WriteBit(1)
	_ = w.WriteBit(0)
	_ = w.Flush()

	r := bitreader.NewReader(bytes.NewReader(buf.Bytes()))
	b1, _ := r.ReadBool()
	b2, _ := r.ReadBool()
	if !b1 {
		t.Error("expected first bit to be true")
	}
	if b2 {
		t.Error("expected second bit to be false")
	}
}
