// desktop/feishu/longconn.go
//
// Feishu long-connection subscriber skeleton. T16 fills in the SDK runtime;
// T17 adds reconnect + auth-class halt.
package feishu

import (
	"context"
	"errors"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type BackoffConfig struct {
	Initial time.Duration
	Max     time.Duration
}

type LongConnConfig struct {
	AppID     string
	AppSecret string
	Backoff   BackoffConfig

	OnBindMessage func(ctx context.Context, senderOpenID, text string)
	OnCardAction  func(ctx context.Context, sessionID, kind, event, operatorOpenID string)
}

// longConnRuntime is the boundary the SDK adapter satisfies.
type longConnRuntime interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

type runtimeFactory func(cfg LongConnConfig) (longConnRuntime, error)

// LongConn manages a single long-connection client across reconnects.
type LongConn struct {
	cfg     LongConnConfig
	factory runtimeFactory

	mu      sync.Mutex
	cancel  context.CancelFunc
	rt      longConnRuntime
	started bool
}

// NewLongConn returns a LongConn using the production SDK-backed factory.
// Task 16 implements the factory body.
func NewLongConn(cfg LongConnConfig) *LongConn {
	return newLongConnWithFactory(cfg, newLarkRuntime)
}

func newLongConnWithFactory(cfg LongConnConfig, factory runtimeFactory) *LongConn {
	if cfg.Backoff.Initial == 0 {
		cfg.Backoff.Initial = time.Second
	}
	if cfg.Backoff.Max == 0 {
		cfg.Backoff.Max = 5 * time.Minute
	}
	return &LongConn{cfg: cfg, factory: factory}
}

// Start opens the long connection.
func (l *LongConn) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return errors.New("longconn: already started")
	}
	rt, err := l.factory(l.cfg)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.rt = rt
	l.started = true
	l.mu.Unlock()
	go func() { _ = rt.Run(runCtx) }()
	return nil
}

func (l *LongConn) Close(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	if l.rt != nil {
		err := l.rt.Close(ctx)
		l.rt = nil
		l.started = false
		return err
	}
	return nil
}

// newLarkRuntime is the production factory. Task 16 fills the body.
func newLarkRuntime(cfg LongConnConfig) (longConnRuntime, error) {
	// placeholder — Task 16 will build a lark.NewClient(...) and attach WS handlers.
	_ = lark.NewClient
	return nil, errors.New("longconn: lark runtime not yet implemented (Task 16)")
}
