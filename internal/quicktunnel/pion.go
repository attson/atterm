package quicktunnel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peertransport"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const pionSignalWriteTimeout = 10 * time.Second

// PionHostBridgeConfig supplies application callbacks for a Quick Tunnel
// signaling channel that answers an existing Pion direct attempt.
type PionHostBridgeConfig struct {
	WebRTC          webrtc.Configuration
	OnAuthenticated func(*peertransport.PionHostChannel)
	OnRecord        func(peertransport.RecordKind, []byte)
	OnConfigMessage func(peertransport.RecordKind, []byte) error
	OnClosed        func(error)
}

// PionClientBridgeConfig supplies application callbacks for a Quick Tunnel
// signaling channel that initiates an existing Pion direct attempt.
type PionClientBridgeConfig struct {
	WebRTC          webrtc.Configuration
	OnAuthenticated func(*peertransport.PionClientChannel)
	OnRecord        func(peertransport.RecordKind, []byte)
	OnConfigMessage func(peertransport.RecordKind, []byte) error
	OnDiagnostics   func(iceState, candidateType string)
	OnClosed        func(error)
}

// BridgePionHost binds this authenticated host signaling channel to one Pion
// attempt. A channel can have only one signal consumer.
func (c *SignalChannel) BridgePionHost(parent context.Context, cfg PionHostBridgeConfig) (*peertransport.PionHostAttempt, error) {
	if err := c.reservePionBridge(parent, signalChannelHost); err != nil {
		return nil, err
	}
	reserved := true
	defer func() {
		if reserved {
			c.releaseSignalHandlerReservation()
		}
	}()
	link := &pionSignalLink{channel: c}
	attempt, err := peertransport.NewPionHostAttempt(parent, peertransport.PionHostConfig{
		Authorization: c.authorization,
		Authenticator: c.authenticator,
		WebRTC:        cfg.WebRTC,
		SendSignal:    c.pionSignalSender(parent),
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if cfg.OnAuthenticated != nil {
				cfg.OnAuthenticated(channel)
			}
		},
		OnRecord:        cfg.OnRecord,
		OnConfigMessage: cfg.OnConfigMessage,
		OnClosed: func(err error) {
			link.pionClosed()
			if cfg.OnClosed != nil {
				cfg.OnClosed(err)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	if err := c.installSignalHandler(func(_ *SignalChannel, signal Signal) error {
		return attempt.HandleSignal(string(signal.Type), signal.Payload)
	}); err != nil {
		_ = attempt.Close()
		return nil, err
	}
	reserved = false
	link.activate()
	c.closePionWithSignal(attempt)
	return attempt, nil
}

// BridgePionClient binds and starts one Pion offerer. The signal handler is
// installed before Start emits the offer, so an immediate answer cannot be
// dropped.
func (c *SignalChannel) BridgePionClient(parent context.Context, cfg PionClientBridgeConfig) (*peertransport.PionClientAttempt, error) {
	if err := c.reservePionBridge(parent, signalChannelClient); err != nil {
		return nil, err
	}
	reserved := true
	defer func() {
		if reserved {
			c.releaseSignalHandlerReservation()
		}
	}()
	link := &pionSignalLink{channel: c}
	attempt, err := peertransport.NewPionClientAttempt(parent, peertransport.PionClientConfig{
		Authorization: c.authorization,
		Authenticator: c.authenticator,
		WebRTC:        cfg.WebRTC,
		SendSignal:    c.pionSignalSender(parent),
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			if cfg.OnAuthenticated != nil {
				cfg.OnAuthenticated(channel)
			}
		},
		OnRecord:        cfg.OnRecord,
		OnConfigMessage: cfg.OnConfigMessage,
		OnDiagnostics:   cfg.OnDiagnostics,
		OnClosed: func(err error) {
			link.pionClosed()
			if cfg.OnClosed != nil {
				cfg.OnClosed(err)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	if err := c.installSignalHandler(func(_ *SignalChannel, signal Signal) error {
		return attempt.HandleSignal(string(signal.Type), signal.Payload)
	}); err != nil {
		_ = attempt.Close()
		return nil, err
	}
	reserved = false
	link.activate()
	c.closePionWithSignal(attempt)
	if err := attempt.Start(); err != nil {
		_ = attempt.Close()
		return nil, fmt.Errorf("start Quick Tunnel Pion attempt: %w", err)
	}
	return attempt, nil
}

type pionAttempt interface {
	Close() error
}

func (c *SignalChannel) reservePionBridge(parent context.Context, role signalChannelRole) error {
	if c == nil || parent == nil || c.role != role || c.authenticator == nil ||
		c.authorization.AttemptID == uuid.Nil {
		return fmt.Errorf("%w: invalid Quick Tunnel bridge", peertransport.ErrDirectTransport)
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("%w: bridge context: %v", peertransport.ErrDirectTransport, err)
	}
	select {
	case <-c.done:
		return fmt.Errorf("%w: signaling channel closed", peertransport.ErrDirectTransport)
	default:
	}
	if err := c.reserveSignalHandler(); err != nil {
		return fmt.Errorf("%w: %v", peertransport.ErrDirectTransport, err)
	}
	return nil
}

func (c *SignalChannel) pionSignalSender(parent context.Context) func(string, string) error {
	return func(signalType, payload string) error {
		ctx, cancel := context.WithTimeout(parent, pionSignalWriteTimeout)
		defer cancel()
		return c.SendSignal(ctx, Signal{Type: SignalType(signalType), Payload: payload})
	}
}

func (c *SignalChannel) closePionWithSignal(attempt pionAttempt) {
	go func() {
		<-c.done
		_ = attempt.Close()
	}()
}

type pionSignalLink struct {
	channel *SignalChannel

	mu     sync.Mutex
	active bool
	closed bool
}

func (l *pionSignalLink) activate() {
	l.mu.Lock()
	l.active = true
	closeSignal := l.closed
	l.mu.Unlock()
	if closeSignal {
		go func() { _ = l.channel.Close() }()
	}
}

func (l *pionSignalLink) pionClosed() {
	l.mu.Lock()
	l.closed = true
	closeSignal := l.active
	l.mu.Unlock()
	if closeSignal {
		go func() { _ = l.channel.Close() }()
	}
}
