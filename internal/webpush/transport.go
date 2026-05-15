package webpush

import (
	"bytes"
	"context"
	"net/http"

	wpgo "github.com/SherClockHolmes/webpush-go"
)

// transport wraps webpush-go.SendNotificationWithContext with an injectable
// HTTPClient so tests can capture requests without hitting a real push
// service.
type transport struct {
	privateKey string
	publicKey  string
	subject    string
	httpClient wpgo.HTTPClient
}

func newTransport(priv, pub, subject string, httpClient wpgo.HTTPClient) *transport {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &transport{privateKey: priv, publicKey: pub, subject: subject, httpClient: httpClient}
}

// Send POSTs an encrypted Web Push notification carrying msg to sub.
// Returns the raw response (caller inspects status code) and any transport
// error. Callers are expected to consume / close the response body.
func (t *transport) Send(ctx context.Context, sub Subscription, msg []byte) (*http.Response, error) {
	wpSub := &wpgo.Subscription{
		Endpoint: sub.Endpoint,
		Keys: wpgo.Keys{
			Auth:   sub.Keys.Auth,
			P256dh: sub.Keys.P256dh,
		},
	}
	opts := &wpgo.Options{
		HTTPClient:      t.httpClient,
		TTL:             30, // seconds
		Subscriber:      t.subject,
		VAPIDPublicKey:  t.publicKey,
		VAPIDPrivateKey: t.privateKey,
	}
	return wpgo.SendNotificationWithContext(ctx, bytes.Clone(msg), wpSub, opts)
}
