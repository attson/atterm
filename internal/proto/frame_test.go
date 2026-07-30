package proto

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func TestPasteFilePayloadRoundtrip(t *testing.T) {
	if TypePasteFile != 0x37 {
		t.Fatalf("TypePasteFile = 0x%02x; want 0x37", TypePasteFile)
	}
	p := PasteFilePayload{
		Filename:    "notes.pdf",
		ContentType: "application/pdf",
		Data:        []byte("hello world"),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PasteFilePayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Filename != p.Filename || got.ContentType != p.ContentType || !bytes.Equal(got.Data, p.Data) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, p)
	}
}

func TestPasteFileFrameCodec(t *testing.T) {
	sid := uuid.New()
	body, err := json.Marshal(PasteFilePayload{
		Filename: "foo.log", ContentType: "text/plain", Data: []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := Frame{Type: TypePasteFile, SessionID: sid, Payload: body}
	buf := Marshal(f)
	got, err := Unmarshal(buf)
	if err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if got.Type != TypePasteFile || got.SessionID != sid || !bytes.Equal(got.Payload, body) {
		t.Fatalf("frame roundtrip mismatch: got %+v want type=%v sid=%v", got, TypePasteFile, sid)
	}
}

func TestFSFrameTypeValues(t *testing.T) {
	if TypeFSRequest != 0x38 {
		t.Fatalf("TypeFSRequest = 0x%02x, want 0x38", TypeFSRequest)
	}
	if TypeFSResponse != 0x39 {
		t.Fatalf("TypeFSResponse = 0x%02x, want 0x39", TypeFSResponse)
	}
	if TypeFSEvent != 0x3a {
		t.Fatalf("TypeFSEvent = 0x%02x, want 0x3a", TypeFSEvent)
	}
}

func TestPrefsChangedFrameTypeValue(t *testing.T) {
	if TypePrefsChanged != 0x14 {
		t.Fatalf("TypePrefsChanged = 0x%02x; want 0x14", TypePrefsChanged)
	}
}
