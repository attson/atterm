package quicktunnel

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peertransport"
	"github.com/google/uuid"
)

func TestWSSFallbackCarriesEncryptedTerminalAndConfigRecords(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hostAuthenticated := make(chan *WSSChannel, 1)
	hostRecords := make(chan wssTestRecord, 2)
	clientRecords := make(chan wssTestRecord, 2)
	hostConfig := make(chan wssTestRecord, 1)
	bindErrors := make(chan error, 1)
	var hostTraffic, clientTraffic wssTrafficTotals
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnAuthenticated: func(signal *SignalChannel) {
			if err := signal.BindWSSFallback(WSSFallbackConfig{
				OnAuthenticated: func(channel *WSSChannel) { hostAuthenticated <- channel },
				OnRecord: func(kind peertransport.RecordKind, payload []byte) {
					hostRecords <- wssTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
				},
				OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
					hostConfig <- wssTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
					return nil
				},
				OnTraffic: hostTraffic.observe,
			}); err != nil {
				bindErrors <- err
			}
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	clientSignal, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientSignal.Close()
	if err := clientSignal.BindWSSFallback(WSSFallbackConfig{
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			clientRecords <- wssTestRecord{kind: kind, payload: append([]byte(nil), payload...)}
		},
		OnTraffic: clientTraffic.observe,
	}); err != nil {
		t.Fatalf("bind client fallback: %v", err)
	}
	client, err := clientSignal.StartWSSFallback(ctx)
	if err != nil {
		t.Fatalf("start WSS fallback: %v", err)
	}
	var host *WSSChannel
	select {
	case err := <-bindErrors:
		t.Fatalf("bind host fallback: %v", err)
	case host = <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host did not activate WSS fallback")
	}

	if token, ok := client.RemoteMembershipToken(); !ok || token != peers.hostMembership {
		t.Fatal("client fallback lost authenticated host membership")
	}
	if token, ok := host.RemoteMembershipToken(); !ok || token != peers.clientMembership {
		t.Fatal("host fallback lost authenticated client membership")
	}
	if err := client.SendRecord(ctx, peertransport.RecordPing, []byte("client-control")); err != nil {
		t.Fatal(err)
	}
	if got := receiveWSSRecord(t, ctx, hostRecords); got.kind != peertransport.RecordPing || !bytes.Equal(got.payload, []byte("client-control")) {
		t.Fatalf("host record = %+v", got)
	}
	largeFrame := bytes.Repeat([]byte("terminal-output"), 4096)
	if err := host.SendFrame(ctx, largeFrame); err != nil {
		t.Fatal(err)
	}
	if got := receiveWSSRecord(t, ctx, clientRecords); got.kind != peertransport.RecordFrame || !bytes.Equal(got.payload, largeFrame) {
		t.Fatalf("client terminal record kind=%d size=%d", got.kind, len(got.payload))
	}
	largeConfig := bytes.Repeat([]byte("config"), 4096)
	if err := client.SendConfigMessage(ctx, peertransport.RecordConfigInventory, largeConfig); err != nil {
		t.Fatal(err)
	}
	if got := receiveWSSRecord(t, ctx, hostConfig); got.kind != peertransport.RecordConfigInventory || !bytes.Equal(got.payload, largeConfig) {
		t.Fatalf("host config record kind=%d size=%d", got.kind, len(got.payload))
	}
	servicePayload, err := peertransport.EncodeServiceMessage(peertransport.ServiceMessage{
		ServiceID: uuid.New(), Kind: peertransport.ServiceData, Connection: 1, Data: []byte("preview"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendRecord(ctx, peertransport.RecordService, servicePayload); !errors.Is(err, peertransport.ErrDirectTransport) {
		t.Fatalf("generic service send error=%v, want ErrDirectTransport", err)
	}
	if err := client.SendServiceMessage(ctx, servicePayload); err != nil {
		t.Fatal(err)
	}
	if got := receiveWSSRecord(t, ctx, hostRecords); got.kind != peertransport.RecordService || !bytes.Equal(got.payload, servicePayload) {
		t.Fatalf("host service record kind=%d payload=%x", got.kind, got.payload)
	}
	frameFragments, err := peertransport.FragmentFrame(1, largeFrame)
	if err != nil {
		t.Fatal(err)
	}
	configFragments, err := peertransport.FragmentConfigMessage(peertransport.RecordConfigInventory, 1, largeConfig)
	if err != nil {
		t.Fatal(err)
	}
	clientSent := wssExpectedTraffic([][]byte{[]byte("client-control"), servicePayload}, configFragments)
	hostSent := wssExpectedTraffic(nil, frameFragments)
	if got := clientTraffic.snapshot(); got.sentBytes != clientSent.bytes || got.sentRecords != clientSent.records ||
		got.receivedBytes != hostSent.bytes || got.receivedRecords != hostSent.records {
		t.Fatalf("client WSS traffic=%+v want sent=%+v received=%+v", got, clientSent, hostSent)
	}
	if got := hostTraffic.snapshot(); got.sentBytes != hostSent.bytes || got.sentRecords != hostSent.records ||
		got.receivedBytes != clientSent.bytes || got.receivedRecords != clientSent.records {
		t.Fatalf("host WSS traffic=%+v want sent=%+v received=%+v", got, hostSent, clientSent)
	}
	if err := clientSignal.SendSignal(ctx, Signal{Type: SignalICEEnd}); !errors.Is(err, peertransport.ErrDirectTransport) {
		t.Fatalf("signal after fallback error = %v, want ErrDirectTransport", err)
	}
}

func TestWSSWriterPrioritizesControlThenTerminalThenServiceThenConfig(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstWrite := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})
	writer := newWSSWriter(done, func(_ context.Context, _ peertransport.RecordKind, payload []byte) error {
		value := string(payload)
		mu.Lock()
		order = append(order, value)
		mu.Unlock()
		if value == "config-1" {
			once.Do(func() { close(firstWrite) })
			<-releaseFirst
		}
		return nil
	})
	go writer.run()
	defer close(done)

	configDone, err := writer.enqueue(ctx, wssPriorityConfig, []wssPlainRecord{
		{kind: peertransport.RecordConfigFragment, payload: []byte("config-1")},
		{kind: peertransport.RecordConfigFragment, payload: []byte("config-2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstWrite:
	case <-ctx.Done():
		t.Fatal("config write did not start")
	}
	terminalDone, err := writer.enqueue(ctx, wssPriorityTerminal, []wssPlainRecord{{
		kind: peertransport.RecordFrame, payload: []byte("terminal"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	controlDone, err := writer.enqueue(ctx, wssPriorityControl, []wssPlainRecord{{
		kind: peertransport.RecordPing, payload: []byte("control"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	serviceDone, err := writer.enqueue(ctx, wssPriorityService, []wssPlainRecord{{
		kind: peertransport.RecordService, payload: []byte("service"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	secondConfigDone, err := writer.enqueue(ctx, wssPriorityConfig, []wssPlainRecord{{
		kind: peertransport.RecordConfigInventory, payload: []byte("config-other"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	close(releaseFirst)
	for _, result := range []<-chan error{configDone, terminalDone, controlDone, serviceDone, secondConfigDone} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("priority writer did not drain")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"config-1", "control", "terminal", "service", "config-2", "config-other"}
	if len(order) != len(want) {
		t.Fatalf("write order = %v", order)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("write order = %v, want %v", order, want)
		}
	}
}

type wssTestRecord struct {
	kind    peertransport.RecordKind
	payload []byte
}

type wssTrafficTotals struct {
	mu              sync.Mutex
	sentBytes       int
	receivedBytes   int
	sentRecords     int
	receivedRecords int
}

func (t *wssTrafficTotals) observe(direction peertransport.TrafficDirection, size int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if direction == peertransport.TrafficSent {
		t.sentBytes += size
		t.sentRecords++
	} else {
		t.receivedBytes += size
		t.receivedRecords++
	}
}

func (t *wssTrafficTotals) snapshot() wssTrafficSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return wssTrafficSnapshot{
		sentBytes: t.sentBytes, receivedBytes: t.receivedBytes,
		sentRecords: t.sentRecords, receivedRecords: t.receivedRecords,
	}
}

type wssTrafficSnapshot struct {
	sentBytes       int
	receivedBytes   int
	sentRecords     int
	receivedRecords int
}

type wssTrafficExpectation struct {
	bytes   int
	records int
}

func wssExpectedTraffic(plain [][]byte, fragmented [][]byte) wssTrafficExpectation {
	const encryptedRecordOverhead = 30
	total := wssTrafficExpectation{records: len(plain) + len(fragmented)}
	for _, payload := range append(plain, fragmented...) {
		total.bytes += encryptedRecordOverhead + len(payload)
	}
	return total
}

func receiveWSSRecord(t *testing.T, ctx context.Context, records <-chan wssTestRecord) wssTestRecord {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-ctx.Done():
		t.Fatal("timed out waiting for WSS fallback record")
		return wssTestRecord{}
	}
}
