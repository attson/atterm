package main

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

func TestPrepareCompressedSubscriberOutputRoundTrip(t *testing.T) {
	id := uuid.New()
	key := mustAccountKey32(t)
	input := bytes.Repeat([]byte("\x1b[2Kworking\r\n"), 300)
	frame, ok, err := prepareUplinkSubscriberFrame(proto.EncodeOut(id, 42, input), true, func() []byte { return key })
	if err != nil || !ok || frame.Type != proto.TypeOutCompressed {
		t.Fatalf("prepare: type=%x ok=%v err=%v", frame.Type, ok, err)
	}
	seq, envelope, err := proto.DecodeOut(frame.Payload)
	if err != nil || seq != 42 {
		t.Fatalf("output seq=%d err=%v", seq, err)
	}
	sk, err := e2eecrypto.DeriveSessionKey(key, id)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := e2eecrypto.OpenOut(sk, id, e2eecrypto.AADTagOutCompressed, seq, envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := terminalcodec.Decode(encoded)
	if err != nil || !bytes.Equal(decoded, input) || encoded[0] != 1 {
		t.Fatalf("decode compressed output: %v", err)
	}
	if _, err := e2eecrypto.OpenOut(sk, id, e2eecrypto.AADTagOut, seq, envelope); err == nil {
		t.Fatal("compressed envelope accepted as ordinary output")
	}
	if _, err := e2eecrypto.OpenOut(sk, id, e2eecrypto.AADTagOutCompressed, seq+1, envelope); err == nil {
		t.Fatal("compressed envelope accepted with altered seq")
	}
}

func TestPrepareCompressedSubscriberOutputFailsClosed(t *testing.T) {
	id := uuid.New()
	for _, key := range []func() []byte{nil, func() []byte { return nil }, func() []byte { return make([]byte, 31) }} {
		frame, ok, err := prepareUplinkSubscriberFrame(proto.EncodeOut(id, 1, []byte("secret")), true, key)
		if err == nil || ok || len(frame.Payload) != 0 {
			t.Fatal("compressed output fell back to an unsealed frame")
		}
	}
	key := func() []byte { return make([]byte, 32) }
	for _, input := range []proto.Frame{
		{Type: proto.TypeOut, SessionID: id, Payload: []byte{1}},
		proto.EncodeOut(id, 1, make([]byte, terminalcodec.MaxDecodedBytes+1)),
	} {
		if _, ok, err := prepareUplinkSubscriberFrame(input, true, key); err == nil || ok {
			t.Fatal("invalid output was accepted for compressed lane")
		}
	}
}

func TestForwardUplinkStreamPreservesMetadataAndResetOrder(t *testing.T) {
	id := uuid.New()
	queue := make(chan proto.Frame, 5)
	queue <- proto.EncodeOut(id, 1, []byte("one"))
	queue <- proto.Frame{Type: proto.TypeMeta, SessionID: id, Payload: []byte(`{"cols":80}`)}
	queue <- proto.EncodeOut(id, 0, []byte("\x1b[2J\x1b[H"))
	queue <- proto.EncodeOut(id, 2, []byte("two"))
	queue <- proto.Frame{Type: proto.TypeClose, SessionID: id}
	close(queue)
	out := make(chan proto.Frame, 5)
	repaints := 0
	reason := forwardUplinkStream(context.Background(), queue, nil, out, nil, func() { repaints++ }, nil, false)
	if reason != "subscriber_closed" || len(out) != 5 || repaints != 1 {
		t.Fatalf("reason=%s out=%d repaints=%d", reason, len(out), repaints)
	}
	for i, want := range []proto.Type{proto.TypeOut, proto.TypeMeta, proto.TypeOut, proto.TypeOut, proto.TypeClose} {
		if got := <-out; got.Type != want {
			t.Fatalf("frame %d=%x want %x", i, got.Type, want)
		}
	}
}

func TestAnnounceCompressionRequiresUnlockedKeyAndChangesSemanticCache(t *testing.T) {
	for _, test := range []struct {
		key  func() []byte
		want string
	}{
		{nil, ""}, {func() []byte { return nil }, ""}, {func() []byte { return make([]byte, 31) }, ""},
		{func() []byte { return make([]byte, 32) }, proto.OutputCompressionDeflate},
	} {
		u := &uplink{accountKey: test.key}
		if got := u.outputCompression(); got != test.want {
			t.Fatalf("capability=%q want %q", got, test.want)
		}
	}
	plain, err := buildAnnouncePayload("id", "host", "user", nil, proto.RemotePermissionFull)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := buildAnnouncePayload("id", "host", "user", nil, proto.RemotePermissionFull, proto.OutputCompressionDeflate)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := buildAnnounceSemanticPayload("id", "host", "user", nil, proto.RemotePermissionFull, proto.OutputCompressionDeflate)
	if err != nil {
		t.Fatal(err)
	}
	var announce proto.AnnouncePayload
	if err := json.Unmarshal(compressed, &announce); err != nil {
		t.Fatal(err)
	}
	if announce.OutputCompression != proto.OutputCompressionDeflate {
		t.Fatal("missing compression capability on wire")
	}
	var cache announceCache
	cache.markSent(plain, plain)
	if !cache.shouldSend(compressed, semantic, false) {
		t.Fatal("new compression capability suppressed as activity-only change")
	}
}

func TestUplinkCompressedAndOrdinaryStreamsAreIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	sess := session.New(id, proto.SessionInfo{ID: id.String(), Cols: 80, Rows: 24})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 3; i++ {
		sess.PushOut(i, []byte{byte('0' + i)})
	}
	key := mustAccountKey32(t)
	u := newUplink("ws"+strings.TrimPrefix(remote.URL, "http"), "", proto.RemotePermissionFull, host, nil, func() []byte { return key }, false)
	barriers := make(chan struct{}, 8)
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
	read := func() proto.Frame {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	announce := read()
	var payload proto.AnnouncePayload
	if announce.Type != proto.TypeAnnounce || json.Unmarshal(announce.Payload, &payload) != nil || payload.OutputCompression != proto.OutputCompressionDeflate {
		t.Fatal("unlocked publisher did not advertise compression")
	}
	send := func(typ proto.Type, since uint64) {
		t.Helper()
		body, err := json.Marshal(proto.StreamRequestPayload{SessionID: id.String(), SinceSeq: since})
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: typ, SessionID: id, Payload: body})); err != nil {
			t.Fatal(err)
		}
	}
	barrier := func(want int) {
		t.Helper()
		body, _ := json.Marshal(proto.ViewersPayload{SessionID: id.String(), Count: 1})
		if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeViewers, Payload: body})); err != nil {
			t.Fatal(err)
		}
		select {
		case <-barriers:
		case <-ctx.Done():
			t.Fatal("uplink control barrier timed out")
		}
		host.mu.Lock()
		count := len(host.uplinkSubs[id])
		host.mu.Unlock()
		if count != want {
			t.Fatalf("active local proxies=%d want %d", count, want)
		}
	}
	sk, err := e2eecrypto.DeriveSessionKey(key, id)
	if err != nil {
		t.Fatal(err)
	}
	readOutput := func(typ proto.Type, wantSeq uint64, want string) {
		t.Helper()
		for {
			frame := read()
			if frame.Type != typ {
				continue
			}
			seq, envelope, err := proto.DecodeOut(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if seq == 0 {
				continue
			}
			if seq != wantSeq {
				t.Fatalf("stream %x seq=%d want %d", typ, seq, wantSeq)
			}
			plain, err := e2eecrypto.OpenOut(sk, id, byte(typ), seq, envelope)
			if err != nil {
				t.Fatal(err)
			}
			if typ == proto.TypeOutCompressed {
				plain, err = terminalcodec.Decode(plain)
			}
			if err != nil || string(plain) != want {
				t.Fatalf("stream %x plaintext=%q want %q err=%v", typ, plain, want, err)
			}
			return
		}
	}
	send(proto.TypeStreamRequestCompressed, 1)
	barrier(1)
	readOutput(proto.TypeOutCompressed, 2, "2")
	readOutput(proto.TypeOutCompressed, 3, "3")
	send(proto.TypeStreamRequestCompressed, 1)
	barrier(1)
	send(proto.TypeStreamRequest, 1)
	barrier(2)
	readOutput(proto.TypeOut, 2, "2")
	readOutput(proto.TypeOut, 3, "3")
	send(proto.TypeStreamStopCompressed, 0)
	barrier(1)
	send(proto.TypeStreamRequestCompressed, 3)
	barrier(2)
	sess.PushOut(4, []byte("4"))
	readOutput(proto.TypeOutCompressed, 4, "4")
	send(proto.TypeStreamStop, 0)
	barrier(1)
	sess.PushOut(5, []byte("5"))
	readOutput(proto.TypeOutCompressed, 5, "5")
	send(proto.TypeStreamStopCompressed, 0)
	barrier(0)
}
