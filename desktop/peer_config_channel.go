package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
)

const peerConfigChannelVersion = 1

type peerConfigTransport interface {
	SendConfigMessage(context.Context, peertransport.RecordKind, []byte) error
	RemoteMembershipToken() (string, bool)
}

type peerConfigWireAck struct {
	V       int                          `json:"v"`
	SpaceID string                       `json:"space_id"`
	Cursor  configsync.AntiEntropyCursor `json:"cursor"`
	Durable configsync.DurableAck        `json:"durable"`
	Done    bool                         `json:"done"`
}

// peerConfigChannel drives bidirectional anti-entropy over an authenticated
// Peer transport. It has no terminal/session dependency by construction.
type peerConfigChannel struct {
	app          *App
	transport    peerConfigTransport
	receiver     *peerConfigSyncReceiver
	remotePeerID string

	mu              sync.Mutex
	maxBatchBytes   int
	outboundPlan    *configsync.AntiEntropyPlan
	outstanding     configsync.AntiEntropyBatch
	outstandingJSON []byte
}

func newPeerConfigChannel(app *App, transport peerConfigTransport) (*peerConfigChannel, error) {
	if app == nil || transport == nil {
		return nil, configsync.ErrInvalidAntiEntropy
	}
	remoteMembership, ok := transport.RemoteMembershipToken()
	if !ok || remoteMembership == "" {
		return nil, errPeerConfigSyncDenied
	}
	receiver, err := app.newPeerConfigSyncReceiver(remoteMembership)
	if err != nil {
		return nil, err
	}
	return &peerConfigChannel{
		app: app, transport: transport, receiver: receiver,
		remotePeerID:  receiver.remote.Document.SubjectPeerID,
		maxBatchBytes: configsync.DefaultAntiEntropyBatchSize,
	}, nil
}

// Start advertises the receiver's durable frontier. Both authenticated peers
// call Start, allowing each direction to converge independently.
func (c *peerConfigChannel) Start(ctx context.Context) error {
	inventory, err := c.localInventory()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("encode Peer config inventory: %w", err)
	}
	return c.transport.SendConfigMessage(ctx, peertransport.RecordConfigInventory, payload)
}

// Handle consumes one already-authenticated config logical message.
func (c *peerConfigChannel) Handle(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	if c == nil || c.transport == nil || len(payload) == 0 || len(payload) > peertransport.MaxConfigMessageSize {
		return configsync.ErrInvalidAntiEntropy
	}
	switch kind {
	case peertransport.RecordConfigInventory:
		var inventory configsync.AntiEntropyInventory
		if err := decodePeerConfigMessage(payload, &inventory); err != nil {
			return err
		}
		return c.handleInventory(ctx, inventory)
	case peertransport.RecordConfigBatch:
		var batch configsync.AntiEntropyBatch
		if err := decodePeerConfigMessage(payload, &batch); err != nil {
			return err
		}
		return c.handleBatch(ctx, batch)
	case peertransport.RecordConfigAck:
		var ack peerConfigWireAck
		if err := decodePeerConfigMessage(payload, &ack); err != nil {
			return err
		}
		return c.handleAck(ctx, ack)
	default:
		return configsync.ErrInvalidAntiEntropy
	}
}

// RetryOutstanding resends the exact last batch. The receiver accepts only an
// exact replay at the same cursor, so a lost durable ack cannot duplicate work.
func (c *peerConfigChannel) RetryOutstanding(ctx context.Context) error {
	c.mu.Lock()
	payload := append([]byte(nil), c.outstandingJSON...)
	c.mu.Unlock()
	if len(payload) == 0 {
		return nil
	}
	return c.transport.SendConfigMessage(ctx, peertransport.RecordConfigBatch, payload)
}

func (c *peerConfigChannel) HasOutstanding() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.outstandingJSON) != 0
}

func (c *peerConfigChannel) handleInventory(ctx context.Context, inventory configsync.AntiEntropyInventory) error {
	plan, err := c.planFor(inventory)
	if err != nil {
		return err
	}
	batch, err := plan.Next(nil)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode Peer config batch: %w", err)
	}
	c.mu.Lock()
	c.outboundPlan = plan
	c.outstanding = batch
	c.outstandingJSON = append(c.outstandingJSON[:0], payload...)
	c.mu.Unlock()
	return c.transport.SendConfigMessage(ctx, peertransport.RecordConfigBatch, payload)
}

