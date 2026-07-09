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

// The injected callback returns the local-mode gate state (enabled+openID+
// autoAttach) read from the keychain blob.
func TestFeishuRemoteTermState_LocalReadsKeychain(t *testing.T) {
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
	if err := svc.Store().SetCredentials(ctx, feishu.Credentials{AppID: "a", AppSecret: "s"}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if err := svc.Store().SetBound(ctx, "ou_user"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	ls, _ := svc.Store().(*feishu.LocalKeychainBindingStore)
	if err := ls.SetRemoteTerminalSettings(ctx, true, "ai"); err != nil {
		t.Fatalf("SetRemoteTerminalSettings: %v", err)
	}

	a := &App{feishuService: svc, feishuMode: "local", ctx: ctx}
	enabled, openID, autoAttach, ok := a.feishuRemoteTermState(ctx)
	if !ok || !enabled || openID != "ou_user" || autoAttach != "ai" {
		t.Fatalf("want ok+enabled+ou_user+ai, got ok=%v enabled=%v openID=%q autoAttach=%q",
			ok, enabled, openID, autoAttach)
	}
}

// No keychain blob → ok=false so the guard skips auto-attach.
func TestFeishuRemoteTermState_LocalNoBindingSkips(t *testing.T) {
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
	if _, _, _, ok := a.feishuRemoteTermState(a.ctx); ok {
		t.Fatal("want ok=false when no blob exists")
	}
}
