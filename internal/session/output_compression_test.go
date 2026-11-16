package session

import (
	"bytes"
	"testing"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

func drainOutputFrames(sub *Subscriber) []proto.Frame {
	var frames []proto.Frame
	for {
		select {
		case f := <-sub.Out():
			if f.Type == proto.TypeOut || f.Type == proto.TypeOutCompressed {
				frames = append(frames, f)
			}
		default:
			return frames
		}
	}
}

func TestOpaqueReplayPreservesEnvelopeBoundaries(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.MarkContentOpaque()
	key := bytes.Repeat([]byte{0x42}, 32)
	for i, plaintext := range []string{"first-envelope", "second-envelope"} {
		seq := []uint64{7, 9}[i]
		envelope, err := e2eecrypto.SealOut(key, s.ID, byte(proto.TypeOut), seq, []byte(plaintext))
		if err != nil {
			t.Fatal(err)
		}
		s.PushOut(seq, envelope)
	}
	sub, seq := s.Subscribe(7, "client", "test")
	frames := drainOutputFrames(sub)
	if len(frames) != 1 || seq != 9 {
		t.Fatalf("replay frames=%d seq=%d; want one frame at 9", len(frames), seq)
	}
	s.Unsubscribe(sub)
	sub, _ = s.Subscribe(0, "client", "test")
	frames = drainOutputFrames(sub)
	var outputs []proto.Frame
	for _, f := range frames {
		seq, _, _ := proto.DecodeOut(f.Payload)
		if seq != 0 {
			outputs = append(outputs, f)
		}
	}
	if len(outputs) != 2 {
		t.Fatalf("opaque replay merged %d envelopes into %d frames", 2, len(outputs))
	}
	for i, want := range []string{"first-envelope", "second-envelope"} {
		seq, data, err := proto.DecodeOut(outputs[i].Payload)
		if err != nil || seq != []uint64{7, 9}[i] {
			t.Fatalf("frame %d: seq=%d err=%v", i, seq, err)
		}
		plaintext, err := e2eecrypto.OpenOut(key, s.ID, byte(proto.TypeOut), seq, data)
		if err != nil || string(plaintext) != want {
			t.Fatalf("replayed frame %d failed authentication or changed content: %v", i, err)
		}
	}
}

func TestOpaqueSlowSubscriberDisconnects(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.MarkContentOpaque()
	sub, _ := s.Subscribe(0, "slow", "test")
	for seq := uint64(1); seq <= subscriberQueueDepth+1; seq++ {
		s.PushOut(seq, []byte("sealed"))
	}
	select {
	case <-sub.Done():
	default:
		t.Fatal("opaque overflow must disconnect instead of emitting sliced ciphertext")
	}
}

func TestResetOutputDoesNotBecomeReplayHistory(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.PushOut(1, []byte("history"))
	s.PushOut(0, []byte("reset"))
	sub, seq := s.Subscribe(0, "client", "test")
	frames := drainOutputFrames(sub)
	if seq != 1 || len(frames) != 1 {
		t.Fatalf("seq=%d frames=%d; reset contaminated replay", seq, len(frames))
	}
	_, data, _ := proto.DecodeOut(frames[0].Payload)
	if !bytes.Equal(data, []byte("history")) {
		t.Fatalf("replay = %q, want history only", data)
	}
}

func waitLifecycle(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("lifecycle=%q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("no lifecycle callback %q", want)
	}
}

func TestOutputFormatsShareDriverAndIndependentLifecycles(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.SetOutputCompressionSupported(true)
	if !s.SupportsOutputCompression() {
		t.Fatal("publisher compression capability was lost")
	}
	ordinaryHooks, compressedHooks, aggregate := make(chan string, 4), make(chan string, 4), make(chan string, 4)
	s.SetOutputSubscriberLifecycle(false, func() { ordinaryHooks <- "first" }, func() { ordinaryHooks <- "last" })
	s.SetOutputSubscriberLifecycle(true, func() { compressedHooks <- "first" }, func() { compressedHooks <- "last" })
	s.SetSubscriberLifecycle(func() { aggregate <- "first" }, func() { aggregate <- "last" })
	ordinary, _ := s.Subscribe(0, "ordinary", "test")
	waitLifecycle(t, ordinaryHooks, "first")
	waitLifecycle(t, aggregate, "first")
	compressed, _ := s.Subscribe(0, "compressed", "test", WithCompressedOutput())
	waitLifecycle(t, compressedHooks, "first")
	if !s.IsDriver(ordinary) || s.IsDriver(compressed) || s.SubscriberCount() != 2 || s.OutputSubscriberCount(false) != 1 || s.OutputSubscriberCount(true) != 1 {
		t.Fatal("mixed output subscriptions must share one session driver and total count")
	}
	drainOutputFrames(ordinary)
	drainOutputFrames(compressed)
	s.PushOut(3, []byte("ordinary"))
	s.PushCompressedOut(8, []byte("encoded"))
	for _, tc := range []struct {
		sub      *Subscriber
		wantType proto.Type
		wantSeq  uint64
		want     string
	}{{ordinary, proto.TypeOut, 3, "ordinary"}, {compressed, proto.TypeOutCompressed, 8, "encoded"}} {
		frames := drainOutputFrames(tc.sub)
		if len(frames) != 1 || frames[0].Type != tc.wantType {
			t.Fatalf("output lane received frames=%v; want one type %v", frames, tc.wantType)
		}
		seq, data, _ := proto.DecodeOut(frames[0].Payload)
		if seq != tc.wantSeq || string(data) != tc.want {
			t.Fatalf("output lane seq=%d data=%q", seq, data)
		}
	}
	if s.LatestOutputSeq(false) != 3 || s.LatestOutputSeq(true) != 8 {
		t.Fatal("formats must retain independent reconnect cursors")
	}
	s.ClaimDriver(compressed, "compressed", "test")
	if !s.IsDriver(compressed) || s.IsDriver(ordinary) {
		t.Fatal("driver transfer must cross output formats")
	}
	s.Unsubscribe(ordinary)
	waitLifecycle(t, ordinaryHooks, "last")
	select {
	case event := <-aggregate:
		t.Fatalf("aggregate lifecycle fired early: %s", event)
	default:
	}
	s.Unsubscribe(compressed)
	waitLifecycle(t, compressedHooks, "last")
	waitLifecycle(t, aggregate, "last")
}

func TestCompressedReplayPreservesFramesAndCursor(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.SetOutputCompressionSupported(true)
	s.PushCompressedOut(1, []byte("first"))
	s.PushCompressedOut(6, []byte("\x1b]133;C;false\x07"))
	s.PushCompressedOut(0, []byte("reset"))
	if s.Info().TaskState != proto.TaskStateIdle || s.LatestOutputSeq(true) != 6 || s.LatestOutputSeq(false) != 0 {
		t.Fatal("encoded output changed semantic metadata or polluted another cursor")
	}
	sub, seq := s.Subscribe(0, "client", "test", WithCompressedOutput())
	frames := drainOutputFrames(sub)
	if len(frames) != 2 || seq != 6 {
		t.Fatalf("compressed replay frames=%d seq=%d", len(frames), seq)
	}
	for i, wantSeq := range []uint64{1, 6} {
		gotSeq, data, _ := proto.DecodeOut(frames[i].Payload)
		if gotSeq != wantSeq || frames[i].Type != proto.TypeOutCompressed || string(data) != []string{"first", "\x1b]133;C;false\x07"}[i] {
			t.Fatalf("compressed replay frame %d corrupted", i)
		}
	}
	s.Unsubscribe(sub)
	sub, seq = s.Subscribe(6, "client", "test", WithCompressedOutput())
	if frames = drainOutputFrames(sub); len(frames) != 0 || seq != 6 {
		t.Fatalf("up-to-date replay frames=%d seq=%d", len(frames), seq)
	}
}

func TestOpaqueReplayCapsFrameCountWithoutSplittingEnvelopes(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		s := newSessionForTest(t)
		s.MarkContentOpaque()
		s.SetOutputCompressionSupported(true)
		for seq := uint64(1); seq <= subscriberQueueDepth*2; seq++ {
			if compressed {
				s.PushCompressedOut(seq, []byte("ciphertext"))
			} else {
				s.PushOut(seq, []byte("ciphertext"))
			}
		}
		var opts []SubscribeOption
		if compressed {
			opts = append(opts, WithCompressedOutput())
		}
		sub, seq := s.Subscribe(1, "client", "test", opts...)
		select {
		case <-sub.Done():
			t.Fatalf("bounded opaque replay disconnected, compressed=%v", compressed)
		default:
		}
		frames := drainOutputFrames(sub)
		if len(frames) < 2 || len(frames) >= subscriberQueueDepth || seq != subscriberQueueDepth*2 {
			t.Fatalf("opaque replay frames=%d seq=%d", len(frames), seq)
		}
		resetSeq, _, _ := proto.DecodeOut(frames[0].Payload)
		if resetSeq != 0 || frames[0].Type != proto.TypeOut {
			t.Fatal("truncated opaque replay needs synthetic plaintext reset")
		}
		for _, f := range frames[1:] {
			_, data, _ := proto.DecodeOut(f.Payload)
			if string(data) != "ciphertext" {
				t.Fatal("opaque replay modified an envelope")
			}
		}
		s.Close()
	}
}

