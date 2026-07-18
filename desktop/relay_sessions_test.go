package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSessionsApp(t *testing.T, srv *httptest.Server) *App {
	t.Helper()
	a := newRelayTestApp(t)
	// Seed relay config so the App methods find a valid URL + token.
	if err := a.cfgStore.Set(appConfig{
		RelayURL:          strings.Replace(srv.URL, "http://", "ws://", 1),
		RelaySessionToken: "atk_test",
		RemotePermission:  "full",
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	return a
}

func TestListRelaySessions_ParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/sessions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer atk_test" {
			t.Errorf("Authorization = %q; want %q", got, "Bearer atk_test")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id_hash":"h1","user_agent":"UA-1","ip_prefix":"1.2.3","created_at":1700000000000,"expires_at":1710000000000,"is_current":true},
			{"id_hash":"h2","user_agent":"UA-2","ip_prefix":"4.5.6","created_at":1700100000000,"expires_at":1710100000000,"is_current":false}
		]`))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	rows, err := a.ListRelaySessions()
	if err != nil {
		t.Fatalf("ListRelaySessions err: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d; want 2", len(rows))
	}
	if rows[0].IDHash != "h1" || !rows[0].IsCurrent {
		t.Errorf("row[0] = %+v; want IDHash=h1 IsCurrent=true", rows[0])
	}
	if rows[1].IDHash != "h2" || rows[1].IsCurrent {
		t.Errorf("row[1] = %+v; want IDHash=h2 IsCurrent=false", rows[1])
	}
}

func TestListRelaySessions_EmptyTokenReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("should not hit relay when token is empty")
	}))
	defer srv.Close()

	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{
		RelayURL: strings.Replace(srv.URL, "http://", "ws://", 1),
		// RelaySessionToken deliberately empty.
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("err = %q; want it to contain 'not authenticated'", err.Error())
	}
}

func TestListRelaySessions_401_ReturnsFriendlyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired"}`))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error on 401; got nil")
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Errorf("err = %q; want 'session expired' phrasing", err.Error())
	}
}

func TestRevokeRelaySession_DELETEsCorrectPath(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	if err := a.RevokeRelaySession("abc"); err != nil {
		t.Fatalf("RevokeRelaySession err: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q; want DELETE", gotMethod)
	}
	if gotPath != "/api/me/sessions/abc" {
		t.Errorf("path = %q; want /api/me/sessions/abc", gotPath)
	}
	if gotAuth != "Bearer atk_test" {
		t.Errorf("Authorization = %q; want %q", gotAuth, "Bearer atk_test")
	}
}

func TestSignOutOthers_ParsesDeletedCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if r.URL.Path != "/api/me/sessions/sign-out-others" {
			t.Errorf("path = %q; want /api/me/sessions/sign-out-others", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted": 3})
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	res, err := a.SignOutOtherRelaySessions()
	if err != nil {
		t.Fatalf("SignOutOtherRelaySessions err: %v", err)
	}
	if res.Deleted != 3 {
		t.Errorf("Deleted = %d; want 3", res.Deleted)
	}
}

// A non-2xx non-401 response should surface a "relay returned NNN: ..."
// error so the frontend can display it verbatim.
func TestListRelaySessions_500_SurfacesRawError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error on 500; got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %q; want mention of 500", err.Error())
	}
}

var _ = context.Background // silence unused import if all tests use httptest
