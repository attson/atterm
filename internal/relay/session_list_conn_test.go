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

func TestClientSessionsStreamsInitialAndChangedSnapshots(t *testing.T) {
	srv := NewServer(Config{})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/client-sessions"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	initial := readListResp(t, ctx, conn)
	if len(initial) != 0 {
		t.Fatalf("initial sessions = %d; want 0", len(initial))
	}

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	srv.registry.Add(session.New(id, proto.SessionInfo{Command: "bash", Cwd: "/tmp"}))

	changed := readListResp(t, ctx, conn)
	if len(changed) != 1 {
		t.Fatalf("changed sessions = %d; want 1", len(changed))
	}
	if changed[0].ID != id.String() || changed[0].Cwd != "/tmp" {
		t.Fatalf("changed session = %+v; want id %s cwd /tmp", changed[0], id)
	}
}

func TestClientResizeUpdatesSessionListSize(t *testing.T) {
	srv := NewServer(Config{})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cwd: "/tmp", Cols: 80, Rows: 24})
	srv.registry.Add(sess)

	listConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client-sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listConn.Close(websocket.StatusNormalClosure, "")
	_ = readListResp(t, ctx, listConn)

	clientConn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/client", nil)
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
