# Driver/Viewer Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate PTY-size cross-client corruption by introducing a single-driver runtime role: one subscriber drives the PTY (sends IN/RESIZE), all others are viewers locked to the PTY's current size. Pressing space in viewer mode claims the driver role without owner confirmation.

**Architecture:** Add a `driverSubscriber` pointer and `driverClientID` string to `Session`. Auto-promote the first subscriber. New `TypeClaimDriver` frame lets viewers take over; META broadcasts the current driver's end-to-end `client_id`. Relay drops IN/RESIZE/PASTE_IMAGE from non-driver subscribers. Frontend tracks its own `clientID`, compares to incoming `meta.driver_client_id`, locks xterm to `meta.cols/rows` in viewer mode, intercepts space-key to claim.

**Tech Stack:** Go (server), TypeScript + Vue 3 + xterm.js (frontend), `nhooyr/websocket`, `vitest`, `crypto.randomUUID` (browser).

**Spec:** `docs/superpowers/specs/2026-05-14-driver-viewer-mode-design.md`

**Prerequisite:** Work on branch `fix/driver-viewer-mode` (already created from `main`).

---

## File Structure

| File | Change |
|---|---|
| `internal/proto/frame.go` | + `TypeClaimDriver` const, + `ClaimDriverPayload` struct, extend `AttachPayload` with `ClientID`, extend `MetaPayload` with `DriverClientID`. |
| `internal/session/session.go` | + `Subscriber.clientID`, + `Session.driverSubscriber`, + `Session.driverClientID`. Extend `Subscribe`, add `ClaimDriver`, `IsDriver`. Driver fields cleared in `removeSubscriber`. Broadcast META on driver change. |
| `internal/session/session_test.go` | Tests for auto-promote, ClaimDriver, IsDriver gating, driver-cleared-on-unsubscribe. |
| `internal/relay/client_conn.go` | Parse `client_id` from ATTACH, pass to Subscribe. Handle `TypeClaimDriver`. Gate IN/RESIZE/PASTE_IMAGE on `sess.IsDriver(sub)`. |
| `internal/relay/client_conn_test.go` (new, if not exists) or extend existing test file | Test ATTACH client_id round-trip; CLAIM_DRIVER from view-permission dropped; non-driver IN dropped. |
| `desktop/uplink.go` | Forward `TypeClaimDriver` both ways (uplink reader → SendLocalInbound; local subscriber's outbox → uplink writer). |
| `desktop/relay_host.go` | `SubscribeLocal` passes a generated uplink clientID to `sess.Subscribe`. |
| `desktop/frontend/src/lib/proto.ts` | + `CLAIM_DRIVER: 0x34` in `TYPE`. |
| `desktop/frontend/src/lib/connection.ts` | + `clientID` field on `SessionConnection`. Send in ATTACH. Parse `driver_client_id` from META. New `claimDriver()` method. New `onDriverChange` handler. |
| `desktop/frontend/src/lib/connection.test.ts` (new) | Verify ATTACH payload contains a UUID `client_id`; META roundtrip flips `isDriver`. |
| `desktop/frontend/src/components/TerminalView.vue` | Reactive `isDriver`. Viewer-mode: stop FitAddon resize-observer, set `term.resize(meta.cols, meta.rows)`, set `term.options.disableStdin = true`, render `<div class="viewer-badge">`. Keydown handler intercepts space in viewer mode → `conn.claimDriver()`. Toast on role flip. |
| `desktop/frontend/src/components/TerminalView.test.ts` | Extend with source-tests for badge text and viewer-mode CSS class. |
| `docs/spec/protocol.md` | Document `TypeClaimDriver`, `AttachPayload.client_id`, `MetaPayload.driver_client_id`, driver state machine. |

---

## Task 1: Protocol — Add `TypeClaimDriver` and payload extensions

**Files:**
- Modify: `internal/proto/frame.go`

- [ ] **Step 1: Add the new frame type and payload structs**

Edit `internal/proto/frame.go`. After the `TypePasteImage` line in the lazy-mirror block, add:

```go
	TypePasteImage    Type = 0x33 // client -> relay -> desktop PTY host
	TypeClaimDriver   Type = 0x34 // client -> relay (viewer claims driver role)
)
```

Then extend `AttachPayload`:

```go
// AttachPayload is the JSON body of a TypeAttach frame.
type AttachPayload struct {
	SessionID string `json:"session_id"`
	SinceSeq  uint64 `json:"since_seq,omitempty"`
	// ClientID is a UUID generated client-side per SessionConnection. The
	// relay echoes it back in META.driver_client_id when this subscriber is
	// the active driver so the client can recognize itself. Empty is allowed
	// (older clients) — they participate but never UI-render as driver.
	ClientID string `json:"client_id,omitempty"`
}
```

Extend `MetaPayload` (adds driver_client_id AND cols/rows in one shot so the proto struct is changed exactly once):

```go
// MetaPayload is the JSON body of a TypeMeta frame.
type MetaPayload struct {
	Cwd   string `json:"cwd,omitempty"`
	Title string `json:"title,omitempty"`
	// DriverClientID is the end-to-end client_id of the subscriber currently
	// allowed to send IN/RESIZE/PASTE_IMAGE. Empty = no driver assigned.
	DriverClientID string `json:"driver_client_id,omitempty"`
	// Cols/Rows snapshot the PTY's current size so viewers can lock their
	// xterm.cols/rows to the PTY (they don't run FitAddon).
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}
```

Add a new payload type near the other payloads:

```go
// ClaimDriverPayload is the JSON body of a TypeClaimDriver frame.
type ClaimDriverPayload struct {
	ClientID string `json:"client_id"`
}
```

- [ ] **Step 2: Verify the package still compiles**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./internal/proto/
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/proto/frame.go
git commit -m "proto: add CLAIM_DRIVER frame and driver_client_id fields"
```

---

## Task 2: Session — Subscriber.clientID + Session driver fields

**Files:**
- Modify: `internal/session/session.go`

- [ ] **Step 1: Add `clientID` to `Subscriber` and driver fields to `Session`**

Find the `Subscriber` struct in `internal/session/session.go` and add `clientID`:

```go
// Subscriber is a client connection's outbox.
type Subscriber struct {
	out      chan proto.Frame
	closed   chan struct{}
	once     sync.Once
	clientID string // end-to-end ID echoed in META.driver_client_id when this sub is driver
}
```

Find the `Session` struct and add two fields after `termTail`:

```go
type Session struct {
	ID        uuid.UUID
	StartedAt time.Time

	mu        sync.RWMutex
	meta      proto.SessionInfo
	closed    bool
	subs      map[*Subscriber]struct{}
	scroll    *ringbuf.Buffer
	inbound   chan proto.Frame
	altScreen bool
	termTail  []byte

	// driverSubscriber is the only subscriber whose IN/RESIZE/PASTE_IMAGE
	// frames are forwarded to the PTY. Nil means no driver is currently
	// assigned. driverClientID is the end-to-end client_id broadcast in META
	// so clients can recognize themselves.
	driverSubscriber *Subscriber
	driverClientID   string

	onFirstSubscribe  func()
	onLastUnsubscribe func()
}
```

- [ ] **Step 2: Add an accessor for tests**

Below `IsClosed`, append:

```go
// IsDriver reports whether sub is currently the session driver.
func (s *Session) IsDriver(sub *Subscriber) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverSubscriber == sub
}
```

- [ ] **Step 3: Verify compiles**

```bash
go vet ./internal/session/
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/session/session.go
git commit -m "session: add driver subscriber tracking fields"
```

---

## Task 3: Session — Subscribe accepts clientID and auto-promotes first subscriber

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/session_test.go`:

