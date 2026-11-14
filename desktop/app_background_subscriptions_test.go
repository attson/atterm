package main

import (
	"encoding/json"
	"testing"
)

func TestPauseRemoteSubscriptionsDefaultsOffAndPersistsLocally(t *testing.T) {
	var cfg appConfig
	if err := json.Unmarshal([]byte(`{"locale_preference":"en"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	a := newAppWithTempCfg(t)
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if a.GetPauseRemoteSubscriptionsInBackground() {
		t.Fatal("old configs must leave background pausing disabled")
	}
	for _, enabled := range []bool{true, false} {
		if err := a.SetPauseRemoteSubscriptionsInBackground(enabled); err != nil {
			t.Fatal(err)
		}
		restored := &App{cfgStore: loadConfig()}
		if restored.GetPauseRemoteSubscriptionsInBackground() != enabled {
			t.Fatalf("preference did not survive reload: want %v", enabled)
		}
		if len(restored.cfgStore.Get().PrefsMeta) != 0 {
			t.Fatal("device preference must not enter account sync")
		}
	}
}

func TestPauseRemoteSubscriptionsRejectsUnavailableStore(t *testing.T) {
	a := &App{}
	if a.GetPauseRemoteSubscriptionsInBackground() {
		t.Fatal("missing store must default off")
	}
	if err := a.SetPauseRemoteSubscriptionsInBackground(true); err == nil {
		t.Fatal("missing store must reject save")
	}
}
