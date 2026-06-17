// desktop/feishu/longconn.go
//
// Feishu long-connection subscriber. T16 fills in the SDK runtime;
// T17 adds reconnect + auth-class halt.
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
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

// imTextContent is the JSON shape of a text IM message body.
type imTextContent struct {
	Text string `json:"text"`
}

// extractIMText decodes the Content field of a received IM message to plain text.
func extractIMText(msg *larkim.EventMessage) string {
	if msg == nil || msg.Content == nil {
		return ""
	}
	var tc imTextContent
	if err := json.Unmarshal([]byte(*msg.Content), &tc); err != nil {
		return ""
	}
	return tc.Text
}

// extractCardActionFields pulls session_id, kind, event, and operator open_id
// from the card action event's Value map.
func extractCardActionFields(ev *callback.CardActionTriggerEvent) (sessionID, kind, eventStr, operatorOpenID string) {
	if ev.Event != nil && ev.Event.Operator != nil {
		operatorOpenID = ev.Event.Operator.OpenID
	}
	if ev.Event != nil && ev.Event.Action != nil {
		v := ev.Event.Action.Value
		if s, ok := v["session_id"].(string); ok {
			sessionID = s
		}
		if s, ok := v["kind"].(string); ok {
			kind = s
		}
		if s, ok := v["event"].(string); ok {
			eventStr = s
		}
	}
	return
}

// newLarkRuntime is the production factory backed by the larksuite SDK.
func newLarkRuntime(cfg LongConnConfig) (longConnRuntime, error) {
	disp := dispatcher.NewEventDispatcher("", "")

	// IM message handler → cfg.OnBindMessage
	disp.OnP2MessageReceiveV1(func(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
		if cfg.OnBindMessage == nil {
			return nil
		}
		var senderOpenID string
		if ev.Event != nil && ev.Event.Sender != nil &&
			ev.Event.Sender.SenderId != nil && ev.Event.Sender.SenderId.OpenId != nil {
			senderOpenID = *ev.Event.Sender.SenderId.OpenId
		}
		var text string
		if ev.Event != nil {
			text = extractIMText(ev.Event.Message)
		}
		cfg.OnBindMessage(ctx, senderOpenID, text)
		return nil
	})

	// Card action handler → cfg.OnCardAction
	disp.OnP2CardActionTrigger(func(ctx context.Context, ev *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
		if cfg.OnCardAction != nil {
			sessionID, kind, eventStr, operatorOpenID := extractCardActionFields(ev)
			cfg.OnCardAction(ctx, sessionID, kind, eventStr, operatorOpenID)
		}
		return nil, nil
	})

	cli := larkws.NewClient(cfg.AppID, cfg.AppSecret,
		larkws.WithEventHandler(disp),
		larkws.WithAutoReconnect(false),
	)
	return &larkRuntime{cli: cli}, nil
}

type larkRuntime struct {
	cli *larkws.Client
}

func (r *larkRuntime) Run(ctx context.Context) error { return r.cli.Start(ctx) }
func (r *larkRuntime) Close(_ context.Context) error {
	r.cli.Close()
	return nil
}

// testableRuntime is the in-process injector used by longconn_test.go to
// exercise the event routing paths without spinning up the real SDK.
type testableRuntime struct{ cfg LongConnConfig }

func newTestableRuntime(cfg LongConnConfig) *testableRuntime { return &testableRuntime{cfg: cfg} }

func (r *testableRuntime) injectIMMessage(senderOpenID, text string) {
	if r.cfg.OnBindMessage != nil {
		r.cfg.OnBindMessage(context.Background(), senderOpenID, text)
	}
}
func (r *testableRuntime) injectCardAction(operatorOpenID, sessionID, kind, event string) {
	if r.cfg.OnCardAction != nil {
		r.cfg.OnCardAction(context.Background(), sessionID, kind, event, operatorOpenID)
	}
}