```go
func TestSubscribeAutoPromotesFirstSubscriberToDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	sub, _ := s.Subscribe(0, "client-alpha")
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

	first, _ := s.Subscribe(0, "client-alpha")
	defer s.Unsubscribe(first)
	second, _ := s.Subscribe(0, "client-beta")
	defer s.Unsubscribe(second)

	if !s.IsDriver(first) {
		t.Fatal("first subscriber should remain driver after second attaches")
	}
	if s.IsDriver(second) {
		t.Fatal("second subscriber should be viewer")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test -run "TestSubscribeAutoPromotesFirstSubscriberToDriver|TestSubscribeSecondSubscriberIsViewer" ./internal/session/
```

Expected: FAIL — compile error (Subscribe has wrong arity, DriverClientID undefined).

- [ ] **Step 3: Change `Subscribe` signature to accept clientID**

In `internal/session/session.go`, change `Subscribe`:

```go
// Subscribe registers a new client outbox and replays scrollback strictly
// greater than sinceSeq. When sinceSeq is 0 the full scrollback is replayed.
// clientID is the end-to-end identifier echoed in META.driver_client_id when
// this subscriber is the active driver; empty is allowed.
// Returns the subscriber and the largest seq replayed.
func (s *Session) Subscribe(sinceSeq uint64, clientID string) (*Subscriber, uint64) {
	sub := &Subscriber{
		out:      make(chan proto.Frame, subscriberQueueDepth),
		closed:   make(chan struct{}),
		clientID: clientID,
	}
	// ... rest unchanged until the s.subs[sub] = struct{}{} line
```

Then inside the section where the subscriber is added (around the existing `s.subs[sub] = struct{}{}` line), promote to driver if none exists AND capture state for a snapshot META to be sent after lock release:

```go
	var (
		promotedToDriver bool
		snapshotMeta     proto.SessionInfo
		snapshotDriverID string
	)
	if !closed && enqueueReplayProgress(sub, s.ID, proto.ReplayProgressEnd, replayedBytes, totalBytes, lastSeq) {
		s.subs[sub] = struct{}{}
		added = true
		if s.driverSubscriber == nil {
			s.driverSubscriber = sub
			s.driverClientID = sub.clientID
			promotedToDriver = true
		}
		snapshotMeta = s.meta
		snapshotDriverID = s.driverClientID
	}
```

Then after the existing `s.mu.Unlock()`, send the snapshot. If `promotedToDriver`, broadcast to ALL subs so existing viewers learn about the new driver. Otherwise, send a snapshot frame only to the new sub so it learns the current driver_client_id and PTY cols/rows.

Find the existing block:

```go
	if closed || !added {
		sub.close()
		return sub, lastSeq
	}
	if wasEmpty && firstHook != nil {
		go firstHook()
	}
	return sub, lastSeq
}
```

Insert the snapshot dispatch before the `wasEmpty` check:

```go
	if closed || !added {
		sub.close()
		return sub, lastSeq
	}
	if added {
		if promotedToDriver {
			s.broadcastDriverMeta(snapshotMeta, snapshotDriverID)
		} else {
			s.sendSnapshotMeta(sub, snapshotMeta, snapshotDriverID)
		}
	}
	if wasEmpty && firstHook != nil {
		go firstHook()
	}
	return sub, lastSeq
}
```

`broadcastDriverMeta` is added in Task 4; `sendSnapshotMeta` is added there too. To avoid a forward dependency, defer the actual `broadcastDriverMeta`/`sendSnapshotMeta` calls until Task 4 — for Task 3, leave the snapshot block as a TODO comment that Task 4 will fill in, OR add the helpers in this task as no-op stubs. **Use the stub approach** for clean TDD:

In Task 3, add these stubs near the bottom of `session.go` (Task 4 will fill them in):

```go
func (s *Session) broadcastDriverMeta(meta proto.SessionInfo, driverClientID string) {
	// Implementation arrives in Task 4.
	_, _ = meta, driverClientID
}

func (s *Session) sendSnapshotMeta(sub *Subscriber, meta proto.SessionInfo, driverClientID string) {
	// Implementation arrives in Task 4.
	_, _, _ = sub, meta, driverClientID
}
```

This lets Task 3's tests focus on `IsDriver` / `DriverClientID` state without observing META frames.

Add a `DriverClientID()` accessor below `IsDriver`:

```go
// DriverClientID returns the end-to-end client_id of the current driver, or
// "" if no driver is assigned.
func (s *Session) DriverClientID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverClientID
}
```

- [ ] **Step 4: Update other Subscribe callers to pass clientID**

Two callers exist. In `internal/relay/client_conn.go`, find:

```go
				sub, _ = sess.Subscribe(ap.SinceSeq)
```

Replace with:

```go
				sub, _ = sess.Subscribe(ap.SinceSeq, ap.ClientID)
```

In `desktop/relay_host.go::SubscribeLocal`, find:

```go
	sub, replayToSeq := sess.Subscribe(sinceSeq)
```

Replace with (uplink generates a per-subscription ID — the public relay's clients will provide their own end-to-end IDs via the public-relay's own ATTACH; this uplink-side ID is a placeholder used for direct-on-local cases only):

```go
	uplinkSubClientID := "uplink:" + uuid.New().String()
	sub, replayToSeq := sess.Subscribe(sinceSeq, uplinkSubClientID)
```

You'll need to confirm `github.com/google/uuid` is already imported in `relay_host.go` (it is — see existing usage).

- [ ] **Step 5: Update the existing session tests that call `Subscribe`**

