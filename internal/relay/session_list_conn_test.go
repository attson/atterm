package relay

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestClientSessionsStreamsInitialAndChangedSnapshots(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/client-sessions"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + tok}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	initial := readListResp(t, ctx, conn)
	if len(initial) != 0 {
		t.Fatalf("initial sessions = %d; want 0", len(initial))
	}

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cwd: "/tmp"})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	changed := readListResp(t, ctx, conn)
	if len(changed) != 1 {
		t.Fatalf("changed sessions = %d; want 1", len(changed))
	}
	if changed[0].ID != id.String() || changed[0].Cwd != "/tmp" {
		t.Fatalf("changed session = %+v; want id %s cwd /tmp", changed[0], id)
	}
}

func TestClientResizeUpdatesSessionListSize(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cwd: "/tmp", Cols: 80, Rows: 24})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	authHeader := map[string][]string{"Authorization": {"Bearer " + tok}}
	listConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client-sessions", &websocket.DialOptions{HTTPHeader: authHeader})
	if err != nil {
		t.Fatal(err)
	}
	defer listConn.Close(websocket.StatusNormalClosure, "")
	_ = readListResp(t, ctx, listConn)

	clientConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client", &websocket.DialOptions{HTTPHeader: authHeader})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close(websocket.StatusNormalClosure, "")
	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := clientConn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeResize, SessionID: id, Payload: proto.EncodeResize(132, 43),
	})); err != nil {
		t.Fatal(err)
	}

	changed := readListResp(t, ctx, listConn)
	if len(changed) != 1 {
		t.Fatalf("changed sessions = %d; want 1", len(changed))
	}
	if changed[0].Cols != 132 || changed[0].Rows != 43 {
		t.Fatalf("changed size = %dx%d; want 132x43", changed[0].Cols, changed[0].Rows)
	}
}

func TestClientWriterPingFailureUnsubscribesSession(t *testing.T) {
	oldPingPeriod, oldWriteWait := clientPingPeriod, clientWriteWait
	clientPingPeriod = 10 * time.Millisecond
	clientWriteWait = 10 * time.Millisecond
	t.Cleanup(func() {
		clientPingPeriod = oldPingPeriod
		clientWriteWait = oldWriteWait
	})

	srv, tok, userID := serverWithSessionAndUser(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	sess := session.New(id, proto.SessionInfo{Command: "bash"})
	sess.OwnerUserID = userID
	first := make(chan struct{}, 1)
	last := make(chan struct{}, 1)
	sess.SetSubscriberLifecycle(
		func() { first <- struct{}{} },
		func() { last <- struct{}{} },
	)
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rawConn, err := dialRawWebSocketClient(httpSrv.URL, "/client", tok)
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close()

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := writeRawWebSocketBinary(rawConn, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}

	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first subscriber hook")
	}
	// Intentionally never read or answer ping control frames. The writer
	// should close the websocket and let handleClient run its unsubscribe
	// cleanup instead of leaving a stale subscriber that suppresses future
	// STREAM_REQUEST transitions.
	select {
	case <-last:
	case <-ctx.Done():
		t.Fatal("timed out waiting for last subscriber hook after ping failure")
	}
}

func dialRawWebSocketClient(serverURL, path, sessionToken string) (net.Conn, error) {
	addr := strings.TrimPrefix(serverURL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n"
	if sessionToken != "" {
		req += "Authorization: Bearer " + sessionToken + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, err
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" {
			return conn, nil
		}
	}
}

func writeRawWebSocketBinary(conn net.Conn, payload []byte) error {
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header := []byte{0x82}
	switch {
	case len(payload) < 126:
		header = append(header, 0x80|byte(len(payload)))
	case len(payload) <= 0xffff:
		header = append(header, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 0x80|127, 0, 0, 0, 0, byte(len(payload)>>24), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
	}
	masked := append([]byte(nil), payload...)
	for i := range masked {
		masked[i] ^= mask[i%4]
	}
	if _, err := conn.Write(append(append(header, mask...), masked...)); err != nil {
		return err
	}
	return nil
}

func readListResp(t *testing.T, ctx context.Context, conn *websocket.Conn) []proto.SessionInfo {
	t.Helper()
	f, err := readFrame(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != proto.TypeListResp {
		t.Fatalf("frame type = 0x%02x; want LIST_RESP", f.Type)
	}
	var sessions []proto.SessionInfo
	if err := json.Unmarshal(f.Payload, &sessions); err != nil {
		t.Fatal(err)
	}
	return sessions
}
