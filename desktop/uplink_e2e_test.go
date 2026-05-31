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
	u1 := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, h1, nil)
	u1.eventsEmit = func(context.Context, string, ...interface{}) {} // no Wails runtime in tests
	go u1.Run(ctx)
	u2 := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, h2, nil)
	u2.eventsEmit = func(context.Context, string, ...interface{}) {}
	go u2.Run(ctx)

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

	atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: "host2-cli", ClientName: "host2"})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: atp,
	})); err != nil {
		t.Fatal(err)
	}
	// Remote clients attach as viewers now; take control before typing.
	cdp, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "host2-cli", ClientName: "host2"})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: sid, Payload: cdp,
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
	u := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, host, nil)
	u.eventsEmit = func(context.Context, string, ...interface{}) {} // no Wails runtime in tests
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
	atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: "remote-cli", ClientName: "remote"})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: atp,
	})); err != nil {
		t.Fatal(err)
	}

	// Remote clients attach as viewers now; take control before typing.
	cdp, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "remote-cli", ClientName: "remote"})
	if err := cliConn.Write(cliCtx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeClaimDriver, SessionID: sid, Payload: cdp,
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

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host, nil)
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

// TestUplink_Viewers_EmitsCount verifies that a TypeViewers (0x36) frame from
// the relay makes the uplink emit a "relay:viewers" event carrying the count.
func TestUplink_Viewers_EmitsCount(t *testing.T) {
	const sid = "11111111-2222-3333-4444-555555555555"
	payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid, Count: 2})

	ready := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/uplink", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_, _, _ = c.Read(r.Context()) // drain ANNOUNCE
		_ = c.Write(r.Context(), websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeViewers, Payload: payload}))
		close(ready)
		<-r.Context().Done()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

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

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host, nil)
	u.eventsEmit = stubEmit

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("relay never sent TypeViewers")
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var found *emitCall
	for i := range calls {
		if calls[i].name == "relay:viewers" {
			found = &calls[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("relay:viewers not emitted; got: %+v", calls)
	}
	m, ok := found.data.(map[string]any)
	if !ok {
		t.Fatalf("relay:viewers data = %T; want map[string]any", found.data)
	}
	if m["session_id"] != sid {
		t.Errorf("session_id = %v; want %s", m["session_id"], sid)
	}
	if m["count"] != 2 {
		t.Errorf("count = %v; want 2", m["count"])
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

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host, nil)
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

// TestUplink_DriverHandoff_SecondClientSteals reproduces the reported bug:
// once client A has taken control, client B's "take control" must steal the
// driver and flip A to viewer. Reads are done by long-lived goroutines (a
// timed-out websocket Read closes the conn in nhooyr, so we never cancel a
// Read mid-flight); driver_client_id updates seen by A are pushed to a channel.
func TestUplink_DriverHandoff_SecondClientSteals(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}
	remoteSrv := relay.NewServer(relay.Config{Token: "rt"})
	remoteLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remoteHTTP := &http.Server{Handler: remoteSrv}
	go func() { _ = remoteHTTP.Serve(remoteLn) }()
	defer remoteHTTP.Close()
	remoteAddr := remoteLn.Addr().String()

	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	sid, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "while read line; do echo got=$line; done"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, host, nil)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	go u.Run(ctx)

	// wait for the mirror to appear on the remote relay
	seen := false
	for d := time.Now().Add(3 * time.Second); time.Now().Before(d) && !seen; {
		res, e := getRemoteSessions(remoteAddr, "rt")
		if e == nil {
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			var infos []proto.SessionInfo
			if json.Unmarshal(body, &infos) == nil {
				for _, i := range infos {
					if i.ID == sid.String() {
						seen = true
					}
				}
			}
		}
		if !seen {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !seen {
		t.Fatal("mirror never appeared on remote relay")
	}

	dial := func(id string) *websocket.Conn {
		c, _, e := websocket.Dial(ctx, fmt.Sprintf("ws://%s/client", remoteAddr), &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer rt"}},
		})
		if e != nil {
			t.Fatal(e)
		}
		c.SetReadLimit(2 * 1024 * 1024)
		atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: id, ClientName: id})
		if e := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAttach, SessionID: sid, Payload: atp})); e != nil {
			t.Fatal(e)
		}
		return c
	}
	claim := func(c *websocket.Conn, id string) {
		cdp, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: id, ClientName: id})
		if e := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeClaimDriver, SessionID: sid, Payload: cdp})); e != nil {
			t.Fatal(e)
		}
	}

	connA := dial("cli-A")
	defer connA.Close(websocket.StatusNormalClosure, "")
	connB := dial("cli-B")
	defer connB.Close(websocket.StatusNormalClosure, "")

	driversA := make(chan string, 256)
	go func() {
		for {
			mt, data, e := connA.Read(ctx)
			if e != nil {
				return
			}
			if mt != websocket.MessageBinary {
				continue
			}
			f, e := proto.Unmarshal(data)
			if e != nil || f.Type != proto.TypeMeta {
				continue
			}
			var m proto.MetaPayload
			if json.Unmarshal(f.Payload, &m) == nil {
				select {
				case driversA <- m.DriverClientID:
				default:
				}
			}
		}
	}()
	go func() { // keep B drained so its subscriber channel never backs up
		for {
			if _, _, e := connB.Read(ctx); e != nil {
				return
			}
		}
	}()

	// collect driver_client_id updates seen by A over dur, return the last one
	lastDriverOnA := func(dur time.Duration) string {
		last := "<none>"
		timeout := time.After(dur)
		for {
			select {
			case d := <-driversA:
				last = d
			case <-timeout:
				return last
			}
		}
	}

	claim(connA, "cli-A")
	if got := lastDriverOnA(2 * time.Second); got != "cli-A" {
		t.Fatalf("A failed to become driver: A sees driver=%q (want cli-A)", got)
	}
	t.Logf("✓ A is driver")

	claim(connB, "cli-B")
	if got := lastDriverOnA(3 * time.Second); got != "cli-B" {
		t.Fatalf("B failed to steal driver from A: A still sees driver=%q (want cli-B)", got)
	}
	t.Logf("✓ B stole driver; A flipped to viewer")
}

