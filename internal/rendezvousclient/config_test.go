package rendezvousclient

import (
	"reflect"
	"testing"
)

func TestResolveConfigModesAndEndpoints(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want ResolvedConfig
	}{
		{
			name: "unset stays disabled with default STUN",
			cfg:  Config{},
			want: ResolvedConfig{
				Mode: ModeDisabled, STUNMode: STUNModeDefault,
				STUNURLs: []string{DefaultSTUNURL},
			},
		},
		{
			name: "official derives stable endpoints",
			cfg:  Config{Mode: ModeOfficial},
			want: ResolvedConfig{
				Mode: ModeOfficial, BaseURL: OfficialURL,
				WebSocketURL: "wss://rendezvous.atterm.dev/v1/connect",
				HealthURL:    "https://rendezvous.atterm.dev/healthz",
				STUNMode:     STUNModeDefault, STUNURLs: []string{DefaultSTUNURL},
			},
		},
		{
			name: "custom canonicalizes base and custom STUN",
			cfg: Config{
				Mode: ModeCustom, URL: "WSS://RV.EXAMPLE.COM:8443/",
				STUNMode: STUNModeCustom,
				STUNURLs: []string{"stun:STUN.EXAMPLE.COM:3478", "stuns:secure.example.com:5349"},
			},
			want: ResolvedConfig{
				Mode: ModeCustom, BaseURL: "https://rv.example.com:8443",
				WebSocketURL: "wss://rv.example.com:8443/v1/connect",
				HealthURL:    "https://rv.example.com:8443/healthz",
				STUNMode:     STUNModeCustom,
				STUNURLs:     []string{"stun:stun.example.com:3478", "stuns:secure.example.com:5349"},
			},
		},
		{
			name: "STUN can be explicitly disabled",
			cfg:  Config{Mode: ModeDisabled, STUNMode: STUNModeDisabled},
			want: ResolvedConfig{Mode: ModeDisabled, STUNMode: STUNModeDisabled, STUNURLs: []string{}},
		},
		{
			name: "TURN is canonicalized separately from STUN",
			cfg: Config{
				Mode: ModeOfficial, TURNEnabled: true,
				TURNURLs:     []string{"turn:TURN.EXAMPLE.COM:3478?transport=udp", "turns:secure.example.com:5349?transport=tcp"},
				TURNUsername: " device-user ", TURNCredential: "secret value",
			},
			want: ResolvedConfig{
				Mode: ModeOfficial, BaseURL: OfficialURL,
				WebSocketURL: "wss://rendezvous.atterm.dev/v1/connect",
				HealthURL:    "https://rendezvous.atterm.dev/healthz",
				STUNMode:     STUNModeDefault, STUNURLs: []string{DefaultSTUNURL},
				TURNEnabled:  true,
				TURNURLs:     []string{"turn:turn.example.com:3478?transport=udp", "turns:secure.example.com:5349?transport=tcp"},
				TURNUsername: "device-user", TURNCredential: "secret value",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveConfig(tt.cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveConfig()=%+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveConfigRejectsInvalidTURNConfiguration(t *testing.T) {
	valid := Config{
		TURNEnabled: true, TURNURLs: []string{"turn:turn.example.com:3478"},
		TURNUsername: "user", TURNCredential: "secret",
	}
	for _, mutate := range []func(*Config){
		func(cfg *Config) { cfg.TURNURLs = nil },
		func(cfg *Config) { cfg.TURNURLs = []string{"stun:stun.example.com:3478"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:user@turn.example.com:3478"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:turn.example.com:3478/path"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:turn.example.com:3478?token=secret"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:turn.example.com:3478?transport=sctp"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turns:turn.example.com:5349?transport=udp"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:turn.example.com:70000"} },
		func(cfg *Config) { cfg.TURNURLs = []string{"turn:turn.example.com:3478", "TURN:TURN.EXAMPLE.COM:3478"} },
		func(cfg *Config) { cfg.TURNUsername = "" },
		func(cfg *Config) { cfg.TURNCredential = "" },
	} {
		cfg := valid
		cfg.TURNURLs = append([]string(nil), valid.TURNURLs...)
		mutate(&cfg)
		if _, err := ResolveConfig(cfg, false); err == nil {
			t.Fatalf("accepted invalid TURN config: %+v", cfg)
		}
	}
}

func TestResolveConfigPreservesDisabledTURNAddressWithoutCredential(t *testing.T) {
	got, err := ResolveConfig(Config{
		TURNURLs:     []string{"turn:TURN.EXAMPLE.COM:3478?transport=tcp"},
		TURNUsername: " saved-user ",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.TURNEnabled || !reflect.DeepEqual(got.TURNURLs, []string{"turn:turn.example.com:3478?transport=tcp"}) ||
		got.TURNUsername != "saved-user" || got.TURNCredential != "" {
		t.Fatalf("disabled TURN config=%+v", got)
	}
}

func TestResolveConfigRejectsUnsafeURLsAndUnsupportedICE(t *testing.T) {
	for _, raw := range []string{
		"http://rendezvous.example",
		"ws://rendezvous.example",
		"https://user@rendezvous.example",
		"https://:443",
		"https://rendezvous.example/v1",
		"https://rendezvous.example?token=secret",
		"https://rendezvous.example#fragment",
	} {
		if _, err := ResolveConfig(Config{Mode: ModeCustom, URL: raw}, false); err == nil {
			t.Fatalf("accepted unsafe Rendezvous URL %q", raw)
		}
	}
	for _, raw := range []string{
		"turn:turn.example.com:3478",
		"stun:user@stun.example.com:3478",
		"stun:stun.example.com:3478?transport=udp",
	} {
		if _, err := ResolveConfig(Config{
			Mode: ModeDisabled, STUNMode: STUNModeCustom, STUNURLs: []string{raw},
		}, false); err == nil {
			t.Fatalf("accepted unsupported ICE URL %q", raw)
		}
	}
}

func TestResolveConfigAllowsExplicitLoopbackDevelopmentOnly(t *testing.T) {
	cfg := Config{Mode: ModeCustom, URL: "http://127.0.0.1:8081"}
	if _, err := ResolveConfig(cfg, false); err == nil {
		t.Fatal("accepted insecure loopback without explicit opt-in")
	}
	got, err := ResolveConfig(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.WebSocketURL != "ws://127.0.0.1:8081/v1/connect" || got.HealthURL != "http://127.0.0.1:8081/healthz" {
		t.Fatalf("loopback endpoints=%+v", got)
	}
	if _, err := ResolveConfig(Config{Mode: ModeCustom, URL: "http://192.0.2.1:8081"}, true); err == nil {
		t.Fatal("accepted insecure non-loopback URL")
	}
}
