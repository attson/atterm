package feishu

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

type Mode int

const (
	ModeRelay Mode = iota
	ModeLocal
)

// ServiceConfig assembles all the moving parts at startup.
type ServiceConfig struct {
	Mode Mode

	// Relay mode:
	RelayURL   string
	RelayToken func() string
	// RelayHTTPClient is the client used for relay REST calls (binding store +
	// borrowed-token source). Production wires the insecure-capable, ALPN-pinned
	// relay client here so a self-signed relay works. Nil falls back to HTTPClient.
	RelayHTTPClient *http.Client

	// Local mode:
	FeishuBase string
	HTTPClient *http.Client

	// Optional clock override for tests.
	Now func() int64

	// Sessions is the registry the hook server uses for existence checks.
	Sessions SessionLookup
}

// Service is the top-level façade.
type Service struct {
	cfg ServiceConfig

	store      BindingStore
	tokenSrc   TokenSource
	imClient   IMClient
	dispatcher *Dispatcher
	hookSrv    *HookServer

	lcMu     sync.Mutex // guards longConn (EnsureLongConn / CloseLongConn)
	longConn *LongConn

	routerMu sync.RWMutex
	router   *internalfeishu.Router
}

// SetRouter installs the inbound router used by handleReplyMessage and
// handleCardAction. Called by relay_host (via app.go) once the CardIndex and
// FeishuSubscriber registry are built. Safe to call concurrently; nil clears
// the router so anchor card actions are no longer routed (existing inject/ack
// paths in handleCardAction are unaffected).
func (s *Service) SetRouter(r *internalfeishu.Router) {
	s.routerMu.Lock()
	s.router = r
	s.routerMu.Unlock()
}

func (s *Service) getRouter() *internalfeishu.Router {
	s.routerMu.RLock()
	defer s.routerMu.RUnlock()
	return s.router
}

// replyText sends a plain text message back to openID using the dispatcher's
// token source and IM client. Best-effort: errors are logged and swallowed.
func (s *Service) replyText(ctx context.Context, openID, text string) {
	if s.dispatcher == nil {
		return
	}
	tok, _, err := s.dispatcher.GetToken(ctx)
	if err != nil {
		log.Printf("feishu: replyText get token: %v", err)
		return
	}
	if err := s.imClient.SendTextToOpenID(ctx, tok, openID, text); err != nil {
		log.Printf("feishu: replyText send to %s: %v", openID, err)
	}
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Mode == ModeRelay && cfg.RelayToken == nil {
		return nil, errors.New("desktop/feishu: relay mode requires RelayToken func")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.FeishuBase == "" {
		cfg.FeishuBase = "https://open.feishu.cn"
	}

	var store BindingStore
	var ts TokenSource
	switch cfg.Mode {
	case ModeRelay:
		// Relay REST calls go through RelayHTTPClient (insecure-capable +
		// ALPN-pinned in production); fall back to the general HTTPClient.
		relayClient := cfg.RelayHTTPClient
		if relayClient == nil {
			relayClient = cfg.HTTPClient
		}
		rs := NewRelayBackedBindingStore(cfg.RelayURL, cfg.RelayToken)
		rs.client = relayClient
		store = rs
		rts := NewRelayBorrowedTokenSource(cfg.RelayURL, cfg.RelayToken)
		rts.client = relayClient
		ts = rts
	case ModeLocal:
		ls := NewLocalKeychainBindingStore()
		store = ls
		ts = NewLocalTenantTokenSource(ls, cfg.FeishuBase, cfg.HTTPClient, func() time.Time { return time.Now() })
	default:
		return nil, errors.New("desktop/feishu: invalid mode")
	}

	im := &authClassAdaptingClient{inner: internalfeishu.NewClient(cfg.FeishuBase, cfg.HTTPClient)}

	d := NewDispatcher(DispatcherConfig{
		Store:   store,
		Token:   ts,
		IM:      im,
		CardKit: im,
		Now:     cfg.Now,
	})

	if cfg.Sessions == nil {
		cfg.Sessions = noOpSessionLookup{}
	}
	sessions := cfg.Sessions
	hookSrv := NewHookServer(d, sessions)

	return &Service{
		cfg: cfg, store: store, tokenSrc: ts,
		imClient: im, dispatcher: d, hookSrv: hookSrv,
	}, nil
}

func (s *Service) Store() BindingStore     { return s.store }
func (s *Service) Dispatcher() *Dispatcher { return s.dispatcher }
func (s *Service) HookServer() *HookServer { return s.hookSrv }
func (s *Service) Token() TokenSource      { return s.tokenSrc }

// Exists makes Service satisfy SessionLookup for embedded use; production
// callers pass an external SessionLookup via ServiceConfig.
func (s *Service) Exists(uuid.UUID) bool { return true }

// Inject satisfies SessionLookup for embedded use; production callers pass an
// external SessionLookup via ServiceConfig.
func (s *Service) Inject(uuid.UUID, string) error { return nil }

// EnsureLongConn starts the long-conn lazily once credentials exist.
// No-op in relay mode.
func (s *Service) EnsureLongConn(ctx context.Context) error {
	if s.cfg.Mode != ModeLocal {
		return nil
	}
	s.lcMu.Lock()
	defer s.lcMu.Unlock()
	v, err := s.store.Get(ctx)
	if err != nil {
		return err
	}
	if v.AppID == "" || v.AppSecret == "" {
		return errors.New("desktop/feishu: credentials missing")
	}
	if s.longConn != nil {
		return nil
	}
	lc := NewLongConn(LongConnConfig{
		AppID:     v.AppID,
		AppSecret: v.AppSecret,
		Backoff:   BackoffConfig{Initial: time.Second, Max: 5 * time.Minute},
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			s.handleBindMessage(ctx, senderOpenID, text)
		},
		OnReplyMessage: func(ctx context.Context, senderOpenID, parentID, text string) {
			s.handleReplyMessage(ctx, senderOpenID, parentID, text)
		},
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID, text string) {
			s.handleCardAction(ctx, sessionID, kind, event, operatorOpenID, text)
		},
		OnAuthClassFailure: func(ctx context.Context, _ error) {
			_ = s.store.SetDisabled(ctx)
		},
	})
	if err := lc.Start(ctx); err != nil {
		return err
	}
	s.longConn = lc
	return nil
}

