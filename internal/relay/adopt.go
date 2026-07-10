package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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

// ImagePasteHost is implemented by desktop PTY wrappers that can bridge a
// remote browser image paste into the local app running inside the PTY.
type ImagePasteHost interface {
	PasteImage(ctx context.Context, sessionID uuid.UUID, payload proto.PasteImagePayload) error
}

// FilePasteHost is implemented by desktop PTY wrappers that can accept a
// remote client's file attachment: sanitize+dedup the filename, write the
// bytes to a session-scoped inbox, and inject the resulting absolute
// path into the PTY master. Symmetric to ImagePasteHost but for arbitrary
// files (no clipboard bridging).
type FilePasteHost interface {
	PasteFile(ctx context.Context, sessionID uuid.UUID, payload proto.PasteFilePayload) error
}

// AdoptSession registers an in-process PTY as a relay session, bypassing the
// /agent WebSocket entirely. It is the desktop app's hook for surfacing
// locally-spawned PTYs to the same xterm.js code path that handles remote
// sessions.
//
// ownerUserID, when non-empty, marks the session as owned by that user so
// requireSession-gated routes (/client, /api/sessions) will surface and
// permit attach. Empty means "no owner" — only relays without Store wiring
// should adopt anonymously; gated relays will refuse to surface the session.
//
// The returned cleanup must be called exactly once, when the caller has
// decided the session is over (PTY exited, app shutting down, etc.). It is
// idempotent. The PtyHost is NOT closed by cleanup — its lifecycle stays
// with the caller.
func (s *Server) AdoptSession(ctx context.Context, id uuid.UUID, info proto.SessionInfo, host PtyHost, ownerUserID string) func() {
	info.ID = id.String()
	sess := session.New(id, info)
	sess.OwnerUserID = ownerUserID
	sess.SetMetaChangedHook(s.registry.NotifyChange)
	s.registry.Add(sess)
	s.debugf("adopt open session=%s command=%q cwd=%q title=%q host_id=%q host=%q user=%q cols=%d rows=%d",
		id, info.Command, info.Cwd, info.Title, info.HostID, info.Host, info.User, info.Cols, info.Rows)

	loopCtx, cancel := context.WithCancel(ctx)
	var (
		seq       atomic.Uint64
		closeOnce sync.Once
	)

	cleanup := func() {
		closeOnce.Do(func() {
			cancel()
			s.removeSession(id)
			s.debugf("adopt cleanup session=%s", id)
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
				nextSeq := seq.Add(1)
				frame := proto.EncodeOut(id, nextSeq, buf[:n])
				s.debugFrame("adopt", "recv", frame)
				if sess.PushOut(nextSeq, append([]byte(nil), buf[:n]...)) {
					s.registry.NotifyChange()
				}
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
				s.debugFrame("adopt", "recv", proto.Frame{Type: proto.TypeClose, SessionID: id, Payload: closePayload})
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
				s.debugFrame("adopt", "send", f)
				switch f.Type {
				case proto.TypeIn:
					if _, err := host.Write(f.Payload); err != nil {
						return
					}
				case proto.TypeResize:
					if cols, rows, err := proto.DecodeResize(f.Payload); err == nil {
						sess.UpdateSize(cols, rows)
						s.registry.NotifyChange()
						_ = host.Resize(cols, rows)
					}
				case proto.TypePasteImage:
					pasteHost, ok := host.(ImagePasteHost)
					if !ok {
						log.Printf("adopt: paste image unavailable session=%s", f.SessionID)
						continue
					}
					var p proto.PasteImagePayload
					if err := json.Unmarshal(f.Payload, &p); err != nil {
						log.Printf("adopt: bad paste image payload session=%s payload_bytes=%d error=%v", f.SessionID, len(f.Payload), err)
						continue
					}
					if err := pasteHost.PasteImage(loopCtx, f.SessionID, p); err != nil {
						log.Printf("adopt: paste image failed session=%s filename=%q content_type=%q image_bytes=%d error=%v", f.SessionID, p.Filename, p.ContentType, len(p.Data), err)
					}
				case proto.TypePasteFile:
					pasteHost, ok := host.(FilePasteHost)
					if !ok {
						log.Printf("adopt: paste file unavailable session=%s", f.SessionID)
						continue
					}
					var p proto.PasteFilePayload
					if err := json.Unmarshal(f.Payload, &p); err != nil {
						log.Printf("adopt: bad paste file payload session=%s payload_bytes=%d error=%v", f.SessionID, len(f.Payload), err)
						continue
					}
					if err := pasteHost.PasteFile(loopCtx, f.SessionID, p); err != nil {
						log.Printf("adopt: paste file failed session=%s filename=%q content_type=%q bytes=%d error=%v", f.SessionID, p.Filename, p.ContentType, len(p.Data), err)
					}
				}
			}
		}
	}()

	return cleanup
}
