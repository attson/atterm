package session

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestUpdateSizeChangesSessionInfo(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	s.UpdateSize(132, 43)

	info := s.Info()
	if info.Cols != 132 || info.Rows != 43 {
		t.Fatalf("size = %dx%d; want 132x43", info.Cols, info.Rows)
	}
}

func TestPushOutTracksTaskLifecycleFromOSC133(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	if changed := s.PushOut(1, []byte("\x1b]133;C;go test ./...\x07")); !changed {
		t.Fatalf("PushOut(C) changed = false; want true")
	}
	info := s.Info()
	if info.TaskState != proto.TaskStateRunning {
		t.Fatalf("TaskState after C = %q; want running", info.TaskState)
	}
	if info.CurrentCommand != "go test ./..." {
		t.Fatalf("CurrentCommand = %q; want go test ./...", info.CurrentCommand)
	}
	if info.CommandStartedAt == 0 {
		t.Fatalf("CommandStartedAt = 0; want unix timestamp")
	}
	if info.CommandEndedAt != 0 {
		t.Fatalf("CommandEndedAt = %d; want 0 while running", info.CommandEndedAt)
	}
	if info.CommandExitCode != nil {
		t.Fatalf("CommandExitCode = %v; want nil while running", *info.CommandExitCode)
	}

	if changed := s.PushOut(2, []byte("ok\n\x1b]133;D;0\x07")); !changed {
		t.Fatalf("PushOut(D) changed = false; want true")
	}
	info = s.Info()
	if info.TaskState != proto.TaskStateCompleted {
		t.Fatalf("TaskState after D;0 = %q; want completed", info.TaskState)
	}
	if info.CurrentCommand != "go test ./..." {
		t.Fatalf("CurrentCommand after finish = %q; want go test ./...", info.CurrentCommand)
	}
	if info.CommandEndedAt == 0 {
		t.Fatalf("CommandEndedAt = 0; want unix timestamp")
	}
	if info.CommandDurationMS < 0 {
		t.Fatalf("CommandDurationMS = %d; want non-negative", info.CommandDurationMS)
	}
	if info.CommandExitCode == nil || *info.CommandExitCode != 0 {
		t.Fatalf("CommandExitCode = %v; want 0", info.CommandExitCode)
	}
}

func TestPushOutDetectsWaitingInputAndLastOutput(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	before := time.Now().Unix()
	if changed := s.PushOut(1, []byte("Proceed? [y/N]")); !changed {
		t.Fatalf("PushOut(waiting prompt) changed = false; want true")
	}
	info := s.Info()
	if info.TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("TaskState = %q; want waiting_input", info.TaskState)
	}
	if info.LastOutputAt < before {
		t.Fatalf("LastOutputAt = %d; want >= %d", info.LastOutputAt, before)
	}

	if changed := s.PushOut(2, []byte("\x1b]133;C;npm test\x07")); !changed {
		t.Fatalf("PushOut(C) changed = false; want true")
	}
	info = s.Info()
	if info.TaskState != proto.TaskStateRunning {
		t.Fatalf("TaskState after C = %q; want running", info.TaskState)
	}
	if info.CurrentCommand != "npm test" {
		t.Fatalf("CurrentCommand = %q; want npm test", info.CurrentCommand)
	}
}

func TestSubscribeIncludesTaskStateInMeta(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.PushOut(1, []byte("\x1b]133;C;codex\x07"))

	sub, _ := s.Subscribe(0, "", "")
	defer s.Unsubscribe(sub)

	f := readFrameForTest(t, sub)
	for f.Type != proto.TypeMeta {
		f = readFrameForTest(t, sub)
	}
	if f.Type != proto.TypeMeta {
		t.Fatalf("frame type = 0x%02x; want META", f.Type)
	}
	var meta proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &meta); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if meta.TaskState != proto.TaskStateRunning {
		t.Fatalf("meta.TaskState = %q; want running", meta.TaskState)
	}
	if meta.CurrentCommand != "codex" {
		t.Fatalf("meta.CurrentCommand = %q; want codex", meta.CurrentCommand)
	}
}

