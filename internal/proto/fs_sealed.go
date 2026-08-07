package proto

import (
	"encoding/json"
	"fmt"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// Sealed field sets for the FS frames. Whole structs go into the
// envelope rather than individual fields: entries[].Name sits beside
// entries[].Size, and splitting them would create index-paired
// plaintext/ciphertext arrays where a mismatch corrupts data silently.
// The relay reads none of these fields, so hiding the metadata
// alongside the names costs nothing.

// SealedFSRequestFields is the sealed segment of a TypeFSRequest.
// Op / RequestID / ClientID stay in the clear because the relay gates
// on them (isReadOnlyFSOperation, open_external driver check).
type SealedFSRequestFields struct {
	Path    string `json:"path,omitempty"`
	NewPath string `json:"new_path,omitempty"`
}

// SealedFSResponseFields is the sealed metadata segment of a
// TypeFSResponse. Content.Data / Chunk.Data are stripped out and ride a
// separate trailing segment as raw bytes.
type SealedFSResponseFields struct {
	Entries []DirEntry      `json:"entries,omitempty"`
	Meta    *FileMetaInfo   `json:"meta,omitempty"`
	Error   string          `json:"error,omitempty"`
	Content *FileContent    `json:"content,omitempty"`
	Chunk   *FSChunkPayload `json:"chunk,omitempty"`
}

// SealedFSEventFields is the sealed segment of a TypeFSEvent.
type SealedFSEventFields struct {
	Path string `json:"path,omitempty"`
}

// EncodeFSRequest seals path/new_path when sessionKey is non-empty. A
// nil key means the sender is keyless and emits single-segment
// plaintext — the "no key = no encryption" rule the OUT/META paths
// already follow.
func EncodeFSRequest(p FSRequestPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}
	sealedBody, err := json.Marshal(SealedFSRequestFields{Path: p.Path, NewPath: p.NewPath})
	if err != nil {
		return nil, err
	}
	env, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSRequest), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs request: %w", err)
	}
	// Red line #23: zero the plaintext copy of everything sealed.
	p.Path = ""
	p.NewPath = ""
	head, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return EncodeSegments([][]byte{head, env})
}

// DecodeFSRequest accepts both shapes: a single plaintext segment from a
// keyless peer, or a head plus sealed segment.
func DecodeFSRequest(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSRequestPayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSRequestPayload{}, err
	}
	var out FSRequestPayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSRequestPayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSRequestPayload{}, fmt.Errorf("fs request is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSRequest), segs[1])
	if err != nil {
		return FSRequestPayload{}, fmt.Errorf("open fs request: %w", err)
	}
	var fields SealedFSRequestFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSRequestPayload{}, err
	}
	out.Path = fields.Path
	out.NewPath = fields.NewPath
	return out, nil
}

// EncodeFSResponse produces up to three segments: the routing head, the
// sealed metadata, and — when the response carries file bytes — those
// bytes sealed on their own so they never pass through JSON.
func EncodeFSResponse(p FSResponsePayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}

	var raw []byte
	fields := SealedFSResponseFields{
		Entries: p.Entries,
		Meta:    p.Meta,
		Error:   p.Error,
	}
	if p.Content != nil {
		clone := *p.Content
		raw = clone.Data
		clone.Data = nil
		fields.Content = &clone
	}
	if p.Chunk != nil {
		clone := *p.Chunk
		raw = clone.Data
		clone.Data = nil
		fields.Chunk = &clone
	}
	sealedBody, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	metaEnv, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs response: %w", err)
	}

	head := FSResponsePayload{RequestID: p.RequestID, OK: p.OK, WatchID: p.WatchID}
	headBody, err := json.Marshal(head)
	if err != nil {
		return nil, err
	}
	segments := [][]byte{headBody, metaEnv}
	if raw != nil {
		contentEnv, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), raw)
		if err != nil {
			return nil, fmt.Errorf("seal fs content: %w", err)
		}
		segments = append(segments, contentEnv)
	}
	return EncodeSegments(segments)
}

// DecodeFSResponse reattaches the trailing raw segment to whichever of
// Content / Chunk the metadata segment carried.
func DecodeFSResponse(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSResponsePayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSResponsePayload{}, err
	}
	var out FSResponsePayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSResponsePayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSResponsePayload{}, fmt.Errorf("fs response is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), segs[1])
	if err != nil {
		return FSResponsePayload{}, fmt.Errorf("open fs response: %w", err)
	}
	var fields SealedFSResponseFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSResponsePayload{}, err
	}
	out.Entries = fields.Entries
	out.Meta = fields.Meta
	out.Error = fields.Error
	out.Content = fields.Content
	out.Chunk = fields.Chunk
	if len(segs) > 2 {
		raw, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSResponse), segs[2])
		if err != nil {
			return FSResponsePayload{}, fmt.Errorf("open fs content: %w", err)
		}
		switch {
		case out.Content != nil:
			out.Content.Data = raw
		case out.Chunk != nil:
			out.Chunk.Data = raw
		}
	}
	return out, nil
}

// EncodeFSEvent seals the watched path; watch_id stays in the clear so
// the relay can route the event to the subscriber that registered it.
func EncodeFSEvent(p FSEventPayload, sessionKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(sessionKey) == 0 {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return EncodeSegments([][]byte{body})
	}
	sealedBody, err := json.Marshal(SealedFSEventFields{Path: p.Path})
	if err != nil {
		return nil, err
	}
	env, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(TypeFSEvent), sealedBody)
	if err != nil {
		return nil, fmt.Errorf("seal fs event: %w", err)
	}
	p.Path = ""
	head, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return EncodeSegments([][]byte{head, env})
}

// DecodeFSEvent is the inverse of EncodeFSEvent.
func DecodeFSEvent(payload []byte, sessionKey []byte, sessionID uuid.UUID) (FSEventPayload, error) {
	segs, err := DecodeSegments(payload)
	if err != nil {
		return FSEventPayload{}, err
	}
	var out FSEventPayload
	if err := json.Unmarshal(segs[0], &out); err != nil {
		return FSEventPayload{}, err
	}
	if len(segs) == 1 {
		return out, nil
	}
	if len(sessionKey) == 0 {
		return FSEventPayload{}, fmt.Errorf("fs event is sealed but no session key is available")
	}
	body, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(TypeFSEvent), segs[1])
	if err != nil {
		return FSEventPayload{}, fmt.Errorf("open fs event: %w", err)
	}
	var fields SealedFSEventFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return FSEventPayload{}, err
	}
	out.Path = fields.Path
	return out, nil
}
