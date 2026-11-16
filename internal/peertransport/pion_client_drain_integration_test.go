package peertransport

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestPionClientDrainFragmentedFrame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	auth, err := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x51}, accountKeySize))
	if err != nil {
		t.Fatal(err)
	}
	authorization := testAuthorization(time.Now().Add(time.Minute))
	authenticated := make(chan *PionClientChannel, 1)
	received := make(chan []byte, 1)
	var client *PionClientAttempt
	host, err := NewPionHostAttempt(ctx, PionHostConfig{
		Authorization: authorization, Authenticator: auth,
		SendSignal: func(kind, payload string) error { return client.HandleSignal(kind, payload) },
		OnRecord: func(kind RecordKind, payload []byte) {
			if kind == RecordFrame {
				received <- payload
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err = NewPionClientAttempt(ctx, PionClientConfig{
		Authorization: authorization, Authenticator: auth,
		SendSignal:      func(kind, payload string) error { return host.HandleSignal(kind, payload) },
		OnAuthenticated: func(channel *PionClientChannel) { authenticated <- channel },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	var channel *PionClientChannel
	select {
	case channel = <-authenticated:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	frame := bytes.Repeat([]byte("fragmented upload"), 128*1024)
	if err := channel.SendFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if err := channel.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if buffered := channel.dc.BufferedAmount(); buffered != 0 {
		t.Fatalf("drain returned with %d buffered bytes", buffered)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, frame) {
			t.Fatalf("received frame has %d bytes, want %d", len(got), len(frame))
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := channel.Drain(ctx); err == nil {
		t.Fatal("drained a closed Pion attempt")
	}
}
