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

// TestFrameTypeValuesPinned locks every Type constant's wire value. A frame
// type number is a wire contract shared with peers running older code —
// renumbering one silently breaks decoding on the other end instead of
// failing loudly, so every constant (not just the two this task adds) is
// pinned here. Before this task, 0x3b and 0x3c were unassigned; this test
// also confirms nothing already claims them.
func TestFrameTypeValuesPinned(t *testing.T) {
	want := map[string]Type{
		"TypeOpen":           0x01,
		"TypeIn":             0x02,
		"TypeOut":            0x03,
		"TypeResize":         0x04,
		"TypeMeta":           0x05,
		"TypeClose":          0x06,
		"TypeAttach":         0x10,
		"TypeList":           0x11,
		"TypeListResp":       0x12,
		"TypeReplayProgress": 0x13,
		"TypePrefsChanged":   0x14,
		"TypePing":           0x20,
		"TypePong":           0x21,
		"TypeAnnounce":       0x30,
		"TypeStreamRequest":  0x31,
		"TypeStreamStop":     0x32,
		"TypePasteImage":     0x33,
		"TypeClaimDriver":    0x34,
		"TypeCommandEvent":   0x35,
		"TypeViewers":        0x36,
		"TypePasteFile":      0x37,
		"TypeFSRequest":      0x38,
		"TypeFSResponse":     0x39,
		"TypeFSEvent":        0x3a,
		"TypeSessionCreate":  0x3b,
		"TypeSessionCreated": 0x3c,
		"TypeAuthInfo":       0x40,
	}
	got := map[string]Type{
		"TypeOpen":           TypeOpen,
		"TypeIn":             TypeIn,
		"TypeOut":            TypeOut,
		"TypeResize":         TypeResize,
		"TypeMeta":           TypeMeta,
		"TypeClose":          TypeClose,
		"TypeAttach":         TypeAttach,
		"TypeList":           TypeList,
		"TypeListResp":       TypeListResp,
		"TypeReplayProgress": TypeReplayProgress,
		"TypePrefsChanged":   TypePrefsChanged,
		"TypePing":           TypePing,
		"TypePong":           TypePong,
		"TypeAnnounce":       TypeAnnounce,
		"TypeStreamRequest":  TypeStreamRequest,
		"TypeStreamStop":     TypeStreamStop,
		"TypePasteImage":     TypePasteImage,
		"TypeClaimDriver":    TypeClaimDriver,
		"TypeCommandEvent":   TypeCommandEvent,
		"TypeViewers":        TypeViewers,
		"TypePasteFile":      TypePasteFile,
		"TypeFSRequest":      TypeFSRequest,
		"TypeFSResponse":     TypeFSResponse,
		"TypeFSEvent":        TypeFSEvent,
		"TypeSessionCreate":  TypeSessionCreate,
		"TypeSessionCreated": TypeSessionCreated,
		"TypeAuthInfo":       TypeAuthInfo,
	}
	for name, wantVal := range want {
		gotVal, ok := got[name]
		if !ok {
			t.Fatalf("%s missing from got map (test bug)", name)
		}
		if gotVal != wantVal {
			t.Errorf("%s = 0x%02x; want 0x%02x (wire value moved — this breaks older peers)", name, gotVal, wantVal)
		}
	}
	// Every value must be unique: two constants sharing a byte is as bad as
	// one moving, and easy to introduce by copy-pasting a const line.
	seen := make(map[Type]string, len(want))
	for name, v := range want {
		if other, dup := seen[v]; dup {
			t.Fatalf("Type 0x%02x assigned to both %s and %s", v, other, name)
		}
		seen[v] = name
	}
	if len(want) != len(got) {
		t.Fatalf("want/got map size mismatch: want=%d got=%d — a constant was added without updating this pin test", len(want), len(got))
	}
}

// TestSessionCreatePayloadRoundTrip pins SessionCreatePayload's wire shape:
// field names and the fact that none of these three fields are omitempty
// (a create request is meaningless with any of them blank, so a decoder
// should see them, not silently default them away).
func TestSessionCreatePayloadRoundTrip(t *testing.T) {
	in := SessionCreatePayload{
		RequestID: "req-1",
		HostID:    "11111111-1111-1111-1111-111111111111",
		ProfileID: "profile-abc",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"request_id", "host_id", "profile_id"} {
		if _, ok := m[key]; !ok {
			t.Errorf("marshaled JSON missing key %q: %s", key, b)
		}
	}
	var out SessionCreatePayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip = %+v; want %+v", out, in)
	}
}