// TestUplink_DriverHandoff_OwnerAndRemote covers the desktop-owner <-> phone
// handoff: the owner attaches to its own in-process mini-relay, a remote phone
// attaches via the central relay's mirror. Control must be stealable BOTH ways.
func TestUplink_DriverHandoff_OwnerAndRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}
	remoteSrv := relay.NewServer(relay.Config{Token: "rt"})
	remoteLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remoteHTTP := &http.Server{Handler: remoteSrv}
	go func() { _ = remoteHTTP.Serve(remoteLn) }()
	defer remoteHTTP.Close()
	remoteAddr := remoteLn.Addr().String()

	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	sid, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "while read line; do echo got=$line; done"},
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := newUplink("ws://"+remoteAddr, "rt", proto.RemotePermissionFull, host, nil)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	go u.Run(ctx)

	dialAttach := func(baseWs, token, id string) *websocket.Conn {
		c, _, e := websocket.Dial(ctx, baseWs+"/client", &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		if e != nil {
			t.Fatal(e)
		}
		c.SetReadLimit(2 * 1024 * 1024)
		atp, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: id, ClientName: id})
		if e := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAttach, SessionID: sid, Payload: atp})); e != nil {
			t.Fatal(e)
		}
		return c
	}
	claim := func(c *websocket.Conn, id string) {
		cdp, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: id, ClientName: id})
		if e := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeClaimDriver, SessionID: sid, Payload: cdp})); e != nil {
			t.Fatal(e)
		}
	}
	driverChan := func(c *websocket.Conn) chan string {
		ch := make(chan string, 256)
		go func() {
			for {
				mt, data, e := c.Read(ctx)
				if e != nil {
					return
				}
				if mt != websocket.MessageBinary {
					continue
				}
				f, e := proto.Unmarshal(data)
				if e != nil || f.Type != proto.TypeMeta {
					continue
				}
				var m proto.MetaPayload
				if json.Unmarshal(f.Payload, &m) == nil {
					select {
					case ch <- m.DriverClientID:
					default:
					}
				}
			}
		}()
		return ch
	}
	lastOn := func(ch chan string, dur time.Duration) string {
		last := "<none>"
		timeout := time.After(dur)
		for {
			select {
			case d := <-ch:
				last = d
			case <-timeout:
				return last
			}
		}
	}

	// owner attaches to its own mini-relay
	ownerConn := dialAttach("ws://"+host.addr, host.token, "owner")
	defer ownerConn.Close(websocket.StatusNormalClosure, "")
	ownerDrv := driverChan(ownerConn)

	// wait for the mirror to appear so the remote can attach
	seen := false
	for d := time.Now().Add(3 * time.Second); time.Now().Before(d) && !seen; {
		res, e := getRemoteSessions(remoteAddr, "rt")
		if e == nil {
			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			var infos []proto.SessionInfo
			if json.Unmarshal(body, &infos) == nil {
				for _, i := range infos {
					if i.ID == sid.String() {
						seen = true
					}
				}
			}
		}
		if !seen {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !seen {
		t.Fatal("mirror never appeared on remote relay")
	}

	remoteConn := dialAttach("ws://"+remoteAddr, "rt", "remote")
	defer remoteConn.Close(websocket.StatusNormalClosure, "")
	remoteDrv := driverChan(remoteConn)

	// owner takes control
	claim(ownerConn, "owner")
	if got := lastOn(ownerDrv, 2*time.Second); got != "owner" {
		t.Fatalf("owner failed to become driver: owner sees driver=%q (want owner)", got)
	}
	t.Logf("✓ owner is driver")

	// remote (phone) steals control — owner must flip to viewer
	claim(remoteConn, "remote")
	if got := lastOn(ownerDrv, 3*time.Second); got != "remote" {
		t.Fatalf("remote failed to steal from owner: owner still sees driver=%q (want remote)", got)
	}
	t.Logf("✓ remote stole driver; owner is viewer")

	// owner takes control back — remote must flip to viewer
	claim(ownerConn, "owner")
	if got := lastOn(remoteDrv, 3*time.Second); got != "owner" {
		t.Fatalf("owner failed to take back from remote: remote still sees driver=%q (want owner)", got)
	}
	t.Logf("✓ owner took control back; remote is viewer")
}
