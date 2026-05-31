package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePairingToken_PostsToRelayWithBearerAndReturnsParsed(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pair/create" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "pair_TESTVALUE",
			"expires_at": int64(1748689200),
			"qr_url":     "http://relay.test/pair?t=pair_TESTVALUE",
		})
	}))
	t.Cleanup(srv.Close)

	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelayToken: "atk_localtest"}); err != nil {
		t.Fatalf("seed cfgStore: %v", err)
	}

	got, err := a.CreatePairingToken()
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	if got.Token != "pair_TESTVALUE" {
		t.Errorf("Token: got %q", got.Token)
	}
	if got.ExpiresAt != 1748689200 {
		t.Errorf("ExpiresAt: got %d", got.ExpiresAt)
	}
	if !strings.HasPrefix(got.QRURL, "http://relay.test/pair?t=") {
		t.Errorf("QRURL: got %q", got.QRURL)
	}
	if gotAuth != "Bearer atk_localtest" {
		t.Errorf("Authorization: got %q", gotAuth)
	}
}

func TestCreatePairingToken_NoRelayConfigured_Errors(t *testing.T) {
	a := newRelayTestApp(t)
	if _, err := a.CreatePairingToken(); err == nil {
		t.Fatal("expected error when relay not configured")
	}
}