In `internal/session/session_test.go`, every existing `s.Subscribe(N)` call needs a clientID argument. Add `""` to each one (these tests don't care about driver identity):

```bash
# Quick sanity check — list all existing Subscribe calls in test file:
grep -n "s.Subscribe(" internal/session/session_test.go
```

For each match like `s.Subscribe(0)`, change to `s.Subscribe(0, "")`. Likewise for `s.Subscribe(2)`, change to `s.Subscribe(2, "")`. Apply this exact transform across the file.

- [ ] **Step 6: Run the new tests to verify they pass**

```bash
go test -run "TestSubscribeAutoPromotesFirstSubscriberToDriver|TestSubscribeSecondSubscriberIsViewer" ./internal/session/
```

Expected: PASS.

- [ ] **Step 7: Run the full session package tests to confirm no regressions**

```bash
go test ./internal/session/
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go internal/relay/client_conn.go desktop/relay_host.go
git commit -m "session: auto-promote first subscriber to driver"
```

---

## Task 4: Session — `ClaimDriver` method and META broadcast

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/session_test.go`:

```go
func TestClaimDriverTransfersAndBroadcastsMeta(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})

	first, _ := s.Subscribe(0, "client-alpha")
	defer s.Unsubscribe(first)
	drainInitialFrames(t, first) // 2 progress + 1 snapshot META

	second, _ := s.Subscribe(0, "client-beta")
	defer s.Unsubscribe(second)
	drainInitialFrames(t, second)

	s.ClaimDriver(second, "client-beta")

	if !s.IsDriver(second) {
		t.Fatal("second should now be driver after ClaimDriver")
	}
	if s.IsDriver(first) {
		t.Fatal("first should no longer be driver")
	}
	if got := s.DriverClientID(); got != "client-beta" {
		t.Fatalf("DriverClientID = %q; want client-beta", got)
	}

	// Both subscribers should receive a META frame with the new driver_client_id.
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
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestClaimDriverTransfersAndBroadcastsMeta ./internal/session/
```

Expected: FAIL — `ClaimDriver` undefined.

- [ ] **Step 3: Implement `ClaimDriver` plus fill in the Task 3 stubs**

In `internal/session/session.go`, below `IsDriver` add:

```go
// ClaimDriver makes sub the active driver, recording clientID as the
// end-to-end identifier. Broadcasts a META frame with the new driver to all
// subscribers. Safe to call when sub is already driver — it's a no-op for
// state but still re-broadcasts (cheap; idempotent for clients).
func (s *Session) ClaimDriver(sub *Subscriber, clientID string) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; !ok {
		s.mu.Unlock()
		return
	}
	s.driverSubscriber = sub
	s.driverClientID = clientID
	metaCopy := s.meta
	s.mu.Unlock()

	s.broadcastDriverMeta(metaCopy, clientID)
}
```

Now REPLACE the two stub functions added in Task 3 with the real implementations. Find:

```go
func (s *Session) broadcastDriverMeta(meta proto.SessionInfo, driverClientID string) {
	// Implementation arrives in Task 4.
	_, _ = meta, driverClientID
}

func (s *Session) sendSnapshotMeta(sub *Subscriber, meta proto.SessionInfo, driverClientID string) {
	// Implementation arrives in Task 4.
	_, _, _ = sub, meta, driverClientID
}
```

Replace with:

```go
func (s *Session) broadcastDriverMeta(meta proto.SessionInfo, driverClientID string) {
	payload, err := encodeMetaPayload(meta, driverClientID)
	if err != nil {
		return
	}
	s.Broadcast(proto.Frame{Type: proto.TypeMeta, SessionID: s.ID, Payload: payload})
}

func (s *Session) sendSnapshotMeta(sub *Subscriber, meta proto.SessionInfo, driverClientID string) {
	payload, err := encodeMetaPayload(meta, driverClientID)
	if err != nil {
		return
	}
	select {
	case sub.out <- proto.Frame{Type: proto.TypeMeta, SessionID: s.ID, Payload: payload}:
	default:
		// channel full — let normal slow-consumer drop handle it next fanout
	}
}

func encodeMetaPayload(meta proto.SessionInfo, driverClientID string) ([]byte, error) {
	return json.Marshal(proto.MetaPayload{
		Cwd:            meta.Cwd,
		Title:          meta.Title,
		DriverClientID: driverClientID,
		Cols:           meta.Cols,
		Rows:           meta.Rows,
	})
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -run TestClaimDriverTransfersAndBroadcastsMeta ./internal/session/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "session: add ClaimDriver method with META broadcast"
```

---

## Task 5: Session — Clear driver on unsubscribe + broadcast META

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/session_test.go`:

```go
func TestRemoveDriverSubscriberClearsAndBroadcasts(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	first, _ := s.Subscribe(0, "client-alpha")
	drainInitialFrames(t, first)

	second, _ := s.Subscribe(0, "client-beta")
	defer s.Unsubscribe(second)
	drainInitialFrames(t, second)

	// Sanity check: first is driver.
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

	// Remaining subscriber receives a META with empty driver_client_id.
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
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestRemoveDriverSubscriberClearsAndBroadcasts ./internal/session/
```

Expected: FAIL — driver still set after unsubscribe.

- [ ] **Step 3: Implement clear-on-unsubscribe**

In `internal/session/session.go`, find `removeSubscriber` and update:

```go
func (s *Session) removeSubscriber(sub *Subscriber) {
	s.mu.Lock()
	_, was := s.subs[sub]
	if was {
		delete(s.subs, sub)
	}
	wasDriver := s.driverSubscriber == sub
	if wasDriver {
		s.driverSubscriber = nil
		s.driverClientID = ""
	}
	nowEmpty := len(s.subs) == 0
	lastHook := s.onLastUnsubscribe
	metaCopy := s.meta
	s.mu.Unlock()
	sub.close()
	if wasDriver {
		s.broadcastDriverMeta(metaCopy, "")
	}
	if was && nowEmpty && lastHook != nil {
		go lastHook()
	}
}
```

- [ ] **Step 4: Run the test**

```bash
go test -run TestRemoveDriverSubscriberClearsAndBroadcasts ./internal/session/
```

Expected: PASS.

- [ ] **Step 5: Run the full session package**

```bash
go test ./internal/session/
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "session: clear driver and broadcast META on unsubscribe"
```

---

## Task 6: Relay — Handle CLAIM_DRIVER frame and gate IN/RESIZE/PASTE_IMAGE on driver

**Files:**
- Modify: `internal/relay/client_conn.go`

- [ ] **Step 1: Add CLAIM_DRIVER handling to the client switch**

In `internal/relay/client_conn.go`, find the switch statement in `handleClient` (around line 109). Add a new case after `proto.TypePasteImage` handling:

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
			if sess == nil {
				s.debugf("client drop frame=%s reason=not_attached", frameTypeName(f.Type))
				continue
			}
			if !frameAllowedByPermission(scope, sessionRemotePermission(sess), f.Type) {
				s.debugf("client drop frame=%s reason=permission", frameTypeName(f.Type))
				continue
			}
			if !sess.IsDriver(sub) {
				s.debugf("client drop frame=%s reason=not_driver session=%s", frameTypeName(f.Type), sess.ID)
				continue
			}
			if f.Type == proto.TypeResize {
				if cols, rows, err := proto.DecodeResize(f.Payload); err == nil {
					sess.UpdateSize(cols, rows)
					s.registry.NotifyChange()
				}
			}
			if !sess.SendInbound(f) {
				log.Printf("client: inbound full, dropping frame type 0x%02x", f.Type)
			}

		case proto.TypeClaimDriver:
			if sess == nil {
				s.debugf("client drop frame=CLAIM_DRIVER reason=not_attached")
				continue
			}
			// read-only-token scope and view-permission sessions can't drive.
			if scope == authRead {
				s.debugf("client drop frame=CLAIM_DRIVER reason=read_only_scope session=%s", sess.ID)
				continue
			}
			if sessionRemotePermission(sess) == permView {
				s.debugf("client drop frame=CLAIM_DRIVER reason=view_only session=%s", sess.ID)
				continue
			}
			var cp proto.ClaimDriverPayload
			if err := json.Unmarshal(f.Payload, &cp); err != nil {
				s.debugf("client drop frame=CLAIM_DRIVER reason=bad_payload session=%s err=%q", sess.ID, err)
				continue
			}
			sess.ClaimDriver(sub, cp.ClientID)
			s.debugf("client claim_driver session=%s client_id=%q", sess.ID, cp.ClientID)
```

(`authRead` and `permView` are package-local types in `internal/relay/auth.go` and `internal/relay/permissions.go` — no new imports needed.)

You'll need `encoding/json` already imported (it is — check the imports at the top of the file).

- [ ] **Step 2: Verify compiles**

```bash
go vet ./internal/relay/
```

Expected: no output.

- [ ] **Step 3: Write a unit test for non-driver IN drop**

Mirror the existing pattern from `internal/relay/session_list_conn_test.go`: register a session directly via `srv.registry.Add(session.New(...))`, no PTY host needed. Verify IN gating by reading from `sess.Inbound()` directly.

Create or extend `internal/relay/client_conn_test.go`. If the file exists, append; if not, create with:

```go
package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// TestNonDriverInDroppedAtRelay: a viewer subscriber's IN frames are silently
// dropped at the relay rather than reaching sess.Inbound(). Driver's IN does
// reach Inbound.
func TestNonDriverInDroppedAtRelay(t *testing.T) {
	srv := NewServer(Config{})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Driver (alpha) attaches first and is auto-promoted by Subscribe.
	driver := dialClientAttach(t, ctx, httpSrv, id, "client-alpha")
	defer driver.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, driver)

	// Viewer (beta) attaches second.
	viewer := dialClientAttach(t, ctx, httpSrv, id, "client-beta")
	defer viewer.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, viewer)

	// Viewer sends IN — should be dropped by relay (not driver).
	writeClientFrame(t, ctx, viewer, proto.TypeIn, id, []byte("hello"))
	select {
	case f := <-sess.Inbound():
		t.Fatalf("Inbound received %v; expected viewer drop", f)
	case <-time.After(150 * time.Millisecond):
		// expected: nothing reached Inbound
	}

	// Sanity: driver's IN reaches Inbound.
	writeClientFrame(t, ctx, driver, proto.TypeIn, id, []byte("ok"))
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "ok" {
			t.Fatalf("got %+v; want IN with payload ok", f)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for driver IN to reach Inbound")
	}
}

