package proto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionInfoAttentionJSON(t *testing.T) {
	in := SessionInfo{ID: "s1", AttentionAt: 1700000000, Unread: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SessionInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.AttentionAt != 1700000000 || !out.Unread {
		t.Fatalf("round-trip lost fields: %+v", out)
	}
	// Zero values must be omitted.
	z, _ := json.Marshal(SessionInfo{ID: "s2"})
	if strings.Contains(string(z), "attention_at") || strings.Contains(string(z), "unread") {
		t.Fatalf("zero values not omitted: %s", z)
	}
}

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
