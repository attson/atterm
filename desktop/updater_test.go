package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRoundTripper fails any request — used to assert "no network calls".
type fakeRoundTripper struct {
	called int
}

func (f *fakeRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.called++
	return nil, http.ErrUseLastResponse
}

func TestUpdater_DevVersion_NeverCallsNetwork(t *testing.T) {
	rt := &fakeRoundTripper{}
	u := newUpdater(updaterConfig{
		current: "dev",
		repo:    "attson/atterm",
		client:  &http.Client{Transport: rt},
	})
	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if rt.called != 0 {
		t.Fatalf("dev version made %d network calls; expected 0", rt.called)
	}
	st := u.State()
	if st.Available {
		t.Fatalf("dev version should never report Available=true")
	}
	if st.Current != "dev" {
		t.Fatalf("State().Current = %q; want %q", st.Current, "dev")
	}
}

func TestUpdater_EmptyVersion_TreatedAsDev(t *testing.T) {
	rt := &fakeRoundTripper{}
	u := newUpdater(updaterConfig{
		current: "",
		repo:    "attson/atterm",
		client:  &http.Client{Transport: rt},
	})
	_ = u.Check(context.Background(), true)
	if rt.called != 0 {
		t.Fatalf("empty version made %d network calls; expected 0", rt.called)
	}
}

// silence-the-unused-import warnings for httptest in later tasks.
var _ = httptest.NewServer

func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "atterm-desktop-linux-amd64.tar.gz"},
		{"darwin", "arm64", "atterm-desktop-darwin-arm64.zip"},
		{"windows", "amd64", "atterm-desktop-windows-amd64.zip"},
	}
	for _, c := range cases {
		got, err := assetNameForPlatform(c.goos, c.goarch)
		if err != nil {
			t.Errorf("assetNameForPlatform(%q,%q) err = %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("assetNameForPlatform(%q,%q) = %q; want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestAssetNameForPlatform_Unsupported(t *testing.T) {
	cases := []struct{ goos, goarch string }{
		{"darwin", "amd64"},
		{"linux", "arm64"},
		{"freebsd", "amd64"},
	}
	for _, c := range cases {
		_, err := assetNameForPlatform(c.goos, c.goarch)
		if err == nil {
			t.Errorf("expected error for %s/%s", c.goos, c.goarch)
		}
	}
}
