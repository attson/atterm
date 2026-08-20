package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

// TestUplink_E2E_OUTSealedThroughRelay is the end-to-end proof that the
// M2a-c stack actually delivers on its goal: when a desktop agent has
// E2EE unlocked, the OUT bytes that cross the wire to the remote relay
// (and on to attached clients) are XChaCha20-Poly1305 ciphertext, not
// plaintext — and decrypting with the same account_key recovers the
// original payload byte-for-byte.
//
// Path under test:
//
//	PTY → relayHost local Session → uplink forwarder (seals with M2b
//	sealOutFrame) → remote relay (M2c marks mirror content-opaque, no
//	OSC parsing) → /client subscriber (this test) → AEAD open
//
// The marker string deliberately includes ANSI escape bytes so a
// false-negative "didn't actually encrypt" path would surface as a
// human-readable substring match against the raw transcript.
func TestUplink_E2E_OUTSealedThroughRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}
	remoteAddr, remoteTok := startE2ERemoteRelay(t)
	host := newTestRelayHost(t)

	// Fresh account_key shared by the uplink seal side and the client
	// open side. Real deployments derive this via OPAQUE login (M1c);
	// here we synthesise it directly to keep the test focused on the
	// encryption surface.
	accountKey := make([]byte, 32)
	if _, err := rand.Read(accountKey); err != nil {
		t.Fatalf("rand account_key: %v", err)
	}

	// A small command whose output is recognisable. The marker is what
	// we'll search for in plaintext (must NOT appear in ciphertext) and
	// what we'll assert recovered after AEAD open.
	const marker = "E2EE_SEAL_MARKER_4B62D81F"
	sid, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "echo " + marker + "; sleep 0.5"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}

	uplinkCtx, uplinkCancel := context.WithCancel(context.Background())
	defer uplinkCancel()
	u := newUplink("ws://"+remoteAddr, remoteTok, proto.RemotePermissionFull, host, nil, func() []byte {
		return accountKey
	}, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	go u.Run(uplinkCtx)

	// Wait for the relay to learn about the session via ANNOUNCE before
	// we attach — otherwise the attach races the publish.
	if !waitForRemoteSession(t, remoteAddr, remoteTok, sid.String(), 3*time.Second) {
		t.Fatalf("remote relay never learned about session %s", sid)
	}

	// Attach as a /client and read OUT frames. Use a fresh context so the
	// uplink keeps running until we're done verifying.
	cliCtx, cliCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cliCancel()
	cliConn, _, err := websocket.Dial(cliCtx, fmt.Sprintf("ws://%s/client", remoteAddr), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + remoteTok}},
	})
	if err != nil {
		t.Fatalf("dial /client: %v", err)
	}
	defer cliConn.Close(websocket.StatusNormalClosure, "")
	cliConn.SetReadLimit(2 * 1024 * 1024)

	atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: "viewer", ClientName: "viewer"})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: atp,
	})); err != nil {
		t.Fatal(err)
	}

	// Derive the same session_key the agent used and collect at least one
	// OUT chunk whose ciphertext decrypts cleanly under that key.
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, sid)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}

	var rawCipherTranscript strings.Builder
	var decryptedTranscript strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, rc := context.WithTimeout(cliCtx, 1*time.Second)
		mt, data, err := cliConn.Read(readCtx)
		rc()
		if err != nil {
			break
		}
		if mt != websocket.MessageBinary {
			continue
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type != proto.TypeOut {
			continue
		}
		seq, payload, err := proto.DecodeOut(f.Payload)
		if err != nil {
			continue
		}
		rawCipherTranscript.Write(payload)

		// Load-bearing assertion #1: the bytes on the wire must NOT
		// contain the plaintext marker. If they do, the agent never
		// sealed and the relay saw plaintext.
		if strings.Contains(string(payload), marker) {
			t.Fatalf("relay forwarded plaintext: payload still contains %q", marker)
		}

		// Load-bearing assertion #2: the bytes must look like an
		// envelope (cipher_id 0x01 prefix). A length check guards
		// against ANSI bytes that incidentally start with 0x01.
		if !looksLikeUnsequencedEnvelope(payload) && !looksLikeOutEnvelope(payload) {
			continue
		}

		opened, err := e2eecrypto.OpenOut(sk, sid, byte(proto.TypeOut), seq, payload)
		if err != nil {
			// Not every OUT frame may be one our agent sealed — replay
			// progress, partial chunks, etc. Skip and keep collecting.
			continue
		}
		decryptedTranscript.Write(opened)
		if strings.Contains(decryptedTranscript.String(), marker) {
			return // success: ciphertext on wire, plaintext after open
		}
	}

	t.Fatalf("never recovered marker %q from %d cipher bytes / %d decrypted bytes",
		marker, rawCipherTranscript.Len(), decryptedTranscript.Len())
}

// looksLikeOutEnvelope checks the byte prefix for a sealed OUT chunk.
// Same heuristic as looksLikeUnsequencedEnvelope; named separately so a
// later wire-format split between seq-bound and unbound frames stays
// readable at the call site.
func looksLikeOutEnvelope(data []byte) bool {
	return looksLikeUnsequencedEnvelope(data)
}

// waitForRemoteSession polls the relay's session list until sid appears
// or the deadline expires. Returns true when seen.
func waitForRemoteSession(t *testing.T, addr, token, sid string, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		res, err := getRemoteSessions(addr, token)
		if err == nil {
			if data, _ := readBodyBest(res); data != nil {
				var infos []proto.SessionInfo
				if json.Unmarshal(data, &infos) == nil {
					for _, info := range infos {
						if info.ID == sid {
							return true
						}
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func readBodyBest(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	buf := make([]byte, 1<<16)
	var out []byte
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			return out, nil
		}
	}
}

// unused stub for go-vet: keep uuid import alive even if a future trim
// drops the only call site.
var _ = uuid.New
