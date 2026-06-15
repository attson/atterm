package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
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

func TestLocalSubscriberFrameForwardedToUplink(t *testing.T) {
	if !localSubscriberFrameForwardedToUplink(proto.TypeOut) {
		t.Fatal("OUT should be forwarded to remote relay")
	}
	if !localSubscriberFrameForwardedToUplink(proto.TypeMeta) {
		t.Fatal("META should be forwarded to remote relay")
	}
	if !localSubscriberFrameForwardedToUplink(proto.TypeClose) {
		t.Fatal("CLOSE should be forwarded to remote relay")
	}
	if localSubscriberFrameForwardedToUplink(proto.TypeReplayProgress) {
		t.Fatal("REPLAY_PROGRESS is local subscriber progress and must not be sent to /uplink")
	}
}

func TestForwardLocalSubscriberFrameRequestsRepaintForAltScreenReset(t *testing.T) {
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	out := make(chan proto.Frame, 1)
	repaints := 0
	frame := proto.EncodeOut(id, 0, []byte("\x1b[?1049h\x1b[2J\x1b[H"))

	if ok := forwardLocalSubscriberFrame(context.Background(), out, frame, nil, func() { repaints++ }, nil); !ok {
		t.Fatal("forwardLocalSubscriberFrame returned false")
	}
	if repaints != 1 {
		t.Fatalf("repaints = %d; want 1", repaints)
	}
	select {
	case got := <-out:
		if got.Type != proto.TypeOut {
			t.Fatalf("forwarded type = 0x%02x; want OUT", got.Type)
		}
	default:
		t.Fatal("reset OUT frame was not forwarded")
	}
}

func TestDesktopUplinkFrameLogDetailsSummarizesPasteImage(t *testing.T) {
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	payload, err := json.Marshal(proto.PasteImagePayload{
		Filename:    "clip.png",
		ContentType: "image/png",
		Data:        []byte("png-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := desktopUplinkFrameLogDetails(proto.Frame{Type: proto.TypePasteImage, SessionID: id, Payload: payload})
	for _, want := range []string{"session=11111111-1111-4111-8111-111111111111", "filename=\"clip.png\"", "content_type=\"image/png\"", "image_bytes=9"} {
		if !strings.Contains(got, want) {
			t.Fatalf("details %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "png-bytes") {
		t.Fatalf("details leaked image payload: %q", got)
	}
}
