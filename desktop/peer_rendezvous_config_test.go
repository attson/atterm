package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
	for _, localValue := range []string{"local-only-rendezvous.example", "local-only-stun.example"} {
		if strings.Contains(string(encoded), localValue) {
			t.Fatalf("local reachability config escaped into replication/export: %s", localValue)
		}
	}
}
