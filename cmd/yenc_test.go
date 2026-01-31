package main

import (
	"bytes"
	"testing"
)

func TestDecodeYEncLineSimple(t *testing.T) {
	// single-line containing bytes [0x00, 0x41, 0xFF] encoded with +42
	// encode back to test vector: original byte b => encoded byte (b+42)&0xFF
	orig := []byte{0x00, 0x41, 0xFF}
	enc := make([]byte, len(orig))
	for i, b := range orig {
		enc[i] = byte((int(b) + 42) & 0xFF)
	}
	got, err := decodeYEncLine(enc)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !bytes.Equal(got, orig) {
		t.Fatalf("expected %v got %v", orig, got)
	}
}

func TestDecodeYEncLineEscaped(t *testing.T) {
	// A byte that would be encoded to '=' (0x3D) must be escaped.
	// Find a byte b such that (b+42)&0xFF == 0x3D => b == (0x3D - 42) & 0xFF
	b := byte((0x3D - 42) & 0xFF)
	// When escaped, encoder sends '=' and (encoded+64)&0xFF
	encEsc := []byte{'=', byte(((int(b) + 42) & 0xFF) + 64)}
	got, err := decodeYEncLine(encEsc)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(got) != 1 || got[0] != b {
		t.Fatalf("expected %02x got %02x", b, got)
	}
}
