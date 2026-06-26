// internal/feishu/service.go
package feishu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
)

// Binding is the service's view of a feishu binding (subset of the
// userstore row — only what the service needs).
type Binding struct {
	UserID      string
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
	AppIDHash   string
	OpenID      string
	DisabledAt  int64
}

// ErrBindingNotFound is the sentinel the BindingStore returns when no
// row matches.
var ErrBindingNotFound = errors.New("feishu: binding not found")

// ErrFeishuPendingBindNotFoundService is the sentinel returned by
// ConsumePendingBind. Renamed to avoid colliding with the userstore
// symbol while keeping the meaning crystal-clear at this layer.
var ErrFeishuPendingBindNotFoundService = errors.New("feishu: pending bind not found")

// ErrDecryptFailed wraps any decryption-side error.
var ErrDecryptFailed = errors.New("feishu: decrypt failed")

// ErrBindingDisabled is returned by RelayToken when the stored binding
// has been marked disabled (typically after auth-class failures). The
// caller surfaces this to the desktop so the UI prompts re-config.
var ErrBindingDisabled = errors.New("feishu: binding disabled")

// BindingStore is what the service needs from userstore.
type BindingStore interface {
	GetBindingByAppIDHash(ctx context.Context, hash string) (*Binding, error)
	GetBindingByUserID(ctx context.Context, userID string) (*Binding, error)
	MarkBound(ctx context.Context, userID, openID string) error
	MarkDisabled(ctx context.Context, userID string) error
	ConsumePendingBind(ctx context.Context, code string) (string, error)
}

// IMClient is what the service needs from the HTTP client.
type IMClient interface {
	SendInteractiveToOpenID(ctx context.Context, token, openID string, cardBody []byte) (string, error)
	SendTextToOpenID(ctx context.Context, token, openID, text string) error
}

// TokenSource is what the service needs from TenantTokenCache.
type TokenSource interface {
	Get(ctx context.Context, appID, secret string) (string, error)
	Invalidate(appID string)
}

// ServiceConfig groups the moving parts.
type ServiceConfig struct {
	Store BindingStore
	IM    IMClient
	Token TokenSource
}

// Service is the aggregate layer. Methods are safe for concurrent use.
type Service struct {
	cfg ServiceConfig
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{cfg: cfg}
}

// RelayToken mints a tenant_access_token for the user's bound app and
// returns it along with the bound open_id + app_id_hash. Used by the
// new POST /v1/feishu/relay-token/me handler so the desktop can send
// IM messages directly without the relay seeing the payload.
//
// Errors:
//
//	ErrBindingNotFound  → caller maps to 404
//	ErrBindingDisabled  → caller maps to 410
//	other               → caller maps to 502 (Feishu unreachable)
func (s *Service) RelayToken(ctx context.Context, userID string) (token, openID, appIDHash string, err error) {
	b, err := s.cfg.Store.GetBindingByUserID(ctx, userID)
	if err != nil {
		return "", "", "", err
	}
	if b.DisabledAt != 0 {
		return "", "", "", ErrBindingDisabled
	}
	tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
	if err != nil {
		return "", "", "", err
	}
	return tok, b.OpenID, b.AppIDHash, nil
}

// HandleStatus is the high-level result for the HTTP handler.
type HandleStatus int

const (
	HandleStatusOK HandleStatus = iota
)

// Injection is surfaced when a card button asks to inject text into a session.
// internal/feishu stays a pure render/parse package: it never touches the
// session registry. The relay HTTP layer consumes this and performs the inject.
type Injection struct {
	SessionID   string
	Text        string
	OwnerUserID string // binding owner, for relay-side session ownership check
}

// HandleResult is what HandleEvent returns; the HTTP handler always
// replies 200 but inspects fields to emit the right body.
type HandleResult struct {
	Status       HandleStatus
	Reason       string       // short tag for logs
	URLChallenge string       // non-empty → reply { "challenge": ... }
	CardUpdate   *AckResponse // non-nil → reply JSON of this object
	Inject       *Injection   // non-nil → relay layer injects text into session
	LogError     error        // attached for tests; HTTP handler logs it
}

