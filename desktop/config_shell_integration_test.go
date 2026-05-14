package main

import "testing"

func TestShellIntegrationEnabledOrDefaultDefaultsTrue(t *testing.T) {
	cfg := appConfig{}
	if got := cfg.ShellIntegrationEnabledOrDefault(); got != true {
		t.Fatalf("ShellIntegrationEnabledOrDefault default = %v; want true", got)
	}
}

func TestShellIntegrationEnabledOrDefaultRoundTripsFalse(t *testing.T) {
	v := false
	cfg := appConfig{ShellIntegrationEnabled: &v}
	if got := cfg.ShellIntegrationEnabledOrDefault(); got != false {
		t.Fatalf("ShellIntegrationEnabledOrDefault(false) = %v; want false", got)
	}
}

func TestCommandNotifyThresholdSecondsOrDefaultDefaultsTo10(t *testing.T) {
	cfg := appConfig{}
	if got := cfg.CommandNotifyThresholdSecondsOrDefault(); got != 10 {
		t.Fatalf("CommandNotifyThresholdSecondsOrDefault default = %d; want 10", got)
	}
}

func TestCommandNotifyThresholdSecondsClampsToRange(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-5, 1},
		{0, 1},
		{1, 1},
		{30, 30},
		{600, 600},
		{1200, 600},
	}
	for _, tc := range cases {
		v := tc.in
		cfg := appConfig{CommandNotifyThresholdSeconds: &v}
		if got := cfg.CommandNotifyThresholdSecondsOrDefault(); got != tc.want {
			t.Fatalf("CommandNotifyThresholdSecondsOrDefault(%d) = %d; want %d", tc.in, got, tc.want)
		}
	}
}
