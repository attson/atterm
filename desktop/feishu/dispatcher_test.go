// desktop/feishu/dispatcher_test.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

type capturingIM struct {
	mu       sync.Mutex
	bodies   []string
	openIDs  []string
	tokens   []string
	err      error
	authFail bool
}

func (c *capturingIM) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authFail {
		return &authError{}
	}
	if c.err != nil {
		return c.err
	}
	c.tokens = append(c.tokens, token)
	c.openIDs = append(c.openIDs, openID)
	c.bodies = append(c.bodies, string(body))
	return nil
}
func (c *capturingIM) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	return nil
}

type authError struct{}

func (*authError) Error() string                { return "auth-class fake" }
func (*authError) IsFeishuAuthClassError() bool { return true }

type stubTokenSource struct {
	tok, openID, hash string
	err               error
	invalidated       atomic.Int32
}

func (s *stubTokenSource) Get(ctx context.Context) (string, string, string, error) {
	if s.err != nil {
		return "", "", "", s.err
	}
	return s.tok, s.openID, s.hash, nil
}
func (s *stubTokenSource) Invalidate() { s.invalidated.Add(1) }

func TestDispatcher_CommandFinishedHappy(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})

	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: uuid.New(),
		ExitCode:  0,
		Label:     "go test",
		ElapsedMS: 2500,
	})
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
	if im.openIDs[0] != "ou_x" || im.tokens[0] != "tt" {
		t.Fatalf("send args: %+v %+v", im.openIDs, im.tokens)
	}
}

func TestDispatcher_NoBindingDrops(t *testing.T) {
	store := &inMemBindingStore{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{err: ErrTokenNotConfigured},
		IM:    &capturingIM{},
	})
	im := d.cfg.IM.(*capturingIM)
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{SessionID: uuid.New()})
	if len(im.bodies) != 0 {
		t.Fatalf("expected drop")
	}
}

func TestDispatcher_DedupWindowSuppressesSecondWaiting(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"},
		IM:    im,
	})
	sid := uuid.New()
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHook, QuestionText: "Q1", DedupKey: "k1",
	})
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHeuristic, QuestionText: "", DedupKey: "",
	})
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
}

func TestDispatcher_AuthFailDisables(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{authFail: true}
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})
	for i := 0; i < 3; i++ {
		d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
			SessionID: uuid.New(),
		})
	}
	v, _ := store.Get(context.Background())
	if v.DisabledAt == 0 {
		t.Fatalf("expected store.DisabledAt set after 3 auth-class failures")
	}
	if ts.invalidated.Load() == 0 {
		t.Fatalf("expected token source to be invalidated")
	}
}

func TestDispatcher_DedupExpires(t *testing.T) {
	now := newAtomicTime(1_000_000)
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"},
		IM:    im,
		Now:   now.read,
	})
	sid := uuid.New()
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHook, DedupKey: "k1",
	})
	now.advance(31)
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHeuristic,
	})
	if len(im.bodies) != 2 {
		t.Fatalf("expected 2 sends after window expiry; got %d", len(im.bodies))
	}
}

func TestDispatch_AskQuestionRendersBlueCard(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})

	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID:    uuid.New(),
		Source:       WaitingSourceHook,
		QuestionText: "Deploy?",
		Options: []QuestionOption{
			{Label: "Yes", Description: "go"},
			{Label: "No", Description: "stop"},
		},
	})
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(im.bodies[0]), &m); err != nil {
		t.Fatalf("unmarshal card body: %v", err)
	}
	card, ok := m["card"].(map[string]any)
	if !ok {
		t.Fatalf("card body missing card key: %v", m)
	}
	header, ok := card["header"].(map[string]any)
	if !ok {
		t.Fatalf("card missing header: %v", card)
	}
	if header["template"] != "blue" {
		t.Fatalf("expected blue header template, got %v", header["template"])
	}
}

// atomicTime is a tiny test clock used by the dedup window test.
type atomicTime struct {
	v atomic.Int64
}

func newAtomicTime(seconds int64) *atomicTime { a := &atomicTime{}; a.v.Store(seconds); return a }
func (a *atomicTime) read() (sec int64)       { return a.v.Load() }
func (a *atomicTime) advance(s int64)         { a.v.Add(s) }

// Force errors import; required by the auth-error helper.
var _ = errors.New