func TestSubscribeEmitsReplayProgress(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	first := []byte("first")
	second := make([]byte, replayProgressIntervalBytes)
	s.PushOut(1, first)
	s.PushOut(2, second)

	sub, _ := s.Subscribe(0, "", "")
	defer s.Unsubscribe(sub)

	start := readFrameForTest(t, sub)
	if start.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", start.Type)
	}
	startProgress := decodeProgressForTest(t, start)
	total := uint64(len(first) + len(second))
	if startProgress.Phase != proto.ReplayProgressStart || startProgress.Bytes != 0 || startProgress.TotalBytes != total {
		t.Fatalf("start progress = %+v; want phase=start bytes=0 total=%d", startProgress, total)
	}

	if got := readFrameForTest(t, sub); got.Type != proto.TypeOut {
		t.Fatalf("second frame type = 0x%02x; want OUT", got.Type)
	}
	if got := readFrameForTest(t, sub); got.Type != proto.TypeOut {
		t.Fatalf("third frame type = 0x%02x; want OUT", got.Type)
	}

	chunk := readFrameForTest(t, sub)
	if chunk.Type != proto.TypeReplayProgress {
		t.Fatalf("fourth frame type = 0x%02x; want REPLAY_PROGRESS", chunk.Type)
	}
	chunkProgress := decodeProgressForTest(t, chunk)
	if chunkProgress.Phase != proto.ReplayProgressChunk || chunkProgress.Bytes != total || chunkProgress.TotalBytes != total || chunkProgress.Seq != 2 {
		t.Fatalf("chunk progress = %+v; want phase=chunk bytes=%d total=%d seq=2", chunkProgress, total, total)
	}

	end := readFrameForTest(t, sub)
	if end.Type != proto.TypeReplayProgress {
		t.Fatalf("fifth frame type = 0x%02x; want REPLAY_PROGRESS", end.Type)
	}
	endProgress := decodeProgressForTest(t, end)
	if endProgress.Phase != proto.ReplayProgressEnd || endProgress.Bytes != total || endProgress.TotalBytes != total || endProgress.Seq != 2 {
		t.Fatalf("end progress = %+v; want phase=end bytes=%d total=%d seq=2", endProgress, total, total)
	}
}

func TestSubscribeFromStartMarksTruncatedScrollback(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	large := make([]byte, scrollbackBytes/2+1)
	s.PushOut(1, large)
	s.PushOut(2, large)
	s.PushOut(3, large)
	if oldest := s.scroll.OldestSeq(); oldest <= 1 {
		t.Fatalf("oldest seq = %d; want scrollback truncated past seq 1", oldest)
	}

	sub, _ := s.Subscribe(0, "", "")
	defer s.Unsubscribe(sub)

	if got := readFrameForTest(t, sub); got.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", got.Type)
	}
	reset := readFrameForTest(t, sub)
	if reset.Type != proto.TypeOut {
		t.Fatalf("second frame type = 0x%02x; want OUT reset marker", reset.Type)
	}
	seq, data, err := proto.DecodeOut(reset.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 0 || string(data) != "\x1b[2J\x1b[H" {
		t.Fatalf("reset marker = seq %d data %q; want seq 0 clear/home", seq, data)
	}
}