// CloseLongConn stops the long connection if one is running and clears it.
// Safe to call in any mode (no-op when there is no long-conn, e.g. relay
// mode). Used when the service is being replaced on a relay login/logout.
func (s *Service) CloseLongConn(ctx context.Context) error {
	s.lcMu.Lock()
	defer s.lcMu.Unlock()
	if s.longConn == nil {
		return nil
	}
	err := s.longConn.Close(ctx)
	s.longConn = nil
	return err
}

// RenderAck returns the ack-update card the long-conn's card-action
// handler echoes back to Feishu. Pulled out so tests don't need the SDK.
func (s *Service) RenderAck(event, sessionID string) internalfeishu.AckResponse {
	return internalfeishu.RenderAckUpdateCard(internalfeishu.AckUpdateInput{
		Event: event, SessionID: sessionID,
	})
}

func (s *Service) handleBindMessage(ctx context.Context, senderOpenID, text string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/bind ") {
		return
	}
	code := strings.TrimSpace(strings.TrimPrefix(t, "/bind "))
	if !s.consumePending(code) {
		// Mirror the relay path's user feedback (internal/feishu/service.go) so
		// local-mode binds aren't silent on an invalid/expired short code.
		s.replyText(ctx, senderOpenID, "❌ 短码无效或已过期")
		return
	}
	if err := s.store.SetBound(ctx, senderOpenID); err != nil {
		log.Printf("feishu: bind SetBound: %v", err)
		s.replyText(ctx, senderOpenID, "❌ 服务端错误,请稍后再试")
		return
	}
	s.replyText(ctx, senderOpenID, "✅ 已绑定到 atterm")
}

