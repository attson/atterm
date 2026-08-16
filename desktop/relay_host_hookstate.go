package main

import (
	"github.com/google/uuid"
)

// ApplyHookTaskState satisfies feishu.TaskStateSink: it resolves the session id
// the hook reported against the local registry and hands the state to the
// session model, which owns the latch that switches the silence heuristic off.
//
// Unknown ids are dropped silently. A hook can outlive its session by a moment
// — the CLI exits, the PTY is reaped, a last Stop is still in flight — and that
// is not worth a log line on every AI turn.
func (h *relayHost) ApplyHookTaskState(sid uuid.UUID, state string) {
	if h.server == nil {
		return
	}
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		return
	}
	sess.ApplyHookState(state)
}
