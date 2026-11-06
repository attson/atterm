package main

import (
	"testing"
	"time"
)

func preserveDesktopDebugConfig(t *testing.T) {
	t.Helper()
	previous := desktopDebugConfig.Load()
	t.Cleanup(func() { desktopDebugConfig.Store(previous) })
}

func TestDesktopDebugAllExcludesPayload(t *testing.T) {
	preserveDesktopDebugConfig(t)
	configureDesktopDebug(true, []string{desktopDebugScopeAll}, "")
	if !desktopDebugScopeEnabled(desktopDebugScopeStateSync) {
		t.Fatal("* did not enable a normal scope")
	}
	if desktopDebugScopeEnabled(desktopDebugScopePayload) {
		t.Fatal("* enabled payload")
	}
}

func TestDesktopDebugScopeSelectionAndExpiry(t *testing.T) {
	preserveDesktopDebugConfig(t)
	configureDesktopDebug(true, []string{desktopDebugScopeStateSync}, "")
	if !desktopDebugScopeEnabled(desktopDebugScopeStateSync) || desktopDebugScopeEnabled(desktopDebugScopeConnection) {
		t.Fatal("selected scope gate mismatch")
	}
	configureDesktopDebug(true, []string{desktopDebugScopeStateSync}, time.Now().Add(-time.Second).Format(time.RFC3339))
	if desktopDebugScopeEnabled(desktopDebugScopeStateSync) {
		t.Fatal("expired debug scope remained enabled")
	}
}

func TestDesktopDebugSensitiveTagsRequirePayload(t *testing.T) {
	for _, tag := range []string{"recovery", "ai-sid", "paste", "feishu-anchor"} {
		if got := desktopDebugScopeForTag(tag); got != desktopDebugScopePayload {
			t.Errorf("scope for %q = %q, want payload", tag, got)
		}
	}
}
