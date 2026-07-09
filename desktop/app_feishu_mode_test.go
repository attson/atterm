package main

import "testing"

func TestFeishuModePrefOrDefault_EmptyReturnsAuto(t *testing.T) {
	c := appConfig{}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, "auto")
	}
}

func TestFeishuModePrefOrDefault_KnownValuesPassthrough(t *testing.T) {
	for _, v := range []string{"auto", "local", "relay"} {
		c := appConfig{FeishuModePref: v}
		if got := c.FeishuModePrefOrDefault(); got != v {
			t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, v)
		}
	}
}

func TestFeishuModePrefOrDefault_UnknownFallsBackToAuto(t *testing.T) {
	c := appConfig{FeishuModePref: "garbage"}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q (defensive default)", got, "auto")
	}
}

func TestFeishuServiceConfig_TruthTable(t *testing.T) {
	cases := []struct {
		name        string
		pref        string
		relayURL    string
		relayToken  string
		relayPaused bool
		want        string // expected effective mode
	}{
		{"default empty no login", "", "", "", true, "local"},
		{"auto + logged in + uplink on", "auto", "wss://r", "tok", false, "relay"},
		{"auto + logged in + uplink paused", "auto", "wss://r", "tok", true, "local"},
		{"auto + no creds", "auto", "", "", false, "local"},
		{"local override while logged in", "local", "wss://r", "tok", false, "local"},
		{"local override no creds", "local", "", "", true, "local"},
		{"relay + logged in + uplink on", "relay", "wss://r", "tok", false, "relay"},
		{"relay fallback when uplink paused", "relay", "wss://r", "tok", true, "local"},
		{"relay fallback when no creds", "relay", "", "", false, "local"},
		{"unknown pref treated as auto", "garbage", "wss://r", "tok", false, "relay"},
	}

	a := newAppWithTempCfg(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appConfig{
				FeishuModePref:    tc.pref,
				RelayURL:          tc.relayURL,
				RelaySessionToken: tc.relayToken,
				RelayPaused:       tc.relayPaused,
			}
			_, mode := a.feishuServiceConfig(cfg)
			if mode != tc.want {
				t.Errorf("mode = %q; want %q", mode, tc.want)
			}
		})
	}
}

func TestGetFeishuModePref_DefaultsToAuto(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetFeishuModePref(); got != "auto" {
		t.Errorf("GetFeishuModePref() = %q; want %q", got, "auto")
	}
}

func TestSetFeishuModePref_PersistsAndRoundTrips(t *testing.T) {
	a := newAppWithTempCfg(t)
	for _, v := range []string{"local", "relay", "auto"} {
		if err := a.SetFeishuModePref(v); err != nil {
			t.Fatalf("SetFeishuModePref(%q): %v", v, err)
		}
		if got := a.GetFeishuModePref(); got != v {
			t.Errorf("after Set(%q), Get() = %q", v, got)
		}
	}
}

func TestSetFeishuModePref_RejectsInvalid(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetFeishuModePref("garbage"); err == nil {
		t.Fatal("expected error for invalid pref; got nil")
	}
	// Persisted value untouched.
	if got := a.GetFeishuModePref(); got != "auto" {
		t.Errorf("after rejected Set, Get() = %q; want %q", got, "auto")
	}
}

func TestGetFeishuEffectiveMode_EmptyBeforeInit(t *testing.T) {
	a := newAppWithTempCfg(t)
	// startFeishu / reconcileFeishuMode have not run, so feishuMode is unset.
	if got := a.GetFeishuEffectiveMode(); got != "" {
		t.Errorf("GetFeishuEffectiveMode() before init = %q; want %q", got, "")
	}
}
