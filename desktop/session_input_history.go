package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

const (
	sessionInputHistoryMaxItems = 200
	sessionInputHistoryMaxBytes = 2 * 1024 * 1024
)

// SessionInputHistoryItem is one executed shell command or submitted AI prompt.
type SessionInputHistoryItem struct {
	Text              string `json:"text"`
	SubmittedAtUnixMS int64  `json:"submitted_at_unix_ms"`
}

// SessionInputHistoryResult is returned newest-first to local and remote UI.
type SessionInputHistoryResult struct {
	Items     []SessionInputHistoryItem `json:"items"`
	Truncated bool                      `json:"truncated"`
}

type sessionInputHistory struct {
	mu        sync.RWMutex
	now       func() time.Time
	items     []SessionInputHistoryItem
	bytes     int
	truncated bool
	onChange  func()
}

func newSessionInputHistory(now func() time.Time) *sessionInputHistory {
	if now == nil {
		now = time.Now
	}
	return &sessionInputHistory{now: now}
}

func (h *sessionInputHistory) Append(text string) {
	if h == nil || text == "" || !utf8.ValidString(text) {
		return
	}
	wasTruncated := false
	if len(text) > sessionInputHistoryMaxBytes {
		text = text[:sessionInputHistoryMaxBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		wasTruncated = true
	}

	h.mu.Lock()
	if wasTruncated {
		h.truncated = true
	}
	h.items = append(h.items, SessionInputHistoryItem{
		Text:              text,
		SubmittedAtUnixMS: h.now().UnixMilli(),
	})
	h.bytes += len(text)
	for len(h.items) > sessionInputHistoryMaxItems || h.bytes > sessionInputHistoryMaxBytes {
		h.bytes -= len(h.items[0].Text)
		h.items = h.items[1:]
		h.truncated = true
	}
	h.mu.Unlock()
	h.changed()
}

// Restore prepends an older persisted generation to the live generation.
// The target may already contain an automatically injected AI resume command,
// so replacement would lose a legitimate new submission.
func (h *sessionInputHistory) Restore(snapshot SessionInputHistoryResult) {
	if h == nil || len(snapshot.Items) == 0 {
		return
	}

	older := make([]SessionInputHistoryItem, 0, len(snapshot.Items))
	for i := len(snapshot.Items) - 1; i >= 0; i-- {
		item := snapshot.Items[i]
		if item.Text == "" || !utf8.ValidString(item.Text) {
			continue
		}
		older = append(older, item)
	}

	h.mu.Lock()
	current := h.items
	h.items = append(older, current...)
	h.bytes = 0
	for _, item := range h.items {
		h.bytes += len(item.Text)
	}
	h.truncated = h.truncated || snapshot.Truncated
	for len(h.items) > sessionInputHistoryMaxItems || h.bytes > sessionInputHistoryMaxBytes {
		h.bytes -= len(h.items[0].Text)
		h.items = h.items[1:]
		h.truncated = true
	}
	h.mu.Unlock()
	h.changed()
}

func (h *sessionInputHistory) setOnChange(fn func()) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.onChange = fn
	h.mu.Unlock()
}