func dialClientAttach(t *testing.T, ctx context.Context, srv *httptest.Server, sid uuid.UUID, clientID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/client"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	payload, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: clientID})
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	return c
}

// drainAttachIntro consumes the frames Subscribe queues for a new attacher:
// REPLAY_PROGRESS start + end, then one snapshot META.
func drainAttachIntro(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	for i := 0; i < 2; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read progress %d: %v", i, err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal progress %d: %v", i, err)
		}
		if f.Type != proto.TypeReplayProgress {
			t.Fatalf("frame %d type 0x%02x; want REPLAY_PROGRESS", i, f.Type)
		}
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read snapshot META: %v", err)
	}
	f, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal snapshot META: %v", err)
	}
	if f.Type != proto.TypeMeta {
		t.Fatalf("snapshot frame type 0x%02x; want META", f.Type)
	}
}

func writeClientFrame(t *testing.T, ctx context.Context, c *websocket.Conn, typ proto.Type, sid uuid.UUID, payload []byte) {
	t.Helper()
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: typ, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("write 0x%02x: %v", typ, err)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -run TestNonDriverInDroppedAtRelay ./internal/relay/
```

Expected: PASS.

- [ ] **Step 5: Run the full relay test suite**

```bash
go test ./internal/relay/
```

Expected: all PASS (no regressions from gating).

- [ ] **Step 6: Commit**

```bash
git add internal/relay/client_conn.go internal/relay/client_conn_test.go
git commit -m "relay: gate IN/RESIZE/PASTE_IMAGE on driver and handle CLAIM_DRIVER"
```

---

## Task 7: Uplink — Forward CLAIM_DRIVER in both directions

**Files:**
- Modify: `desktop/uplink.go`

- [ ] **Step 1: Allow CLAIM_DRIVER from a local subscriber's outbox to flow to the public relay**

In `desktop/uplink.go`, find `localSubscriberFrameForwardedToUplink` (likely just above `forwardLocalSubscriberFrame`). It returns true for the frame types that should flow uplink-bound. Add `TypeClaimDriver` if not already there — but wait, CLAIM_DRIVER flows IN the opposite direction (client→relay), so the local sub's outbox should normally NOT contain it. Skip this; instead, ensure the inbound path forwards CLAIM_DRIVER through.

Find the inbound switch statement in `desktop/uplink.go` (around line 342):

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
			log.Printf("desktop-uplink: inbound_recv type=%s %s", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f))
			if !localFrameAllowedByPermission(u.remotePermission, f.Type) {
				log.Printf("desktop-uplink: inbound_drop_permission type=%s permission=%s %s", desktopUplinkFrameTypeName(f.Type), u.remotePermission, desktopUplinkFrameLogDetails(f))
				continue
			}
			if err := u.host.SendLocalInbound(f.SessionID, f); err != nil {
				log.Printf("desktop-uplink: inbound_forward_failed type=%s %s error=%v", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f), err)
			} else {
				log.Printf("desktop-uplink: inbound_forward_ok type=%s %s", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f))
			}
```

Add `proto.TypeClaimDriver` to the case label so it's forwarded too. Note: this assumes the local session.ClaimDriver routes through SendInbound → adopt.go inbound loop. Adopt.go currently only handles IN/RESIZE/PASTE_IMAGE in its inbound switch — see the next sub-step.

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage, proto.TypeClaimDriver:
```

Also extend `desktopUplinkFrameTypeName` (in the same file, around line 453) to recognize `TypeClaimDriver`:

```go
	case proto.TypePasteImage:
		return "PASTE_IMAGE"
	case proto.TypeClaimDriver:
		return "CLAIM_DRIVER"
```

- [ ] **Step 2: Route CLAIM_DRIVER through `adopt.go`'s inbound switch to the session, not the PTY**

CLAIM_DRIVER must NOT be written to the PTY. It belongs to the session itself. In `internal/relay/adopt.go`, find the inbound switch (around line 110) and add a CLAIM_DRIVER arm that no-ops at the PTY layer:

```go
				case proto.TypeIn:
					if _, err := host.Write(f.Payload); err != nil {
						return
					}
				case proto.TypeResize:
					// existing
				case proto.TypePasteImage:
					// existing
				case proto.TypeClaimDriver:
					// Adoption layer ignores CLAIM_DRIVER — the relay client_conn
					// has already called sess.ClaimDriver(sub, payload). The frame
					// reached the inbound channel because uplink forwarded it; we
					// silently consume it here.
				}
