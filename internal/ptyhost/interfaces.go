package ptyhost

import (
	"context"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// PtyHost is the minimal contract a same-process PTY must satisfy for a
// relay Server to adopt it as a session. It mirrors the concrete Host
// type's read / write / resize surface as an interface so callers can
// pass any adapter — an actual OS PTY, an SSH channel, a browser
// WebSocket, a test double — without pulling in the whole Host type.
//
// Lives here rather than in internal/relay so the interface can be
// implemented and named by any package (desktop's sshPtyHost /
// desktopPtyHost, the browser bridge, tests) without dragging in a
// relay import just to reference the type.
type PtyHost interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
}

// ImagePasteHost is implemented by PtyHost adapters that can bridge a
// remote client's image paste into the local app running inside the
// PTY. AdoptSession type-asserts on this optional capability, so an
// adapter that doesn't implement it just silently drops PASTE_IMAGE
// frames rather than failing the session.
type ImagePasteHost interface {
	PasteImage(ctx context.Context, sessionID uuid.UUID, payload proto.PasteImagePayload) error
}

// FilePasteHost is implemented by PtyHost adapters that can accept a
// remote client's file attachment: sanitize+dedup the filename, write
// the bytes to a session-scoped inbox, and inject the resulting
// absolute path into the PTY master. Symmetric to ImagePasteHost but
// for arbitrary files (no clipboard bridging). Also optional — an
// adapter without it drops PASTE_FILE frames.
type FilePasteHost interface {
	PasteFile(ctx context.Context, sessionID uuid.UUID, payload proto.PasteFilePayload) error
}
