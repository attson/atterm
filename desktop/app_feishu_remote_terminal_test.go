package main

import (
	"context"
	"testing"

	"github.com/attson/atterm/desktop/feishu"
	"github.com/attson/atterm/internal/safekeyring"
)

// In local mode the remote-terminal settings round-trip through the keychain
// blob, independent of the relay sqlite store (which has no row for the user).
func TestRemoteTerminalSettings_LocalRoundTrip(t *testing.T) {
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
	a := &App{feishuService: svc, feishuMode: "local", ctx: context.Background()}

	if err := a.SetFeishuRemoteTerminalSettings(true, "all"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := a.GetFeishuRemoteTerminalSettings()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled || got.AutoAttach != "all" {
		t.Fatalf("want enabled+all, got %+v", got)
	}
}
