package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if err := a.cfgStore.Set(appConfig{RelayURL: srv.URL, RelaySessionToken: "atk_localtest"}); err != nil {
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

func TestCreatePairingToken_WrapsWhenAccountKeyUnlocked(t *testing.T) {
	// stub relay: capture the request body and reply with a fixed token
	var capturedBody map[string]string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pair/create" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "pair_stub", "expires_at": time.Now().Add(5 * time.Minute).Unix(),
			"qr_url": ts.URL + "/pair?t=pair_stub",
		})
	}))
	defer ts.Close()

	app := newAppWithRelay(t, ts.URL)
	app.setAccountKeyForTest(bytes.Repeat([]byte{0x33}, 32))

	out, err := app.CreatePairingToken()
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	if !out.Wrapped {
		t.Fatalf("Wrapped = false, want true")
	}
	if capturedBody["wrap"] == "" {
		t.Fatalf("relay saw no wrap in body")
	}
	if !strings.Contains(out.QRURL, "&k=") {
		t.Fatalf("QR missing &k=: %s", out.QRURL)
	}
}

func TestCreatePairingToken_LockedAccountKey_SkipsWrap(t *testing.T) {
	var capturedBody map[string]string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "pair_stub", "expires_at": time.Now().Add(5 * time.Minute).Unix(),
			"qr_url": ts.URL + "/pair?t=pair_stub",
		})
	}))
	defer ts.Close()

	app := newAppWithRelay(t, ts.URL) // account key not set
	out, err := app.CreatePairingToken()
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	if out.Wrapped {
		t.Fatalf("Wrapped = true, want false")
	}
	if capturedBody["wrap"] != "" {
		t.Fatalf("relay saw wrap in body: %q", capturedBody["wrap"])
	}
	if strings.Contains(out.QRURL, "&k=") {
		t.Fatalf("QR should not have &k=: %s", out.QRURL)
	}
}
