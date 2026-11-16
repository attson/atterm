package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/terminalcodec"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func attachOutputClient(t *testing.T, ctx context.Context, httpServer *httptest.Server, token string, id uuid.UUID, compression string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/client", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	payload, _ := json.Marshal(proto.AttachPayload{SessionID: id.String(), ClientID: uuid.NewString(), OutputCompression: compression})
	writeClientFrame(t, ctx, c, proto.TypeAttach, id, payload)
	drainAttachIntro(t, ctx, c)
	return c
}

func TestOutputCompressionSelectsPerSubscriberAndFallsBack(t *testing.T) {
	for _, capable := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-publisher", true: "mixed-viewers"}[capable], func(t *testing.T) {
			srv, token, owner := serverWithSessionAndUser(t)
			httpServer := httptest.NewServer(srv)
			defer httpServer.Close()
			id := uuid.New()
			sess := session.New(id, proto.SessionInfo{ID: id.String(), RemotePermission: proto.RemotePermissionFull})
			sess.OwnerUserID = owner
			sess.MarkContentOpaque()
			sess.SetOutputCompressionSupported(capable)
			if _, err := srv.registry.Add(sess); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			modern := attachOutputClient(t, ctx, httpServer, token, id, proto.OutputCompressionDeflate)
			legacy := attachOutputClient(t, ctx, httpServer, token, id, "")
			if capable && (sess.OutputSubscriberCount(true) != 1 || sess.OutputSubscriberCount(false) != 1) {
				t.Fatal("formats must share one session with separate output subscriptions")
			}
			if !capable && sess.OutputSubscriberCount(false) != 2 {
				t.Fatal("legacy publisher must use ordinary output for both clients")
			}
			raw := bytes.Repeat([]byte{1}, 60)
			compressed := bytes.Repeat([]byte{2}, 60)
			sess.PushOut(10, raw)
			if capable {
				sess.PushCompressedOut(10, compressed)
			}
			for _, tc := range []struct {
				conn     *websocket.Conn
				wantType proto.Type
				want     []byte
			}{{modern, map[bool]proto.Type{false: proto.TypeOut, true: proto.TypeOutCompressed}[capable], map[bool][]byte{false: raw, true: compressed}[capable]}, {legacy, proto.TypeOut, raw}} {
				for {
					frame, err := readFrame(ctx, tc.conn)
					if err != nil {
						t.Fatal(err)
					}
					if frame.Type == proto.TypeMeta {
						continue
					}
					if frame.Type != tc.wantType {
						t.Fatalf("output type = %x, want %x", frame.Type, tc.wantType)
					}
					seq, data, err := proto.DecodeOut(frame.Payload)
					if err != nil || seq != 10 || !bytes.Equal(data, tc.want) {
						t.Fatal("relay changed opaque output")
					}
					break
				}
			}
		})
	}
}

func TestCompressedTrafficAndReplayPacing(t *testing.T) {
	if frameCategory(proto.TypeOutCompressed) != "terminal" || frameCategory(proto.TypeStreamRequestCompressed) != "state" || frameCategory(proto.TypeStreamStopCompressed) != "state" {
		t.Fatal("compressed frames need normal traffic categories")
	}
	p := newReplayPacer(4)
	start, _ := json.Marshal(proto.ReplayProgressPayload{Phase: proto.ReplayProgressStart})
	p.observe(proto.Frame{Type: proto.TypeReplayProgress, Payload: start})
	f := proto.EncodeOut(uuid.New(), 1, []byte("1234"))
	f.Type = proto.TypeOutCompressed
	if !p.observe(f) {
		t.Fatal("compressed replay bypassed pacing")
	}
}

