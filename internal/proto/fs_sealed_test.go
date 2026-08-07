package proto

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func fsTestKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestFSResponseSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := fsTestKey()
	in := FSResponsePayload{
		RequestID: "r1",
		OK:        true,
		Entries:   []DirEntry{{Name: ".env", IsDir: false, Size: 12}},
		Content:   &FileContent{Path: "/home/u/.env", Data: []byte("SECRET=1"), IsBinary: false},
		Error:     "boom /home/u/.env",
	}
	encoded, err := EncodeFSResponse(in, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3", len(segs))
	}
	plain := string(segs[0])
	for _, secret := range []string{".env", "SECRET=1", "/home/u", "boom"} {
		if strings.Contains(plain, secret) {
			t.Fatalf("segment 0 leaked %q: %s", secret, plain)
		}
	}
	// The whole wire payload must not carry the file bytes in the clear.
	if strings.Contains(string(encoded), "SECRET=1") {
		t.Fatal("file bytes present in plaintext on the wire")
	}
	out, err := DecodeFSResponse(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.RequestID != "r1" || !out.OK {
		t.Fatalf("routing fields lost: %+v", out)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != ".env" {
		t.Fatalf("entries lost: %+v", out.Entries)
	}
	if out.Content == nil || string(out.Content.Data) != "SECRET=1" || out.Content.Path != "/home/u/.env" {
		t.Fatalf("content lost: %+v", out.Content)
	}
	if out.Error != "boom /home/u/.env" {
		t.Fatalf("error lost: %q", out.Error)
	}
}

func TestFSResponseSealsChunkData(t *testing.T) {
	sid := uuid.New()
	key := fsTestKey()
	in := FSResponsePayload{
		RequestID: "c1",
		OK:        true,
		Chunk:     &FSChunkPayload{Path: "/home/u/img.png", Data: []byte("BINARYBYTES"), Offset: 0, Length: 11},
	}
	encoded, err := EncodeFSResponse(in, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "BINARYBYTES") {
		t.Fatal("chunk bytes present in plaintext")
	}
	out, err := DecodeFSResponse(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Chunk == nil || string(out.Chunk.Data) != "BINARYBYTES" || out.Chunk.Path != "/home/u/img.png" {
		t.Fatalf("chunk lost: %+v", out.Chunk)
	}
}

func TestFSResponseKeyedAlwaysSealsEvenWhenEmpty(t *testing.T) {
	// Segment count signals key state, not payload presence — otherwise
	// an empty response would look keyless to the peer.
	sid := uuid.New()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "u1", OK: true, WatchID: "3"}, fsTestKey(), sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}
}

func TestFSResponsePlaintextWhenNoKey(t *testing.T) {
	sid := uuid.New()
	in := FSResponsePayload{RequestID: "r2", OK: true, Entries: []DirEntry{{Name: "a.txt"}}}
	encoded, err := EncodeFSResponse(in, nil, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	out, err := DecodeFSResponse(encoded, nil, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != "a.txt" {
		t.Fatalf("entries lost: %+v", out.Entries)
	}
}

func TestFSSealedAADIsFrameBound(t *testing.T) {
	sid := uuid.New()
	key := fsTestKey()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "r3", OK: true}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFSRequest(encoded, key, sid); err == nil {
		t.Fatal("response envelope opened under FS_REQUEST AAD")
	}
}

func TestFSSealedRejectsWrongSession(t *testing.T) {
	key := fsTestKey()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "r4", OK: true}, key, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFSResponse(encoded, key, uuid.New()); err == nil {
		t.Fatal("envelope opened under a different session id")
	}
}

func TestFSSealedRejectsMissingKeyOnOpen(t *testing.T) {
	sid := uuid.New()
	encoded, err := EncodeFSResponse(FSResponsePayload{RequestID: "r5", OK: true}, fsTestKey(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFSResponse(encoded, nil, sid); err == nil {
		t.Fatal("sealed response opened without a key")
	}
}

func TestFSRequestSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := fsTestKey()
	in := FSRequestPayload{RequestID: "q1", Op: "read_file", Path: "/home/u/.env", MaxBytes: 1024}
	encoded, err := EncodeFSRequest(in, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(segs[0]), ".env") {
		t.Fatalf("segment 0 leaked path: %s", segs[0])
	}
	if !strings.Contains(string(segs[0]), "read_file") {
		t.Fatalf("segment 0 must keep op for the relay gate: %s", segs[0])
	}
	out, err := DecodeFSRequest(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/home/u/.env" || out.Op != "read_file" || out.MaxBytes != 1024 {
		t.Fatalf("round trip lost fields: %+v", out)
	}
}

func TestFSEventSealedRoundTrip(t *testing.T) {
	sid := uuid.New()
	key := fsTestKey()
	encoded, err := EncodeFSEvent(FSEventPayload{WatchID: "7", Path: "/home/u/secrets", Event: "changed"}, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := DecodeSegments(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(segs[0]), "secrets") {
		t.Fatalf("segment 0 leaked path: %s", segs[0])
	}
	if !strings.Contains(string(segs[0]), `"watch_id":"7"`) {
		t.Fatalf("segment 0 must keep watch_id for routing: %s", segs[0])
	}
	out, err := DecodeFSEvent(encoded, key, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/home/u/secrets" || out.WatchID != "7" || out.Event != "changed" {
		t.Fatalf("round trip lost fields: %+v", out)
	}
}
