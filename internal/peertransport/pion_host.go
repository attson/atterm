package peertransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	TerminalDataChannelLabel = "atterm-terminal-v1"
	directBufferedHighWater  = 1024 * 1024
	directBufferedPoll       = 2 * time.Millisecond
)

var ErrDirectTransport = errors.New("peertransport: direct transport failed")

// PionHostConfig contains one Relay-authorized host attempt. SendSignal must
// enqueue bounded signaling messages without logging their payloads.
type PionHostConfig struct {
	Authorization   Authorization
	Authenticator   HandshakeAuthenticator
	WebRTC          webrtc.Configuration
	SendSignal      func(signalType, payload string) error
	OnAuthenticated func(*PionHostChannel)
	OnRecord        func(RecordKind, []byte)
	OnClosed        func(error)
}

// PionHostAttempt owns one PeerConnection and consumes one direct ticket.
type PionHostAttempt struct {
	ctx       context.Context
	cancel    context.CancelFunc
	pc        *webrtc.PeerConnection
	handshake *HostHandshake
	cfg       PionHostConfig

	mu          sync.Mutex
	handshakeMu sync.Mutex
	offerSeen   bool
	dataChannel *webrtc.DataChannel
	channel     *PionHostChannel
	timer       *time.Timer
	closed      bool
}

// PionHostChannel is available only after account-key authentication. Send
// methods serialize counters and enforce DataChannel backpressure.
type PionHostChannel struct {
	attempt     *PionHostAttempt
	dc          *webrtc.DataChannel
	sealer      *RecordSealer
	opener      *RecordOpener
	reassembler Reassembler

	sendMu        sync.Mutex
	receiveMu     sync.Mutex
	nextMessageID uint64
}

func NewPionHostAttempt(parent context.Context, cfg PionHostConfig) (*PionHostAttempt, error) {
	if parent == nil || cfg.SendSignal == nil {
		return nil, fmt.Errorf("%w: missing context or signaling sender", ErrDirectTransport)
	}
	handshake, err := NewHostHandshake(cfg.Authenticator, cfg.Authorization)
	if err != nil {
		return nil, err
	}
	pc, err := webrtc.NewPeerConnection(cfg.WebRTC)
	if err != nil {
		return nil, fmt.Errorf("%w: create peer connection: %v", ErrDirectTransport, err)
	}
	ctx, cancel := context.WithCancel(parent)
	a := &PionHostAttempt{ctx: ctx, cancel: cancel, pc: pc, handshake: handshake, cfg: cfg}
	pc.OnDataChannel(a.onDataChannel)
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed:
			a.fail(fmt.Errorf("%w: peer connection failed", ErrDirectTransport))
		case webrtc.PeerConnectionStateClosed:
			a.finish(nil)
		}
	})
	go func() {
		<-ctx.Done()
		a.finish(context.Cause(ctx))
	}()
	return a, nil
}