func TestReplacingSubscriberSwitchesOutputFormatWithoutAggregateRestart(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.SetOutputCompressionSupported(true)
	ordinaryHooks, compressedHooks, aggregate := make(chan string, 4), make(chan string, 4), make(chan string, 4)
	s.SetOutputSubscriberLifecycle(false, func() { ordinaryHooks <- "first" }, func() { ordinaryHooks <- "last" })
	s.SetOutputSubscriberLifecycle(true, func() { compressedHooks <- "first" }, func() { compressedHooks <- "last" })
	s.SetSubscriberLifecycle(func() { aggregate <- "first" }, func() { aggregate <- "last" })
	old, _ := s.Subscribe(0, "old", "test")
	waitLifecycle(t, ordinaryHooks, "first")
	waitLifecycle(t, aggregate, "first")
	next, _ := s.Subscribe(0, "next", "test", WithCompressedOutput(), ReplacingSubscriber(old))
	waitLifecycle(t, ordinaryHooks, "last")
	waitLifecycle(t, compressedHooks, "first")
	if !s.IsDriver(next) || s.SubscriberCount() != 1 || s.OutputSubscriberCount(false) != 0 || s.OutputSubscriberCount(true) != 1 {
		t.Fatal("replacement must transfer the driver and output demand atomically")
	}
	select {
	case <-old.Done():
	default:
		t.Fatal("replaced subscription stayed open")
	}
	s.Unsubscribe(old)
	select {
	case event := <-aggregate:
		t.Fatalf("replacement restarted aggregate stream: %s", event)
	case event := <-ordinaryHooks:
		t.Fatalf("removing replaced subscriber repeated ordinary transition: %s", event)
	default:
	}
}