func (c *peerConfigChannel) handleBatch(ctx context.Context, batch configsync.AntiEntropyBatch) error {
	c.mu.Lock()
	accepted, err := c.receiver.Add(batch)
	if errors.Is(err, configsync.ErrAntiEntropyOrder) && c.receiver.completed && batch.Start == (configsync.AntiEntropyCursor{}) {
		remoteMembership, ok := c.transport.RemoteMembershipToken()
		if !ok || remoteMembership == "" {
			c.mu.Unlock()
			return errPeerConfigSyncDenied
		}
		c.receiver, err = c.app.newPeerConfigSyncReceiver(remoteMembership)
		if err == nil {
			accepted, err = c.receiver.Add(batch)
		}
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	ack := peerConfigWireAck{
		V: peerConfigChannelVersion, SpaceID: accepted.Durable.SpaceID,
		Cursor: accepted.Cursor, Durable: accepted.Durable, Done: accepted.Done,
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("encode Peer config ack: %w", err)
	}
	if err := c.transport.SendConfigMessage(ctx, peertransport.RecordConfigAck, payload); err != nil {
		return err
	}
	if accepted.Done {
		return c.recordExchange(nil)
	}
	return nil
}

func (c *peerConfigChannel) handleAck(ctx context.Context, ack peerConfigWireAck) error {
	c.mu.Lock()
	if c.outboundPlan == nil || len(c.outstandingJSON) == 0 ||
		ack.V != peerConfigChannelVersion || ack.SpaceID == "" || ack.SpaceID != ack.Durable.SpaceID ||
		ack.SpaceID != c.outstanding.SpaceID || ack.Done != c.outstanding.Done ||
		ack.Cursor != acceptedAntiEntropyCursor(c.outstanding) {
		c.mu.Unlock()
		return configsync.ErrAntiEntropyOrder
	}
	if ack.Done {
		c.outboundPlan = nil
		c.outstanding = configsync.AntiEntropyBatch{}
		c.outstandingJSON = nil
		c.mu.Unlock()
		return c.recordExchange(ack.Durable.Vector)
	}
	next, err := c.outboundPlan.Next(&ack.Cursor)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("encode Peer config batch: %w", err)
	}
	c.outstanding = next
	c.outstandingJSON = append(c.outstandingJSON[:0], payload...)
	c.mu.Unlock()
	return c.transport.SendConfigMessage(ctx, peertransport.RecordConfigBatch, payload)
}

func (c *peerConfigChannel) recordExchange(acknowledged configsync.VersionVector) error {
	manager, err := c.app.peerManager()
	if err != nil {
		return err
	}
	if err := manager.store.RecordConfigExchange(c.remotePeerID, acknowledged.Clone(), manager.now()); err != nil {
		return err
	}
	runtime, err := manager.ensureConfigReplica()
	if err != nil {
		return err
	}
	_, err = runtime.pruneStableTombstones()
	return err
}

func (c *peerConfigChannel) localInventory() (configsync.AntiEntropyInventory, error) {
	manager, err := c.app.peerManager()
	if err != nil {
		return configsync.AntiEntropyInventory{}, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return configsync.AntiEntropyInventory{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return configsync.AntiEntropyInventory{}, err
	}
	runtime, err := manager.ensureConfigReplica()
	if err != nil {
		return configsync.AntiEntropyInventory{}, err
	}
	return configsync.BuildAntiEntropyInventory(
		genesis, runtime.replica.Vector(), state.Memberships, state.Revocations, state.EpochRotations,
	)
}

func (c *peerConfigChannel) planFor(remote configsync.AntiEntropyInventory) (*configsync.AntiEntropyPlan, error) {
	manager, err := c.app.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	runtime, err := manager.ensureConfigReplica()
	if err != nil {
		return nil, err
	}
	return runtime.replica.PlanAntiEntropy(
		genesis, state.Memberships, state.Revocations, state.EpochRotations, remote, c.maxBatchBytes,
	)
}

func decodePeerConfigMessage(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode Peer config message: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return configsync.ErrInvalidAntiEntropy
	}
	return nil
}
