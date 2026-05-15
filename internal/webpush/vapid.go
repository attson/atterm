// Package webpush implements self-hosted Web Push delivery: VAPID keypair
// management, browser subscription state, and dispatch of notifications via
// RFC 8030 / 8291. The relay calls into this package; nothing else does.
package webpush

import wpgo "github.com/SherClockHolmes/webpush-go"

// generateVAPIDKeypair returns a fresh P-256 keypair as base64url-encoded
// strings (matching the format the JavaScript Push API expects for
// applicationServerKey).
func generateVAPIDKeypair() (privateKey, publicKey string, err error) {
	return wpgo.GenerateVAPIDKeys()
}