func TestSubscribeTruncatedAltScreenReplayRestoresAltScreenMode(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	s.PushOut(1, []byte("\x1b[?1049h"))
	large := make([]byte, scrollbackBytes/2+1)
	s.PushOut(2, large)
	s.PushOut(3, large)
	s.PushOut(4, large)
	if oldest := s.scroll.OldestSeq(); oldest <= 1 {
		t.Fatalf("oldest seq = %d; want scrollback truncated past alt-screen enter", oldest)
	}

	sub, _ := s.Subscribe(0, "", "")
	defer s.Unsubscribe(sub)

	if got := readFrameForTest(t, sub); got.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", got.Type)
	}
	reset := readFrameForTest(t, sub)
	seq, data, err := proto.DecodeOut(reset.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 0 || string(data) != "\x1b[?1049h\x1b[2J\x1b[H" {
		t.Fatalf("reset marker = seq %d data %q; want seq 0 alt-screen clear/home", seq, data)
	}
}

func TestAltScreenTrackingHandlesSplitEscapeSequence(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})

	s.PushOut(1, []byte("\x1b[?"))
	s.PushOut(2, []byte("10"))
	s.PushOut(3, []byte("49h"))
	if !s.altScreen {
		t.Fatal("altScreen = false; want true after split ?1049h")
	}
	s.PushOut(4, []byte("\x1b[?1049l"))
	if s.altScreen {
		t.Fatal("altScreen = true; want false after ?1049l")
	}
}

func TestSubscribeManySmallChunksCoalescesIntoBatches(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	chunkCount := subscriberQueueDepth + 1000
	for i := 0; i < chunkCount; i++ {
		s.PushOut(uint64(i+1), []byte{'a'})
	}
	sub, replayToSeq := s.Subscribe(0, "", "")
	defer s.Unsubscribe(sub)

	select {
	case <-sub.Done():
		t.Fatal("Subscribe closed sub even though replay should now coalesce")
	default:
	}
	if replayToSeq != uint64(chunkCount) {
		t.Fatalf("replayToSeq = %d; want %d (every chunk replayed)", replayToSeq, chunkCount)
	}

	var (
		outBytes  int
		outFrames int
		sawEnd    bool
	)
	timeout := time.After(time.Second)
	for !sawEnd {
		select {
		case f := <-sub.Out():
			switch f.Type {
			case proto.TypeOut:
				_, data, err := proto.DecodeOut(f.Payload)
				if err != nil {
					t.Fatalf("decode OUT: %v", err)
				}
				outBytes += len(data)
				outFrames++
			case proto.TypeReplayProgress:
				p := decodeProgressForTest(t, f)
				if p.Phase == proto.ReplayProgressEnd {
					sawEnd = true
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for replay end (got %d OUT frames, %d bytes)", outFrames, outBytes)
		}
	}
	if outBytes != chunkCount {
		t.Fatalf("replayed bytes = %d; want %d", outBytes, chunkCount)
	}
	// 16 KiB batches over 5096 bytes should land in well under chunkCount/subscriberQueueDepth frames.
	if outFrames >= chunkCount/4 {
		t.Fatalf("OUT frame count = %d; coalescing should have collapsed it to a small constant (chunkCount=%d)", outFrames, chunkCount)
	}
}

func readFrameForTest(t *testing.T, sub *Subscriber) proto.Frame {
	t.Helper()
	select {
	case f := <-sub.Out():
		return f
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber frame")
		return proto.Frame{}
	}
}

func decodeProgressForTest(t *testing.T, f proto.Frame) proto.ReplayProgressPayload {
	t.Helper()
	var p proto.ReplayProgressPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubscribeAutoPromotesFirstSubscriberToDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	sub, _ := s.Subscribe(0, "client-alpha", "alpha-host")
	defer s.Unsubscribe(sub)

	if !s.IsDriver(sub) {
		t.Fatal("first subscriber should be driver")
	}
	if got := s.DriverClientID(); got != "client-alpha" {
		t.Fatalf("driver client id = %q; want %q", got, "client-alpha")
	}
}

func TestSubscribeSecondSubscriberIsViewer(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	first, _ := s.Subscribe(0, "client-alpha", "alpha-host")
	defer s.Unsubscribe(first)
	second, _ := s.Subscribe(0, "client-beta", "beta-host")
	defer s.Unsubscribe(second)

	if !s.IsDriver(first) {
		t.Fatal("first subscriber should remain driver after second attaches")
	}
	if s.IsDriver(second) {
		t.Fatal("second subscriber should be viewer")
	}
}

func TestClaimDriverTransfersAndBroadcastsMeta(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})

	first, _ := s.Subscribe(0, "client-alpha", "alpha-host")
	defer s.Unsubscribe(first)
	drainInitialFrames(t, first)

	second, _ := s.Subscribe(0, "client-beta", "beta-host")
	defer s.Unsubscribe(second)
	drainInitialFrames(t, second)

	s.ClaimDriver(second, "client-beta", "beta-host")

	if !s.IsDriver(second) {
		t.Fatal("second should now be driver after ClaimDriver")
	}
	if s.IsDriver(first) {
		t.Fatal("first should no longer be driver")
	}
	if got := s.DriverClientID(); got != "client-beta" {
		t.Fatalf("DriverClientID = %q; want client-beta", got)
	}

	for label, sub := range map[string]*Subscriber{"first": first, "second": second} {
		f := readFrameForTest(t, sub)
		if f.Type != proto.TypeMeta {
			t.Fatalf("%s: next frame type = 0x%02x; want META (0x05)", label, f.Type)
		}
		var meta proto.MetaPayload
		if err := json.Unmarshal(f.Payload, &meta); err != nil {
			t.Fatalf("%s: meta unmarshal: %v", label, err)
		}
		if meta.DriverClientID != "client-beta" {
			t.Fatalf("%s: meta.DriverClientID = %q; want client-beta", label, meta.DriverClientID)
		}
		if meta.Cols != 80 || meta.Rows != 24 {
			t.Fatalf("%s: meta.Cols/Rows = %dx%d; want 80x24", label, meta.Cols, meta.Rows)
		}
	}
}

