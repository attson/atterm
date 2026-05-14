package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// TestNonDriverInDroppedAtRelay: a viewer subscriber's IN frames are silently
// dropped at the relay rather than reaching sess.Inbound(). Driver's IN does
// reach Inbound.
func TestNonDriverInDroppedAtRelay(t *testing.T) {
	srv := NewServer(Config{})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	driver := dialClientAttach(t, ctx, httpSrv, id, "client-alpha")
	defer driver.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, driver)

	viewer := dialClientAttach(t, ctx, httpSrv, id, "client-beta")
	defer viewer.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, viewer)

	writeClientFrame(t, ctx, viewer, proto.TypeIn, id, []byte("hello"))
	select {
	case f := <-sess.Inbound():
		t.Fatalf("Inbound received %v; expected viewer drop", f)
	case <-time.After(150 * time.Millisecond):
	}

	writeClientFrame(t, ctx, driver, proto.TypeIn, id, []byte("ok"))
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "ok" {
			t.Fatalf("got %+v; want IN with payload ok", f)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for driver IN to reach Inbound")
	}
}

func dialClientAttach(t *testing.T, ctx context.Context, srv *httptest.Server, sid uuid.UUID, clientID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/client"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	payload, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: clientID})
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	return c
}

// drainAttachIntro consumes the frames Subscribe queues for a new attacher:
// REPLAY_PROGRESS start + end, then one snapshot META.
func drainAttachIntro(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	for i := 0; i < 2; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read progress %d: %v", i, err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal progress %d: %v", i, err)
		}
		if f.Type != proto.TypeReplayProgress {
			t.Fatalf("frame %d type 0x%02x; want REPLAY_PROGRESS", i, f.Type)
		}
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read snapshot META: %v", err)
	}
	f, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal snapshot META: %v", err)
	}
	if f.Type != proto.TypeMeta {
		t.Fatalf("snapshot frame type 0x%02x; want META", f.Type)
	}
}

func writeClientFrame(t *testing.T, ctx context.Context, c *websocket.Conn, typ proto.Type, sid uuid.UUID, payload []byte) {
	t.Helper()
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: typ, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("write 0x%02x: %v", typ, err)
	}
}
