package relay

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
)

const (
	DebugScopeAll           = "*"
	DebugScopeStateSync     = "state-sync"
	DebugScopeConnection    = "connection"
	DebugScopePrefsSync     = "prefs-sync"
	DebugScopeTerminalIO    = "terminal-io"
	DebugScopeNotifications = "notifications"
	DebugScopeAuth          = "auth"
	DebugScopePeer          = "peer"
	DebugScopePayload       = "payload"
)

var knownDebugScopes = map[string]struct{}{
	DebugScopeAll: {}, DebugScopeStateSync: {}, DebugScopeConnection: {},
	DebugScopePrefsSync: {}, DebugScopeTerminalIO: {}, DebugScopeNotifications: {},
	DebugScopeAuth: {}, DebugScopePeer: {}, DebugScopePayload: {},
}

type debugRuntimeConfig struct {
	enabled   bool
	scopes    map[string]struct{}
	untilUnix int64
}

func normalizeDebugScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if _, ok := knownDebugScopes[scope]; !ok {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func validateDebugScopes(scopes []string) ([]string, error) {
	for _, scope := range scopes {
		normalized := strings.ToLower(strings.TrimSpace(scope))
		if _, ok := knownDebugScopes[normalized]; !ok {
			return nil, fmt.Errorf("unknown debug scope %q", scope)
		}
	}
	return normalizeDebugScopes(scopes), nil
}

func containsDebugScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if strings.EqualFold(strings.TrimSpace(scope), want) {
			return true
		}
	}
	return false
}

func (s *Server) debugOn() bool {
	cfg := s.debugConfig.Load()
	return cfg != nil && cfg.enabled && (cfg.untilUnix == 0 || time.Now().Unix() < cfg.untilUnix)
}

func (s *Server) debugPayloadOn() bool { return s.debugScopeOn(DebugScopePayload) }

func (s *Server) debugScopeOn(scope string) bool {
	cfg := s.debugConfig.Load()
	if cfg == nil || !cfg.enabled || (cfg.untilUnix > 0 && time.Now().Unix() >= cfg.untilUnix) {
		return false
	}
	if _, ok := cfg.scopes[scope]; ok {
		return true
	}
	if scope == DebugScopePayload {
		return false
	}
	_, all := cfg.scopes[DebugScopeAll]
	return all
}

// SetDebug preserves the pre-scope API for tests and old embedders.
func (s *Server) SetDebug(debug, payload bool) {
	scopes := []string{DebugScopeAll}
	if payload {
		scopes = append(scopes, DebugScopePayload)
	}
	s.SetDebugScopes(debug || payload, scopes, "")
}

// SetDebugScopes hot-swaps debug generation without restarting the relay.
func (s *Server) SetDebugScopes(enabled bool, scopes []string, until string) {
	normalized := normalizeDebugScopes(scopes)
	if enabled && len(normalized) == 0 {
		// Legacy debug=true meant all non-payload verbose records.
		normalized = []string{DebugScopeAll}
	}
	set := make(map[string]struct{}, len(normalized))
	for _, scope := range normalized {
		set[scope] = struct{}{}
	}
	var untilUnix int64
	if parsed, err := time.Parse(time.RFC3339, until); err == nil {
		untilUnix = parsed.Unix()
	}
	s.debugConfig.Store(&debugRuntimeConfig{enabled: enabled, scopes: set, untilUnix: untilUnix})
}

// debugf writes through EmitForced rather than logging.Debug: debugOn() is
// already the gate, and it is an explicit operator switch in the admin UI.
// Making it also depend on the process log level would mean flipping "debug"
// on and seeing nothing.
func (s *Server) debugScopef(scope, format string, args ...any) {
	if !s.debugScopeOn(scope) {
		return
	}
	logging.EmitForced(logging.LevelDebug, "relay-debug/"+scope, fmt.Sprintf(format, args...))
}

func (s *Server) debugf(format string, args ...any) {
	s.debugScopef(DebugScopeConnection, format, args...)
}

func (s *Server) debugFrame(component, direction string, f proto.Frame) {
	scope := debugFrameScope(f.Type)
	if s.debugScopeOn(scope) {
		s.debugScopef(scope, "%s %s %s session=%s%s",
			component, direction, frameTypeName(f.Type), f.SessionID, s.debugFrameDetails(f, false))
	}
	if s.debugScopeOn(DebugScopePayload) {
		if details := s.debugFrameDetails(f, true); details != "" {
			s.debugScopef(DebugScopePayload, "%s %s %s session=%s%s",
				component, direction, frameTypeName(f.Type), f.SessionID, details)
		}
	}
}

func debugFrameScope(t proto.Type) string {
	switch t {
	case proto.TypeIn, proto.TypeOut, proto.TypeResize, proto.TypeReplayProgress,
		proto.TypeStreamRequest, proto.TypeStreamStop, proto.TypePasteImage,
		proto.TypePasteFile, proto.TypeFSRequest, proto.TypeFSResponse, proto.TypeFSEvent:
		return DebugScopeTerminalIO
	case proto.TypeMeta, proto.TypeList, proto.TypeListResp, proto.TypeAnnounce, proto.TypeViewers:
		return DebugScopeStateSync
	case proto.TypePrefsChanged:
		return DebugScopePrefsSync
	case proto.TypeCommandEvent:
		return DebugScopeNotifications
	case proto.TypeAuthInfo:
		return DebugScopeAuth
	default:
		return DebugScopeConnection
	}
}