// TestSessionCreatedPayloadRoundTrip covers both the success and failure
// shapes of the reply: OK+SessionID with Error omitted, and !OK+Error with
// SessionID omitted, since both are real wire states a client must handle.
func TestSessionCreatedPayloadRoundTrip(t *testing.T) {
	ok := SessionCreatedPayload{RequestID: "req-1", OK: true, SessionID: "22222222-2222-2222-2222-222222222222"}
	b, err := json.Marshal(ok)
	if err != nil {
		t.Fatalf("marshal ok: %v", err)
	}
	if strings.Contains(string(b), "\"error\"") {
		t.Fatalf("success payload should omit error field: %s", b)
	}
	// Decode to a map, not just back into the struct. A struct round-trip
	// proves only that Marshal and Unmarshal agree with each other, so a
	// renamed json tag survives it — and the tag IS the wire contract that
	// the relay router and the mobile client read. The sibling
	// TestSessionCreatePayloadRoundTrip already checks its keys this way.
	var okMap map[string]any
	if err := json.Unmarshal(b, &okMap); err != nil {
		t.Fatalf("unmarshal ok to map: %v", err)
	}
	for _, key := range []string{"request_id", "ok", "session_id"} {
		if _, present := okMap[key]; !present {
			t.Errorf("success payload missing wire key %q: %s", key, b)
		}
	}
	var gotOK SessionCreatedPayload
	if err := json.Unmarshal(b, &gotOK); err != nil {
		t.Fatalf("unmarshal ok: %v", err)
	}
	if gotOK != ok {
		t.Fatalf("round-trip ok = %+v; want %+v", gotOK, ok)
	}

	fail := SessionCreatedPayload{RequestID: "req-2", OK: false, Error: "unknown_profile"}
	b, err = json.Marshal(fail)
	if err != nil {
		t.Fatalf("marshal fail: %v", err)
	}
	if strings.Contains(string(b), "\"session_id\"") {
		t.Fatalf("failure payload should omit session_id field: %s", b)
	}
	var failMap map[string]any
	if err := json.Unmarshal(b, &failMap); err != nil {
		t.Fatalf("unmarshal fail to map: %v", err)
	}
	for _, key := range []string{"request_id", "error"} {
		if _, present := failMap[key]; !present {
			t.Errorf("failure payload missing wire key %q: %s", key, b)
		}
	}
	var gotFail SessionCreatedPayload
	if err := json.Unmarshal(b, &gotFail); err != nil {
		t.Fatalf("unmarshal fail: %v", err)
	}
	if gotFail != fail {
		t.Fatalf("round-trip fail = %+v; want %+v", gotFail, fail)
	}
}

// TestSessionCreateFrameCodec exercises SessionCreatePayload through the
// actual wire codec (Marshal/Unmarshal), not just encoding/json, matching
// TestPasteFileFrameCodec's coverage for the FS pair's sibling frames.
func TestSessionCreateFrameCodec(t *testing.T) {
	body, err := json.Marshal(SessionCreatePayload{
		RequestID: "req-3", HostID: "host-1", ProfileID: "profile-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// SessionCreate has no session yet — SessionID is the zero UUID on the
	// wire, which is the whole point of routing this pair by host_id
	// instead (see the type doc comment in frame.go).
	f := Frame{Type: TypeSessionCreate, Payload: body}
	buf := Marshal(f)
	got, err := Unmarshal(buf)
	if err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if got.Type != TypeSessionCreate || got.SessionID != uuid.Nil || !bytes.Equal(got.Payload, body) {
		t.Fatalf("frame roundtrip mismatch: got %+v want type=%v sid=nil", got, TypeSessionCreate)
	}
	var decoded SessionCreatePayload
	if err := json.Unmarshal(got.Payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.RequestID != "req-3" || decoded.HostID != "host-1" || decoded.ProfileID != "profile-1" {
		t.Fatalf("decoded payload = %+v", decoded)
	}
}

// TestSessionCreatedFrameCodec is TestSessionCreateFrameCodec's mirror for
// the reply frame.
func TestSessionCreatedFrameCodec(t *testing.T) {
	sid := uuid.New()
	body, err := json.Marshal(SessionCreatedPayload{
		RequestID: "req-3", OK: true, SessionID: sid.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	f := Frame{Type: TypeSessionCreated, SessionID: sid, Payload: body}
	buf := Marshal(f)
	got, err := Unmarshal(buf)
	if err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if got.Type != TypeSessionCreated || got.SessionID != sid || !bytes.Equal(got.Payload, body) {
		t.Fatalf("frame roundtrip mismatch: got %+v want type=%v sid=%v", got, TypeSessionCreated, sid)
	}
}
