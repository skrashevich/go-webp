package main

import "testing"

func TestParseArgsAcceptsFlagsAfterInput(t *testing.T) {
	input, output, lossy, quality, err := parseArgs([]string{"1.webp", "-o", "1.jpg", "-lossy", "-quality", "80"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if input != "1.webp" {
		t.Fatalf("input = %q, want %q", input, "1.webp")
	}
	if output != "1.jpg" {
		t.Fatalf("output = %q, want %q", output, "1.jpg")
	}
	if !lossy {
		t.Fatal("lossy = false, want true")
	}
	if quality != 80 {
		t.Fatalf("quality = %v, want 80", quality)
	}
}

func TestParseArgsAcceptsFlagsBeforeInput(t *testing.T) {
	input, output, lossy, quality, err := parseArgs([]string{"-o=1.jpg", "-lossy=false", "-quality=60", "1.webp"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if input != "1.webp" {
		t.Fatalf("input = %q, want %q", input, "1.webp")
	}
	if output != "1.jpg" {
		t.Fatalf("output = %q, want %q", output, "1.jpg")
	}
	if lossy {
		t.Fatal("lossy = true, want false")
	}
	if quality != 60 {
		t.Fatalf("quality = %v, want 60", quality)
	}
}

func TestParseArgsRequiresOutput(t *testing.T) {
	_, _, _, _, err := parseArgs([]string{"1.webp"})
	if err == nil {
		t.Fatal("parseArgs returned nil error, want error")
	}
	if err.Error() != "-o output file is required" {
		t.Fatalf("error = %q, want %q", err.Error(), "-o output file is required")
	}
}