func (h *sessionInputHistory) changed() {
	if h == nil {
		return
	}
	h.mu.RLock()
	fn := h.onChange
	h.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (h *sessionInputHistory) Snapshot() SessionInputHistoryResult {
	if h == nil {
		return SessionInputHistoryResult{Items: []SessionInputHistoryItem{}}
	}
	h.mu.RLock()
	items := make([]SessionInputHistoryItem, len(h.items))
	for i := range h.items {
		items[len(h.items)-1-i] = h.items[i]
	}
	truncated := h.truncated
	h.mu.RUnlock()
	return SessionInputHistoryResult{Items: items, Truncated: truncated}
}

func (h *relayHost) sessionInputHistory(id uuid.UUID) (SessionInputHistoryResult, error) {
	if h == nil {
		return SessionInputHistoryResult{}, errors.New("session history unavailable")
	}
	h.mu.Lock()
	active := h.sessions[id]
	h.mu.Unlock()
	if active == nil {
		return SessionInputHistoryResult{}, errors.New("session not found")
	}
	return active.inputHistory.Snapshot(), nil
}

func (h *relayHost) recordSessionInput(id uuid.UUID, text string) error {
	if h == nil {
		return errors.New("session history unavailable")
	}
	h.mu.Lock()
	active := h.sessions[id]
	h.mu.Unlock()
	if active == nil {
		return errors.New("session not found")
	}
	active.inputHistory.Append(text)
	return nil
}

func (h *relayHost) newSessionInputHistory(id uuid.UUID) *sessionInputHistory {
	history := newSessionInputHistory(time.Now)
	if h == nil || h.inputHistoryStore == nil {
		return history
	}
	history.setOnChange(func() {
		h.inputHistoryStore.Queue(id, history)
	})
	return history
}

func (h *relayHost) restoreSessionInputHistory(previousID, currentID uuid.UUID) error {
	if h == nil || h.inputHistoryStore == nil {
		return nil
	}
	h.mu.Lock()
	active := h.sessions[currentID]
	h.mu.Unlock()
	if active == nil {
		return errors.New("restored session not found")
	}
	return h.restorePersistedInputHistory(previousID, active.inputHistory)
}

func (h *relayHost) restorePersistedInputHistory(previousID uuid.UUID, target *sessionInputHistory) error {
	if h == nil || h.inputHistoryStore == nil || target == nil {
		return nil
	}
	snapshot, found, err := h.inputHistoryStore.Load(previousID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	target.Restore(snapshot)
	return nil
}

// GetSessionInputHistory returns submitted AI prompts for one active local PTY.
func (a *App) GetSessionInputHistory(sessionID string) (SessionInputHistoryResult, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil || id == uuid.Nil {
		return SessionInputHistoryResult{}, errors.New("invalid session id")
	}
	if a == nil || a.host == nil {
		return SessionInputHistoryResult{}, errors.New("session history unavailable")
	}
	return a.host.sessionInputHistory(id)
}

// RestoreSessionInputHistory carries a previous local PTY generation's input
// records into the freshly spawned replacement session.
func (a *App) RestoreSessionInputHistory(previousSessionID, currentSessionID string) error {
	previousID, err := uuid.Parse(previousSessionID)
	if err != nil || previousID == uuid.Nil {
		return errors.New("invalid previous session id")
	}
	currentID, err := uuid.Parse(currentSessionID)
	if err != nil || currentID == uuid.Nil {
		return errors.New("invalid current session id")
	}
	if previousID == currentID {
		return nil
	}
	if a == nil || a.host == nil {
		return errors.New("session history unavailable")
	}
	return a.host.restoreSessionInputHistory(previousID, currentID)
}

func sealSessionInputHistoryResponse(sessionID uuid.UUID, requestID string, snapshot SessionInputHistoryResult, accountKey []byte) (proto.InputHistoryResponsePayload, error) {
	failure := proto.InputHistoryResponsePayload{RequestID: requestID, Error: "history_locked"}
	if requestID == "" {
		return failure, errors.New("missing request id")
	}
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return failure, errors.New("account key is not unlocked")
	}
	items := make([]proto.InputHistoryItem, len(snapshot.Items))
	for i, item := range snapshot.Items {
		items[i] = proto.InputHistoryItem{Text: item.Text, SubmittedAtUnixMS: item.SubmittedAtUnixMS}
	}
	body, err := json.Marshal(proto.SealedInputHistoryResponse{
		RequestID: requestID,
		Items:     items,
		Truncated: snapshot.Truncated,
	})
	if err != nil {
		return failure, fmt.Errorf("marshal input history: %w", err)
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
	if err != nil {
		return failure, fmt.Errorf("derive input history key: %w", err)
	}
	sealed, err := e2eecrypto.SealUnsequenced(
		sessionKey, sessionID, byte(proto.TypeInputHistoryResponse), body,
	)
	if err != nil {
		return failure, fmt.Errorf("seal input history: %w", err)
	}
	return proto.InputHistoryResponsePayload{RequestID: requestID, OK: true, Sealed: sealed}, nil
}

func (h *relayHost) buildInputHistoryResponse(sessionID uuid.UUID, requestID string, accountKey []byte) proto.Frame {
	response := proto.InputHistoryResponsePayload{RequestID: requestID}
	snapshot, err := h.sessionInputHistory(sessionID)
	if err != nil {
		response.Error = "session_unavailable"
	} else if response, err = sealSessionInputHistoryResponse(sessionID, requestID, snapshot, accountKey); err != nil {
		response = proto.InputHistoryResponsePayload{RequestID: requestID, Error: "history_locked"}
	}
	payload, _ := json.Marshal(response)
	return proto.Frame{Type: proto.TypeInputHistoryResponse, SessionID: sessionID, Payload: payload}
}
