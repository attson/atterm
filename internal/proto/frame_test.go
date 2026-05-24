package proto

import (
	"encoding/json"
	"testing"
)

func TestViewersPayloadRoundTrip(t *testing.T) {
	if TypeViewers != 0x36 {
		t.Fatalf("TypeViewers = 0x%02x; want 0x36", TypeViewers)
	}
	in := ViewersPayload{SessionID: "11111111-2222-3333-4444-555555555555", Count: 3}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ViewersPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round-trip = %+v; want %+v", out, in)
	}
}
