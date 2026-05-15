package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	headerLen    = 6  // ver(1) + type(1) + payload_len(4 BE)
	sessionIDLen = 16 // uuid bytes
	minFrameLen  = headerLen + sessionIDLen
	maxPayload   = 16 * 1024 * 1024 // 16 MiB hard cap to prevent abuse
)

var (
	ErrShort      = errors.New("proto: frame too short")
	ErrVersion    = errors.New("proto: unsupported version")
	ErrTooLarge   = errors.New("proto: payload exceeds limit")
	ErrPayloadLen = errors.New("proto: payload length mismatch")
)

// Marshal encodes a frame into the wire format.
func Marshal(f Frame) []byte {
	buf := make([]byte, minFrameLen+len(f.Payload))
	buf[0] = Version
	buf[1] = byte(f.Type)
	binary.BigEndian.PutUint32(buf[2:6], uint32(len(f.Payload)))
	copy(buf[headerLen:headerLen+sessionIDLen], f.SessionID[:])
	copy(buf[minFrameLen:], f.Payload)
	return buf
}

// Unmarshal decodes a single frame from a complete WS binary message.
func Unmarshal(data []byte) (Frame, error) {
	if len(data) < minFrameLen {
		return Frame{}, ErrShort
	}
	if data[0] != Version {
		return Frame{}, fmt.Errorf("%w: got %d want %d", ErrVersion, data[0], Version)
	}
	plen := binary.BigEndian.Uint32(data[2:6])
	if plen > maxPayload {
		return Frame{}, ErrTooLarge
	}
	if int(plen) != len(data)-minFrameLen {
		return Frame{}, ErrPayloadLen
	}
	var sid uuid.UUID
	copy(sid[:], data[headerLen:headerLen+sessionIDLen])
	payload := make([]byte, plen)
	copy(payload, data[minFrameLen:])
	return Frame{
		Type:      Type(data[1]),
		SessionID: sid,
		Payload:   payload,
	}, nil
}

// EncodeOut builds a TypeOut frame with a u64 BE seq prefix.
func EncodeOut(sessionID uuid.UUID, seq uint64, data []byte) Frame {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(payload[:8], seq)
	copy(payload[8:], data)
	return Frame{Type: TypeOut, SessionID: sessionID, Payload: payload}
}

// DecodeOut returns (seq, data) from a TypeOut frame's payload.
func DecodeOut(payload []byte) (uint64, []byte, error) {
	if len(payload) < 8 {
		return 0, nil, ErrShort
	}
	return binary.BigEndian.Uint64(payload[:8]), payload[8:], nil
}

// EncodeResize builds a TypeResize payload (cols u16 BE | rows u16 BE).
func EncodeResize(cols, rows uint16) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], cols)
	binary.BigEndian.PutUint16(b[2:4], rows)
	return b
}

// DecodeResize extracts cols/rows from a TypeResize payload.
func DecodeResize(payload []byte) (uint16, uint16, error) {
	if len(payload) < 4 {
		return 0, 0, ErrShort
	}
	return binary.BigEndian.Uint16(payload[0:2]), binary.BigEndian.Uint16(payload[2:4]), nil
}

// CommandEventPayload is the JSON body of a TypeCommandEvent frame.
// Direction: uplink -> relay. Not forwarded to clients.
type CommandEventPayload struct {
	ExitCode  int    `json:"exit_code"`
	ElapsedMS int    `json:"elapsed_ms"`
	Label     string `json:"label,omitempty"`
}

// EncodeCommandEvent builds a TypeCommandEvent frame.
func EncodeCommandEvent(sessionID uuid.UUID, payload CommandEventPayload) (Frame, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Frame{}, fmt.Errorf("marshal command event: %w", err)
	}
	return Frame{Type: TypeCommandEvent, SessionID: sessionID, Payload: body}, nil
}

// DecodeCommandEvent extracts the JSON payload from a TypeCommandEvent frame.
func DecodeCommandEvent(f Frame) (CommandEventPayload, error) {
	if f.Type != TypeCommandEvent {
		return CommandEventPayload{}, fmt.Errorf("not a TypeCommandEvent frame: %v", f.Type)
	}
	var out CommandEventPayload
	if err := json.Unmarshal(f.Payload, &out); err != nil {
		return CommandEventPayload{}, fmt.Errorf("unmarshal command event: %w", err)
	}
	return out, nil
}
