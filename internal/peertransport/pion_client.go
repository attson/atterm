package peertransport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// PionClientConfig contains one Relay-authorized client attempt.
type PionClientConfig struct {
	Authorization   Authorization
	Authenticator   HandshakeAuthenticator
	WebRTC          webrtc.Configuration
	SendSignal      func(signalType, payload string) error
	OnAuthenticated func(*PionClientChannel)
	OnRecord        func(RecordKind, []byte)
	OnDiagnostics   func(iceState, candidateType string)
	OnClosed        func(error)
}

// PionClientAttempt owns the offerer side of one direct route.
type PionClientAttempt struct {
	ctx       context.Context
	cancel    context.CancelFunc
	pc        *webrtc.PeerConnection
	dc        *webrtc.DataChannel
	handshake *ClientHandshake
	cfg       PionClientConfig

	mu      sync.Mutex
	channel *PionClientChannel
	timer   *time.Timer
	started bool
	closed  bool
}

// PionClientChannel is available only after account-key authentication.
type PionClientChannel struct {
	attempt     *PionClientAttempt
	dc          *webrtc.DataChannel
	sealer      *RecordSealer
	opener      *RecordOpener
	reassembler Reassembler

	sendMu        sync.Mutex
	receiveMu     sync.Mutex
	nextMessageID uint64
}

func NewPionClientAttempt(parent context.Context, cfg PionClientConfig) (*PionClientAttempt, error) {
	if parent == nil || cfg.SendSignal == nil {
		return nil, fmt.Errorf("%w: missing context or signaling sender", ErrDirectTransport)
	}
	handshake, err := NewClientHandshake(cfg.Authenticator, cfg.Authorization)
	if err != nil {
		return nil, err
	}
	pc, err := webrtc.NewPeerConnection(cfg.WebRTC)
	if err != nil {
		return nil, fmt.Errorf("%w: create peer connection: %v", ErrDirectTransport, err)
	}
	dc, err := pc.CreateDataChannel(TerminalDataChannelLabel, nil)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("%w: create data channel: %v", ErrDirectTransport, err)
	}
	ctx, cancel := context.WithCancel(parent)
	a := &PionClientAttempt{ctx: ctx, cancel: cancel, pc: pc, dc: dc, handshake: handshake, cfg: cfg}
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		candidateType := ""
		if state == webrtc.ICEConnectionStateConnected || state == webrtc.ICEConnectionStateCompleted {
			if sctp := pc.SCTP(); sctp != nil && sctp.Transport() != nil && sctp.Transport().ICETransport() != nil {
				if pair, pairErr := sctp.Transport().ICETransport().GetSelectedCandidatePair(); pairErr == nil && pair != nil && pair.Local != nil {
					candidateType = pair.Local.Typ.String()
				}
			}
		}
		if cfg.OnDiagnostics != nil {
			cfg.OnDiagnostics(state.String(), candidateType)
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected:
			a.fail(fmt.Errorf("%w: peer connection %s", ErrDirectTransport, state.String()))
		case webrtc.PeerConnectionStateClosed:
			a.fail(fmt.Errorf("%w: peer connection closed", ErrDirectTransport))
		}
	})
	dc.OnOpen(a.onDataChannelOpen)
	dc.OnMessage(a.onDataChannelMessage)
	dc.OnClose(func() { a.fail(fmt.Errorf("%w: data channel closed", ErrDirectTransport)) })
	dc.OnError(func(err error) { a.fail(fmt.Errorf("%w: data channel: %v", ErrDirectTransport, err)) })
	go func() {
		<-ctx.Done()
		a.finish(context.Cause(ctx))
	}()
	return a, nil
}

func (a *PionClientAttempt) Start() error {
	a.mu.Lock()
	if a.closed || a.started {
		a.mu.Unlock()
		return fmt.Errorf("%w: client attempt unavailable", ErrDirectTransport)
	}
	a.started = true
	a.timer = time.AfterFunc(directHandshakeTimeout, func() {
		a.fail(fmt.Errorf("%w: handshake timeout", ErrDirectTransport))
	})
	a.mu.Unlock()
	offer, err := a.pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("%w: create offer: %v", ErrDirectTransport, err)
	}
	gathered := webrtc.GatheringCompletePromise(a.pc)
	if err := a.pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("%w: set offer: %v", ErrDirectTransport, err)
	}
	select {
	case <-a.ctx.Done():
		return fmt.Errorf("%w: gathering cancelled", ErrDirectTransport)
	case <-gathered:
	}
	encoded, err := json.Marshal(a.pc.LocalDescription())
	if err != nil {
		return fmt.Errorf("%w: encode offer: %v", ErrDirectTransport, err)
	}
	if err := a.cfg.SendSignal("offer", string(encoded)); err != nil {
		return fmt.Errorf("%w: send offer: %v", ErrDirectTransport, err)
	}
	return a.cfg.SendSignal("ice_end", "")
}

