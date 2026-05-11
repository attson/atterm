package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestVersionEndpointReturnsConfiguredVersion(t *testing.T) {
	srv := NewServer(Config{Token: "rt", Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version?token=rt", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var got struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("version = %q; want v1.2.3", got.Version)
	}
}

func TestVersionEndpointRequiresAuth(t *testing.T) {
	srv := NewServer(Config{Token: "rt", Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3"})
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy header is empty")
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q; want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q; want nosniff", got)
	}
}

func TestRateLimitRejectsExcessRequests(t *testing.T) {
	srv := NewServer(Config{Version: "v1.2.3", RateLimitPerMinute: 1})

	req1 := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req1.RemoteAddr = "203.0.113.10:10000"
	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d; want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req2.RemoteAddr = "203.0.113.10:10001"
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d; want 429", rec2.Code)
	}
}

func TestRequestLimitKeyDoesNotExposeBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/version?token=secret-token", nil)
	req.RemoteAddr = "203.0.113.10:10000"

	key := requestLimitKey(req)

	if strings.Contains(key, "secret-token") {
		t.Fatalf("limit key exposes token: %q", key)
	}
	if !strings.HasPrefix(key, "203.0.113.10\x00") {
		t.Fatalf("limit key = %q; want remote host prefix", key)
	}
}

func TestConnectionLimitRejectsExcessWebSockets(t *testing.T) {
	srv := NewServer(Config{MaxConnectionsPerKey: 1})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	second, resp, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client", nil)
	if err == nil {
		second.Close(websocket.StatusNormalClosure, "")
		t.Fatal("second websocket connected; want connection limit rejection")
	}
	if resp == nil {
		t.Fatalf("second response is nil; err=%v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d; want 429; err=%v", resp.StatusCode, err)
	}
}

func TestReadOnlyTokenCanListButCannotSendInput(t *testing.T) {
	srv := NewServer(Config{Token: "rw", ReadOnlyTokens: []string{"ro"}})
	id := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	sess := session.New(id, proto.SessionInfo{Command: "bash"})
	srv.registry.Add(sess)

	apiReq := httptest.NewRequest(http.MethodGet, "/api/sessions?token=ro", nil)
	apiRec := httptest.NewRecorder()
	srv.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusOK {
		t.Fatalf("read-only list status = %d; want 200", apiRec.Code)
	}

	agentReq := httptest.NewRequest(http.MethodGet, "/agent?token=ro", nil)
	agentRec := httptest.NewRecorder()
	srv.ServeHTTP(agentRec, agentReq)
	if agentRec.Code != http.StatusUnauthorized {
		t.Fatalf("read-only agent status = %d; want 401", agentRec.Code)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client?token=ro", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("read-only token delivered inbound frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHashedReadOnlyTokenCanListButCannotSendInput(t *testing.T) {
	srv := NewServer(Config{Token: "rw", ReadOnlyTokenHashes: []string{HashBearerToken("ro-hash")}})
	id := uuid.MustParse("33333333-3333-4333-8333-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash"})
	srv.registry.Add(sess)

	apiReq := httptest.NewRequest(http.MethodGet, "/api/sessions?token=ro-hash", nil)
	apiRec := httptest.NewRecorder()
	srv.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusOK {
		t.Fatalf("hashed read-only list status = %d; want 200", apiRec.Code)
	}

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client?token=ro-hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("hashed read-only token delivered inbound frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionPermissionViewDropsInputForWriteToken(t *testing.T) {
	srv := NewServer(Config{Token: "rw"})
	id := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionView})
	srv.registry.Add(sess)

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client?token=rw", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("view permission delivered inbound frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionPermissionControlAllowsInputButDropsPasteImage(t *testing.T) {
	srv := NewServer(Config{Token: "rw"})
	id := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	sess := session.New(id, proto.SessionInfo{Command: "bash", RemotePermission: proto.RemotePermissionControl})
	srv.registry.Add(sess)

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+httpSrv.URL[len("http"):]+"/client?token=rw", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	attachPayload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String()})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: id, Payload: attachPayload,
	})); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeIn, SessionID: id, Payload: []byte("whoami\n"),
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn {
			t.Fatalf("inbound type=%v; want IN", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allowed IN")
	}
	pastePayload, _ := json.Marshal(proto.PasteImagePayload{ContentType: "image/png", Data: []byte("png")})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypePasteImage, SessionID: id, Payload: pastePayload,
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-sess.Inbound():
		t.Fatalf("control permission delivered paste frame: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}
