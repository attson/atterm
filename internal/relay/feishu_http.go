// internal/relay/feishu_http.go
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// FeishuHTTPHandler exposes /v1/feishu/* routes.
type FeishuHTTPHandler struct {
	store *userstore.SQLiteStore
	svc   *feishu.Service
}

func NewFeishuHTTPHandler(store *userstore.SQLiteStore, svc *feishu.Service) *FeishuHTTPHandler {
	return &FeishuHTTPHandler{store: store, svc: svc}
}

// ServeHTTPSession is the handler registered behind requireSession.
// It dispatches the five authenticated routes (bindings + relay-token).
func (h *FeishuHTTPHandler) ServeHTTPSession(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/feishu/bindings/me" && r.Method == http.MethodGet:
		h.handleGetMe(w, r)
	case r.URL.Path == "/v1/feishu/bindings/me" && r.Method == http.MethodPost:
		h.handleUpsert(w, r)
	case r.URL.Path == "/v1/feishu/bindings/me" && r.Method == http.MethodDelete:
		h.handleDelete(w, r)
	case r.URL.Path == "/v1/feishu/bindings/me/begin-pair" && r.Method == http.MethodPost:
		h.handleBeginPair(w, r)
	case r.URL.Path == "/v1/feishu/relay-token/me" && r.Method == http.MethodPost:
		h.handleRelayToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

// ServeHTTPEvents handles the unauthenticated event callback.
func (h *FeishuHTTPHandler) ServeHTTPEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/v1/feishu/events/")
	hash = strings.TrimSuffix(hash, "/")
	if hash == "" || strings.Contains(hash, "/") {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		// Even read errors must 200 — log only.
		w.WriteHeader(http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
	defer cancel()
	resp, _ := h.svc.HandleEvent(ctx, hash, body)
	if resp == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	if resp.URLChallenge != "" {
		writeJSONStatus(w, http.StatusOK, map[string]string{"challenge": resp.URLChallenge})
		return
	}
	if resp.CardUpdate != nil {
		writeJSONStatus(w, http.StatusOK, resp.CardUpdate)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *FeishuHTTPHandler) callbackURL(r *http.Request, hash string) string {
	return publicBaseURL(r) + "/v1/feishu/events/" + hash
}

func currentUserID(r *http.Request) (string, bool) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil || u.ID == "" {
		return "", false
	}
	return u.ID, true
}

func (h *FeishuHTTPHandler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	b, err := h.store.GetFeishuBinding(r.Context(), uid)
	if errors.Is(err, userstore.ErrFeishuBindingNotFound) {
		writeJSONStatus(w, http.StatusOK, map[string]any{"configured": false, "bound": false})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"configured":   true,
		"bound":        b.OpenID != "",
		"open_id":      b.OpenID,
		"disabled_at":  b.DisabledAt,
		"callback_url": h.callbackURL(r, b.AppIDHash),
	})
}

type feishuUpsertReq struct {
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	EncryptKey  string `json:"encrypt_key"`
	VerifyToken string `json:"verify_token"`
}

func (h *FeishuHTTPHandler) handleUpsert(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var in feishuUpsertReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if in.AppID == "" || in.AppSecret == "" || in.EncryptKey == "" || in.VerifyToken == "" {
		http.Error(w, "all four fields are required", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.MintTokenForCreds(r.Context(), in.AppID, in.AppSecret); err != nil {
		if feishu.IsAuthClassError(err) {
			http.Error(w, "feishu rejected app_id/app_secret: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "feishu unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := h.store.UpsertFeishuBinding(r.Context(), uid, userstore.FeishuBindingCredentials{
		AppID: in.AppID, AppSecret: in.AppSecret, EncryptKey: in.EncryptKey, VerifyToken: in.VerifyToken,
	}); err != nil {
		if errors.Is(err, userstore.ErrFeishuAppIDConflict) {
			http.Error(w, "this Feishu app is already bound to another atterm user", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	full, err := h.store.GetFeishuBinding(r.Context(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"app_id_hash":  full.AppIDHash,
		"callback_url": h.callbackURL(r, full.AppIDHash),
	})
}

func (h *FeishuHTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if b, err := h.store.GetFeishuBinding(r.Context(), uid); err == nil {
		h.svc.InvalidateTokenForAppID(b.AppID)
	}
	if err := h.store.DeleteFeishuBinding(r.Context(), uid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FeishuHTTPHandler) handleBeginPair(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := h.store.GetFeishuBinding(r.Context(), uid); err != nil {
		if errors.Is(err, userstore.ErrFeishuBindingNotFound) {
			http.Error(w, "configure credentials first", http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code := userstore.GenerateFeishuPairCode()
	expires := time.Now().Add(15 * time.Minute).Unix()
	if err := h.store.PutFeishuPendingBind(r.Context(), uid, code, expires); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"code":       code,
		"expires_at": expires,
	})
}

// handleRelayToken hands the caller a fresh tenant_access_token plus
// the bound open_id + app_id_hash. Used by the desktop in relay-backed
// mode to send Feishu IM messages directly without round-tripping
// payloads through the relay.
func (h *FeishuHTTPHandler) handleRelayToken(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tok, openID, hash, err := h.svc.RelayToken(r.Context(), uid)
	if errors.Is(err, feishu.ErrBindingNotFound) {
		http.Error(w, "feishu binding not configured", http.StatusNotFound)
		return
	}
	if errors.Is(err, feishu.ErrBindingDisabled) {
		http.Error(w, "feishu binding disabled — re-configure", http.StatusGone)
		return
	}
	if err != nil {
		http.Error(w, "feishu upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"tenant_access_token": tok,
		"open_id":             openID,
		"app_id_hash":         hash,
		"expires_in":          3000,
	})
}
