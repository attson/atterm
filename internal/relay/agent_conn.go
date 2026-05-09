package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"nhooyr.io/websocket"
)

const (
	agentReadLimit  = 17 * 1024 * 1024 // 16MiB payload + small header room
	agentWriteWait  = 10 * time.Second
	agentPingPeriod = 25 * time.Second
)

// handleAgent owns one agent's lifecycle: read the OPEN, register the session,
// then pump frames in both directions until the connection drops or CLOSE arrives.
func (s *Server) handleAgent(ctx context.Context, c *websocket.Conn) {
	c.SetReadLimit(agentReadLimit)

	// First frame must be OPEN.
	openFrame, err := readFrame(ctx, c)
	if err != nil {
		log.Printf("agent: read OPEN: %v", err)
		_ = c.Close(websocket.StatusPolicyViolation, "expected OPEN")
		return
	}
	if openFrame.Type != proto.TypeOpen {
		_ = c.Close(websocket.StatusPolicyViolation, "first frame must be OPEN")
		return
	}
	var op proto.OpenPayload
	if err := json.Unmarshal(openFrame.Payload, &op); err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "bad OPEN payload")
		return
	}
	info := proto.SessionInfo{
		Command: op.Command,
		Cwd:     op.Cwd,
		Title:   op.Title,
		Cols:    op.Cols,
		Rows:    op.Rows,
		HostID:  op.HostID,
		Host:    op.Host,
		User:    op.User,
	}
	sess := session.New(openFrame.SessionID, info)
	s.registry.Add(sess)
	log.Printf("agent: session %s opened (%q)", openFrame.SessionID, op.Command)
	defer func() {
		s.registry.Remove(openFrame.SessionID)
		log.Printf("agent: session %s closed", openFrame.SessionID)
	}()

	// Writer pump: drains session.Inbound() (client->agent IN/RESIZE frames).
	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	go func() {
		ticker := time.NewTicker(agentPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-writerCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(writerCtx, agentWriteWait)
				err := c.Ping(ctx)
				cancel()
				if err != nil {
					return
				}
			case f, ok := <-sess.Inbound():
				if !ok {
					return
				}
				ctx, cancel := context.WithTimeout(writerCtx, agentWriteWait)
				err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// Reader loop: agent's OUT/META/CLOSE frames.
	for {
		f, err := readFrame(ctx, c)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) {
				log.Printf("agent: read: %v", err)
			}
			return
		}
		switch f.Type {
		case proto.TypeOut:
			seq, data, err := proto.DecodeOut(f.Payload)
			if err != nil {
				log.Printf("agent: bad OUT: %v", err)
				return
			}
			sess.PushOut(seq, data)
		case proto.TypeMeta:
			var m proto.MetaPayload
			if err := json.Unmarshal(f.Payload, &m); err == nil {
				sess.UpdateMeta(m)
				sess.Broadcast(f)
			}
		case proto.TypeClose:
			sess.Broadcast(f)
			return
		case proto.TypePong:
			// keepalive response; nothing to do
		default:
			log.Printf("agent: unexpected frame type 0x%02x", f.Type)
		}
	}
}