func TestRevokingCompressionDisconnectsOnlyCompressedViewers(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.SetOutputCompressionSupported(true)
	ordinary, _ := s.Subscribe(0, "ordinary", "test")
	compressed, _ := s.Subscribe(0, "compressed", "test", WithCompressedOutput())
	lastCompressed := make(chan string, 2)
	s.SetOutputSubscriberLifecycle(true, nil, func() { lastCompressed <- "last" })
	aggregate := make(chan string, 2)
	s.SetSubscriberLifecycle(nil, func() { aggregate <- "last" })
	s.SetOutputCompressionSupported(false)
	if s.SupportsOutputCompression() {
		t.Fatal("compression capability remained enabled")
	}
	select {
	case <-compressed.Done():
	default:
		t.Fatal("compressed viewer remained attached after capability withdrawal")
	}
	if s.SubscriberCount() != 1 || s.OutputSubscriberCount(true) != 0 || !s.IsDriver(ordinary) {
		t.Fatal("capability withdrawal changed the ordinary viewer or its driver")
	}
	waitLifecycle(t, lastCompressed, "last")
	select {
	case <-ordinary.Done():
		t.Fatal("ordinary viewer disconnected on compression withdrawal")
	case event := <-aggregate:
		t.Fatalf("aggregate lifecycle stopped with an ordinary viewer attached: %s", event)
	default:
	}
	// Negotiation and Subscribe are separate calls: a connection that read the
	// old capability before withdrawal must not attach to a now inactive lane.
	stale, _ := s.Subscribe(0, "stale", "test", WithCompressedOutput())
	select {
	case <-stale.Done():
	default:
		t.Fatal("stale capability negotiation attached to an inactive output lane")
	}
	if s.SubscriberCount() != 1 {
		t.Fatal("stale negotiation changed the subscriber count")
	}
}