func (a *PionClientAttempt) HandleSignal(signalType, payload string) error {
	if a == nil || a.pc == nil {
		return fmt.Errorf("%w: nil attempt", ErrDirectTransport)
	}
	switch signalType {
	case "answer":
		var answer webrtc.SessionDescription
		if err := json.Unmarshal([]byte(payload), &answer); err != nil || answer.Type != webrtc.SDPTypeAnswer {
			return fmt.Errorf("%w: invalid answer", ErrDirectTransport)
		}
		if err := a.pc.SetRemoteDescription(answer); err != nil {
			return fmt.Errorf("%w: set answer: %v", ErrDirectTransport, err)
		}
		return nil
	case "ice_candidate":
		var candidate webrtc.ICECandidateInit
		if err := json.Unmarshal([]byte(payload), &candidate); err != nil {
			return fmt.Errorf("%w: invalid ICE candidate", ErrDirectTransport)
		}
		if err := a.pc.AddICECandidate(candidate); err != nil {
			return fmt.Errorf("%w: add ICE candidate: %v", ErrDirectTransport, err)
		}
		return nil
	case "ice_end":
		return nil
	default:
		return fmt.Errorf("%w: unsupported signal %q", ErrDirectTransport, signalType)
	}
}

func (a *PionClientAttempt) onDataChannelOpen() {
	hello, err := a.handshake.ClientHello()
	if err != nil {
		a.fail(err)
		return
	}
	if err := a.dc.Send(hello); err != nil {
		a.fail(fmt.Errorf("%w: send client hello: %v", ErrDirectTransport, err))
	}
}

func (a *PionClientAttempt) onDataChannelMessage(message webrtc.DataChannelMessage) {
	if message.IsString {
		a.fail(fmt.Errorf("%w: text data channel message", ErrDirectTransport))
		return
	}
	a.mu.Lock()
	channel := a.channel
	a.mu.Unlock()
	if channel != nil {
		if err := channel.receive(message.Data); err != nil {
			a.fail(err)
		}
		return
	}
	result, err := a.handshake.Handle(message.Data)
	if err != nil {
		a.fail(err)
		return
	}
	if len(result.Response) > 0 {
		if err := a.dc.Send(result.Response); err != nil {
			a.fail(fmt.Errorf("%w: send handshake: %v", ErrDirectTransport, err))
			return
		}
	}
	if !result.Authenticated {
		return
	}
	sealer, err := NewRecordSealer(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		a.fail(err)
		return
	}
	opener, err := NewRecordOpener(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		a.fail(err)
		return
	}
	channel = &PionClientChannel{attempt: a, dc: a.dc, sealer: sealer, opener: opener, nextMessageID: 1}
	a.mu.Lock()
	a.channel = channel
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	a.mu.Unlock()
	if a.cfg.OnAuthenticated != nil {
		a.cfg.OnAuthenticated(channel)
	}
}

func (c *PionClientChannel) receive(record []byte) error {
	c.receiveMu.Lock()
	defer c.receiveMu.Unlock()
	kind, plaintext, err := c.opener.Open(record)
	if err != nil {
		return err
	}
	if kind == RecordFragment {
		frame, complete, err := c.reassembler.Add(plaintext, time.Now())
		if err != nil || !complete {
			return err
		}
		kind = RecordFrame
		plaintext = frame
	}
	if c.attempt.cfg.OnRecord != nil {
		c.attempt.cfg.OnRecord(kind, append([]byte(nil), plaintext...))
	}
	return nil
}

func (c *PionClientChannel) SendRecord(ctx context.Context, kind RecordKind, plaintext []byte) error {
	if c == nil || c.dc == nil || c.sealer == nil {
		return fmt.Errorf("%w: unauthenticated channel", ErrDirectTransport)
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.sendRecordLocked(ctx, kind, plaintext)
}

func (c *PionClientChannel) SendFrame(ctx context.Context, frame []byte) error {
	if c == nil || c.dc == nil || c.sealer == nil {
		return fmt.Errorf("%w: unauthenticated channel", ErrDirectTransport)
	}
	if len(frame) > MaxFrameSize {
		return fmt.Errorf("%w: frame too large", ErrDirectTransport)
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if len(frame) <= MaxRecordPlaintext {
		return c.sendRecordLocked(ctx, RecordFrame, frame)
	}
	fragments, err := FragmentFrame(c.nextMessageID, frame)
	if err != nil {
		return err
	}
	c.nextMessageID++
	for _, fragment := range fragments {
		if err := c.sendRecordLocked(ctx, RecordFragment, fragment); err != nil {
			return err
		}
	}
	return nil
}

func (c *PionClientChannel) sendRecordLocked(ctx context.Context, kind RecordKind, plaintext []byte) error {
	for c.dc.BufferedAmount() > directBufferedHighWater {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.attempt.ctx.Done():
			return fmt.Errorf("%w: attempt closed", ErrDirectTransport)
		case <-time.After(directBufferedPoll):
		}
	}
	record, err := c.sealer.Seal(kind, plaintext)
	if err != nil {
		return err
	}
	if err := c.dc.Send(record); err != nil {
		return fmt.Errorf("%w: send record: %v", ErrDirectTransport, err)
	}
	return nil
}

func (c *PionClientChannel) Close() error {
	if c == nil || c.attempt == nil {
		return nil
	}
	return c.attempt.Close()
}

func (a *PionClientAttempt) Close() error {
	if a == nil {
		return nil
	}
	a.finish(nil)
	return nil
}

func (a *PionClientAttempt) fail(err error) { a.finish(err) }

func (a *PionClientAttempt) finish(err error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	a.mu.Unlock()
	a.cancel()
	_ = a.pc.Close()
	if a.cfg.OnClosed != nil {
		a.cfg.OnClosed(err)
	}
}
