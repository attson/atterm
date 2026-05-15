package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/relay"
	"nhooyr.io/websocket"
)

func getRemoteSessions(remoteAddr, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/sessions", remoteAddr), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

// TestTwoHostsCrossAttach simulates two desktop apps connected to the same
// remote relay: host1 spawns a session, host2 attaches to it through the
// remote relay. This is the "open atterm twice and look at each other" path.
func TestTwoHostsCrossAttach(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}
	remoteSrv := relay.NewServer(relay.Config{Token: "rt"})
	remoteLn, _ := net.Listen("tcp", "127.0.0.1:0")
	remoteHTTP := &http.Server{Handler: remoteSrv}
	go func() { _ = remoteHTTP.Serve(remoteLn) }()
	defer remoteHTTP.Close()
	remoteAddr := remoteLn.Addr().String()

	h1, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Stop()
	h2, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Stop()

	// host1 owns the session
	sid, err := h1.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "while read line; do echo got=$line; done"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, h1).Run(ctx)
	go newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, h2).Run(ctx)

	// host2 acts as a viewer: it should see h1's session via remote relay's
	// /api/sessions and successfully attach.
	deadline := time.Now().Add(3 * time.Second)
	saw := false
	for time.Now().Before(deadline) && !saw {
		res, err := getRemoteSessions(remoteAddr, "rt")
		if err == nil {
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			var infos []proto.SessionInfo
			if err := json.Unmarshal(body, &infos); err == nil {
				for _, i := range infos {
					if i.ID == sid.String() {
						saw = true
						break
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !saw {
		t.Fatalf("host2 never saw host1's session on remote relay")
	}
	t.Logf("✓ host2 sees host1's session listed on remote relay")

	// host2-as-client: attach via /client and round-trip a command.
	cliCtx, cliCancel := context.WithTimeout(ctx, 10*time.Second)
	defer cliCancel()
	cliConn, _, err := websocket.Dial(cliCtx, fmt.Sprintf("ws://%s/client", remoteAddr), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer rt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cliConn.Close(websocket.StatusNormalClosure, "")
	cliConn.SetReadLimit(2 * 1024 * 1024)

	atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String()})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: atp,
	})); err != nil {
		t.Fatal(err)
	}
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: sid, Payload: []byte("from-host2\n"),
	})); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(5 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		readCtx, rc := context.WithTimeout(cliCtx, 2*time.Second)
		mt, data, err := cliConn.Read(readCtx)
		rc()
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, seen.String())
		}
		if mt != websocket.MessageBinary {
			continue
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type == proto.TypeOut {
			_, payload, _ := proto.DecodeOut(f.Payload)
			seen.Write(payload)
			if strings.Contains(seen.String(), "got=from-host2") {
				t.Logf("✓ host2 successfully cross-attached host1's session")
				return
			}
		}
	}
	t.Fatalf("did not see expected cross-attach echo in %q", seen.String())
}

// TestUplinkE2E exercises the lazy-mirror path end to end:
// 1. start a remote atterm-relay
// 2. start a local desktop relayHost + spawn a bash session in it
// 3. start an uplink pointing at the remote relay
// 4. assert /api/sessions on the remote shows the session via ANNOUNCE
// 5. open a /client WS to the remote, ATTACH, send "echo hi\n" as IN
// 6. assert OUT frames carry the echo back through the mirror
func TestUplinkE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}

	// 1. remote relay
	remoteSrv := relay.NewServer(relay.Config{Token: "rt"})
	remoteLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remoteHTTP := &http.Server{Handler: remoteSrv}
	go func() { _ = remoteHTTP.Serve(remoteLn) }()
	defer remoteHTTP.Close()
	remoteAddr := remoteLn.Addr().String()

	// 2. local relayHost
	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()

	// spawn a bash session that waits for input
	sid, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "echo HELLO; while read line; do echo got=$line; done"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// 3. uplink
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, host)
	go u.Run(ctx)

	// 4. wait for ANNOUNCE → fetch /api/sessions on remote
	mustSeeMirror := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			res, err := getRemoteSessions(remoteAddr, "rt")
			if err == nil {
				body, _ := io.ReadAll(res.Body)
				_ = res.Body.Close()
				var infos []proto.SessionInfo
				if err := json.Unmarshal(body, &infos); err == nil {
					for _, i := range infos {
						if i.ID == sid.String() {
							return
						}
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("mirror session %s not seen on remote within 3s", sid)
	}
	mustSeeMirror()
	t.Logf("✓ ANNOUNCE delivered: mirror visible on remote")

	// 5. attach as a remote client
	cliCtx, cliCancel := context.WithTimeout(ctx, 10*time.Second)
	defer cliCancel()
	cliConn, _, err := websocket.Dial(cliCtx, fmt.Sprintf("ws://%s/client", remoteAddr), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer rt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cliConn.Close(websocket.StatusNormalClosure, "")
	cliConn.SetReadLimit(2 * 1024 * 1024)

	// send ATTACH(sid, since=0)
	atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String()})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: atp,
	})); err != nil {
		t.Fatal(err)
	}

	// kick the bash so it produces output we can recognise
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: sid, Payload: []byte("ping-from-remote\n"),
	})); err != nil {
		t.Fatal(err)
	}

	// 6. drain frames until we see "got=ping-from-remote"
	deadline := time.Now().Add(5 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		readCtx, readCancel := context.WithTimeout(cliCtx, 2*time.Second)
		mt, data, err := cliConn.Read(readCtx)
		readCancel()
		if err != nil {
			t.Fatalf("read frame: %v (got so far: %q)", err, seen.String())
		}
		if mt != websocket.MessageBinary {
			continue
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type == proto.TypeOut {
			_, payload, _ := proto.DecodeOut(f.Payload)
			seen.Write(payload)
			if strings.Contains(seen.String(), "got=ping-from-remote") {
				t.Logf("✓ round-trip succeeded; saw bash echo back")
				return
			}
		}
	}
	t.Fatalf("did not see expected echo in %q", seen.String())
}

