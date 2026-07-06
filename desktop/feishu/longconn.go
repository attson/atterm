// desktop/feishu/longconn.go
//
// Feishu long-connection subscriber. T16 fills in the SDK runtime;
// T17 adds reconnect + auth-class halt.
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	// OnReplyMessage fires for the same IM message events as OnBindMessage, but
	// carries parentID (the replied-to message's message_id). It is used to route
	// a Feishu reply quoting a previously sent card back to its session (9B).
	OnReplyMessage func(ctx context.Context, senderOpenID, parentID, text string)
	OnCardAction   func(ctx context.Context, sessionID, kind, event, operatorOpenID, text string, formValue map[string]any)

	// OnAuthClassFailure fires once when the SDK returns an auth-class
	// error (invalid app secret, app disabled, etc.). The reconnect loop
	// halts after invoking it.
	OnAuthClassFailure func(ctx context.Context, err error)
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
	runCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.started = true
	l.mu.Unlock()
	go l.runLoop(runCtx)
	return nil
}

func (l *LongConn) runLoop(ctx context.Context) {
	backoff := l.cfg.Backoff.Initial
	for {
		if ctx.Err() != nil {
			return
		}
		rt, err := l.factory(l.cfg)
		if err != nil {
			if isAuthClass(err) {
				if l.cfg.OnAuthClassFailure != nil {
					l.cfg.OnAuthClassFailure(ctx, err)
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, l.cfg.Backoff.Max)
			continue
		}
		l.mu.Lock()
		l.rt = rt
		l.mu.Unlock()
		err = rt.Run(ctx)
		_ = rt.Close(context.Background())
		if ctx.Err() != nil {
			return
		}
		if isAuthClass(err) {
			if l.cfg.OnAuthClassFailure != nil {
				l.cfg.OnAuthClassFailure(ctx, err)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, l.cfg.Backoff.Max)
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

func isAuthClass(err error) bool {
	var ac interface{ IsFeishuAuthClassError() bool }
	return errors.As(err, &ac) && ac.IsFeishuAuthClassError()
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

// extractCardActionFields pulls session_id, kind, event, operator open_id, and
// text from the card action event. V2 schema input elements deliver the
// user-typed text on CallBackAction.InputValue (a top-level string), NOT in
// Value["text"]; reading the wrong slot makes every Feishu reply silently
// fail the router's empty-text gate.
func extractCardActionFields(ev *callback.CardActionTriggerEvent) (sessionID, kind, eventStr, operatorOpenID, text string) {
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
		// V2 input element: typed text lives here.
		text = ev.Event.Action.InputValue
		// Defensive fallback for any older card flavour that still sets text
		// inside the value map.
		if text == "" {
			if s, ok := v["text"].(string); ok {
				text = s
			}
		}
	}
	return
}

// newLarkRuntime is the production factory backed by the larksuite SDK.
func newLarkRuntime(cfg LongConnConfig) (longConnRuntime, error) {
	disp := dispatcher.NewEventDispatcher("", "")

	// IM message handler → cfg.OnBindMessage (/bind) + cfg.OnReplyMessage (reply).
	// Both callbacks fire for every IM message; each decides whether to act.
	disp.OnP2MessageReceiveV1(func(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
		if cfg.OnBindMessage == nil && cfg.OnReplyMessage == nil {
			return nil
		}
		var senderOpenID string
		if ev.Event != nil && ev.Event.Sender != nil &&
			ev.Event.Sender.SenderId != nil && ev.Event.Sender.SenderId.OpenId != nil {
			senderOpenID = *ev.Event.Sender.SenderId.OpenId
		}
		var text, parentID string
		if ev.Event != nil {
			text = extractIMText(ev.Event.Message)
			// ParentId is the message_id of the message this one replies to
			// (the original card, for a reply-to-card). It is nil for a fresh
			// top-level message; guard the deref.
			if ev.Event.Message != nil && ev.Event.Message.ParentId != nil {
				parentID = *ev.Event.Message.ParentId
			}
		}
		if cfg.OnBindMessage != nil {
			cfg.OnBindMessage(ctx, senderOpenID, text)
		}
		if cfg.OnReplyMessage != nil && parentID != "" {
			cfg.OnReplyMessage(ctx, senderOpenID, parentID, text)
		}
		return nil
	})

	// Card action handler → cfg.OnCardAction
	disp.OnP2CardActionTrigger(func(ctx context.Context, ev *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
		// Unconditional ingress log so we can tell whether Feishu is even
		// delivering card.action events to LongConn (separate question from
		// "did our extractor / router parse them correctly").
		hasAction := ev != nil && ev.Event != nil && ev.Event.Action != nil
		var tag string
		if hasAction {
			tag = ev.Event.Action.Tag
		}
		log.Printf("feishu-longconn: card_action ingress has_action=%v tag=%q has_cb=%v",
			hasAction, tag, cfg.OnCardAction != nil)
		if cfg.OnCardAction != nil {
			sessionID, kind, eventStr, operatorOpenID, text := extractCardActionFields(ev)
			var formValue map[string]any
			if ev != nil && ev.Event != nil && ev.Event.Action != nil {
				formValue = ev.Event.Action.FormValue
			}
			cfg.OnCardAction(ctx, sessionID, kind, eventStr, operatorOpenID, text, formValue)
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
	r.injectIMMessageReply(senderOpenID, "", text)
}

// injectIMMessageReply mirrors the production OnP2MessageReceiveV1 routing: both
// OnBindMessage and (when parentID is set) OnReplyMessage fire for one message.
func (r *testableRuntime) injectIMMessageReply(senderOpenID, parentID, text string) {
	if r.cfg.OnBindMessage != nil {
		r.cfg.OnBindMessage(context.Background(), senderOpenID, text)
	}
	if r.cfg.OnReplyMessage != nil && parentID != "" {
		r.cfg.OnReplyMessage(context.Background(), senderOpenID, parentID, text)
	}
}
func (r *testableRuntime) injectCardAction(operatorOpenID, sessionID, kind, event, text string) {
	if r.cfg.OnCardAction != nil {
		r.cfg.OnCardAction(context.Background(), sessionID, kind, event, operatorOpenID, text, nil)
	}
}
