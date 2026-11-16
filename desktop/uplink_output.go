package main

import (
	"context"
	"fmt"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/terminalcodec"
	"github.com/google/uuid"
)

func (u *uplink) outputCompression() string {
	if u.accountKey != nil && len(u.accountKey()) >= e2eecrypto.SessionKeySize {
		return proto.OutputCompressionDeflate
	}
	return ""
}

func (u *uplink) localSessionLive(id uuid.UUID) bool {
	sess, ok := u.host.server.Registry().Get(id)
	return ok && !sess.IsClosed()
}

// prepareUplinkSubscriberFrame preserves the shared metadata policy while the
// compressed lane fails closed: an ordinary fallback would violate the
// negotiated format and could expose bytes to the relay.
func prepareUplinkSubscriberFrame(f proto.Frame, compressed bool, accountKey func() []byte) (proto.Frame, bool, error) {
	if !compressed || f.Type != proto.TypeOut {
		prepared, ok := prepareRemoteSubscriberFrame(f, accountKey)
		return prepared, ok, nil
	}
	var key []byte
	if accountKey != nil {
		key = accountKey()
	}
	sk, err := e2eecrypto.DeriveSessionKey(key, f.SessionID)
	if err != nil {
		return proto.Frame{}, false, fmt.Errorf("compressed output key: %w", err)
	}
	seq, data, err := proto.DecodeOut(f.Payload)
	if err != nil {
		return proto.Frame{}, false, fmt.Errorf("compressed output frame: %w", err)
	}
	encoded, err := terminalcodec.Encode(data)
	if err != nil {
		return proto.Frame{}, false, fmt.Errorf("encode compressed output: %w", err)
	}
	envelope, err := e2eecrypto.SealOut(sk, f.SessionID, e2eecrypto.AADTagOutCompressed, seq, encoded)
	if err != nil {
		return proto.Frame{}, false, fmt.Errorf("seal compressed output: %w", err)
	}
	prepared := proto.EncodeOut(f.SessionID, seq, envelope)
	prepared.Type = proto.TypeOutCompressed
	return prepared, true, nil
}

func forwardUplinkStream(ctx context.Context, input <-chan proto.Frame, done <-chan struct{}, out chan<- proto.Frame, stats *streamForwardStats, requestRepaint func(), accountKey func() []byte, compressed bool) string {
	for {
		if ctx.Err() != nil {
			return "context_done"
		}
		var frame proto.Frame
		select {
		case <-ctx.Done():
			return "context_done"
		case <-done:
			return "subscriber_done"
		case f, ok := <-input:
			if !ok {
				return "subscriber_closed"
			}
			frame = f
		}
		if stats != nil {
			stats.observe(frame)
		}
		if localSubscriberFrameRequestsRepaint(frame) && requestRepaint != nil {
			requestRepaint()
		}
		prepared, ok, err := prepareUplinkSubscriberFrame(frame, compressed, accountKey)
		if err != nil {
			logWarn("uplink", "stream_output_failed session=%s compressed=%t error=%v", frame.SessionID, compressed, err)
			return "output_prepare_failed"
		}
		if !ok {
			continue
		}
		select {
		case out <- prepared:
		case <-ctx.Done():
			return "context_done"
		}
	}
}
