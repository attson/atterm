package main

import "testing"

func TestNotificationsEnabledDefaultsToTrue(t *testing.T) {
	cfg := appConfig{}
	if !cfg.NotificationsEnabledOrDefault() {
		t.Fatal("fresh config should default NotificationsEnabled to true")
	}
}

func TestNotificationsEnabledRoundtripFalse(t *testing.T) {
	f := false
	cfg := appConfig{NotificationsEnabled: &f}
	if cfg.NotificationsEnabledOrDefault() {
		t.Fatal("explicit false should not be overridden by the default")
	}
}

func TestNotificationsEnabledRoundtripTrue(t *testing.T) {
	tr := true
	cfg := appConfig{NotificationsEnabled: &tr}
	if !cfg.NotificationsEnabledOrDefault() {
		t.Fatal("explicit true should be preserved")
	}
}

func TestAINotificationsOnlyOrDefault(t *testing.T) {
	if got := (appConfig{}).AINotificationsOnlyOrDefault(); !got {
		t.Errorf("nil AINotificationsOnly = %v, want true (default on)", got)
	}
	f := false
	if got := (appConfig{AINotificationsOnly: &f}).AINotificationsOnlyOrDefault(); got {
		t.Errorf("&false = %v, want false", got)
	}
	tr := true
	if got := (appConfig{AINotificationsOnly: &tr}).AINotificationsOnlyOrDefault(); !got {
		t.Errorf("&true = %v, want true", got)
	}
}