// TestUplink_AuthInfo_EmitsUserID verifies that when the relay sends an AUTH_INFO
// frame (TypeAuthInfo, 0x40) with payload {"user_id":"01HXABC"} after the WS
// upgrade, the uplink emits a "relay:auth-info" Wails event carrying the same
// data. The connection is then kept open so we can verify no double-emit.
func TestUplink_AuthInfo_EmitsUserID(t *testing.T) {
	const testUserID = "01HXABC"
	payload, _ := json.Marshal(map[string]string{"user_id": testUserID})

	// Fake relay: accept the /uplink connection, send one AUTH_INFO frame, then
	// stay open (relay never closes — we let ctx cancel to end the test).
	ready := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/uplink", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// Drain the ANNOUNCE the uplink sends immediately after connecting.
		_, _, _ = c.Read(r.Context())
		// Send AUTH_INFO frame.
		frame := proto.Frame{Type: proto.TypeAuthInfo, Payload: payload}
		_ = c.Write(r.Context(), websocket.MessageBinary, proto.Marshal(frame))
		close(ready)
		// Keep the connection alive until the test context closes.
		<-r.Context().Done()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// Stub EventsEmit to capture calls.
	type emitCall struct {
		name string
		data interface{}
	}
	var (
		mu    sync.Mutex
		calls []emitCall
	)
	stubEmit := func(_ context.Context, name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		var d interface{}
		if len(data) > 0 {
			d = data[0]
		}
		calls = append(calls, emitCall{name: name, data: d})
	}

	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host)
	u.eventsEmit = stubEmit

	// Run the uplink; cancel after the relay signals it sent the frame.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = u.runOnce(ctx)
	}()

	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("timed out waiting for relay to send AUTH_INFO")
	}
	// Give the uplink a moment to process the frame.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var found *emitCall
	for i := range calls {
		if calls[i].name == "relay:auth-info" {
			found = &calls[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("relay:auth-info event not emitted; got calls: %+v", calls)
	}
	// The data should be a struct with UserID field; JSON round-trip via map is fine.
	b, err := json.Marshal(found.data)
	if err != nil {
		t.Fatalf("cannot marshal emitted data: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("emitted data not JSON-able to map: %v", err)
	}
	if got["user_id"] != testUserID {
		t.Errorf("relay:auth-info user_id = %q; want %q", got["user_id"], testUserID)
	}
}

// TestUplink_AuthErrorClose_EmitsEvent verifies that when the relay closes the
// uplink WS with code 4001 (auth_invalid_token), the uplink calls EventsEmit
// with event "relay:auth-error" and payload {"reason":"auth_invalid_token"}.
func TestUplink_AuthErrorClose_EmitsEvent(t *testing.T) {
	// Spin up a fake relay that immediately closes the /uplink connection with
	// WS close code 4001 and reason "auth_invalid_token".
	mux := http.NewServeMux()
	mux.HandleFunc("/uplink", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately with auth error code.
		_ = c.Close(websocket.StatusCode(4001), "auth_invalid_token")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// Stub EventsEmit to capture calls.
	type emitCall struct {
		name string
		data interface{}
	}
	var (
		mu    sync.Mutex
		calls []emitCall
	)
	stubEmit := func(_ context.Context, name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		var d interface{}
		if len(data) > 0 {
			d = data[0]
		}
		calls = append(calls, emitCall{name: name, data: d})
	}

	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host)
	u.eventsEmit = stubEmit

	// Run returns after the connection is closed (no reconnect: ctx times out quickly).
	// We run a single runOnce to avoid the retry loop.
	_ = u.runOnce(ctx)

	// Assert the stub was called with the expected event.
	mu.Lock()
	defer mu.Unlock()
	var found *emitCall
	for i := range calls {
		if calls[i].name == "relay:auth-error" {
			found = &calls[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("relay:auth-error event not emitted; got calls: %+v", calls)
	}
	m, ok := found.data.(map[string]string)
	if !ok {
		t.Fatalf("relay:auth-error data is %T, want map[string]string; value=%v", found.data, found.data)
	}
	if m["reason"] != "auth_invalid_token" {
		t.Errorf("relay:auth-error reason = %q; want %q", m["reason"], "auth_invalid_token")
	}
}
