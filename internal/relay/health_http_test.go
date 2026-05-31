package relay

import "testing"

func TestIsMobileOriginCompatible(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
		want    bool
	}{
		{"empty (wildcard)", nil, true},
		{"capacitor", []string{"capacitor://localhost"}, true},
		{"capacitor wildcard", []string{"capacitor://*"}, true},
		{"ionic", []string{"ionic://localhost"}, true},
		{"https-localhost", []string{"https://localhost"}, true},
		{"https-localhost-port", []string{"https://localhost:1234"}, true},
		{"null", []string{"null"}, true},
		{"wails only", []string{"wails.localhost"}, false},
		{"mixed has capacitor", []string{"wails.localhost", "capacitor://localhost"}, true},
		{"mixed no capacitor", []string{"wails.localhost", "https://example.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMobileOriginCompatible(tc.origins)
			if got != tc.want {
				t.Fatalf("got %v want %v for origins=%v", got, tc.want, tc.origins)
			}
		})
	}
}
