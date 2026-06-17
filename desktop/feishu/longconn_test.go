package feishu

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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

func TestLongConn_OnCardAction_RoutesAck(t *testing.T) {
	called := 0
	gotSID := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID string) {
			called++
			gotSID = sessionID
		},
	}
	r := newTestableRuntime(cfg)
	r.injectCardAction("ou_op", "sid-99", "ack", "command_finished")
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