```

Wait — this exposes the gap: CLAIM_DRIVER from uplink reaches `SendLocalInbound`, which pushes onto `sess.Inbound()`, which `adopt.go` reads. But adopt.go's inbound consumer doesn't know about the subscriber that originated the frame — it can't call `sess.ClaimDriver(sub, ...)` without the sub.

The cleaner path: the uplink forwarder's local subscriber IS the sub. So when uplink receives CLAIM_DRIVER from the public relay, it should directly call `sess.ClaimDriver(u.localSub, payload.ClientID)` rather than route through `Inbound`.

Refine step 1 to use this direct route. Reopen `desktop/uplink.go` and change the case to:

```go
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
			// existing inbound forwarding via SendLocalInbound
			// ...

		case proto.TypeClaimDriver:
			log.Printf("desktop-uplink: inbound_recv type=CLAIM_DRIVER %s", desktopUplinkFrameLogDetails(f))
			var cp proto.ClaimDriverPayload
			if err := json.Unmarshal(f.Payload, &cp); err != nil {
				log.Printf("desktop-uplink: inbound_drop type=CLAIM_DRIVER reason=bad_payload session=%s err=%q", f.SessionID, err)
				continue
			}
			if err := u.host.ClaimLocalDriver(f.SessionID, cp.ClientID); err != nil {
				log.Printf("desktop-uplink: inbound_drop type=CLAIM_DRIVER reason=%q session=%s", err, f.SessionID)
			} else {
				log.Printf("desktop-uplink: inbound_forward_ok type=CLAIM_DRIVER session=%s client_id=%q", f.SessionID, cp.ClientID)
			}
```

Revert the `adopt.go` change — no need to consume CLAIM_DRIVER in the PTY inbound switch since we won't route it there.

- [ ] **Step 3: Add `ClaimLocalDriver` to `relayHost`**

In `desktop/relay_host.go`, add a new method below `SubscribeLocal`:

```go
// ClaimLocalDriver promotes the uplink's own local-session subscriber to
// driver for the given session, attributing the end-to-end client_id. Used
// by uplink when a remote subscriber on the public relay sends CLAIM_DRIVER.
func (h *relayHost) ClaimLocalDriver(id uuid.UUID, clientID string) error {
	h.mu.Lock()
	active := h.uplinkSubs[id]
	h.mu.Unlock()
	if active == nil {
		return fmt.Errorf("no uplink subscriber for session %s", id)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return fmt.Errorf("no local session %s", id)
	}
	sess.ClaimDriver(active, clientID)
	return nil
}
```

We need `h.uplinkSubs` — a map from session ID to the local Subscriber created in `SubscribeLocal`. This requires bookkeeping. In `relayHost` struct, add:

```go
type relayHost struct {
	// ...existing fields...
	uplinkSubs map[uuid.UUID]*session.Subscriber
}
```

Initialize in `startRelayHost`:

```go
		uplinkSubs: make(map[uuid.UUID]*session.Subscriber),
```

Update `SubscribeLocal` to record:

```go
func (h *relayHost) SubscribeLocal(id uuid.UUID, sinceSeq uint64) (*session.Subscriber, uint64, error) {
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		return nil, 0, fmt.Errorf("no such local session %s", id)
	}
	uplinkSubClientID := "uplink:" + uuid.New().String()
	sub, replayToSeq := sess.Subscribe(sinceSeq, uplinkSubClientID)
	h.mu.Lock()
	h.uplinkSubs[id] = sub
	h.mu.Unlock()
	info := sess.Info()
	log.Printf("desktop-uplink: subscribe_local_ok session=%s since_seq=%d replay_to_seq=%d cols=%d rows=%d", id, sinceSeq, replayToSeq, info.Cols, info.Rows)
	return sub, replayToSeq, nil
}
```

Update `UnsubscribeLocal` to forget:

```go
func (h *relayHost) UnsubscribeLocal(id uuid.UUID, sub *session.Subscriber) {
	if sess, ok := h.server.Registry().Get(id); ok {
		sess.Unsubscribe(sub)
	}
	h.mu.Lock()
	if h.uplinkSubs[id] == sub {
		delete(h.uplinkSubs, id)
	}
	h.mu.Unlock()
}
```

- [ ] **Step 4: Verify compiles**

```bash
go vet -tags webkit2_41 ./desktop/ ./internal/...
```

Expected: no output.

- [ ] **Step 5: Run all backend tests**

```bash
go test -tags webkit2_41 ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/uplink.go desktop/relay_host.go
git commit -m "uplink: route CLAIM_DRIVER to local session driver state"
```

---

## Task 8: Frontend proto — Add CLAIM_DRIVER constant

**Files:**
- Modify: `desktop/frontend/src/lib/proto.ts`

- [ ] **Step 1: Add the constant**

In `desktop/frontend/src/lib/proto.ts`, in the `TYPE` object, add after `PASTE_IMAGE`:

```typescript
export const TYPE = {
  OPEN: 0x01,
  IN: 0x02,
  OUT: 0x03,
  RESIZE: 0x04,
  META: 0x05,
  CLOSE: 0x06,
  ATTACH: 0x10,
  LIST: 0x11,
  LIST_RESP: 0x12,
  REPLAY_PROGRESS: 0x13,
  PING: 0x20,
  PONG: 0x21,
  PASTE_IMAGE: 0x33,
  CLAIM_DRIVER: 0x34,
} as const;
```

- [ ] **Step 2: Verify type-check**

```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds. (The build is `vue-tsc --noEmit && vite build`, so type errors fail the build.)

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/proto.ts
git commit -m "frontend proto: add CLAIM_DRIVER type"
```

---

## Task 9: Frontend connection — clientID, ATTACH payload, driver state, claimDriver()

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts`
- Test: `desktop/frontend/src/lib/connection.test.ts` (new)

- [ ] **Step 1: Write a failing test**

Create `desktop/frontend/src/lib/connection.test.ts`:

```typescript
import { describe, expect, test } from "vitest";
import source from "./connection.ts?raw";

describe("SessionConnection driver state", () => {
  test("generates and stores a clientID per instance", () => {
    expect(source).toMatch(/clientID\s*:\s*string/);
    expect(source).toMatch(/crypto\.randomUUID\(\)/);
  });

  test("includes client_id in ATTACH payload", () => {
    expect(source).toMatch(/client_id\s*:\s*this\.clientID/);
  });

  test("parses driver_client_id from META and surfaces onDriverChange", () => {
    expect(source).toMatch(/driver_client_id/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("exposes claimDriver() that sends a CLAIM_DRIVER frame", () => {
    expect(source).toMatch(/claimDriver\s*\(/);
    expect(source).toMatch(/TYPE\.CLAIM_DRIVER/);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend
npx vitest run src/lib/connection.test.ts
```