func TestCompressedUplinkLazyMixedStreamsAndCachedResume(t *testing.T) {
	srv, token, _ := serverWithSessionAndUser(t)
	httpServer := httptest.NewServer(srv)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	up, _, err := dialUplinkWS(t, ctx, httpServer, "Bearer "+token)
	if err != nil {
		t.Fatal(err)
	}
	defer up.CloseNow()
	id := uuid.New()
	changes := srv.registry.SubscribeChanges()
	defer changes.Close()
	ann, _ := json.Marshal(proto.AnnouncePayload{HostID: uuid.NewString(), OutputCompression: proto.OutputCompressionDeflate, Sessions: []proto.SessionInfo{{ID: id.String(), RemotePermission: proto.RemotePermissionFull}}})
	writeClientFrame(t, ctx, up, proto.TypeAnnounce, uuid.Nil, ann)
	for {
		if _, ok := srv.registry.Get(id); ok {
			break
		}
		select {
		case <-changes.C():
		case <-ctx.Done():
			t.Fatal("mirror was not registered")
		}
	}
	readControl := func(want proto.Type) proto.Frame {
		for {
			f, err := readFrame(ctx, up)
			if err != nil {
				t.Fatal(err)
			}
			switch f.Type {
			case proto.TypeStreamRequest, proto.TypeStreamStop, proto.TypeStreamRequestCompressed, proto.TypeStreamStopCompressed:
				if f.Type != want {
					t.Fatalf("unexpected stream control %x, want %x", f.Type, want)
				}
				return f
			}
		}
	}
	modern := attachOutputClient(t, ctx, httpServer, token, id, proto.OutputCompressionDeflate)
	readControl(proto.TypeStreamRequestCompressed)
	key, err := e2eecrypto.DeriveSessionKey(bytes.Repeat([]byte{0x42}, 32), id)
	if err != nil {
		t.Fatal(err)
	}
	sendCompressed := func(seq uint64, text string) {
		packed, err := terminalcodec.Encode([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		env, err := e2eecrypto.SealOut(key, id, byte(proto.TypeOutCompressed), seq, packed)
		if err != nil {
			t.Fatal(err)
		}
		f := proto.EncodeOut(id, seq, env)
		f.Type = proto.TypeOutCompressed
		writeClientFrame(t, ctx, up, f.Type, id, f.Payload)
	}
	readOutput := func(c *websocket.Conn, wantType proto.Type, wantSeq uint64, want string) {
		for {
			f, err := readFrame(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			if f.Type == proto.TypeMeta || f.Type == proto.TypeReplayProgress {
				continue
			}
			if f.Type != wantType {
				t.Fatalf("wrong stream type %x", f.Type)
			}
			seq, data, err := proto.DecodeOut(f.Payload)
			if err != nil || seq != wantSeq {
				t.Fatalf("output seq=%d want=%d err=%v", seq, wantSeq, err)
			}
			if wantType == proto.TypeOutCompressed {
				packed, err := e2eecrypto.OpenOut(key, id, byte(proto.TypeOutCompressed), seq, data)
				if err != nil {
					t.Fatal(err)
				}
				data, err = terminalcodec.Decode(packed)
				if err != nil {
					t.Fatal(err)
				}
			}
			if string(data) != want {
				t.Fatal("opaque uplink output changed")
			}
			return
		}
	}
	sendCompressed(1, "compressed first")
	readOutput(modern, proto.TypeOutCompressed, 1, "compressed first")
	legacy := attachOutputClient(t, ctx, httpServer, token, id, "")
	readControl(proto.TypeStreamRequest)
	writeClientFrame(t, ctx, up, proto.TypeOut, id, proto.EncodeOut(id, 1, []byte("legacy first")).Payload)
	readOutput(legacy, proto.TypeOut, 1, "legacy first")
	_ = legacy.CloseNow()
	readControl(proto.TypeStreamStop)
	sendCompressed(2, "compressed second")
	readOutput(modern, proto.TypeOutCompressed, 2, "compressed second")
	_ = modern.CloseNow()
	readControl(proto.TypeStreamStopCompressed)
	// Reattachment first replays the mirror cache, then resumes the publisher
	// from its stored cursor instead of uploading the same history again.
	resumed, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/client", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.CloseNow()
	ap, _ := json.Marshal(proto.AttachPayload{SessionID: id.String(), ClientID: uuid.NewString(), OutputCompression: proto.OutputCompressionDeflate})
	writeClientFrame(t, ctx, resumed, proto.TypeAttach, id, ap)
	request := readControl(proto.TypeStreamRequestCompressed)
	var rp proto.StreamRequestPayload
	if err := json.Unmarshal(request.Payload, &rp); err != nil || rp.SinceSeq != 2 {
		t.Fatalf("cached resume cursor=%d err=%v", rp.SinceSeq, err)
	}
	readOutput(resumed, proto.TypeOutCompressed, 1, "compressed first")
	readOutput(resumed, proto.TypeOutCompressed, 2, "compressed second")
}
