package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestAnnounceCacheSkipsUnchangedSnapshot(t *testing.T) {
	var cache announceCache
	first := mustAnnouncePayload(t, []proto.SessionInfo{{
		ID:    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Cwd:   "/Users/attson",
		Title: "/bin/zsh",
	}})
	same := mustAnnouncePayload(t, []proto.SessionInfo{{
		ID:    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Cwd:   "/Users/attson",
		Title: "/bin/zsh",
	}})
	changed := mustAnnouncePayload(t, []proto.SessionInfo{{
		ID:    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Cwd:   "/tmp",
		Title: "/bin/zsh",
	}})

	if !cache.shouldSend(first) {
		t.Fatal("first announce should send")
	}
	cache.markSent(first)
	if cache.shouldSend(same) {
		t.Fatal("unchanged announce should be skipped")
	}
	if !cache.shouldSend(changed) {
		t.Fatal("changed cwd should send")
	}
	cache.markSent(changed)
	if cache.shouldSend(changed) {
		t.Fatal("same changed payload should be skipped after markSent")
	}
}

func TestBuildAnnouncePayloadSortsSessionsForStableComparison(t *testing.T) {
	a := proto.SessionInfo{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Cwd: "/a"}
	b := proto.SessionInfo{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Cwd: "/b"}

	ab := mustAnnouncePayload(t, []proto.SessionInfo{a, b})
	ba := mustAnnouncePayload(t, []proto.SessionInfo{b, a})
	if !bytes.Equal(ab, ba) {
		t.Fatalf("same sessions produced different announce payloads:\n%s\n%s", ab, ba)
	}

	var ann proto.AnnouncePayload
	if err := json.Unmarshal(ab, &ann); err != nil {
		t.Fatal(err)
	}
	if got := []string{ann.Sessions[0].ID, ann.Sessions[1].ID}; got[0] != a.ID || got[1] != b.ID {
		t.Fatalf("sessions not sorted by id: %v", got)
	}
}

func TestBuildAnnouncePayloadStampsRemotePermission(t *testing.T) {
	payload, err := buildAnnouncePayload("host-id", "host", "user", []proto.SessionInfo{{
		ID:      "11111111-1111-4111-8111-111111111111",
		Command: "bash",
	}}, proto.RemotePermissionView)
	if err != nil {
		t.Fatal(err)
	}
	var ann proto.AnnouncePayload
	if err := json.Unmarshal(payload, &ann); err != nil {
		t.Fatal(err)
	}
	if got := ann.Sessions[0].RemotePermission; got != proto.RemotePermissionView {
		t.Fatalf("RemotePermission=%q; want view", got)
	}
}

func TestLocalFrameAllowedByPermission(t *testing.T) {
	if localFrameAllowedByPermission(proto.RemotePermissionView, proto.TypeIn) {
		t.Fatal("view permission allowed IN")
	}
	if localFrameAllowedByPermission(proto.RemotePermissionControl, proto.TypePasteImage) {
		t.Fatal("control permission allowed PASTE_IMAGE")
	}
	if !localFrameAllowedByPermission(proto.RemotePermissionControl, proto.TypeResize) {
		t.Fatal("control permission blocked RESIZE")
	}
	if !localFrameAllowedByPermission(proto.RemotePermissionFull, proto.TypePasteImage) {
		t.Fatal("full permission blocked PASTE_IMAGE")
	}
}

func mustAnnouncePayload(t *testing.T, sessions []proto.SessionInfo) []byte {
	t.Helper()
	payload, err := buildAnnouncePayload("host-id", "host", "user", sessions, proto.RemotePermissionFull)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
