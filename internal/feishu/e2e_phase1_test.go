package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// TestPhase1_ShellAttachOutputReplyInject is the integration test for the
// MVP: a fake Feishu server records create + PATCH requests; a real Session
// is set up; FeishuSubscriber drains PTY output and PATCHes the anchor;
// router injects a reply back into the PTY's inbound queue.
func TestPhase1_ShellAttachOutputReplyInject(t *testing.T) {
	var mu sync.Mutex
	var patchCount int
	var lastPatchBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cardkit/v1/cards"):
			// Step 1 of send: CardKit entity create. Returns card_id.
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"card_id":"card_xyz"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/im/v1/messages"):
			// Step 2 of send: IM delivery referencing card_id.
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_xyz"}}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/cardkit/v1/cards/"):
			b, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(b, &payload)
			if pe, ok := payload["partial_element"].(string); ok {
				var inner map[string]any
				_ = json.Unmarshal([]byte(pe), &inner)
				if v, ok := inner["content"].(string); ok {
					lastPatchBody = v
				}
			}
			patchCount++
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	idx := NewCardIndex()

	// Build session + attach feishu subscriber.
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	cardJSON, _ := RenderAnchorCreate(AnchorState{SessionID: sess.ID.String(), SessionLabel: "test", StatusText: "running"})
	msgID, cardToken, err := client.SendAnchorCard(context.Background(), "tenant_tok", "ou_owner", cardJSON)
	if err != nil {
		t.Fatalf("send anchor: %v", err)
	}
	anchor := &CardAnchor{SessionID: sess.ID.String(), CardMsgID: msgID, CardToken: cardToken, OwnerOpenID: "ou_owner", CreatedAt: time.Now()}
	idx.Put(anchor)

	fs := AttachFeishuSubscriber(sess, "ou_owner", true /*pumpPTYBytes — shell e2e*/, func(body string) {
		go func() {
			_ = client.PatchCard(context.Background(), "tenant_tok", anchor.CardToken, AnchorBodyElementID, body, time.Now().UnixNano())
		}()
	})
	defer fs.Detach()

	// Push PTY output.
	sess.PushOut(1, []byte("hello\n"))
	sess.PushOut(2, []byte("world\n"))

	// Wait for the chunker's flush window plus a PATCH round-trip.
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	if patchCount < 1 {
		t.Errorf("expected ≥1 PATCH, got %d", patchCount)
	}
	if !strings.Contains(lastPatchBody, "hello") || !strings.Contains(lastPatchBody, "world") {
		t.Errorf("PATCH body missing output, got: %q", lastPatchBody)
	}
	mu.Unlock()

	// Now test the inbound router: simulate a reply.
	stubLookup := func(sid string) Subscriber {
		if sid == sess.ID.String() {
			return &feishuSubAdapter{fs: fs}
		}
		return nil
	}
	r := NewRouter(idx, stubLookup)
	dec := r.RouteReply(msgID, "ou_owner", "ls -la")
	if dec.Action != ActionInject {
		t.Fatalf("router decision = %v, want inject", dec.Action)
	}

	// Verify the inject reached the session's inbound queue.
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || !strings.Contains(string(f.Payload), "ls -la") {
			t.Errorf("inbound frame = %+v, want TypeIn with 'ls -la'", f)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no inbound frame within 500ms")
	}
}

// feishuSubAdapter bridges the concrete *FeishuSubscriber to the router's
// Subscriber interface for the e2e test.
type feishuSubAdapter struct{ fs *FeishuSubscriber }

func (a *feishuSubAdapter) ClaimDriver()              { a.fs.ClaimDriver() }
func (a *feishuSubAdapter) SendInput(b []byte) bool   { return a.fs.SendInput(b) }
func (a *feishuSubAdapter) OwnerOpenID() string       { return a.fs.OwnerOpenID() }
func (a *feishuSubAdapter) CurrentDriverName() string { return a.fs.CurrentDriverName() }
