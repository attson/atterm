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
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

type signalDirectionState struct {
	offer         bool
	answer        bool
	iceEnd        bool
	fallback      bool
	fallbackReady bool
	candidates    int
}

type signalChannelRole byte

const (
	signalChannelClient signalChannelRole = iota + 1
	signalChannelHost
)

// SignalChannel is an authenticated, application-encrypted signaling path.
// The WebSocket carries only Peer records after the membership handshake.
type SignalChannel struct {
	conn             *websocket.Conn
	sealer           *peertransport.RecordSealer
	opener           *peertransport.RecordOpener
	remoteMembership string
	onClosed         func(*SignalChannel, error)
	role             signalChannelRole
	authorization    peertransport.Authorization
	authenticator    peertransport.HandshakeAuthenticator

	writeMu       sync.Mutex
	sendState     signalDirectionState
	nextMessageID uint64
	handlerMu     sync.RWMutex
	onSignal      func(*SignalChannel, Signal) error
	bridgeBound   bool
	fallbackMu    sync.Mutex
	fallbackCfg   *WSSFallbackConfig
	fallback      *WSSChannel
	fallbackClose func()
	fallbackDone  chan struct{}
	closeOnce     sync.Once
	closeDone     chan struct{}
	closeErr      error
	finishOnce    sync.Once
	done          chan struct{}
}

// ChannelBinding exposes only the routing claims bound into both Peer
// handshakes. The random ticket and membership documents remain internal to
// the transport after authentication.
type ChannelBinding struct {
	AttemptID        uuid.UUID
	SessionID        uuid.UUID
	ClientInstanceID string
	Permission       peertransport.Permission
}