// handleReplyMessage routes a Feishu reply (quoting a previously sent card) back
// into the originating session's PTY. parentID is the replied-to card's
// message_id, looked up in the dispatcher's message→session map.
//
// ModeLocal only: ModeRelay free-text reply is not yet wired (cross-process map).
// The relay process holds no copy of this in-process cardMsgs map, so relay users
// must use the card's quick-action buttons instead.
func (s *Service) handleReplyMessage(ctx context.Context, senderOpenID, parentID, text string) {
	t := strings.TrimSpace(text)
	if parentID == "" || t == "" {
		return
	}
	if strings.HasPrefix(t, "/bind ") {
		return // a bind command, not a reply-to-card
	}

	// Anchor card routing takes precedence: if a Router is installed and the
	// replied-to message is an anchor card, let the router handle it (permission
	// gate + subscriber inject). Fall through to the legacy cardMsgs path only
	// when the router is nil or doesn't know the card.
	if r := s.getRouter(); r != nil {
		decision := r.RouteReply(parentID, senderOpenID, t)
		switch decision.Action {
		case internalfeishu.ActionInject:
			return // router handled it; done
		case internalfeishu.ActionReject:
			if decision.Toast != "" {
				s.replyText(ctx, senderOpenID, decision.Toast)
			}
			return
		case internalfeishu.ActionPreempt:
			// Phase 2; treat as reject with toast for now.
			if decision.Toast != "" {
				s.replyText(ctx, senderOpenID, decision.Toast)
			}
			return
		}
		// If the router returned an unknown action (shouldn't happen), fall through.
	}

	// Legacy fallback: look up the card in the dispatcher's in-process map and
	// inject directly via Sessions. This path covers local-mode non-anchor cards
	// (e.g. WaitingInput cards sent before anchor cards were introduced).
	if s.dispatcher == nil {
		return
	}
	sid, ok := s.dispatcher.LookupCardSession(parentID)
	if !ok {
		return
	}
	if err := s.cfg.Sessions.Inject(sid, text+"\n"); err != nil {
		log.Printf("feishu: reply inject session=%s: %v", sid, err)
	}
}

func (s *Service) handleCardAction(ctx context.Context, sessionID, kind, event, operatorOpenID, text string) {
	textLen := len(text)
	log.Printf("feishu-card-action: sid=%s kind=%s event=%q op=%s text_len=%d", sessionID, kind, event, operatorOpenID, textLen)
	switch kind {
	case "input", "key", "end":
		// Anchor card actions: route through the inbound router.
		r := s.getRouter()
		if r == nil {
			return // anchor card routing not configured; drop silently
		}
		decision := r.RouteCardActionBySession(sessionID, operatorOpenID, kind, event, text)
		switch decision.Action {
		case internalfeishu.ActionInject:
			// Happy path. For input submissions, PUT a fresh input element
			// so the visible textbox clears — Feishu V2 input doesn't
			// auto-clear, and a PATCH default_value:"" is a client-side
			// no-op once user has typed. See Dispatcher.ClearAnchorInput.
			if kind == "input" {
				cardToken := r.CardTokenFor(sessionID)
				if cardToken != "" {
					seq := r.NextPatchSeq(sessionID)
					go s.clearAnchorInput(cardToken, sessionID, seq)
				}
			}
		case internalfeishu.ActionReject:
			if decision.Toast != "" && operatorOpenID != "" {
				s.replyText(ctx, operatorOpenID, decision.Toast)
			}
		case internalfeishu.ActionPreempt:
			// Phase 2; treat as reject with toast for now.
			if decision.Toast != "" && operatorOpenID != "" {
				s.replyText(ctx, operatorOpenID, decision.Toast)
			}
		}
	case "inject":
		// Legacy AskQuestion inject path (PR #250): inject text directly into
		// the session via SessionLookup. Keep existing behaviour intact.
		if text == "" {
			return
		}
		sid, err := uuid.Parse(sessionID)
		if err != nil {
			log.Printf("feishu: card inject bad session_id %q: %v", sessionID, err)
			return
		}
		if err := s.cfg.Sessions.Inject(sid, text); err != nil {
			log.Printf("feishu: card inject session=%s: %v", sid, err)
		}
	default:
		// Unknown kind; ignore.
	}
}

// clearAnchorInput PATCHes the anchor card's input element back to empty so
// the user's just-submitted reply doesn't linger in the textbox. Best-effort:
// errors logged, never bubble up (the inject itself already succeeded).
func (s *Service) clearAnchorInput(cardToken, sessionID string, sequence int64) {
	log.Printf("feishu: clear input entered card=%s seq=%d disp_nil=%v", cardToken, sequence, s.dispatcher == nil)
	if s.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := s.dispatcher.GetToken(ctx)
	if err != nil {
		log.Printf("feishu: clear input get token: %v", err)
		return
	}
	if err := s.dispatcher.ClearAnchorInput(ctx, tok, cardToken, sessionID, sequence); err != nil {
		log.Printf("feishu: clear input PUT card=%s seq=%d: %v", cardToken, sequence, err)
		return
	}
	log.Printf("feishu: clear input PUT ok card=%s seq=%d", cardToken, sequence)
}