func (s *Server) debugFrameDetails(f proto.Frame, includePayload bool) string {
	switch f.Type {
	case proto.TypeOpen:
		var p proto.OpenPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		if includePayload {
			return fmt.Sprintf(" command=%q cwd=%q title=%q host_id=%q host=%q user=%q cols=%d rows=%d",
				p.Command, p.Cwd, p.Title, p.HostID, p.Host, p.User, p.Cols, p.Rows)
		}
		return fmt.Sprintf(" cols=%d rows=%d payload_bytes=%d", p.Cols, p.Rows, len(f.Payload))
	case proto.TypeIn:
		return s.debugBytes("bytes", f.Payload, includePayload)
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" seq=%d%s", seq, s.debugBytes("bytes", data, includePayload))
	case proto.TypeResize:
		cols, rows, err := proto.DecodeResize(f.Payload)
		if err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" cols=%d rows=%d", cols, rows)
	case proto.TypeMeta:
		var p proto.MetaPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		if includePayload {
			return fmt.Sprintf(" cwd=%q title=%q", p.Cwd, p.Title)
		}
		return fmt.Sprintf(" payload_bytes=%d", len(f.Payload))
	case proto.TypeClose:
		var p proto.ClosePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		if includePayload {
			return fmt.Sprintf(" exit_code=%d reason=%q", p.ExitCode, p.Reason)
		}
		return fmt.Sprintf(" exit_code=%d payload_bytes=%d", p.ExitCode, len(f.Payload))
	case proto.TypeAttach:
		var p proto.AttachPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" attach_session=%s since_seq=%d", p.SessionID, p.SinceSeq)
	case proto.TypeListResp:
		var sessions []proto.SessionInfo
		if err := json.Unmarshal(f.Payload, &sessions); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" sessions=%d", len(sessions))
	case proto.TypeAnnounce:
		var p proto.AnnouncePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		if includePayload {
			return fmt.Sprintf(" host_id=%q host=%q user=%q sessions=%d", p.HostID, p.Host, p.User, len(p.Sessions))
		}
		return fmt.Sprintf(" sessions=%d payload_bytes=%d", len(p.Sessions), len(f.Payload))
	case proto.TypeStreamRequest:
		var p proto.StreamRequestPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" stream_session=%s since_seq=%d", p.SessionID, p.SinceSeq)
	case proto.TypeStreamStop:
		var p proto.StreamStopPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" stream_session=%s", p.SessionID)
	case proto.TypePasteImage:
		var p proto.PasteImagePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		if includePayload {
			return fmt.Sprintf(" filename=%q content_type=%q image_bytes=%d", p.Filename, p.ContentType, len(p.Data))
		}
		return fmt.Sprintf(" content_type=%q image_bytes=%d", p.ContentType, len(p.Data))
	default:
		if includePayload {
			return ""
		}
		return fmt.Sprintf(" payload_bytes=%d", len(f.Payload))
	}
}

func (s *Server) debugBytes(label string, data []byte, includePayload bool) string {
	out := fmt.Sprintf(" %s=%d", label, len(data))
	if includePayload {
		out += fmt.Sprintf(" payload=%q", data)
	}
	return out
}

func frameTypeName(t proto.Type) string {
	switch t {
	case proto.TypeOpen:
		return "OPEN"
	case proto.TypeIn:
		return "IN"
	case proto.TypeOut:
		return "OUT"
	case proto.TypeResize:
		return "RESIZE"
	case proto.TypeMeta:
		return "META"
	case proto.TypeClose:
		return "CLOSE"
	case proto.TypeAttach:
		return "ATTACH"
	case proto.TypeList:
		return "LIST"
	case proto.TypeListResp:
		return "LIST_RESP"
	case proto.TypeReplayProgress:
		return "REPLAY_PROGRESS"
	case proto.TypePrefsChanged:
		return "PREFS_CHANGED"
	case proto.TypePing:
		return "PING"
	case proto.TypePong:
		return "PONG"
	case proto.TypeAnnounce:
		return "ANNOUNCE"
	case proto.TypeStreamRequest:
		return "STREAM_REQUEST"
	case proto.TypeStreamStop:
		return "STREAM_STOP"
	case proto.TypePasteImage:
		return "PASTE_IMAGE"
	case proto.TypeClaimDriver:
		return "CLAIM_DRIVER"
	case proto.TypeCommandEvent:
		return "COMMAND_EVENT"
	case proto.TypeViewers:
		return "VIEWERS"
	case proto.TypePasteFile:
		return "PASTE_FILE"
	case proto.TypeFSRequest:
		return "FS_REQUEST"
	case proto.TypeFSResponse:
		return "FS_RESPONSE"
	case proto.TypeFSEvent:
		return "FS_EVENT"
	case proto.TypeSessionCreate:
		return "SESSION_CREATE"
	case proto.TypeSessionCreated:
		return "SESSION_CREATED"
	case proto.TypeServiceOpen:
		return "SERVICE_OPEN"
	case proto.TypeServiceOpened:
		return "SERVICE_OPENED"
	case proto.TypeServiceClose:
		return "SERVICE_CLOSE"
	case proto.TypeAuthInfo:
		return "AUTH_INFO"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", byte(t))
	}
}