func newHostSignalChannel(conn *websocket.Conn, result peertransport.HostHandshakeResult, authorization peertransport.Authorization, authenticator peertransport.HandshakeAuthenticator, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) (*SignalChannel, error) {
	sealer, err := peertransport.NewRecordSealer(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	opener, err := peertransport.NewRecordOpener(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	return newSignalChannel(conn, sealer, opener, signalChannelHost, authorization, authenticator, remoteMembership, onSignal, onClosed), nil
}

func newClientSignalChannel(conn *websocket.Conn, result peertransport.ClientHandshakeResult, authorization peertransport.Authorization, authenticator peertransport.HandshakeAuthenticator, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) (*SignalChannel, error) {
	sealer, err := peertransport.NewRecordSealer(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	opener, err := peertransport.NewRecordOpener(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		return nil, err
	}
	return newSignalChannel(conn, sealer, opener, signalChannelClient, authorization, authenticator, remoteMembership, onSignal, onClosed), nil
}

func newSignalChannel(conn *websocket.Conn, sealer *peertransport.RecordSealer, opener *peertransport.RecordOpener, role signalChannelRole, authorization peertransport.Authorization, authenticator peertransport.HandshakeAuthenticator, remoteMembership string, onSignal func(*SignalChannel, Signal) error, onClosed func(*SignalChannel, error)) *SignalChannel {
	authorization.Ticket = append([]byte(nil), authorization.Ticket...)
	return &SignalChannel{
		conn: conn, sealer: sealer, opener: opener, remoteMembership: remoteMembership,
		onSignal: onSignal, onClosed: onClosed, role: role,
		authorization: authorization, authenticator: authenticator,
		closeDone: make(chan struct{}), done: make(chan struct{}), fallbackDone: make(chan struct{}),
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

// Binding returns the immutable routing claims authenticated for this channel.
func (c *SignalChannel) Binding() ChannelBinding {
	if c == nil {
		return ChannelBinding{}
	}
	return ChannelBinding{
		AttemptID: c.authorization.AttemptID, SessionID: c.authorization.SessionID,
		ClientInstanceID: c.authorization.ClientInstanceID, Permission: c.authorization.Permission,
	}
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
	if c.activeWSSFallback() != nil {
		return fmt.Errorf("%w: WSS fallback is active", peertransport.ErrDirectTransport)
	}
	if signal.Version == 0 {
		signal.Version = signalVersion
	}
	if err := c.sendState.accept(signal, c.role); err != nil {
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
	if fallback := c.activeWSSFallback(); fallback != nil && kind.IsDataMessage() && fallback.onTraffic != nil {
		fallback.onTraffic(peertransport.TrafficSent, len(record))
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
		if fallback := c.activeWSSFallback(); fallback != nil {
			if !kind.IsDataMessage() {
				return fmt.Errorf("%w: record kind %d is not valid on WSS fallback", peertransport.ErrDirectTransport, kind)
			}
			if fallback.onTraffic != nil {
				fallback.onTraffic(peertransport.TrafficReceived, len(record))
			}
			if err := fallback.receive(kind, plaintext); err != nil {
				return err
			}
			continue
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
	senderRole := signalChannelHost
	if c.role == signalChannelHost {
		senderRole = signalChannelClient
	}
	if err := state.accept(signal, senderRole); err != nil {
		return err
	}
	if signal.Type == SignalWSSFallback {
		if err := c.acknowledgeWSSFallback(); err != nil {
			return err
		}
		return nil
	}
	if signal.Type == SignalWSSReady {
		if !c.requestedWSSFallback() {
			return fmt.Errorf("%w: unsolicited WSS ready", ErrInvalidSignal)
		}
		if _, err := c.activateWSSFallback(); err != nil {
			return err
		}
		return nil
	}
	c.handlerMu.RLock()
	handler := c.onSignal
	c.handlerMu.RUnlock()
	if handler == nil {
		return fmt.Errorf("%w: no signal handler", ErrInvalidSignal)
	}
	if err := handler(c, signal); err != nil {
		return fmt.Errorf("handle encrypted signal: %w", err)
	}
	return nil
}

func (c *SignalChannel) requestedWSSFallback() bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.sendState.fallback
}

func (c *SignalChannel) reserveSignalHandler() error {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	if c.bridgeBound || c.onSignal != nil {
		return errors.New("quicktunnel: signaling handler already bound")
	}
	select {
	case <-c.done:
		return errors.New("quicktunnel: closed signal channel")
	default:
	}
	c.bridgeBound = true
	return nil
}

func (c *SignalChannel) releaseSignalHandlerReservation() {
	c.handlerMu.Lock()
	if c.onSignal == nil {
		c.bridgeBound = false
	}
	c.handlerMu.Unlock()
}

func (c *SignalChannel) installSignalHandler(handler func(*SignalChannel, Signal) error) error {
	if handler == nil {
		return errors.New("quicktunnel: invalid signaling bridge")
	}
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	if !c.bridgeBound || c.onSignal != nil {
		return errors.New("quicktunnel: signaling handler reservation lost")
	}
	c.onSignal = handler
	return nil
}

func (s *signalDirectionState) accept(signal Signal, senderRole signalChannelRole) error {
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
	case SignalWSSFallback:
		if senderRole != signalChannelClient || s.fallback || signal.Payload != "" {
			return fmt.Errorf("%w: WSS fallback", ErrInvalidSignal)
		}
		s.fallback = true
	case SignalWSSReady:
		if senderRole != signalChannelHost || s.fallbackReady || signal.Payload != "" {
			return fmt.Errorf("%w: WSS ready", ErrInvalidSignal)
		}
		s.fallbackReady = true
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
	c.closeOnce.Do(func() {
		closed := make(chan error, 1)
		go func() {
			closed <- c.conn.Close(websocket.StatusNormalClosure, "")
		}()
		select {
		case c.closeErr = <-closed:
		case <-time.After(time.Second):
			c.closeErr = c.conn.CloseNow()
		}
		close(c.closeDone)
	})
	<-c.closeDone
	c.finish(nil)
	return c.closeErr
}

func (c *SignalChannel) finish(err error) {
	c.finishOnce.Do(func() {
		close(c.done)
		if c.onClosed != nil {
			c.onClosed(c, err)
		}
	})
}
