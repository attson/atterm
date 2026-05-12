package relay

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/attson/atterm/internal/proto"
)

func (s *Server) debugf(format string, args ...any) {
	if !s.cfg.Debug {
		return
	}
	log.New(s.cfg.DebugLog, "", log.LstdFlags).Printf("relay-debug "+format, args...)
}

func (s *Server) debugFrame(component, direction string, f proto.Frame) {
	if !s.cfg.Debug {
		return
	}
	s.debugf("%s %s %s session=%s%s",
		component,
		direction,
		frameTypeName(f.Type),
		f.SessionID,
		s.debugFrameDetails(f),
	)
}

func (s *Server) debugFrameDetails(f proto.Frame) string {
	switch f.Type {
	case proto.TypeOpen:
		var p proto.OpenPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" command=%q cwd=%q title=%q host_id=%q host=%q user=%q cols=%d rows=%d",
			p.Command, p.Cwd, p.Title, p.HostID, p.Host, p.User, p.Cols, p.Rows)
	case proto.TypeIn:
		return s.debugBytes("bytes", f.Payload)
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" seq=%d%s", seq, s.debugBytes("bytes", data))
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
		return fmt.Sprintf(" cwd=%q title=%q", p.Cwd, p.Title)
	case proto.TypeClose:
		var p proto.ClosePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return fmt.Sprintf(" payload_bytes=%d bad_payload=%q", len(f.Payload), err)
		}
		return fmt.Sprintf(" exit_code=%d reason=%q", p.ExitCode, p.Reason)
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
		return fmt.Sprintf(" host_id=%q host=%q user=%q sessions=%d", p.HostID, p.Host, p.User, len(p.Sessions))
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
		return fmt.Sprintf(" filename=%q content_type=%q image_bytes=%d", p.Filename, p.ContentType, len(p.Data))
	default:
		return fmt.Sprintf(" payload_bytes=%d", len(f.Payload))
	}
}

func (s *Server) debugBytes(label string, data []byte) string {
	out := fmt.Sprintf(" %s=%d", label, len(data))
	if s.cfg.DebugPayload {
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
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", byte(t))
	}
}