Expected: FAIL — none of the strings present yet.

- [ ] **Step 3: Implement the changes in connection.ts**

In `desktop/frontend/src/lib/connection.ts`, extend `ConnectionHandlers`:

```typescript
export interface ConnectionHandlers {
  onOutput?: (data: Uint8Array) => void;
  onClose?: (info: ClosePayload) => void;
  onMeta?: (meta: { cwd?: string; title?: string }) => void;
  onStatus?: (s: Status) => void;
  onReplayProgress?: (progress: ReplayProgress) => void;
  // onDriverChange fires whenever this connection's driver-or-viewer role
  // changes. isMe is true when the broadcast driver_client_id matches our
  // locally-generated clientID; false otherwise (including empty/no-driver).
  onDriverChange?: (driverClientID: string, isMe: boolean) => void;
}
```

In `SessionConnection`, add a `clientID` field initialized in the constructor:

```typescript
export class SessionConnection {
  private ws: WebSocket | null = null;
  private sidBytes: Uint8Array;
  private lastSeq = 0;
  private reconnectAttempts = 0;
  private reconnectTimer: number | null = null;
  private detached = false;
  private pendingResize: { cols: number; rows: number } | null = null;
  private clientID: string;
  private currentDriverClientID = "";

  constructor(
    private endpoint: Endpoint,
    private sessionId: string,
    private handlers: ConnectionHandlers = {}
  ) {
    this.sidBytes = uuidParse(sessionId);
    this.clientID = crypto.randomUUID();
  }
```

Update the ATTACH payload in `openWS`:

```typescript
    ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.handlers.onStatus?.("attached");
      const attachPayload = encodeText(
        JSON.stringify({
          session_id: this.sessionId,
          since_seq: this.lastSeq,
          client_id: this.clientID,
        })
      );
      ws.send(encodeFrame(TYPE.ATTACH, this.sidBytes, attachPayload));
      // ... existing pendingResize flush
    };
```

Update the META handler in `ws.onmessage`:

```typescript
      } else if (f.type === TYPE.META) {
        try {
          const meta = JSON.parse(decodeText(f.payload));
          this.handlers.onMeta?.(meta);
          const newDriver = String(meta.driver_client_id ?? "");
          if (newDriver !== this.currentDriverClientID) {
            this.currentDriverClientID = newDriver;
            this.handlers.onDriverChange?.(newDriver, newDriver === this.clientID && newDriver !== "");
          }
        } catch {
          /* ignore */
        }
      }
```

Add a `claimDriver` method at the same level as `sendInput`:

```typescript
  claimDriver(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const payload = encodeText(JSON.stringify({ client_id: this.clientID }));
    this.ws.send(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload));
  }
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend
npx vitest run src/lib/connection.test.ts
```

Expected: PASS.

- [ ] **Step 5: Run the full frontend build to confirm no type errors**

```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/connection.ts desktop/frontend/src/lib/connection.test.ts
git commit -m "frontend connection: clientID + driver state + claimDriver()"
```

---

## Task 10: Frontend TerminalView — Track isDriver and lock xterm to PTY size in viewer mode

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write the failing test (source-test style)**

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```typescript
describe("TerminalView driver/viewer mode", () => {
  test("tracks isDriver ref from onDriverChange", () => {
    expect(source).toMatch(/const\s+isDriver\s*=\s*ref/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("locks term to META dims in viewer mode", () => {
    expect(source).toMatch(/term\?\.resize\(.*cols.*rows/);
  });

  test("disables stdin when not driver", () => {
    expect(source).toMatch(/disableStdin/);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: FAIL for the new tests.

- [ ] **Step 3: Add isDriver state and onDriverChange handler**

In `desktop/frontend/src/components/TerminalView.vue`, add reactive state near the existing refs:

```typescript
const isDriver = ref(true); // optimistic; SessionConnection's first META corrects it
const ptyCols = ref<number | null>(null);
const ptyRows = ref<number | null>(null);
```

In `startConnection`, extend the handlers:

```typescript
function startConnection() {
  if (!term) return;
  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(data),
    onClose: (info) => {
      term?.write(
        `\r\n\x1b[33m[AT Term] session ended (exit ${info.exit_code})\x1b[0m\r\n`
      );
    },
    onStatus: (s) => {
      status.value = s;
    },
    onReplayProgress: (progress) => {
      replayProgress.value = progress.phase === "end" ? null : progress;
    },
    onMeta: (meta: any) => {
      if (typeof meta?.cols === "number") ptyCols.value = meta.cols;
      if (typeof meta?.rows === "number") ptyRows.value = meta.rows;
      applyViewerSize();
    },
    onDriverChange: (_driverID, isMe) => {
      const wasDriver = isDriver.value;
      isDriver.value = isMe;
      if (term) term.options.disableStdin = !isMe;
      applyViewerSize();
      if (wasDriver !== isMe) {
        emit("toast", isMe ? "you are now the driver" : "you are now a viewer");
      }
    },
  });
  conn.attach();
  // ... existing expectedCols/Rows resize logic stays as-is — but only effective
  // when we end up as driver. Viewer-mode applyViewerSize() will override below.
  if (
    term &&
    (props.expectedCols !== term.cols || props.expectedRows !== term.rows)
  ) {
    conn.sendResize(term.cols, term.rows);
  }
}
```

META frames carry cols/rows already (Task 1 added those fields to `MetaPayload`, and Task 4's `encodeMetaPayload` propagates them). The remaining backend change is: `UpdateSize` should broadcast META when the size genuinely changes so viewers learn the new PTY dims.

- [ ] **Step 4: Make `UpdateSize` broadcast META on actual change**

Re-open `internal/session/session.go`. Modify `UpdateSize`:

```go
func (s *Session) UpdateSize(cols, rows uint16) {
	s.mu.Lock()
	changed := false
	if cols > 0 && s.meta.Cols != cols {
		s.meta.Cols = cols
		changed = true
	}
	if rows > 0 && s.meta.Rows != rows {
		s.meta.Rows = rows
		changed = true
	}
	driverClientID := s.driverClientID
	metaCopy := s.meta
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverClientID)
	}
}
```

Run the existing session tests to confirm UpdateSize-broadcasts didn't break things:

```bash
go test ./internal/session/
```

Expected: all PASS (existing tests don't depend on META silence after UpdateSize).

- [ ] **Step 5: Implement `applyViewerSize` in TerminalView.vue**

Inside the `<script>` section, add a helper:

```typescript
function applyViewerSize() {
  if (!term) return;
  if (isDriver.value) {
    // Driver: re-engage FitAddon (will fire onResize → sendResize).
    safeFit();
    return;
  }
  // Viewer: lock xterm cols/rows to PTY's reported dims.
  const cols = ptyCols.value;
  const rows = ptyRows.value;
  if (typeof cols === "number" && typeof rows === "number" && cols > 0 && rows > 0) {
    if (term.cols !== cols || term.rows !== rows) {
      term.resize(cols, rows);
    }
  }
}
```

Modify the existing `safeFit` to early-return when in viewer mode:

```typescript
function safeFit() {
  if (!fit || !termContainer.value || !isDriver.value) return;
  // ... existing implementation
}
```

Also gate the `term.onResize` handler so we never send RESIZE when not driver:

```typescript
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return;
    conn?.sendResize(cols, rows);
  });
