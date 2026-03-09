package vp8

import (
	"bytes"
	"testing"
)

func TestBoolHighProb(t *testing.T) {
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)
	enc.writeBool(250, true)
	enc.writeBool(250, false)
	enc.flush()

	data := buf.Bytes()
	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := dec.ReadBool(250); got != true {
		t.Errorf("bool 0: want true, got false")
	}
	if got := dec.ReadBool(250); got != false {
		t.Errorf("bool 1: want false, got true")
	}
}

func TestBoolAllFalse(t *testing.T) {
	// Test with only false values — no carry possible.
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)
	for i := 0; i < 100; i++ {
		enc.writeBool(128, false)
	}
	enc.flush()

	data := buf.Bytes()
	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if got := dec.ReadBool(128); got != false {
			t.Errorf("bool %d: want false, got true", i)
		}
	}
}

func TestBoolSingleTrue(t *testing.T) {
	// One true followed by falses.
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)
	enc.writeBool(128, true)
	for i := 0; i < 99; i++ {
		enc.writeBool(128, false)
	}
	enc.flush()

	data := buf.Bytes()
	t.Logf("bytes: %x", data[:min(10, len(data))])
	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := dec.ReadBool(128); got != true {
		t.Errorf("bool 0: want true, got false")
	}
	for i := 1; i < 100; i++ {
		if got := dec.ReadBool(128); got != false {
			t.Errorf("bool %d: want false, got true", i)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBoolTrace8(t *testing.T) {
	type bv struct {
		prob uint8
		val  bool
	}
	all := []bv{
		{128, true}, {128, false}, {128, true},
		{200, false}, {200, true},
		{10, true}, {10, false},
		{250, true},
	}

	// Encode with tracing.
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)
	for i, v := range all {
		t.Logf("ENC before bool %d: low=0x%08x range=%d count=%d", i, enc.low, enc.range_, enc.count)
		enc.writeBool(v.prob, v.val)
	}
	t.Logf("ENC final: low=0x%08x range=%d count=%d", enc.low, enc.range_, enc.count)
	enc.flush()
	data := buf.Bytes()
	t.Logf("Encoded bytes: %x", data)

	// Decode with tracing.
	dec, _ := newBoolDecoder(data, 0)
	for i, v := range all {
		t.Logf("DEC before bool %d: bits=0x%04x rangeM1=%d nBits=%d", i, dec.bits, dec.rangeM1, dec.nBits)
		got := dec.ReadBool(v.prob)
		if got != v.val {
			t.Errorf("MISMATCH bool %d: prob=%d want=%v got=%v", i, v.prob, v.val, got)
		}
	}
}

func TestBoolNarrowDown(t *testing.T) {
	// Incrementally add bools until failure.
	type bv struct {
		prob uint8
		val  bool
	}
	all := []bv{
		{128, true}, {128, false}, {128, true},
		{200, false}, {200, true},
		{10, true}, {10, false},
		{250, true}, {250, false},
	}

	for n := 1; n <= len(all); n++ {
		var buf bytes.Buffer
		enc := newBoolEncoder(&buf)
		for _, v := range all[:n] {
			enc.writeBool(v.prob, v.val)
		}
		enc.flush()

		data := buf.Bytes()
		dec, err := newBoolDecoder(data, 0)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}

		ok := true
		for i, v := range all[:n] {
			got := dec.ReadBool(v.prob)
			if got != v.val {
				t.Errorf("n=%d: bool %d (prob=%d) want=%v got=%v, bytes=%x",
					n, i, v.prob, v.val, got, data[:min(8, len(data))])
				ok = false
				break
			}
		}
		if ok {
			t.Logf("n=%d: OK, bytes=%x", n, data[:min(8, len(data))])
		}
	}
}

func TestBoolStress(t *testing.T) {
	// Encode 1000 random-ish bools, then decode and verify.
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)

	type bv struct {
		prob uint8
		val  bool
	}
	var values []bv
	// Generate deterministic test values.
	for i := 0; i < 1000; i++ {
		p := uint8((i*37 + 13) % 256)
		if p == 0 {
			p = 1
		}
		v := (i*53+7)%3 != 0
		values = append(values, bv{p, v})
	}

	for _, v := range values {
		enc.writeBool(v.prob, v.val)
	}
	enc.flush()

	data := buf.Bytes()
	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	errors := 0
	for i, v := range values {
		got := dec.ReadBool(v.prob)
		if got != v.val {
			if errors < 10 {
				t.Errorf("bool %d: prob=%d, want=%v, got=%v", i, v.prob, v.val, got)
			}
			errors++
		}
	}
	if errors > 0 {
		t.Errorf("Total errors: %d / %d", errors, len(values))
	}
}

func TestBoolEncoderDecoderRoundtrip(t *testing.T) {
	// Write some bools with various probabilities, then read them back.
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)

	type boolVal struct {
		prob uint8
		val  bool
	}

	values := []boolVal{
		{128, true}, {128, false}, {128, true},
		{200, false}, {200, true},
		{10, true}, {10, false},
		{250, true}, {250, false},
	}

	for _, v := range values {
		enc.writeBool(v.prob, v.val)
	}
	enc.flush()

	data := buf.Bytes()
	t.Logf("Encoded %d bools into %d bytes", len(values), len(data))

	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal("newBoolDecoder:", err)
	}

	for i, v := range values {
		got := dec.ReadBool(v.prob)
		if got != v.val {
			t.Errorf("bool %d: prob=%d, want=%v, got=%v", i, v.prob, v.val, got)
		}
	}
}

func TestBoolLiteralRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	enc := newBoolEncoder(&buf)

	enc.writeLiteral(7, 42)  // 7-bit value
	enc.writeLiteral(1, 0)   // 1-bit false
	enc.writeLiteral(1, 1)   // 1-bit true
	enc.writeLiteral(6, 63)  // 6-bit max
	enc.writeLiteral(2, 0)   // 2-bit zero
	enc.flush()

	data := buf.Bytes()
	dec, err := newBoolDecoder(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	if v := dec.ReadLiteral(7); v != 42 {
		t.Errorf("literal 7-bit: want 42, got %d", v)
	}
	if v := dec.ReadLiteral(1); v != 0 {
		t.Errorf("literal 1-bit: want 0, got %d", v)
	}
	if v := dec.ReadLiteral(1); v != 1 {
		t.Errorf("literal 1-bit: want 1, got %d", v)
	}
	if v := dec.ReadLiteral(6); v != 63 {
		t.Errorf("literal 6-bit: want 63, got %d", v)
	}
	if v := dec.ReadLiteral(2); v != 0 {
		t.Errorf("literal 2-bit: want 0, got %d", v)
	}
}
