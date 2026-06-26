// desktop/feishu/hook_server.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const maxHookBody = 128 * 1024

// SessionLookup is the subset of session bookkeeping the hook server needs.
type SessionLookup interface {
	Exists(sid uuid.UUID) bool
	// Inject writes text into the session's PTY as if typed by the user.
	Inject(sid uuid.UUID, text string) error
}

// WaitingDispatcher is the subset of *Dispatcher the hook server uses.
type WaitingDispatcher interface {
	DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent)
}

// HookServer terminates POSTs from the atterm-hook CLI.
//
// disp is swappable (guarded by mu) so the listener can outlive a Feishu
// service rebuild: when the relay login state changes the service is
// reconstructed, but the same HookServer keeps running on the same port —
// otherwise the ATTERM_HOOK_ENDPOINT baked into already-spawned PTYs would go
// stale. Only the dispatcher it forwards to is replaced via SetDispatcher.
type HookServer struct {
	mu        sync.RWMutex
	disp      WaitingDispatcher
	sessions  SessionLookup
	onSuspect func()
}

type hookNotifyRequest struct {
	SessionID   string          `json:"session_id"`
	AgentKind   string          `json:"agent_kind"`
	HookInput   json.RawMessage `json:"hook_input"`
	HookVersion string          `json:"hook_version,omitempty"`
}

func NewHookServer(disp WaitingDispatcher, sessions SessionLookup) *HookServer {
	return &HookServer{disp: disp, sessions: sessions}
}

// SetSuspectCallback registers a callback invoked when a POST is well-
// formed but the agent_kind isn't recognized, signaling that the
// installed hook is mis-wired (e.g. a stale binary, broken adapter).
// Safe to call before or after Start.
func (h *HookServer) SetSuspectCallback(fn func()) {
	h.onSuspect = fn
}

// SetDispatcher swaps the dispatcher the server forwards hook events to.
// Safe to call concurrently with serving (e.g. on a relay login/logout that
// rebuilds the Feishu service). A nil dispatcher makes notifies a no-op.
func (h *HookServer) SetDispatcher(d WaitingDispatcher) {
	h.mu.Lock()
	h.disp = d
	h.mu.Unlock()
}

func (h *HookServer) dispatcher() WaitingDispatcher {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.disp
}

// Start binds a localhost listener on 127.0.0.1:0 and returns the chosen
// address + the *http.Server so the caller can Shutdown.
func (h *HookServer) Start() (addr string, srv *http.Server, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/atterm-hook/notify", h)
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	return ln.Addr().String(), server, nil
}

func (h *HookServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "127.0.0.1" && host != "::1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxHookBody)+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxHookBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req hookNotifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	sid, err := uuid.Parse(strings.TrimSpace(req.SessionID))
	if err != nil || sid == uuid.Nil {
		http.Error(w, "bad session id", http.StatusBadRequest)
		return
	}
	if h.sessions != nil && !h.sessions.Exists(sid) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	adapter, ok := LookupHookAdapter(req.AgentKind)
	if !ok {
		if h.onSuspect != nil {
			h.onSuspect()
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	ev, emit := adapter.Parse(req.HookInput, req.HookVersion)
	if !emit {
		// emit=false is a normal "this event isn't a question we route"
		// signal (e.g. unrelated matcher type). Intentionally NOT
		// invoking onSuspect here — that callback exists for "the hook
		// is mis-wired" signals (unknown agent_kind), not for routine
		// per-event filtering.
		w.WriteHeader(http.StatusOK)
		return
	}

	if disp := h.dispatcher(); disp != nil {
		disp.DispatchWaitingInput(r.Context(), WaitingInputDispatchEvent{
			SessionID:    sid,
			Source:       WaitingSourceHook,
			QuestionText: ev.QuestionText,
			DedupKey:     ev.DedupKey,
			Options:      ev.Options,
		})
	}

	_ = errors.New
	w.WriteHeader(http.StatusOK)
}
