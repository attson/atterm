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
