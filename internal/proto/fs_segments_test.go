package proto

import (
	"bytes"
	"testing"
)

func TestSegmentsRoundTrip(t *testing.T) {
	cases := [][][]byte{
		{[]byte(`{"a":1}`)},
		{[]byte(`{"a":1}`), []byte("sealed")},
		{[]byte(`{"a":1}`), []byte("sealed"), []byte("content")},
		{[]byte(`{}`), {}},
	}
	for i, segs := range cases {
		encoded, err := EncodeSegments(segs)
		if err != nil {
			t.Fatalf("case %d: encode: %v", i, err)
		}
		decoded, err := DecodeSegments(encoded)
		if err != nil {
			t.Fatalf("case %d: decode: %v", i, err)
		}
		if len(decoded) != len(segs) {
			t.Fatalf("case %d: got %d segments, want %d", i, len(decoded), len(segs))
		}
		for j := range segs {
			if !bytes.Equal(decoded[j], segs[j]) {
				t.Fatalf("case %d seg %d: got %q want %q", i, j, decoded[j], segs[j])
			}
		}
	}
}

func TestSegmentsRejectsMalformed(t *testing.T) {
	valid, err := EncodeSegments([][]byte{[]byte("ab"), []byte("cd")})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":            {},
		"zero segments":    {0x00},
		"truncated prefix": valid[:3],
		"truncated body":   valid[:len(valid)-1],
		"trailing bytes":   append(append([]byte{}, valid...), 0xff),
	}
	for name, payload := range cases {
		if _, err := DecodeSegments(payload); err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
	}
}

func TestEncodeSegmentsRejectsOversized(t *testing.T) {
	if _, err := EncodeSegments([][]byte{make([]byte, maxPayload+1)}); err == nil {
		t.Fatal("expected error for oversized segment")
	}
}

func TestEncodeSegmentsRequiresOne(t *testing.T) {
	if _, err := EncodeSegments(nil); err == nil {
		t.Fatal("expected error for zero segments")
	}
}
