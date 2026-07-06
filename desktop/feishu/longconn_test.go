package feishu

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestLongConn_NewAndClose(t *testing.T) {
	lc := NewLongConn(LongConnConfig{
		AppID:     "cli_x",
		AppSecret: "s",
		Backoff:   BackoffConfig{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	})
	if err := lc.Close(context.Background()); err != nil {
		t.Fatalf("Close on never-started: %v", err)
	}
}

func TestLongConn_StartCallsSDKFactory(t *testing.T) {
	var called int32
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		atomic.AddInt32(&called, 1)
		return &fakeRuntime{}, nil
	}
	lc := newLongConnWithFactory(LongConnConfig{AppID: "cli_x", AppSecret: "s"}, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Factory is called inside the run goroutine; give it time to fire.
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&called) < 1 {
		t.Fatalf("factory called %d times", atomic.LoadInt32(&called))
	}
	_ = lc.Close(context.Background())
}

// fakeRuntime is the SDK boundary stub used by the longconn tests.
type fakeRuntime struct {
	closed int32 // accessed atomically
}

func (r *fakeRuntime) Run(ctx context.Context) error { <-ctx.Done(); return nil }
func (r *fakeRuntime) Close(ctx context.Context) error {
	atomic.StoreInt32(&r.closed, 1)
	return nil
}

func TestLongConn_OnBindMessage_RoutesText(t *testing.T) {
	gotSender := ""
	gotText := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			gotSender = senderOpenID
			gotText = text
		},
	}
	r := newTestableRuntime(cfg)
	r.injectIMMessage("ou_sender", "/bind ABC123")
	if gotSender != "ou_sender" || gotText != "/bind ABC123" {
		t.Fatalf("not routed: sender=%q text=%q", gotSender, gotText)
	}
}

func TestLongConn_OnReplyMessage_RoutesParentID(t *testing.T) {
	replyCalled := 0
	replySender := ""
	replyParent := ""
	replyText := ""
	bindCalled := 0
	bindText := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			bindCalled++
			bindText = text
		},
		OnReplyMessage: func(ctx context.Context, senderOpenID, parentID, text string) {
			replyCalled++
			replySender = senderOpenID
			replyParent = parentID
			replyText = text
		},
	}
	r := newTestableRuntime(cfg)

	// parentID != "" → both OnBindMessage and OnReplyMessage fire.
	r.injectIMMessageReply("ou_x", "om_parent", "yes")
	if replyCalled != 1 {
		t.Fatalf("expected OnReplyMessage to fire once, got %d", replyCalled)
	}
	if replySender != "ou_x" || replyParent != "om_parent" || replyText != "yes" {
		t.Fatalf("reply routed wrong: sender=%q parent=%q text=%q", replySender, replyParent, replyText)
	}
	if bindCalled != 1 || bindText != "yes" {
		t.Fatalf("expected OnBindMessage to fire once with text=yes, got %d / %q", bindCalled, bindText)
	}

	// parentID == "" → gating blocks OnReplyMessage; OnBindMessage still fires.
	r.injectIMMessageReply("ou_x", "", "hello")
	if replyCalled != 1 {
		t.Fatalf("expected OnReplyMessage NOT to fire for empty parentID, total calls=%d", replyCalled)
	}
	if bindCalled != 2 {
		t.Fatalf("expected OnBindMessage to fire again, got %d", bindCalled)
	}
}

func TestLongConn_OnCardAction_RoutesAck(t *testing.T) {
	called := 0
	gotSID := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID, text string, formValue map[string]any) {
			called++
			gotSID = sessionID
		},
	}
	r := newTestableRuntime(cfg)
	r.injectCardAction("ou_op", "sid-99", "ack", "command_finished", "")
	if called != 1 || gotSID != "sid-99" {
		t.Fatalf("expected one card-action callback with sid-99, got %d / %q", called, gotSID)
	}
}

func TestLongConn_ReconnectBackoff(t *testing.T) {
	var attempts int32
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 4 {
			return &flakyRuntime{returns: errors.New("conn drop")}, nil
		}
		return &fakeRuntime{}, nil
	}
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		Backoff: BackoffConfig{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	}
	lc := newLongConnWithFactory(cfg, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(&attempts) < 4 {
		t.Fatalf("expected ≥4 attempts, got %d", atomic.LoadInt32(&attempts))
	}
	_ = lc.Close(context.Background())
}

func TestLongConn_AuthClassDisablesAndStops(t *testing.T) {
	var disabled int32
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		Backoff: BackoffConfig{Initial: 10 * time.Millisecond, Max: 20 * time.Millisecond},
		OnAuthClassFailure: func(ctx context.Context, _ error) {
			atomic.StoreInt32(&disabled, 1)
		},
	}
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		return &flakyRuntime{returns: &authError{}}, nil
	}
	lc := newLongConnWithFactory(cfg, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if atomic.LoadInt32(&disabled) != 1 {
		t.Fatalf("expected OnAuthClassFailure to fire")
	}
	_ = lc.Close(context.Background())
}

type flakyRuntime struct{ returns error }

func (r *flakyRuntime) Run(ctx context.Context) error { return r.returns }
func (r *flakyRuntime) Close(_ context.Context) error { return nil }

// V2 schema card input puts user-typed text in CallBackAction.InputValue,
// not in CallBackAction.Value["text"]. Reading the wrong slot makes every
// Feishu reply silently fail the router's empty-text gate. Regression
// guard.
func TestExtractCardActionFields_InputValueWins(t *testing.T) {
	ev := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "ou_owner"},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"kind":       "input",
					"session_id": "sid-42",
				},
				InputValue: "user typed this",
			},
		},
	}
	sid, kind, _, op, text := extractCardActionFields(ev)
	if sid != "sid-42" || kind != "input" || op != "ou_owner" {
		t.Errorf("metadata wrong: sid=%q kind=%q op=%q", sid, kind, op)
	}
	if text != "user typed this" {
		t.Errorf("text = %q; want %q from InputValue", text, "user typed this")
	}
}

// Buttons (kind=key) carry their event name in Value["event"]; InputValue
// is empty. Make sure that path still works after the InputValue fix.
func TestExtractCardActionFields_KeyButtonStillWorks(t *testing.T) {
	ev := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "ou_owner"},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"kind":       "key",
					"session_id": "sid-42",
					"event":      "ctrl_c",
				},
			},
		},
	}
	_, kind, eventStr, _, text := extractCardActionFields(ev)
	if kind != "key" || eventStr != "ctrl_c" {
		t.Errorf("kind=%q event=%q; want key+ctrl_c", kind, eventStr)
	}
	if text != "" {
		t.Errorf("text = %q; want empty for key buttons", text)
	}
}