// HandleSignal applies one Relay-routed offer or ICE message. The v0.6 client
// is the sole offerer; renegotiation is intentionally unsupported.
func (a *PionHostAttempt) HandleSignal(signalType, payload string) error {
	if a == nil || a.pc == nil {
		return fmt.Errorf("%w: nil attempt", ErrDirectTransport)
	}
	select {
	case <-a.ctx.Done():
		return fmt.Errorf("%w: attempt closed", ErrDirectTransport)
	default:
	}
	switch signalType {
	case "offer":
		a.mu.Lock()
		if a.offerSeen {
			a.mu.Unlock()
			return fmt.Errorf("%w: duplicate offer", ErrDirectTransport)
		}
		a.offerSeen = true
		a.mu.Unlock()
		var offer webrtc.SessionDescription
		if err := json.Unmarshal([]byte(payload), &offer); err != nil || offer.Type != webrtc.SDPTypeOffer {
			return fmt.Errorf("%w: invalid offer", ErrDirectTransport)
		}
		if err := a.pc.SetRemoteDescription(offer); err != nil {
			return fmt.Errorf("%w: set offer: %v", ErrDirectTransport, err)
		}
		answer, err := a.pc.CreateAnswer(nil)
		if err != nil {
			return fmt.Errorf("%w: create answer: %v", ErrDirectTransport, err)
		}
		gathered := webrtc.GatheringCompletePromise(a.pc)
		if err := a.pc.SetLocalDescription(answer); err != nil {
			return fmt.Errorf("%w: set answer: %v", ErrDirectTransport, err)
		}
		select {
		case <-a.ctx.Done():
			return fmt.Errorf("%w: gathering cancelled", ErrDirectTransport)
		case <-gathered:
		}
		encoded, err := json.Marshal(a.pc.LocalDescription())
		if err != nil {
			return fmt.Errorf("%w: encode answer: %v", ErrDirectTransport, err)
		}
		if err := a.cfg.SendSignal("answer", string(encoded)); err != nil {
			return fmt.Errorf("%w: send answer: %v", ErrDirectTransport, err)
		}
		return a.cfg.SendSignal("ice_end", "")
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

func (a *PionHostAttempt) onDataChannel(dc *webrtc.DataChannel) {
	if dc.Label() != TerminalDataChannelLabel || !dc.Ordered() || dc.MaxPacketLifeTime() != nil || dc.MaxRetransmits() != nil {
		a.fail(fmt.Errorf("%w: invalid data channel", ErrDirectTransport))
		return
	}
	a.mu.Lock()
	if a.dataChannel != nil {
		a.mu.Unlock()
		a.fail(fmt.Errorf("%w: duplicate data channel", ErrDirectTransport))
		return
	}
	a.dataChannel = dc
	a.mu.Unlock()
	dc.OnOpen(func() {
		a.mu.Lock()
		if a.timer == nil {
			a.timer = time.AfterFunc(directHandshakeTimeout, func() {
				a.fail(fmt.Errorf("%w: handshake timeout", ErrDirectTransport))
			})
		}
		a.mu.Unlock()
	})
	dc.OnMessage(a.onDataChannelMessage)
	dc.OnClose(func() { a.finish(nil) })
}

func (a *PionHostAttempt) onDataChannelMessage(message webrtc.DataChannelMessage) {
	if message.IsString {
		a.fail(fmt.Errorf("%w: text data channel message", ErrDirectTransport))
		return
	}
	a.mu.Lock()
	channel := a.channel
	dc := a.dataChannel
	a.mu.Unlock()
	if channel != nil {
		if err := channel.receive(message.Data); err != nil {
			a.fail(err)
		}
		return
	}
	a.handshakeMu.Lock()
	result, err := a.handshake.Handle(message.Data)
	a.handshakeMu.Unlock()
	if err != nil {
		a.fail(err)
		return
	}
	if err := dc.Send(result.Response); err != nil {
		a.fail(fmt.Errorf("%w: send handshake: %v", ErrDirectTransport, err))
		return
	}
	if !result.Authenticated {
		return
	}
	sealer, err := NewRecordSealer(result.TrafficKeys.HostToClientKey[:], result.TrafficKeys.HostToClientNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		a.fail(err)
		return
	}
	opener, err := NewRecordOpener(result.TrafficKeys.ClientToHostKey[:], result.TrafficKeys.ClientToHostNoncePrefix[:], result.TranscriptHash)
	if err != nil {
		a.fail(err)
		return
	}
	channel = &PionHostChannel{attempt: a, dc: dc, sealer: sealer, opener: opener, nextMessageID: 1}
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

func (c *PionHostChannel) receive(record []byte) error {
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

func (c *PionHostChannel) SendRecord(ctx context.Context, kind RecordKind, plaintext []byte) error {
	if c == nil || c.dc == nil || c.sealer == nil {
		return fmt.Errorf("%w: unauthenticated channel", ErrDirectTransport)
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.sendRecordLocked(ctx, kind, plaintext)
}

// SendFrame encrypts one marshaled proto.Frame, fragmenting it when needed.
func (c *PionHostChannel) SendFrame(ctx context.Context, frame []byte) error {
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

func (c *PionHostChannel) sendRecordLocked(ctx context.Context, kind RecordKind, plaintext []byte) error {
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

func (c *PionHostChannel) Close() error {
	if c == nil || c.attempt == nil {
		return nil
	}
	return c.attempt.Close()
}

func (a *PionHostAttempt) Close() error {
	if a == nil {
		return nil
	}
	a.finish(nil)
	return nil
}

func (a *PionHostAttempt) fail(err error) { a.finish(err) }

func (a *PionHostAttempt) finish(err error) {
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
