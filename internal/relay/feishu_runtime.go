package relay

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// feishuRuntime is the swappable Feishu handler holder. mu serializes
// enable/disable transitions; handler is loaded lock-free on the hot path.
type feishuRuntime struct {
	mu      sync.Mutex
	handler atomic.Pointer[FeishuHTTPHandler]
}

// serveFeishuSession / serveFeishuEvents are the stable route handlers that
// gate on the runtime Feishu handler. When disabled, session routes return
// 503 and the (unauthenticated) events route returns 404 — close to the
// pre-registration "not mounted → 404" behavior.
func (s *Server) serveFeishuSession(w http.ResponseWriter, r *http.Request) {
	h := s.feishu.handler.Load()
	if h == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "feishu integration disabled"})
		return
	}
	h.ServeHTTPSession(w, r)
}

func (s *Server) serveFeishuEvents(w http.ResponseWriter, r *http.Request) {
	h := s.feishu.handler.Load()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h.ServeHTTPEvents(w, r)
}

// FeishuEnabled reports whether a Feishu handler is currently attached.
func (s *Server) FeishuEnabled() bool { return s.feishu.handler.Load() != nil }

// ApplyFeishuConfig hot-applies a Feishu integration state without a restart.
// When enabling, key must be 32 bytes: it sets the store's field-encryption
// cipher and attaches a fresh handler. When disabling, it detaches the
// handler first, then clears the cipher (so in-flight requests that already
// snapshotted the cipher finish safely). Requires a *userstore.DBStore.
func (s *Server) ApplyFeishuConfig(enabled bool, key []byte, baseURL string) error {
	store, ok := s.cfg.Store.(*userstore.DBStore)
	if !ok {
		return errors.New("feishu requires a SQLite store")
	}
	s.feishu.mu.Lock()
	defer s.feishu.mu.Unlock()
	if !enabled {
		s.feishu.handler.Store(nil)
		store.SetSecretCipher(nil)
		return nil
	}
	cipher, err := userstore.NewSecretCipher(key)
	if err != nil {
		return err
	}
	store.SetSecretCipher(cipher)
	s.feishu.handler.Store(NewFeishuHTTPHandler(store, buildFeishuService(store, baseURL), s.registry))
	return nil
}

// buildFeishuService constructs a Feishu service bound to store. Mirrors the
// startup wiring so both the bootstrap and the admin hot-apply path share one
// definition. An empty baseURL falls back to Feishu's public endpoint.
func buildFeishuService(store *userstore.DBStore, baseURL string) *feishu.Service {
	if baseURL == "" {
		baseURL = "https://open.feishu.cn"
	}
	httpc := &http.Client{Timeout: 10 * time.Second}
	return feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(store),
		IM:    feishu.NewClient(baseURL, httpc),
		Token: feishu.NewTenantTokenCache(baseURL, httpc, time.Now),
	})
}
