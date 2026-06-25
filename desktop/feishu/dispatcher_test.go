// desktop/feishu/dispatcher_test.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func (c *capturingIM) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authFail {
		return "", &authError{}
	}
	if c.err != nil {
		return "", c.err
	}
	c.tokens = append(c.tokens, token)
	c.openIDs = append(c.openIDs, openID)
	c.bodies = append(c.bodies, string(body))
	return "om_test", nil
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

func TestDispatch_NoOptionsRendersOrangeCard(t *testing.T) {
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
	if header["template"] != "orange" {
		t.Fatalf("expected orange header template, got %v", header["template"])
	}
}

func TestDispatch_AskQuestionOptionInjectText(t *testing.T) {
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
	elements, ok := card["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("card missing elements: %v", card)
	}
	actionRow, ok := elements[len(elements)-1].(map[string]any)
	if !ok {
		t.Fatalf("last element not an object: %v", elements[len(elements)-1])
	}
	if actionRow["tag"] != "action" {
		t.Fatalf("expected last element to be an action row, got tag %v", actionRow["tag"])
	}
	actions, ok := actionRow["actions"].([]any)
	if !ok {
		t.Fatalf("action row missing actions: %v", actionRow)
	}
	// actions[0] is the jump (url) button with no value.text; the inject
	// option buttons follow.
	if len(actions) != 3 {
		t.Fatalf("expected 3 action buttons (jump + 2 inject), got %d", len(actions))
	}
	want := []string{"1\n", "2\n"}
	for i, w := range want {
		btn, ok := actions[i+1].(map[string]any)
		if !ok {
			t.Fatalf("inject button %d not an object: %v", i+1, actions[i+1])
		}
		val, ok := btn["value"].(map[string]any)
		if !ok {
			t.Fatalf("inject button %d missing value: %v", i+1, btn)
		}
		if val["text"] != w {
			t.Fatalf("inject button %d: expected value.text %q, got %v", i+1, w, val["text"])
		}
	}
}

func TestDispatch_CommandFailureCount(t *testing.T) {
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

	// First failure: count == 1, no "连续第 N 次" banner.
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: sid, ExitCode: 1, Label: "go test",
	})
	// Advance past the dedup window so the second send is not suppressed.
	now.advance(dedupWindowSeconds + 1)
	// Second consecutive failure: count == 2 → banner present.
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: sid, ExitCode: 1, Label: "go test",
	})
	if len(im.bodies) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(im.bodies))
	}
	if strings.Contains(im.bodies[0], "连续第") {
		t.Fatalf("first failure must not show a streak banner: %s", im.bodies[0])
	}
	if !strings.Contains(im.bodies[1], "连续第 2 次") {
		t.Fatalf("second failure should show 连续第 2 次: %s", im.bodies[1])
	}

	// A success resets the streak counter.
	now.advance(dedupWindowSeconds + 1)
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: sid, ExitCode: 0, Label: "go test",
	})
	// Next failure should be back to count == 1 (no banner).
	now.advance(dedupWindowSeconds + 1)
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: sid, ExitCode: 1, Label: "go test",
	})
	if len(im.bodies) != 4 {
		t.Fatalf("expected 4 sends, got %d", len(im.bodies))
	}
	if strings.Contains(im.bodies[3], "连续第") {
		t.Fatalf("failure after a success must reset the streak: %s", im.bodies[3])
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