// HandleEvent is the inbound entry point. The HTTP handler calls this
// with the raw request body + the app_id_hash from the URL path.
//
// Contract: HandleEvent never returns an error that should produce a
// non-200 HTTP response. Decryption / verification failures are
// reported via HandleResult.LogError.
func (s *Service) HandleEvent(ctx context.Context, appIDHash string, body []byte) (*HandleResult, error) {
	b, err := s.cfg.Store.GetBindingByAppIDHash(ctx, appIDHash)
	if errors.Is(err, ErrBindingNotFound) {
		return &HandleResult{Reason: "unknown_app_id_hash"}, nil
	}
	if err != nil {
		return &HandleResult{Reason: "store_error", LogError: err}, nil
	}
	plain, err := DecryptEnvelope(body, b.EncryptKey)
	if err != nil {
		return &HandleResult{Reason: "decrypt_failed", LogError: fmt.Errorf("%w: %v", ErrDecryptFailed, err)}, nil
	}
	env, err := ParseEnvelope(plain)
	if err != nil {
		return &HandleResult{Reason: "parse_failed", LogError: err}, nil
	}
	if err := VerifyEnvelopeToken(env, b.VerifyToken); err != nil {
		return &HandleResult{Reason: "verify_token_mismatch", LogError: err}, nil
	}
	if env.URLVerification != nil {
		return &HandleResult{URLChallenge: env.URLVerification.Challenge}, nil
	}
	if env.Header == nil {
		return &HandleResult{Reason: "no_header"}, nil
	}
	switch env.Header.EventType {
	case "im.message.receive_v1":
		if env.Message == nil {
			return &HandleResult{Reason: "no_message"}, nil
		}
		go s.handleBindMessage(context.WithoutCancel(ctx), b, env.Message)
		return &HandleResult{Reason: "im_message_dispatched"}, nil
	case "card.action.trigger":
		if env.CardAction == nil {
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
		switch env.CardAction.Kind {
		case "ack":
			ack := RenderAckUpdateCard(AckUpdateInput{Event: env.CardAction.Event, SessionID: env.CardAction.SessionID})
			return &HandleResult{CardUpdate: &ack, Reason: "card_ack"}, nil
		case "inject":
			ack := RenderAckUpdateCard(AckUpdateInput{Event: "inject", SessionID: env.CardAction.SessionID})
			return &HandleResult{
				CardUpdate: &ack,
				Inject:     &Injection{SessionID: env.CardAction.SessionID, Text: env.CardAction.Text, OwnerUserID: b.UserID},
				Reason:     "card_inject",
			}, nil
		default:
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
	default:
		return &HandleResult{Reason: "ignored_event_type"}, nil
	}
}

// handleBindMessage processes "/bind <CODE>" text messages. Async-safe;
// HTTP handler does NOT wait on this.
func (s *Service) handleBindMessage(ctx context.Context, b *Binding, msg *MessageReceive) {
	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, "/bind ") {
		return
	}
	code := strings.TrimSpace(strings.TrimPrefix(text, "/bind "))
	uid, err := s.cfg.Store.ConsumePendingBind(ctx, code)
	if err != nil {
		log.Printf("feishu: consume pending bind: %v", err)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 短码无效或已过期")
		return
	}
	if uid != b.UserID {
		// The code belongs to a different atterm user — should not happen
		// because the binding is per-user, but guard.
		log.Printf("feishu: pending bind user mismatch: %s vs %s", uid, b.UserID)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 短码无效或已过期")
		return
	}
	if err := s.cfg.Store.MarkBound(ctx, b.UserID, msg.SenderOpenID); err != nil {
		log.Printf("feishu: mark bound: %v", err)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 服务端错误,请稍后再试")
		return
	}
	s.sendBindReply(ctx, b, msg.SenderOpenID, "✅ 已绑定到 atterm")
}

func (s *Service) sendBindReply(ctx context.Context, b *Binding, openID, text string) {
	tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
	if err != nil {
		log.Printf("feishu: bind reply token: %v", err)
		return
	}
	if err := s.cfg.IM.SendTextToOpenID(ctx, tok, openID, text); err != nil {
		log.Printf("feishu: bind reply: %v", err)
	}
}

// MintTokenForCreds is a thin wrapper around the configured TokenSource;
// used by the relay HTTP handler to validate user-pasted credentials.
func (s *Service) MintTokenForCreds(ctx context.Context, appID, appSecret string) (string, error) {
	return s.cfg.Token.Get(ctx, appID, appSecret)
}

// InvalidateTokenForAppID drops a cached token; called by the HTTP
// handler after DELETE /v1/feishu/bindings/me.
func (s *Service) InvalidateTokenForAppID(appID string) {
	s.cfg.Token.Invalidate(appID)
}