// drainInitialFrames consumes the frames Subscribe enqueues for a new
// subscriber: REPLAY_PROGRESS start, REPLAY_PROGRESS end, and one snapshot
// META carrying the current driver_client_id and PTY cols/rows.
func drainInitialFrames(t *testing.T, sub *Subscriber) {
	t.Helper()
	for i := 0; i < 2; i++ {
		select {
		case f := <-sub.Out():
			if f.Type != proto.TypeReplayProgress {
				t.Fatalf("frame %d type = 0x%02x; want REPLAY_PROGRESS", i, f.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout draining replay progress")
		}
	}
	select {
	case f := <-sub.Out():
		if f.Type != proto.TypeMeta {
			t.Fatalf("3rd initial frame type = 0x%02x; want META snapshot", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout draining snapshot META")
	}
}

func TestRemoveDriverSubscriberClearsAndBroadcasts(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	first, _ := s.Subscribe(0, "client-alpha", "alpha-host")
	drainInitialFrames(t, first)

	second, _ := s.Subscribe(0, "client-beta", "beta-host")
	defer s.Unsubscribe(second)
	drainInitialFrames(t, second)

	if !s.IsDriver(first) {
		t.Fatal("first should be driver before unsubscribe")
	}

	s.Unsubscribe(first)

	if s.IsDriver(first) {
		t.Fatal("driver flag should clear after unsubscribe")
	}
	if got := s.DriverClientID(); got != "" {
		t.Fatalf("DriverClientID after driver unsub = %q; want empty", got)
	}

	f := readFrameForTest(t, second)
	if f.Type != proto.TypeMeta {
		t.Fatalf("next frame type = 0x%02x; want META", f.Type)
	}
	var meta proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &meta); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if meta.DriverClientID != "" {
		t.Fatalf("meta.DriverClientID after driver unsub = %q; want empty", meta.DriverClientID)
	}
}

func TestUpdateMetaBroadcastsPreservesDriverState(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	sub, _ := s.Subscribe(0, "client-alpha", "alpha-host")
	defer s.Unsubscribe(sub)
	drainInitialFrames(t, sub) // 2 progress + 1 snapshot META

	// Trigger a cwd change (mimics watchCwd detecting `cd /tmp`).
	s.UpdateMeta(proto.MetaPayload{Cwd: "/tmp"})

	// Subscriber should receive a META frame with the new cwd AND the
	// existing driver_client_id/driver_client_name still set — otherwise
	// the client thinks "no driver" and renders the viewer overlay.
	f := readFrameForTest(t, sub)
	if f.Type != proto.TypeMeta {
		t.Fatalf("frame type = 0x%02x; want META", f.Type)
	}
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if m.Cwd != "/tmp" {
		t.Fatalf("meta.Cwd = %q; want /tmp", m.Cwd)
	}
	if m.DriverClientID != "client-alpha" {
		t.Fatalf("meta.DriverClientID = %q; want client-alpha (was clobbered by lite META)", m.DriverClientID)
	}
	if m.DriverClientName != "alpha-host" {
		t.Fatalf("meta.DriverClientName = %q; want alpha-host", m.DriverClientName)
	}
}

// BenchmarkFanoutHotPath measures the per-PushOut cost when several subscribers
// are draining quickly. The key win from the sync.Pool refactor is zero allocs
// on the snapshot slice; previously every call allocated len(s.subs)*8 bytes.
func BenchmarkFanoutHotPath(b *testing.B) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	const nSubs = 4
	subs := make([]*Subscriber, nSubs)
	for i := range subs {
		sub, _ := s.Subscribe(0, "", "")
		subs[i] = sub
		go func(out <-chan proto.Frame, done <-chan struct{}) {
			for {
				select {
				case <-out:
				case <-done:
					return
				}
			}
		}(sub.Out(), sub.Done())
	}

	payload := []byte("x")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.PushOut(uint64(i+1), payload)
	}
}

func TestSubscriberCountHookFiresOnAttachAndDetach(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	var (
		mu   sync.Mutex
		seen []int
	)
	s.SetSubscriberCountHook(func(n int) {
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})

	a, _ := s.Subscribe(0, "a", "ha")
	b, _ := s.Subscribe(0, "b", "hb")
	s.Unsubscribe(a)
	s.Unsubscribe(b)

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	want := []int{1, 2, 1, 0}
	if len(seen) != len(want) {
		t.Fatalf("counts = %v; want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("counts = %v; want %v", seen, want)
		}
	}
}

func TestMirrorSessionDoesNotSelfPromoteDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.SetDriverFromUpstream(true)

	sub, _ := s.Subscribe(0, "client-remote", "remote-host")
	defer s.Unsubscribe(sub)

	if got := s.DriverClientID(); got != "" {
		t.Fatalf("mirror DriverClientID after first subscribe = %q; want empty (no self-promote)", got)
	}
	if s.IsDriver(sub) {
		t.Fatal("mirror must not auto-promote its first subscriber to driver")
	}
}

