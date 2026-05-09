package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// PtyHost is the minimal contract a same-process PTY must satisfy for the
// relay to adopt it as a session. It mirrors internal/ptyhost.Host's read /
// write / resize methods without dragging in that import to keep this layer
// reusable from any caller.
type PtyHost interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
}

// AdoptSession registers an in-process PTY as a relay session, bypassing the
// /agent WebSocket entirely. It is the desktop app's hook for surfacing
// locally-spawned PTYs to the same xterm.js code path that handles remote
// sessions.
//
// The returned cleanup must be called exactly once, when the caller has
// decided the session is over (PTY exited, app shutting down, etc.). It is
// idempotent. The PtyHost is NOT closed by cleanup — its lifecycle stays
// with the caller.
func (s *Server) AdoptSession(ctx context.Context, id uuid.UUID, info proto.SessionInfo, host PtyHost) func() {
	info.ID = id.String()
	sess := session.New(id, info)
	s.registry.Add(sess)

	loopCtx, cancel := context.WithCancel(ctx)
	var (
		seq       atomic.Uint64
		closeOnce sync.Once
	)

	cleanup := func() {
		closeOnce.Do(func() {
			cancel()
			s.registry.Remove(id)
		})
	}

	// PTY → session (out frames). Exits when host.Read returns an error
	// (typically io.EOF after the child process exits).
	go func() {
		buf := make([]byte, 4096)
		for {
			if loopCtx.Err() != nil {
				return
			}
			n, err := host.Read(buf)
			if n > 0 {
				sess.PushOut(seq.Add(1), append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && loopCtx.Err() == nil {
					// Read failed before cleanup — likely PTY torn down.
					// Fall through to broadcast CLOSE and clean up.
				}
				closePayload, _ := json.Marshal(proto.ClosePayload{
					ExitCode: 0,
					Reason:   err.Error(),
				})
				sess.Broadcast(proto.Frame{
					Type:      proto.TypeClose,
					SessionID: id,
					Payload:   closePayload,
				})
				cleanup()
				return
			}
		}
	}()

	// session inbound → PTY. Drives client-originated IN/RESIZE.
	go func() {
		for {
			select {
			case <-loopCtx.Done():
				return
			case f, ok := <-sess.Inbound():
				if !ok {
					return
				}
				switch f.Type {
				case proto.TypeIn:
					if _, err := host.Write(f.Payload); err != nil {
						return
					}
				case proto.TypeResize:
					if cols, rows, err := proto.DecodeResize(f.Payload); err == nil {
						_ = host.Resize(cols, rows)
					}
				}
			}
		}
	}()

	return cleanup
}
