package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/pion/webrtc/v4"
)

func TestPeerRendezvousConfigPersistsWithoutChangingRelay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{cfgStore: loadConfig()}
	cfg := app.cfgStore.Get()
	cfg.RelayURL = "wss://relay.example"
	cfg.RelaySessionToken = "atk_preserved"
	if err := app.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	err := app.SetPeerRendezvousConfig(SetPeerRendezvousConfigReq{
		Mode: "custom", URL: "https://rv.example.com",
		STUNMode: "custom", STUNURLs: []string{"stun:stun.example.com:3478"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.GetPeerRendezvousConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "custom" || got.URL != "https://rv.example.com" || got.WebSocketURL != "wss://rv.example.com/v1/connect" ||
		got.STUNMode != "custom" || !reflect.DeepEqual(got.STUNURLs, []string{"stun:stun.example.com:3478"}) {
		t.Fatalf("config=%+v", got)
	}
	persisted := app.cfgStore.Get()
	if persisted.RelayURL != "wss://relay.example" || persisted.RelaySessionToken != "atk_preserved" {
		t.Fatalf("Rendezvous update changed Relay config: %+v", persisted)
	}
}

func TestPeerRendezvousConfigDefaultsDisabledAndRejectsPublicPlaintext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{cfgStore: loadConfig()}
	got, err := app.GetPeerRendezvousConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "disabled" || got.URL != "" || got.STUNMode != "default" {
		t.Fatalf("default config=%+v", got)
	}
	if err := app.SetPeerRendezvousConfig(SetPeerRendezvousConfigReq{
		Mode: "custom", URL: "ws://rendezvous.example",
	}); err == nil {
		t.Fatal("accepted plaintext public Rendezvous")
	}
}

func TestPeerRendezvousConfigStaysOutOfReplicationAndExport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{cfgStore: loadConfig()}
	if err := app.SetPeerRendezvousConfig(SetPeerRendezvousConfigReq{
		Mode: "custom", URL: "https://local-only-rendezvous.example",
		STUNMode: "custom", STUNURLs: []string{"stun:local-only-stun.example:3478"},
		TURNEnabled: true, TURNURLs: []string{"turn:local-only-turn.example:3478"},
		TURNUsername: "local-turn-user", TURNCredential: "local-turn-secret",
	}); err != nil {
		t.Fatal(err)
	}

	cfg := app.cfgStore.Get()
	if len(cfg.PrefsMeta) != 0 {
		t.Fatalf("Rendezvous update scheduled Relay prefs sync: %+v", cfg.PrefsMeta)
	}
	records, err := peerBootstrapRecords(cfg)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := app.BuildConfigExport(false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(struct {
		Records any
		Export  ConfigExport
	}{records, exported})
	if err != nil {
		t.Fatal(err)
	}
	for _, localValue := range []string{
		"local-only-rendezvous.example", "local-only-stun.example", "local-only-turn.example",
		"local-turn-user", "local-turn-secret",
	} {
		if strings.Contains(string(encoded), localValue) {
			t.Fatalf("local reachability config escaped into replication/export: %s", localValue)
		}
	}
}

func TestPeerTURNCredentialUsesKeychainAndCanBePreservedOrCleared(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{cfgStore: loadConfig()}
	request := SetPeerRendezvousConfigReq{
		Mode: "official", STUNMode: "default",
		TURNEnabled: true, TURNURLs: []string{"turn:turn.example.com:3478?transport=tcp"},
		TURNUsername: "turn-user", TURNCredential: "turn-secret",
	}
	if err := app.SetPeerRendezvousConfig(request); err != nil {
		t.Fatal(err)
	}

	got, err := app.GetPeerRendezvousConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.TURNEnabled || !got.TURNCredentialConfigured || got.TURNUsername != "turn-user" ||
		!reflect.DeepEqual(got.TURNURLs, request.TURNURLs) {
		t.Fatalf("TURN config=%+v", got)
	}
	credential, err := loadPeerTURNCredential()
	if err != nil || credential != "turn-secret" {
		t.Fatalf("credential=%q err=%v", credential, err)
	}
	for name, value := range map[string]any{"api": got, "config": app.cfgStore.Get()} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "turn-secret") || strings.Contains(string(encoded), "turn_credential\"") {
			t.Fatalf("%s exposed TURN credential: %s", name, encoded)
		}
	}
	persistedJSON, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persistedJSON), "turn-secret") || strings.Contains(string(persistedJSON), "turn_credential\"") {
		t.Fatalf("config.json exposed TURN credential: %s", persistedJSON)
	}

	request.TURNCredential = ""
	request.TURNURLs = []string{"turns:turn.example.com:5349?transport=tcp"}
	if err := app.SetPeerRendezvousConfig(request); err != nil {
		t.Fatal(err)
	}
	credential, err = loadPeerTURNCredential()
	if err != nil || credential != "turn-secret" {
		t.Fatalf("preserved credential=%q err=%v", credential, err)
	}

	request.TURNEnabled = false
	if err := app.SetPeerRendezvousConfig(request); err != nil {
		t.Fatal(err)
	}
	credential, err = loadPeerTURNCredential()
	if err != nil || credential != "" {
		t.Fatalf("cleared credential=%q err=%v", credential, err)
	}
}

func TestPeerTURNRequiresCredentialOnFirstEnable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := &App{cfgStore: loadConfig()}
	err := app.SetPeerRendezvousConfig(SetPeerRendezvousConfigReq{
		Mode: "official", STUNMode: "default", TURNEnabled: true,
		TURNURLs: []string{"turn:turn.example.com:3478"}, TURNUsername: "turn-user",
	})
	if err == nil {
		t.Fatal("enabled TURN without a credential")
	}
	if app.cfgStore.Get().PeerTURNEnabled {
		t.Fatal("persisted invalid TURN configuration")
	}
}

func TestPeerWebRTCConfigurationSeparatesSTUNAndTURNCredentials(t *testing.T) {
	got := peerWebRTCConfiguration(rendezvousclient.ResolvedConfig{
		STUNURLs:    []string{"stun:stun.example.com:3478"},
		TURNEnabled: true, TURNURLs: []string{"turn:turn.example.com:3478"},
		TURNUsername: "turn-user", TURNCredential: "turn-secret",
	})
	if len(got.ICEServers) != 2 {
		t.Fatalf("ICE servers=%+v", got.ICEServers)
	}
	if got.ICEServers[0].Username != "" || got.ICEServers[0].Credential != nil {
		t.Fatalf("STUN server received TURN credentials: %+v", got.ICEServers[0])
	}
	turn := got.ICEServers[1]
	if turn.Username != "turn-user" || turn.Credential != "turn-secret" || turn.CredentialType != webrtc.ICECredentialTypePassword {
		t.Fatalf("TURN server=%+v", turn)
	}
}
