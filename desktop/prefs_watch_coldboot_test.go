package main

import (
	"bytes"
	"os"
	"testing"
)

// The preference watch used to never start on a cold boot.
//
// startup calls applyRelayConfig, which calls applyRelayPrefsWatch, which
// returns early when a.prefsSync == nil -- and a.prefsSync was not constructed
// until sixteen lines further down startup. So the watch only ever came up on
// a LATER applyRelayConfig: a relay reconfigure, or a login. Until the user
// happened to do one of those, a preference changed on another machine simply
// never arrived, which from the outside is indistinguishable from "sync is
// broken" -- exactly the impression the sync indicator this feature adds would
// otherwise have to explain away.
//
// This asserts the ordering in source rather than by running startup, because
// startup restores keychain material, installs hook binaries and starts the
// updater, none of which a test wants, and no test calls it today. A source
// assertion is coarse, but it fails for exactly the reason the bug existed:
// the call site sitting above the construction it depends on.
//
// The search is confined to startup's own body. A first attempt at this test
// searched the whole file from the engine construction onward and passed
// vacuously, because applyRelayPrefsWatch's own definition lives further down
// app.go and was matched instead of any call inside startup.
func TestPrefsWatchStartsAfterTheSyncEngineExists(t *testing.T) {
	data, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	start := bytes.Index(data, []byte("func (a *App) startup(ctx context.Context) {"))
	if start < 0 {
		t.Fatal("could not find func (a *App) startup in app.go")
	}
	rest := data[start:]
	end := bytes.Index(rest[1:], []byte("\nfunc "))
	if end < 0 {
		t.Fatal("could not find the end of startup's body")
	}
	body := rest[:end]

	engine := bytes.Index(body, []byte("a.prefsSync = prefssync.NewEngine("))
	if engine < 0 {
		t.Fatal("startup no longer constructs the prefsSync engine; this test needs rewriting")
	}
	watch := bytes.Index(body[engine:], []byte("a.applyRelayPrefsWatch("))
	if watch < 0 {
		t.Fatal("startup does not call applyRelayPrefsWatch after constructing the engine. " +
			"applyRelayPrefsWatch no-ops while a.prefsSync is nil, so without a call placed after " +
			"the engine exists, the preference watch never starts on a cold boot at all")
	}
}
