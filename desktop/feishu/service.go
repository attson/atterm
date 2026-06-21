package feishu

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
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
	longConn   *LongConn
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
		Store: store,
		Token: ts,
		IM:    im,
		Now:   cfg.Now,
	})

	sessions := cfg.Sessions
	if sessions == nil {
		sessions = noOpSessionLookup{}
	}
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

// EnsureLongConn starts the long-conn lazily once credentials exist.
// No-op in relay mode.
func (s *Service) EnsureLongConn(ctx context.Context) error {
	if s.cfg.Mode != ModeLocal {
		return nil
	}
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
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID string) {
			s.handleCardAction(ctx, sessionID, kind, event)
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
		return
	}
	_ = s.store.SetBound(ctx, senderOpenID)
}

func (s *Service) handleCardAction(ctx context.Context, sessionID, kind, event string) {
	_ = sessionID
	_ = kind
	_ = event
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

func (noOpSessionLookup) Exists(uuid.UUID) bool { return true }

// authClassAdaptingClient promotes internal/feishu.AuthClassError to
// satisfy the desktop dispatcher's IsFeishuAuthClassError contract.
type authClassAdaptingClient struct {
	inner *internalfeishu.Client
}

func (c *authClassAdaptingClient) SendInteractiveToOpenID(ctx context.Context, tok, open string, body []byte) error {
	return c.adapt(c.inner.SendInteractiveToOpenID(ctx, tok, open, body))
}
func (c *authClassAdaptingClient) SendTextToOpenID(ctx context.Context, tok, open, text string) error {
	return c.adapt(c.inner.SendTextToOpenID(ctx, tok, open, text))
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
