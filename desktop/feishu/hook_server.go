// desktop/feishu/hook_server.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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
	DispatchTurn(sessionID string, ev TurnEvent)
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

	// One INFO line per incoming POST, before validation gates. Volume is low
	// (a handful per AI turn) and the trail is invaluable when diagnosing
	// "card body not updating" — distinguishes "claude never called the hook"
	// from "we got it but dropped it downstream". Keep this BEFORE the
	// sessions.Exists / adapter lookup so we still see misrouted POSTs.
	log.Printf("feishu-hook: arrive sid_raw=%q agent=%q body_len=%d",
		req.SessionID, req.AgentKind, len(req.HookInput))

	sid, err := uuid.Parse(strings.TrimSpace(req.SessionID))
	if err != nil || sid == uuid.Nil {
		http.Error(w, "bad session id", http.StatusBadRequest)
		return
	}
	if h.sessions != nil && !h.sessions.Exists(sid) {
		log.Printf("feishu-hook: drop reason=session-unknown sid=%s", sid)
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	adapter, ok := LookupHookAdapter(req.AgentKind)
	if !ok {
		log.Printf("feishu-hook: drop reason=unknown-agent agent=%q sid=%s", req.AgentKind, sid)
		if h.onSuspect != nil {
			h.onSuspect()
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// WaitingInput path: Parse returns emit=true for AskUserQuestion /
	// permission_prompt / idle_prompt events. emit=false is normal for any
	// hook event type that is not a waiting-on-user signal.
	ev, emit := adapter.Parse(req.HookInput, req.HookVersion)
	if emit {
		log.Printf("feishu-hook: parse=waiting sid=%s dedup=%q", sid, ev.DedupKey)
		if disp := h.dispatcher(); disp != nil {
			disp.DispatchWaitingInput(r.Context(), WaitingInputDispatchEvent{
				SessionID:    sid,
				Source:       WaitingSourceHook,
				QuestionText: ev.QuestionText,
				DedupKey:     ev.DedupKey,
				Options:      ev.Options,
			})
		}
	}

	// AI streaming path: ParseTurn is independent of Parse — a hook event may
	// match the streaming path (UserPromptSubmit, Stop, Pre/PostToolUse) without
	// matching the WaitingInput path, and vice versa. Type-assert to
	// *claudeCodeAdapter since only that adapter has ParseTurn today; unknown
	// adapters fall through silently.
	dispatchedTurn := false
	if ccAdapter, ok := adapter.(*claudeCodeAdapter); ok {
		if turn, hasTurn := ccAdapter.ParseTurn(req.HookInput, req.HookVersion); hasTurn {
			log.Printf("feishu-hook: parse=turn sid=%s kind=%v tool=%q text_len=%d",
				sid, turn.Kind, turn.ToolName, len(turn.Text))
			if disp := h.dispatcher(); disp != nil {
				disp.DispatchTurn(sid.String(), turn)
				dispatchedTurn = true
			}
		}
	}
	if !emit && !dispatchedTurn {
		// Body for diagnosing why claude payload yielded neither path. Truncate
		// so a 100KB notification body doesn't blow up the log line.
		preview := string(req.HookInput)
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		log.Printf("feishu-hook: parse=none sid=%s body=%s", sid, preview)
	}

	_ = errors.New
	w.WriteHeader(http.StatusOK)
}
