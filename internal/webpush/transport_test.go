package webpush

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	wpgo "github.com/SherClockHolmes/webpush-go"
)

type fakeHTTPClient struct {
	lastReq  *http.Request
	respCode int
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.lastReq = req
	return &http.Response{
		StatusCode: f.respCode,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func TestSendNotificationPostsToEndpoint(t *testing.T) {
	// Use real VAPID keys so webpush-go can build a valid JWT.
	priv, pub, err := wpgo.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}

	fake := &fakeHTTPClient{respCode: 201}
	tr := newTransport(priv, pub, "mailto:test@example.com", fake)

	// Use real P-256 subscription keys from the webpush-go test suite.
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"

	resp, err := tr.Send(context.Background(), sub, []byte(`{"title":"x"}`))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d; want 201", resp.StatusCode)
	}
	if fake.lastReq == nil {
		t.Fatal("HTTPClient.Do not invoked")
	}
	if fake.lastReq.URL.String() != sub.Endpoint {
		t.Fatalf("URL = %s; want %s", fake.lastReq.URL.String(), sub.Endpoint)
	}
	auth := fake.lastReq.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "vapid t=") {
		t.Fatalf("Authorization = %q; want vapid t=... prefix", auth)
	}
}
