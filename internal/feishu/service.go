// internal/feishu/service.go
package feishu

import (
	"context"
	"encoding/json"
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
	SendInteractiveToOpenID(ctx context.Context, token, openID string, cardBody []byte) error
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
	cfg          ServiceConfig
	authFailMu   chan struct{} // semaphore-of-1 for the auth-fail counter map
	authFailures map[string]int
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{
		cfg:          cfg,
		authFailMu:   make(chan struct{}, 1),
		authFailures: map[string]int{},
	}
}

// SendCommandFinished spawns a goroutine that renders the card and
// posts it. Always fire-and-forget; any error is logged. Tests should
// call sendCommandFinishedSync to avoid timing races.
func (s *Service) SendCommandFinished(ctx context.Context, userID string, in CommandFinishedInput) {
	go s.sendCommandFinishedSync(ctx, userID, in)
}

func (s *Service) sendCommandFinishedSync(ctx context.Context, userID string, in CommandFinishedInput) {
	s.send(ctx, userID, func() ([]byte, error) {
		card := RenderCommandFinishedCard(in)
		return json.Marshal(card)
	})
}

// SendSessionNotification mirrors SendCommandFinished for the
// waiting_input event. Same fire-and-forget semantics.
func (s *Service) SendSessionNotification(ctx context.Context, userID string, in WaitingInputInput) {
	go s.sendSessionNotificationSync(ctx, userID, in)
}

func (s *Service) sendSessionNotificationSync(ctx context.Context, userID string, in WaitingInputInput) {
	s.send(ctx, userID, func() ([]byte, error) {
		card := RenderWaitingInputCard(in)
		return json.Marshal(card)
	})
}

func (s *Service) send(ctx context.Context, userID string, render func() ([]byte, error)) {
	b, err := s.cfg.Store.GetBindingByUserID(ctx, userID)
	if errors.Is(err, ErrBindingNotFound) {
		return
	}
	if err != nil {
		log.Printf("feishu: lookup binding for %s: %v", userID, err)
		return
	}
	if b.OpenID == "" || b.DisabledAt != 0 {
		return
	}
	cardBody, err := render()
	if err != nil {
		log.Printf("feishu: render card: %v", err)
		return
	}
	tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
	if err != nil {
		s.recordSendError(ctx, b, err)
		return
	}
	if err := s.cfg.IM.SendInteractiveToOpenID(ctx, tok, b.OpenID, cardBody); err != nil {
		s.recordSendError(ctx, b, err)
	}
}

// recordSendError counts consecutive auth-class failures per binding;
// 3-in-a-row → mark disabled.
func (s *Service) recordSendError(ctx context.Context, b *Binding, err error) {
	if !IsAuthClassError(err) {
		log.Printf("feishu: send to %s: %v", b.UserID, err)
		return
	}
	s.authFailMu <- struct{}{}
	s.authFailures[b.UserID]++
	count := s.authFailures[b.UserID]
	<-s.authFailMu
	log.Printf("feishu: auth failure #%d for %s: %v", count, b.UserID, err)
	if count >= 3 {
		if err := s.cfg.Store.MarkDisabled(ctx, b.UserID); err != nil {
			log.Printf("feishu: mark disabled: %v", err)
		}
		s.cfg.Token.Invalidate(b.AppID)
	}
}

// HandleStatus is the high-level result for the HTTP handler.
type HandleStatus int

const (
	HandleStatusOK HandleStatus = iota
)

// HandleResult is what HandleEvent returns; the HTTP handler always
// replies 200 but inspects fields to emit the right body.
type HandleResult struct {
	Status       HandleStatus
	Reason       string       // short tag for logs
	URLChallenge string       // non-empty → reply { "challenge": ... }
	CardUpdate   *AckResponse // non-nil → reply JSON of this object
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
		go s.handleBindMessage(ctx, b, env.Message)
		return &HandleResult{Reason: "im_message_dispatched"}, nil
	case "card.action.trigger":
		if env.CardAction == nil || env.CardAction.Kind != "ack" {
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
		ack := RenderAckUpdateCard(AckUpdateInput{Event: env.CardAction.Event, SessionID: env.CardAction.SessionID})
		return &HandleResult{CardUpdate: &ack, Reason: "card_ack"}, nil
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
