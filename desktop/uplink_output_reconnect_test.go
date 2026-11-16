package main

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

func TestUplinkReconnectsFailedLiveOutputStream(t *testing.T) {
	for _, mode := range []string{"subscribe-failure", "subscriber-closed", "normal-stop", "session-closed", "request-closed-session", "missing-session"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			accepted := make(chan *websocket.Conn, 1)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				accepted <- conn
				<-ctx.Done()
				conn.CloseNow()
			}))
			defer remote.Close()
			host := newTestRelayHost(t)
			id := uuid.New()
			sess := session.New(id, proto.SessionInfo{ID: id.String()})
			if _, err := host.server.Registry().Add(sess); err != nil {
				t.Fatal(err)
			}
			u := newUplink("ws"+strings.TrimPrefix(remote.URL, "http"), "", proto.RemotePermissionFull, host, nil, nil, false)
			barriers := make(chan struct{}, 4)
			u.eventsEmit = func(_ context.Context, name string, _ ...interface{}) {
				if name == "relay:viewers" {
					barriers <- struct{}{}
				}
			}
			stopped := make(chan struct{})
			go func() { defer close(stopped); _ = u.runOnce(ctx) }()
			defer func() { cancel(); <-stopped }()
			var conn *websocket.Conn
			select {
			case conn = <-accepted:
			case <-ctx.Done():
				t.Fatal("uplink did not connect")
			}
			defer conn.CloseNow()
			if _, _, err := conn.Read(ctx); err != nil {
				t.Fatal(err)
			}
			go func() {
				for {
					if _, _, err := conn.Read(ctx); err != nil {
						return
					}
				}
			}()
			send := func(typ proto.Type) {
				t.Helper()
				body, _ := json.Marshal(proto.StreamRequestPayload{SessionID: id.String()})
				if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: typ, SessionID: id, Payload: body})); err != nil {
					t.Fatal(err)
				}
			}
			barrier := func() {
				t.Helper()
				body, _ := json.Marshal(proto.ViewersPayload{SessionID: id.String(), Count: 1})
				if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeViewers, Payload: body})); err != nil {
					t.Fatal(err)
				}
				select {
				case <-barriers:
				case <-stopped:
					t.Fatal("normal stream termination reconnected the uplink")
				case <-ctx.Done():
					t.Fatal("control barrier timed out")
				}
			}
			switch mode {
			case "subscribe-failure":
				sess.SetSubscriberCountHook(func(count int) {
					if count != 1 {
						return
					}
					sess.MarkContentOpaque()
					for seq := uint64(1); seq <= 5000 && sess.SubscriberCount() != 0; seq++ {
						sess.PushOut(seq, []byte("opaque"))
					}
				})
			case "request-closed-session":
				sess.Close()
			case "missing-session":
				id = uuid.New()
			}
			send(proto.TypeStreamRequest)
			if mode == "subscribe-failure" {
				select {
				case <-stopped:
				case <-time.After(2 * time.Second):
					t.Fatal("failed live subscription left the uplink permanently active")
				}
				return
			}
			barrier()
			switch mode {
			case "subscriber-closed":
				host.mu.Lock()
				var sub *session.Subscriber
				for proxy := range host.uplinkSubs[id] {
					sub = proxy
					break
				}
				host.mu.Unlock()
				if sub == nil {
					t.Fatal("stream did not subscribe")
				}
				sess.Unsubscribe(sub)
				select {
				case <-stopped:
				case <-time.After(2 * time.Second):
					t.Fatal("unexpected live subscriber closure stalled the uplink")
				}
			case "normal-stop":
				send(proto.TypeStreamStop)
				barrier()
			case "session-closed":
				sess.Close()
				barrier()
			}
		})
	}
}