func TestPreserveOutputFramesKeepsPlaintextReplayBoundaries(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	s.PushOut(1, []byte("A"))
	s.PushOut(2, []byte("B"))
	sub, seq := s.Subscribe(0, "publisher", "test", PreserveOutputFrames())
	frames := drainOutputFrames(sub)
	if len(frames) != 2 || seq != 2 {
		t.Fatalf("canonical replay frames=%d seq=%d, want two original frames through 2", len(frames), seq)
	}
	for i, frame := range frames {
		seq, data, err := proto.DecodeOut(frame.Payload)
		if err != nil || frame.Type != proto.TypeOut || seq != []uint64{1, 2}[i] || string(data) != []string{"A", "B"}[i] {
			t.Fatalf("source boundary %d changed: seq=%d data=%q err=%v", i, seq, data, err)
		}
	}
}

func TestPreserveOutputFramesResyncsWithWholeSourceChunks(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"frame-budget", 1}, {"byte-budget", 100}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSessionForTest(t)
			defer s.Close()
			sub, _ := s.Subscribe(0, "publisher", "test", PreserveOutputFrames())
			drainOutputFrames(sub)
			// Tiny chunks exercise the frame budget; the larger fixture crosses
			// the byte budget between source chunks and must not split one.
			for seq := uint64(1); seq <= subscriberQueueDepth+1; seq++ {
				s.PushOut(seq, bytes.Repeat([]byte{byte('A' + seq%26)}, tc.size))
			}
			if s.SubscriberCount() != 1 || !s.IsDriver(sub) {
				t.Fatal("plaintext source resync disconnected the publisher or lost its driver")
			}
			frames := drainOutputFrames(sub)
			if len(frames) < 2 || len(frames) >= subscriberQueueDepth {
				t.Fatalf("resync queued %d frames, want a bounded whole-frame tail", len(frames))
			}
			seq, _, err := proto.DecodeOut(frames[0].Payload)
			if err != nil || seq != 0 {
				t.Fatal("canonical resync must begin with the synthetic reset marker")
			}
			var previous uint64
			var totalBytes int
			for _, frame := range frames[1:] {
				seq, data, err := proto.DecodeOut(frame.Payload)
				want := bytes.Repeat([]byte{byte('A' + seq%26)}, tc.size)
				if err != nil || frame.Type != proto.TypeOut || seq == 0 || !bytes.Equal(data, want) {
					t.Fatalf("source chunk was combined or sliced: seq=%d bytes=%d err=%v", seq, len(data), err)
				}
				if previous != 0 && seq != previous+1 {
					t.Fatalf("resync skipped a source chunk: %d -> %d", previous, seq)
				}
				previous = seq
				totalBytes += len(data)
			}
			if totalBytes > resyncTailBytes || previous != subscriberQueueDepth+1 || s.LatestOutputSeq(false) != subscriberQueueDepth+1 {
				t.Fatal("resync did not retain a bounded tail through the newest source cursor")
			}
		})
	}
}
