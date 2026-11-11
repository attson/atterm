package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestMarkSessionsSeen_LocalListStaysRead(t *testing.T) {
	h := newTestRelayHost(t)
	id := uuid.New()
	sess := session.New(id, proto.SessionInfo{TaskState: proto.TaskStateWaitingInput, AttentionAt: 1000})
	sess.OwnerUserID = h.adminUserID
	if _, err := h.server.Registry().Add(sess); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer remote.Close()
	if err := h.cfg.Set(appConfig{RelayURL: remote.URL, RelaySessionToken: "test-token", AllowInsecureRelay: true}); err != nil {
		t.Fatal(err)
	}
	a := &App{host: h, cfgStore: h.cfg}
	assertLocalSessionUnread(t, h, id, true)
	if err := a.MarkSessionsSeen([]string{id.String()}, false); err != nil {
		t.Fatal(err)
	}
	assertLocalSessionUnread(t, h, id, false)
	// A later list refresh must not resurrect the unread flag.
	h.server.Registry().NotifyChange()
	assertLocalSessionUnread(t, h, id, false)
}

func TestMarkSessionsSeen_LocalWithoutRemote(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "all"}[all], func(t *testing.T) {
			h := newTestRelayHost(t)
			id := uuid.New()
			sess := session.New(id, proto.SessionInfo{AttentionAt: 1000})
			sess.OwnerUserID = h.adminUserID
			if _, err := h.server.Registry().Add(sess); err != nil {
				t.Fatal(err)
			}
			a := &App{host: h, cfgStore: h.cfg}
			assertLocalSessionUnread(t, h, id, true)
			if err := a.MarkSessionsSeen([]string{id.String()}, all); err != nil {
				t.Fatalf("local mark seen without remote: %v", err)
			}
			assertLocalSessionUnread(t, h, id, false)
		})
	}
}

func TestMarkSessionsSeen_MixedLocalAndRemote(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "all"}[all], func(t *testing.T) {
			h := newTestRelayHost(t)
			localID, remoteID := uuid.New(), uuid.New()
			sess := session.New(localID, proto.SessionInfo{AttentionAt: 1000})
			sess.OwnerUserID = h.adminUserID
			if _, err := h.server.Registry().Add(sess); err != nil {
				t.Fatal(err)
			}
			type receipt struct {
				IDs []string `json:"session_ids"`
				All bool     `json:"all"`
			}
			received := make(chan receipt, 1)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/sessions/seen" || r.Header.Get("Authorization") != "Bearer test-token" {
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				var body receipt
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				received <- body
				w.WriteHeader(http.StatusNoContent)
			}))
			defer remote.Close()
			if err := h.cfg.Set(appConfig{RelayURL: remote.URL, RelaySessionToken: "test-token", AllowInsecureRelay: true}); err != nil {
				t.Fatal(err)
			}
			a := &App{host: h, cfgStore: h.cfg}
			if err := a.MarkSessionsSeen([]string{localID.String(), remoteID.String()}, all); err != nil {
				t.Fatal(err)
			}
			assertLocalSessionUnread(t, h, localID, false)
			select {
			case body := <-received:
				if body.All != all || (!all && (len(body.IDs) != 1 || body.IDs[0] != remoteID.String())) {
					t.Fatalf("remote receipt = %+v, want all=%v or remote ID %s only", body, all, remoteID)
				}
			default:
				t.Fatal("remote receipt was not sent")
			}
		})
	}
}

func assertLocalSessionUnread(t *testing.T, h *relayHost, id uuid.UUID, want bool) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+h.addr+"/api/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var infos []proto.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&infos); err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.ID == id.String() {
			if info.Unread != want {
				t.Fatalf("local list unread = %v, want %v after MarkSessionsSeen", info.Unread, want)
			}
			return
		}
	}
	t.Fatalf("session %s missing from local list", id)
}