// In-memory short-code table for local mode.
var (
	pendingMu    sync.Mutex
	pendingCodes = map[string]int64{}
)

// IssuePending generates a 6-char short-code valid for 15 minutes.
func (s *Service) IssuePending() string {
	code := internalfeishuPairCode()
	pendingMu.Lock()
	pendingCodes[code] = time.Now().Add(15 * time.Minute).Unix()
	pendingMu.Unlock()
	return code
}

// BeginPair returns a short-code the user can send to the bot to complete the
// bind flow. In relay mode the code is issued by the relay; in local mode it
// is generated in-process via IssuePending.
func (s *Service) BeginPair(ctx context.Context) (string, error) {
	if s.cfg.Mode == ModeRelay {
		url := strings.TrimRight(s.cfg.RelayURL, "/") + "/v1/feishu/bindings/me/begin-pair"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
		if err != nil {
			return "", fmt.Errorf("feishu begin-pair: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.cfg.RelayToken())
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.cfg.HTTPClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("feishu begin-pair: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("feishu begin-pair: relay status %d", resp.StatusCode)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return "", fmt.Errorf("feishu begin-pair: decode: %w", err)
		}
		if body.Code == "" {
			return "", errors.New("feishu begin-pair: relay returned empty code")
		}
		return body.Code, nil
	}
	return s.IssuePending(), nil
}

func (s *Service) consumePending(code string) bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	exp, ok := pendingCodes[code]
	if !ok || exp < time.Now().Unix() {
		delete(pendingCodes, code)
		return false
	}
	delete(pendingCodes, code)
	return true
}

// internalfeishuPairCode generates a 6-char short-code from a
// confusable-free alphabet.
func internalfeishuPairCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 6)
	rb := make([]byte, 6)
	if _, err := cryptorand.Read(rb); err != nil {
		panic(err)
	}
	for i, b := range rb {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

type noOpSessionLookup struct{}

func (noOpSessionLookup) Exists(uuid.UUID) bool          { return true }
func (noOpSessionLookup) Inject(uuid.UUID, string) error { return nil }

// authClassAdaptingClient promotes internal/feishu.AuthClassError to
// satisfy the desktop dispatcher's IsFeishuAuthClassError contract.
type authClassAdaptingClient struct {
	inner *internalfeishu.Client
}

func (c *authClassAdaptingClient) SendInteractiveToOpenID(ctx context.Context, tok, open string, body []byte) (string, error) {
	mid, err := c.inner.SendInteractiveToOpenID(ctx, tok, open, body)
	return mid, c.adapt(err)
}
func (c *authClassAdaptingClient) SendTextToOpenID(ctx context.Context, tok, open, text string) error {
	return c.adapt(c.inner.SendTextToOpenID(ctx, tok, open, text))
}
func (c *authClassAdaptingClient) SendAnchorCard(ctx context.Context, tok, openID string, cardBody []byte) (string, string, error) {
	mid, token, err := c.inner.SendAnchorCard(ctx, tok, openID, cardBody)
	return mid, token, c.adapt(err)
}
func (c *authClassAdaptingClient) PatchCard(ctx context.Context, tok, cardToken, elementID, bodyMarkdown string, sequence int64) error {
	return c.adapt(c.inner.PatchCard(ctx, tok, cardToken, elementID, bodyMarkdown, sequence))
}
func (c *authClassAdaptingClient) PatchCardElement(ctx context.Context, tok, cardToken, elementID string, partial map[string]any, sequence int64) error {
	return c.adapt(c.inner.PatchCardElement(ctx, tok, cardToken, elementID, partial, sequence))
}
func (c *authClassAdaptingClient) UpdateCardElement(ctx context.Context, tok, cardToken, elementID string, element map[string]any, sequence int64) error {
	return c.adapt(c.inner.UpdateCardElement(ctx, tok, cardToken, elementID, element, sequence))
}
func (c *authClassAdaptingClient) adapt(err error) error {
	if err == nil {
		return nil
	}
	if internalfeishu.IsAuthClassError(err) {
		return &authClassErr{inner: err}
	}
	return err
}

type authClassErr struct{ inner error }

func (e *authClassErr) Error() string                { return e.inner.Error() }
func (e *authClassErr) Unwrap() error                { return e.inner }
func (e *authClassErr) IsFeishuAuthClassError() bool { return true }