func TestMirrorSessionAdoptsUpstreamDriverFromUpdateMeta(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.SetDriverFromUpstream(true)

	sub, _ := s.Subscribe(0, "client-remote", "remote-host")
	defer s.Unsubscribe(sub)
	drainInitialFrames(t, sub)

	// Upstream desktop reports its local driver "owner-A".
	s.UpdateMeta(proto.MetaPayload{Cwd: "/work", DriverClientID: "owner-A", DriverClientName: "mac-mini"})

	if got := s.DriverClientID(); got != "owner-A" {
		t.Fatalf("mirror DriverClientID after UpdateMeta = %q; want owner-A", got)
	}
	f := readFrameForTest(t, sub)
	if f.Type != proto.TypeMeta {
		t.Fatalf("next frame type = 0x%02x; want META", f.Type)
	}
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if m.DriverClientID != "owner-A" || m.DriverClientName != "mac-mini" {
		t.Fatalf("broadcast META driver = %q/%q; want owner-A/mac-mini", m.DriverClientID, m.DriverClientName)
	}
}

func TestMirrorUpdateCwdTitlePreservesAdoptedDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.SetDriverFromUpstream(true)
	sub, _ := s.Subscribe(0, "client-remote", "remote-host")
	defer s.Unsubscribe(sub)
	drainInitialFrames(t, sub)

	// Streamed META adopts the upstream driver.
	s.UpdateMeta(proto.MetaPayload{DriverClientID: "owner-A", DriverClientName: "mac-mini"})
	if got := s.DriverClientID(); got != "owner-A" {
		t.Fatalf("setup: driver after UpdateMeta = %q; want owner-A", got)
	}
	// drain the UpdateMeta broadcast
	_ = readFrameForTest(t, sub)

	// ANNOUNCE-driven cwd/title update (no driver) must NOT clobber the driver.
	s.UpdateCwdTitle("/work", "/bin/zsh")

	if got := s.DriverClientID(); got != "owner-A" {
		t.Fatalf("UpdateCwdTitle clobbered driver: %q; want owner-A", got)
	}
	f := readFrameForTest(t, sub)
	if f.Type != proto.TypeMeta {
		t.Fatalf("next frame type = 0x%02x; want META", f.Type)
	}
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if m.DriverClientID != "owner-A" {
		t.Fatalf("UpdateCwdTitle broadcast driver = %q; want owner-A (preserved)", m.DriverClientID)
	}
	if m.Cwd != "/work" {
		t.Fatalf("UpdateCwdTitle cwd = %q; want /work", m.Cwd)
	}
}