```

- [ ] **Step 6: Run the TerminalView test**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: PASS.

- [ ] **Step 7: Run frontend build**

```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 8: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/session.go desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "frontend: lock xterm to PTY size when viewer; broadcast META on UpdateSize"
```

---

## Task 11: Frontend TerminalView — Space-key intercept in viewer mode

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```typescript
test("intercepts space in viewer mode and calls claimDriver", () => {
  expect(source).toMatch(/handleViewerKeydown/);
  expect(source).toMatch(/claimDriver/);
  expect(source).toMatch(/event\.key\s*===\s*" "/);
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Add the keydown handler**

In `TerminalView.vue`, add the handler near `handleCopyShortcut`. **Only intercept bare Space** — let every other key flow through to xterm (its `disableStdin=true` already blocks IN forwarding, but selection / scroll / Cmd+C copy still work):

```typescript
function handleViewerKeydown(event: KeyboardEvent) {
  if (isDriver.value) return; // driver mode passes through
  // Only intercept bare space (no modifiers) so existing shortcuts (Cmd+C
  // copy, arrow-key scroll, etc.) still work in viewer mode.
  if (event.key === " " && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) {
    event.preventDefault();
    event.stopPropagation();
    conn?.claimDriver();
  }
  // Any other key: do nothing here. disableStdin prevents IN forwarding;
  // xterm still handles selection / scroll natively.
}
```

Wire it up in `ensureTerm`, next to the existing copy/paste listeners:

```typescript
  keyTarget.addEventListener("keydown", handleCopyShortcut, { capture: true });
  keyTarget.addEventListener("keydown", handleViewerKeydown, { capture: true });
  keyTarget.addEventListener("paste", handleImagePaste, { capture: true });
```

Also remove in `onBeforeUnmount`:

```typescript
  copyKeyTarget?.removeEventListener("keydown", handleCopyShortcut, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("keydown", handleViewerKeydown, { capture: true } as EventListenerOptions);
  copyKeyTarget?.removeEventListener("paste", handleImagePaste, { capture: true } as EventListenerOptions);
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: PASS.

- [ ] **Step 5: Run frontend build**

```bash
cd desktop/frontend
npm run build
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "frontend: intercept space in viewer mode to claim driver"
```

---

## Task 12: Frontend TerminalView — Viewer badge UI

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```typescript
test("renders a viewer badge when not driver", () => {
  expect(source).toContain('viewer · press space to take over');
  expect(source).toContain('class="viewer-badge"');
  expect(source).toMatch(/v-if=["']!isDriver["']/);
});

test("viewer-badge style sits in the bottom-left", () => {
  const css = styleBlockFor(".viewer-badge");
  expect(css).toMatch(/position\s*:\s*absolute/);
  expect(css).toMatch(/bottom\s*:/);
  expect(css).toMatch(/left\s*:/);
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Add the badge to the template**

In the `<template>` block of `TerminalView.vue`, just before the closing `</div>` of `.term-view`, add:

```html
    <div v-if="!isDriver" class="viewer-badge">viewer · press space to take over</div>
```

In the `<style scoped>` block, add:

```css
.viewer-badge {
  position: absolute;
  bottom: 8px;
  left: 12px;
  background: var(--terminal-overlay);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 3px 8px;
  font-size: 11px;
  color: var(--fg-dim);
  pointer-events: none;
  user-select: none;
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend
npx vitest run src/components/TerminalView.test.ts
```

Expected: PASS.

- [ ] **Step 5: Run the full frontend test suite + build**

```bash
cd desktop/frontend
npx vitest run
npm run build
```

Expected: all PASS / build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "frontend: render viewer badge with take-over hint"
```

---

## Task 13: Docs — Update `docs/spec/protocol.md`

**Files:**
- Modify: `docs/spec/protocol.md`

- [ ] **Step 1: Document the driver/viewer state machine and protocol additions**

Open `docs/spec/protocol.md` and find the section listing frame types (around the constants discussion). Add a new subsection:

```markdown
## Driver/Viewer 模型

每个 session 在任意时刻最多有一个 driver subscriber。Driver 是唯一允许把 IN/RESIZE/PASTE_IMAGE 发给 PTY 的连接；其它都是 viewer（只接收 OUT/META/CLOSE/REPLAY_PROGRESS）。

- **client_id**：每个 `SessionConnection` 在创建时生成一个 UUID，写入 `ATTACH.client_id`。relay 把它原样存在 `Subscriber` 上，并在 META 的 `driver_client_id` 里回放，让每个客户端通过本地 ID 比对识别自己是不是当前 driver。
- **自动晋升**：第一个 attach 上来的 subscriber（无论 loopback 还是 uplink）自动成为 driver。
- **CLAIM_DRIVER**（type `0x34`）：viewer 想接管时发一帧。payload `{"client_id": "<requester>"}`。relay 收到后把 driver 切到该 sub，并广播一帧 META，包含新的 `driver_client_id`。无需当前 driver 确认。`view`-权限的 subscriber 不允许 claim。
- **driver 解绑**：driver subscriber 断开时 driver 字段清空，META 广播 `driver_client_id=""`。剩下的 viewer 全部处于"无 driver"状态，第一个 claim 的人胜出。
- **uplink 层简化**：当前公网 relay 不在自己一层做 driver 仲裁；它把所有 subscribers 当作一个集合代理到 uplink 上的本地 session。多个 mobile/web 同时连同一个 session 时它们共享"远端 driver"位（cooperative 客户端 v1）。

### ATTACH 增加字段

```json
{ "session_id": "<uuid>", "since_seq": 0, "client_id": "<uuid>" }
```

`client_id` 可选（向前兼容），缺失时 relay 接受订阅但客户端永远识别不出自己是 driver（始终 viewer 渲染）；服务端的 driver 指针仍然指向该 sub，IN/RESIZE 仍可通过——只是 UI 表现像 viewer。

### META 增加字段

```json
{ "cwd": "...", "title": "...", "driver_client_id": "<uuid|empty>", "cols": 132, "rows": 39 }
```

`cols`/`rows` 是 PTY 当前真实尺寸；viewer 把自己的 xterm `term.resize(cols, rows)` 锁到这个值。
`driver_client_id` 为空时表示当前无 driver，所有 subscriber 渲染成 viewer。

### CLAIM_DRIVER frame

| 字段 | 内容 |
|---|---|
| type | `0x34` |
| session_id | 目标 session 的 16 字节 UUID |
| payload | JSON：`{"client_id":"<uuid>"}` |

### Driver 切换时的副作用

- relay 广播一帧 META（带新 `driver_client_id` 和当前 cols/rows）给所有 subscriber
- 新 driver 端的 xterm 把 FitAddon 重启，发一个 RESIZE 给 PTY，PTY 重排
- 老 driver 端切到 viewer 模式：停用 FitAddon，把 xterm 锁到 PTY 当前尺寸，禁用 stdin
```

- [ ] **Step 2: Verify the markdown renders sensibly**

```bash
head -100 docs/spec/protocol.md
```

(Visual sanity check; no automated test for prose docs.)

- [ ] **Step 3: Commit**

```bash
git add docs/spec/protocol.md
git commit -m "docs: spec driver/viewer state machine and CLAIM_DRIVER frame"
```

---

## Task 14: Manual integration test

**Files:** none (manual test); record outcomes in commit message.

- [ ] **Step 1: Start the desktop app in dev mode**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop
ATTERM_RELAY_DEBUG=1 wails dev -tags webkit2_41
```

- [ ] **Step 2: In the running atterm desktop, start a new local session running `claude`**

Pick a working directory and verify the local xterm shows claude's UI cleanly. Note the dimensions (e.g., 133×39).

- [ ] **Step 3: Attach a remote client (web or another desktop) to the same session**

Use the web client at `http://127.0.0.1:<relay-port>` (the port is printed at startup) or another atterm desktop instance attached via the public relay configured in your settings. Configure that remote so its xterm fit dims differ from local (resize the window).

**Verify:**
- The local xterm in the original desktop does **not** garble. Claude's UI continues to render correctly at the original 133×39.
- The remote client renders as viewer: a small `viewer · press space to take over` badge appears bottom-left; the xterm shows the content at PTY's reported dims (133×39), with padding if the remote container is larger.
- The remote client cannot type into claude (keystrokes are swallowed).

- [ ] **Step 4: Take over from the remote**

Press `Space` on the remote client.

**Verify:**
- Remote's badge disappears; remote becomes driver.
- Local xterm shows the badge `viewer · press space to take over`.
- A 2-second toast appears on both sides: `you are now a viewer` on local, `you are now the driver` on remote.
- Local xterm locks to the remote's PTY size (resized to remote's fit dims); claude's UI redraws cleanly at the new size on **both** screens.

- [ ] **Step 5: Take back from the local**

Press `Space` on the local desktop's xterm.

**Verify:**
- Local becomes driver; PTY resizes to local's container size.
- Remote shows `viewer` badge.
- No corruption on either side.

- [ ] **Step 6: Disconnect remote mid-driver**

Have the remote close its tab/window.

**Verify on local:**
- Toast: nothing (we don't broadcast for view-only flips; flipping back to driver may show `you are now the driver`).
- Local's badge stays — there's no driver. Press space to reclaim.

(If toast behavior on driver-loss-without-claim is undesired, adjust in a follow-up.)

- [ ] **Step 7: Final commit (manifest only, no code)**

```bash
git commit --allow-empty -m "test: driver/viewer manual integration passed (local+remote attach scenarios)"
```

---

## Cross-cutting tasks

- [ ] **Run the full Go test suite once before opening a PR**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 -timeout 60s ./...
```

Expected: all PASS.

- [ ] **Run the full frontend test + build once before opening a PR**

```bash
cd desktop/frontend
npx vitest run
npm run build
```

Expected: all PASS / build succeeds.

- [ ] **Run web vanilla tests (smoke check; this feature doesn't touch web/, but verify no regression)**

```bash
cd /Users/attson/code/github.com.attson/atterm
node --test web/*.test.mjs
```

Expected: all PASS.

- [ ] **Open the PR**

```bash
gh pr create --title "feat: driver/viewer mode for multi-client PTY sizing" --body "$(cat <<'EOF'
## Summary
- Adds a single-driver runtime role to sessions: one subscriber drives the PTY (IN/RESIZE/PASTE_IMAGE), others are viewers locked to the PTY's current size.
- New `CLAIM_DRIVER` frame (0x34) lets viewers take over by pressing space; no owner confirmation required.
- Fixes visible xterm corruption when a remote attacher with different fit dims joined a local session.

## Test plan
- [x] `go test ./...` passes
- [x] `npm run build` + `vitest run` passes
- [x] Manual: owner local + one remote attacher renders without corruption; space takes over both directions; remote disconnect drops driver gracefully
EOF
)"
```

Expected: PR URL printed.

---

## Self-Review

**Spec coverage check:**

- Driver assignment (auto-promote first) — Task 3 ✓
- Driver semantics (server enforcement + client UI) — Tasks 6, 9, 10, 11 ✓
- Transition feedback (banner + badge) — Tasks 10, 12 ✓
- Permission interaction (view-permission can't claim) — Task 6 (CLAIM_DRIVER drop logic) ✓
- Edge cases (driver disconnect, race on claim, owner reconnect, container size mismatch) — Tasks 5 (clear on unsubscribe), spec section ✓ — note: race-on-claim is handled by session lock serialization in Task 4 (`s.mu.Lock()` in `ClaimDriver`); container size mismatch is documented behavior, no code path needed.
- Protocol additions (TypeClaimDriver, ATTACH.client_id, MetaPayload.driver_client_id/cols/rows) — Tasks 1, 10 (Step 4) ✓
- Components touched — all listed files appear in the plan ✓
- Risks (old client compatibility, FitAddon race, view-permission protection) — addressed in code (empty client_id allowed; `safeFit` early-returns when !isDriver; CLAIM_DRIVER drops view-permission) ✓

**Placeholder scan:** Searched for "TBD", "TODO", "fill in", "implement later", "appropriate error handling", "similar to Task" — none found.

**Type consistency:**
- `Subscribe(sinceSeq uint64, clientID string)` — used consistently in Tasks 3, 7, and existing callers.
- `ClaimDriver(sub *Subscriber, clientID string)` — used in Tasks 4, 6 (relay), 7 (uplink via `ClaimLocalDriver`).
- `IsDriver(sub *Subscriber) bool` — defined Task 2, used Task 6.
- `DriverClientID() string` — defined Task 3, used in Tasks 4 and 5 tests.
- `broadcastDriverMeta(meta proto.SessionInfo, driverClientID string)` — defined Task 4, used Tasks 5 (clear on unsubscribe), 10 (UpdateSize broadcast).
- `claimDriver()` (frontend) — defined Task 9, used Task 11.
- `onDriverChange(driverClientID string, isMe boolean)` — defined Task 9, consumed Task 10.

All signatures consistent across tasks.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-14-driver-viewer-mode.md`. Two execution options:

**1. Subagent-Driven (recommended)** — Fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session with checkpoints for review.

Which approach?
