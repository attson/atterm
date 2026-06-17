package feishu

import (
	"context"
	"errors"
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
	var called int
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		called++
		return &fakeRuntime{}, nil
	}
	lc := newLongConnWithFactory(LongConnConfig{AppID: "cli_x", AppSecret: "s"}, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if called != 1 {
		t.Fatalf("factory called %d times", called)
	}
	_ = lc.Close(context.Background())
}

func TestLongConn_StartFailure(t *testing.T) {
	bad := errors.New("boom")
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		return nil, bad
	}
	lc := newLongConnWithFactory(LongConnConfig{AppID: "cli_x", AppSecret: "s"}, factory)
	err := lc.Start(context.Background())
	if !errors.Is(err, bad) {
		t.Fatalf("want bad error, got %v", err)
	}
}

// fakeRuntime is the SDK boundary stub used by the longconn tests.
type fakeRuntime struct {
	closed bool
}

func (r *fakeRuntime) Run(ctx context.Context) error   { <-ctx.Done(); return nil }
func (r *fakeRuntime) Close(ctx context.Context) error { r.closed = true; return nil }
