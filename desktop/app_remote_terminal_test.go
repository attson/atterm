package main

import (
	"errors"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestGetFeishuRemoteTerminalSettings_DefaultsWhenNoBinding(t *testing.T) {
	// App with a real relayHost but no Feishu binding seeded: returns defaults.
	a := newRecoveryTestApp(t)
	got, err := a.GetFeishuRemoteTerminalSettings()
	if err != nil {
		t.Fatalf("GetFeishuRemoteTerminalSettings() error: %v", err)
	}
	if got.Enabled {
		t.Errorf("Enabled = true; want false (default)")
	}
	if got.AutoAttach != "ai" {
		t.Errorf("AutoAttach = %q; want %q (default)", got.AutoAttach, "ai")
	}
}

func TestGetFeishuRemoteTerminalSettings_NilHost(t *testing.T) {
	// App without a relayHost should return defaults gracefully.
	a := newAppWithTempCfg(t) // no host
	got, err := a.GetFeishuRemoteTerminalSettings()
	if err != nil {
		t.Fatalf("GetFeishuRemoteTerminalSettings() with nil host: %v", err)
	}
	if got.Enabled || got.AutoAttach != "ai" {
		t.Errorf("want defaults {false, ai}, got {%v, %q}", got.Enabled, got.AutoAttach)
	}
}

func TestSetFeishuRemoteTerminalSettings_NilHost(t *testing.T) {
	// Without a host the method should return a clean error.
	a := newAppWithTempCfg(t)
	err := a.SetFeishuRemoteTerminalSettings(true, "ai")
	if err == nil {
		t.Fatal("expected error when host is nil; got nil")
	}
}

func TestSetFeishuRemoteTerminalSettings_NoBindingReturnsError(t *testing.T) {
	// relayHost exists but no Feishu binding row ⇒ ErrFeishuBindingNotFound.
	a := newRecoveryTestApp(t)
	err := a.SetFeishuRemoteTerminalSettings(true, "ai")
	if err == nil {
		t.Fatal("expected error when binding does not exist; got nil")
	}
	if !errors.Is(err, userstore.ErrFeishuBindingNotFound) {
		t.Errorf("error = %v; want ErrFeishuBindingNotFound", err)
	}
}

func TestSetFeishuRemoteTerminalSettings_RejectsInvalidAutoAttach(t *testing.T) {
	// Even with a missing binding the autoAttach validation fires first.
	// Use the sqliteStore directly to insert a row, bypassing cipher (which
	// requires the full keychain setup unavailable in unit tests).
	a := newRecoveryTestApp(t)
	// Directly exec a minimal row so SetRemoteTerminalSettings has something
	// to UPDATE.  We bypass UpsertFeishuBinding because that needs a cipher.
	_, err := a.host.sqliteStore.DB().ExecContext(
		a.ctx,
		`INSERT INTO feishu_bindings (user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc, created_at)
		 VALUES (?, 'hash', X'', X'', X'', X'', 0)`,
		a.host.adminUserID,
	)
	if err != nil {
		t.Fatalf("seed binding row: %v", err)
	}

	if err := a.SetFeishuRemoteTerminalSettings(true, "garbage"); err == nil {
		t.Fatal("expected error for invalid autoAttach; got nil")
	}
}