func TestMirrorLateSubscriberSeesAdoptedUpstreamDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.SetDriverFromUpstream(true)
	s.UpdateMeta(proto.MetaPayload{DriverClientID: "owner-A", DriverClientName: "mac-mini"})

	sub, _ := s.Subscribe(0, "client-remote", "remote-host")
	defer s.Unsubscribe(sub)

	f := readFrameForTest(t, sub)
	for f.Type != proto.TypeMeta {
		f = readFrameForTest(t, sub)
	}
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if m.DriverClientID != "owner-A" {
		t.Fatalf("late subscriber snapshot driver = %q; want owner-A", m.DriverClientID)
	}
}

func TestPushOut_AssignsTypeOnNonShellCommand(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	if got := s.Info().Type; got != SessionTypeShell {
		t.Fatalf("initial Type: got %q want %q", got, SessionTypeShell)
	}
	// OSC 133 C; claude --help
	s.PushOut(1, []byte("\x1b]133;C;claude --help\x07"))
	if got := s.Info().Type; got != SessionTypeAI {
		t.Fatalf("after claude: Type got %q want %q", got, SessionTypeAI)
	}
}

func TestPushOut_TypeStickyAfterShellCommand(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut(1, []byte("\x1b]133;C;claude\x07"))
	if got := s.Info().Type; got != SessionTypeAI {
		t.Fatalf("post-claude: %q", got)
	}
	// "D;0" closes the running command, then a new C runs "ls".
	s.PushOut(2, []byte("\x1b]133;D;0\x07"))
	s.PushOut(3, []byte("\x1b]133;C;ls -la\x07"))
	if got := s.Info().Type; got != SessionTypeAI {
		t.Fatalf("after ls: Type got %q want %q (sticky non-shell)", got, SessionTypeAI)
	}
}

func TestPushOut_TypeChangesBetweenTwoNonShells(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut(1, []byte("\x1b]133;C;claude\x07"))
	s.PushOut(2, []byte("\x1b]133;D;0\x07"))
	s.PushOut(3, []byte("\x1b]133;C;npm test\x07"))
	if got := s.Info().Type; got != SessionTypeTest {
		t.Fatalf("after npm test: Type got %q want %q", got, SessionTypeTest)
	}
}
