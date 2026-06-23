package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/internal/safekeyring"
)

// relayStatusApp builds an App whose Feishu service is relay-backed and points
// at srv. feishuMode mirrors the production "relay" wiring.
func relayStatusApp(t *testing.T, srvURL string) *App {
	t.Helper()
	svc, err := feishu.NewService(feishu.ServiceConfig{
		Mode:       feishu.ModeRelay,
		RelayURL:   srvURL,
		RelayToken: func() string { return "tok" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return &App{feishuService: svc, feishuMode: "relay", ctx: context.Background()}
}

// TestGetFeishuStatus_ServiceNil: integration genuinely off (service failed to
// start). Reported as not-enabled with no error.
func TestGetFeishuStatus_ServiceNil(t *testing.T) {
	a := &App{ctx: context.Background()}
	resp, err := a.GetFeishuStatus()
	if err != nil {
		t.Fatalf("GetFeishuStatus must never return a Go error, got %v", err)
	}
	if resp.Enabled || resp.RelayDisabled || resp.Error != "" {
		t.Fatalf("nil service: want plain disabled, got %+v", resp)
	}
}

// TestGetFeishuStatus_RelayDisabled: relay admin turned Feishu off (503). The
// response distinguishes this from a transient failure and from not-configured.
func TestGetFeishuStatus_RelayDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "feishu integration disabled"})
	}))
	defer srv.Close()

	resp, err := relayStatusApp(t, srv.URL).GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if resp.Enabled || !resp.RelayDisabled || resp.Error != "" {
		t.Fatalf("want RelayDisabled, got %+v", resp)
	}
}

// TestGetFeishuStatus_LoadError: any other relay failure surfaces as a non-empty
// Error (so the UI can show it) rather than a thrown error the frontend swallows.
func TestGetFeishuStatus_LoadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resp, err := relayStatusApp(t, srv.URL).GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if resp.Enabled || resp.RelayDisabled || resp.Error == "" {
		t.Fatalf("want load Error set, got %+v", resp)
	}
}

// TestGetFeishuStatus_EnabledUnbound: relay reachable, no binding yet.
func TestGetFeishuStatus_EnabledUnbound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": false, "bound": false})
	}))
	defer srv.Close()

	resp, err := relayStatusApp(t, srv.URL).GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !resp.Enabled || resp.Bound || resp.RelayDisabled || resp.Error != "" {
		t.Fatalf("want enabled+unbound, got %+v", resp)
	}
}

// TestGetFeishuStatus_EnabledBound: relay reachable, binding present.
func TestGetFeishuStatus_EnabledBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured": true, "bound": true, "open_id": "ou_x",
		})
	}))
	defer srv.Close()

	resp, err := relayStatusApp(t, srv.URL).GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !resp.Enabled || !resp.Bound || resp.OpenID != "ou_x" {
		t.Fatalf("want enabled+bound, got %+v", resp)
	}
}

// TestGetFeishuStatus_RelayConfiguredUnbound: credentials saved on the relay but
// not yet bound. The status must report Configured + the event callback URL so
// the UI can show the relay endpoint to paste into Feishu (issue: reopened
// dialog looked blank).
func TestGetFeishuStatus_RelayConfiguredUnbound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":   true,
			"bound":        false,
			"callback_url": "https://relay.example.com/v1/feishu/events/abc123",
			"app_id_hash":  "abc123",
		})
	}))
	defer srv.Close()

	resp, err := relayStatusApp(t, srv.URL).GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !resp.Enabled || resp.Bound || !resp.Configured {
		t.Fatalf("want enabled+configured+unbound, got %+v", resp)
	}
	if resp.CallbackURL == "" || resp.AppIDHash == "" {
		t.Fatalf("want callback_url + app_id_hash populated, got %+v", resp)
	}
}

// TestGetFeishuStatus_LocalConfigured: local mode credentials persist to the
// keyring (here forced to the 0600 file store). Status must report Configured
// with the App ID echoed and NO callback URL — local mode has no public event
// endpoint (events arrive over the long connection).
func TestGetFeishuStatus_LocalConfigured(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})

	svc, err := feishu.NewService(feishu.ServiceConfig{Mode: feishu.ModeLocal})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx := context.Background()
	if err := svc.Store().SetCredentials(ctx, feishu.Credentials{
		AppID: "cli_app", AppSecret: "sec", EncryptKey: "enc", VerifyToken: "vt",
	}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	a := &App{feishuService: svc, feishuMode: "local", ctx: ctx}
	resp, err := a.GetFeishuStatus()
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !resp.Enabled || resp.Bound || !resp.Configured {
		t.Fatalf("want enabled+configured+unbound, got %+v", resp)
	}
	if resp.AppID != "cli_app" {
		t.Fatalf("want AppID echoed, got %q", resp.AppID)
	}
	if resp.CallbackURL != "" {
		t.Fatalf("local mode must have no callback URL, got %q", resp.CallbackURL)
	}
	if resp.AppIDHash == "" {
		t.Fatalf("want app_id_hash populated, got %+v", resp)
	}
}
