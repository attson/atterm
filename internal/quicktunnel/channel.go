package quicktunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/peertransport"
	"nhooyr.io/websocket"
)

type signalDirectionState struct {
	offer      bool
	answer     bool
	iceEnd     bool
	candidates int
}

// SignalChannel is an authenticated, application-encrypted signaling path.
// The WebSocket carries only Peer records after the membership handshake.
type SignalChannel struct {
	conn             *websocket.Conn
	sealer           *peertransport.RecordSealer
	opener           *peertransport.RecordOpener
	remoteMembership string
	onSignal         func(*SignalChannel, Signal) error
	onClosed         func(*SignalChannel, error)

	writeMu       sync.Mutex
	sendState     signalDirectionState
	nextMessageID uint64
	finishOnce    sync.Once
	done          chan struct{}
}

func newHostSignalChannel(conn *websocket.Conn, result peertransport.HostHandshakeResult, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) (*SignalChannel, error) {
	sealer, err := peertransport.NewRecordSealer(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	opener, err := peertransport.NewRecordOpener(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	return newSignalChannel(conn, sealer, opener, remoteMembership, onSignal, onClosed), nil
}

func newClientSignalChannel(conn *websocket.Conn, result peertransport.ClientHandshakeResult, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) (*SignalChannel, error) {
	sealer, err := peertransport.NewRecordSealer(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	opener, err := peertransport.NewRecordOpener(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	return newSignalChannel(conn, sealer, opener, remoteMembership, onSignal, onClosed), nil
}

func newSignalChannel(conn *websocket.Conn, sealer *peertransport.RecordSealer, opener *peertransport.RecordOpener, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) *SignalChannel {
	return &SignalChannel{
		conn: conn, sealer: sealer, opener: opener, remoteMembership: remoteMembership,
		onSignal: onSignal, onClosed: onClosed, done: make(chan struct{}),
	}
}

// RemoteMembershipToken returns the exact verified membership bound into the
// handshake. Config sync must use this value instead of caller-selected data.
func (c *SignalChannel) RemoteMembershipToken() string {
	if c == nil {
		return ""
	}
	return c.remoteMembership
}

// SendSignal validates, serializes, fragments when necessary, and encrypts a
// signaling message. Concurrent callers are serialized with record counters.
func (c *SignalChannel) SendSignal(ctx context.Context, signal Signal) error {
	if c == nil || c.conn == nil || c.sealer == nil {
		return errors.New("quicktunnel: closed signal channel")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return errors.New("quicktunnel: closed signal channel")
	default:
	}
	if signal.Version == 0 {
		signal.Version = signalVersion
	}
	if err := c.sendState.accept(signal); err != nil {
		return err
	}
	payload, err := json.Marshal(signal)
	if err != nil || len(payload) > peertransport.MaxSignalMessageSize {
		return fmt.Errorf("%w: encoded size", ErrInvalidSignal)
	}
	if len(payload) <= peertransport.MaxRecordPlaintext {
		return c.writeRecord(ctx, peertransport.RecordSignal, payload)
	}
	c.nextMessageID++
	fragments, err := peertransport.FragmentSignalMessage(c.nextMessageID, payload)
	if err != nil {
		return err
	}
	for _, fragment := range fragments {
		if err := c.writeRecord(ctx, peertransport.RecordSignalFragment, fragment); err != nil {
			return err
		}
	}
	return nil
}

func (c *SignalChannel) writeRecord(ctx context.Context, kind peertransport.RecordKind, plaintext []byte) error {
	record, err := c.sealer.Seal(kind, plaintext)
	if err != nil {
		return err
	}
	if err := c.conn.Write(ctx, websocket.MessageBinary, record); err != nil {
		return fmt.Errorf("write encrypted signal: %w", err)
	}
	return nil
}

func (c *SignalChannel) readLoop(ctx context.Context) error {
	var fragments peertransport.SignalReassembler
	var receiveState signalDirectionState
	for {
		messageType, record, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageBinary {
			return fmt.Errorf("%w: non-binary encrypted record", ErrInvalidSignal)
		}
		kind, plaintext, err := c.opener.Open(record)
		if err != nil {
			return err
		}
		switch kind {
		case peertransport.RecordSignal:
			if err := c.deliverSignal(plaintext, &receiveState); err != nil {
				return err
			}
		case peertransport.RecordSignalFragment:
			message, complete, err := fragments.Add(plaintext, time.Now())
			if err != nil {
				return err
			}
			if complete {
				if err := c.deliverSignal(message, &receiveState); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%w: record kind %d", ErrInvalidSignal, kind)
		}
	}
}

func (c *SignalChannel) deliverSignal(payload []byte, state *signalDirectionState) error {
	if len(payload) == 0 || len(payload) > peertransport.MaxSignalMessageSize {
		return fmt.Errorf("%w: encoded size", ErrInvalidSignal)
	}
	var signal Signal
	if err := decodeStrictJSON(payload, &signal); err != nil {
		return fmt.Errorf("%w: JSON", ErrInvalidSignal)
	}
	if err := state.accept(signal); err != nil {
		return err
	}
	if c.onSignal != nil {
		if err := c.onSignal(c, signal); err != nil {
			return fmt.Errorf("handle encrypted signal: %w", err)
		}
	}
	return nil
}

func (s *signalDirectionState) accept(signal Signal) error {
	if signal.Version != signalVersion || !utf8.ValidString(signal.Payload) {
		return fmt.Errorf("%w: version or encoding", ErrInvalidSignal)
	}
	switch signal.Type {
	case SignalOffer:
		if s.offer || signal.Payload == "" || len(signal.Payload) > signalOfferLimit {
			return fmt.Errorf("%w: offer", ErrInvalidSignal)
		}
		s.offer = true
	case SignalAnswer:
		if s.answer || signal.Payload == "" || len(signal.Payload) > signalOfferLimit {
			return fmt.Errorf("%w: answer", ErrInvalidSignal)
		}
		s.answer = true
	case SignalICECandidate:
		if s.iceEnd || s.candidates >= maxCandidates || signal.Payload == "" || len(signal.Payload) > signalCandidateLimit {
			return fmt.Errorf("%w: ICE candidate", ErrInvalidSignal)
		}
		s.candidates++
	case SignalICEEnd:
		if s.iceEnd || signal.Payload != "" {
			return fmt.Errorf("%w: ICE end", ErrInvalidSignal)
		}
		s.iceEnd = true
	default:
		return fmt.Errorf("%w: type %q", ErrInvalidSignal, signal.Type)
	}
	return nil
}

// Close gives the peer one second to complete the WebSocket close handshake,
// then forces the socket closed so app shutdown remains bounded.
func (c *SignalChannel) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	closed := make(chan error, 1)
	go func() {
		closed <- c.conn.Close(websocket.StatusNormalClosure, "")
	}()
	var err error
	select {
	case err = <-closed:
	case <-time.After(time.Second):
		err = c.conn.CloseNow()
	}
	c.finish(nil)
	return err
}

func (c *SignalChannel) finish(err error) {
	c.finishOnce.Do(func() {
		close(c.done)
		if c.onClosed != nil {
			c.onClosed(c, err)
		}
	})
}
