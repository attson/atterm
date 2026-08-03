package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

type streamForwardStats struct {
	sessionID        uuid.UUID
	started          time.Time
	outFrames        int
	outBytes         int
	metaFrames       int
	closeFrames      int
	firstOutLogged   bool
	replayDropLogged bool
	nextReportBytes  int
	nextReportFrames int
}

func newStreamForwardStats(id uuid.UUID) *streamForwardStats {
	return &streamForwardStats{
		sessionID:        id,
		started:          time.Now(),
		nextReportBytes:  1024 * 1024,
		nextReportFrames: 256,
	}
}

func (s *streamForwardStats) observe(f proto.Frame) {
	switch f.Type {
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			log.Printf("desktop-uplink: stream_out_decode_failed session=%s error=%v", s.sessionID, err)
			return
		}
		s.outFrames++
		s.outBytes += len(data)
		if !s.firstOutLogged {
			s.firstOutLogged = true
			log.Printf("desktop-uplink: stream_out_first session=%s seq=%d bytes=%d", s.sessionID, seq, len(data))
		}
		if s.outBytes >= s.nextReportBytes || s.outFrames >= s.nextReportFrames {
			log.Printf("desktop-uplink: stream_out_progress session=%s out_frames=%d out_bytes=%d last_seq=%d", s.sessionID, s.outFrames, s.outBytes, seq)
			for s.outBytes >= s.nextReportBytes {
				s.nextReportBytes += 1024 * 1024
			}
			for s.outFrames >= s.nextReportFrames {
				s.nextReportFrames += 256
			}
		}
	case proto.TypeMeta:
		s.metaFrames++
		log.Printf("desktop-uplink: stream_meta_forward session=%s payload_bytes=%d", s.sessionID, len(f.Payload))
	case proto.TypeClose:
		s.closeFrames++
		log.Printf("desktop-uplink: stream_close_forward session=%s payload_bytes=%d", s.sessionID, len(f.Payload))
	case proto.TypeReplayProgress:
		if !s.replayDropLogged {
			s.replayDropLogged = true
			log.Printf("desktop-uplink: stream_replay_progress_drop session=%s reason=local_subscriber_only", s.sessionID)
		}
	}
}

func (s *streamForwardStats) finish(reason string) {
	log.Printf(
		"desktop-uplink: stream_end session=%s reason=%s out_frames=%d out_bytes=%d meta_frames=%d close_frames=%d duration=%s",
		s.sessionID,
		reason,
		s.outFrames,
		s.outBytes,
		s.metaFrames,
		s.closeFrames,
		time.Since(s.started).Round(time.Millisecond),
	)
}


func desktopUplinkFrameTypeName(typ proto.Type) string {
	switch typ {
	case proto.TypeIn:
		return "IN"
	case proto.TypeResize:
		return "RESIZE"
	case proto.TypeOut:
		return "OUT"
	case proto.TypeMeta:
		return "META"
	case proto.TypeClose:
		return "CLOSE"
	case proto.TypePasteImage:
		return "PASTE_IMAGE"
	case proto.TypeClaimDriver:
		return "CLAIM_DRIVER"
	case proto.TypeReplayProgress:
		return "REPLAY_PROGRESS"
	default:
		return fmt.Sprintf("0x%02x", typ)
	}
}

func desktopUplinkFrameLogDetails(f proto.Frame) string {
	prefix := fmt.Sprintf("session=%s payload_bytes=%d", f.SessionID, len(f.Payload))
	switch f.Type {
	case proto.TypePasteImage:
		var p proto.PasteImagePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf(
			"session=%s filename=%q content_type=%q image_bytes=%d payload_bytes=%d",
			f.SessionID,
			p.Filename,
			p.ContentType,
			len(p.Data),
			len(f.Payload),
		)
	case proto.TypeResize:
		cols, rows, err := proto.DecodeResize(f.Payload)
		if err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf("session=%s cols=%d rows=%d", f.SessionID, cols, rows)
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf("session=%s seq=%d out_bytes=%d", f.SessionID, seq, len(data))
	default:
		return prefix
	}
}

// SendCommandEvent queues a TypeCommandEvent frame for the writer goroutine.
// Drops on the floor when the uplink is nil, not connected, or its out
// channel buffer is full. Failure is silent because the local OS
// notification has already fired and Web Push misses are acceptable.
