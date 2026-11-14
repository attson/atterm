package main

import (
	"context"
	"errors"
	"testing"

	"github.com/attson/atterm/internal/peertransport"
)

type peerNativeDrainTestChannel struct {
	peerNativeTestChannel
	drain func(context.Context) error
}

func (c *peerNativeDrainTestChannel) Drain(ctx context.Context) error { return c.drain(ctx) }

func TestDrainNativeDirectRejectsUnavailableChannels(t *testing.T) {
	app := &App{nativeDirect: map[string]*nativeDirectClient{
		"pending": {ctx: context.Background()},
		"stopped": {ctx: context.Background(), authenticated: true, stopped: true},
		"invalid": {ctx: context.Background(), authenticated: true, channel: &peertransport.PionClientChannel{}},
	}}
	for _, id := range []string{"missing", "pending", "stopped", "invalid"} {
		if err := app.DrainNativeDirect(id); err == nil {
			t.Fatalf("drained unavailable native channel %q", id)
		}
	}
}

func TestDrainPeerNativeDirectDelegatesAndPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	channel := &peerNativeDrainTestChannel{drain: func(drainCtx context.Context) error {
		close(entered)
		<-drainCtx.Done()
		return drainCtx.Err()
	}}
	app := &App{peerNative: map[string]*peerNativeDirectClient{
		"active": {ctx: ctx, authenticated: true, channel: channel},
	}}
	done := make(chan error, 1)
	go func() { done <- app.DrainPeerNativeDirect("active") }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("drain cancellation=%v", err)
	}
}

func TestDrainPeerNativeDirectHandlesSynchronousWSSAndUnavailableChannels(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	app := &App{peerNative: map[string]*peerNativeDirectClient{
		"wss":      {ctx: context.Background(), authenticated: true, channel: &peerNativeTestChannel{}},
		"pending":  {ctx: context.Background()},
		"stopped":  {ctx: context.Background(), authenticated: true, stopped: true, channel: &peerNativeTestChannel{}},
		"canceled": {ctx: canceled, authenticated: true, channel: &peerNativeTestChannel{}},
	}}
	if err := app.DrainPeerNativeDirect("wss"); err != nil {
		t.Fatalf("synchronous WSS drain=%v", err)
	}
	for _, id := range []string{"missing", "pending", "stopped", "canceled"} {
		if err := app.DrainPeerNativeDirect(id); err == nil {
			t.Fatalf("drained unavailable Peer channel %q", id)
		}
	}
}
