# Connection Health Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Continuously monitor each client's WebSocket link to the relay (RTT, byte rate, reconnect history, seq gaps) and surface its quality through a pill in the title/header that opens an on-demand drawer.

**Architecture:** Reuse the unused `TypePing (0x20)` / `TypePong (0x21)` frames — clients send PING with an 8-byte BE monotonic-ms timestamp every 5 s; the relay's `/uplink` and `/client` readers echo the payload back as a PONG. Clients compute `RTT = now_ms - decoded_ts`. A small `connhealth.Tracker` (Go for desktop uplink, TS for web/mobile) aggregates RTT samples (60-point ring), connection state, reconnect count, byte/sec EMAs, and seq-gap count. A `ConnHealthPill` + `ConnHealthDrawer` Vue pair (in `web/src/shared/components`, reused by both desktop and web/mobile) renders the data.

**Tech Stack:** Go (relay + desktop uplink), TypeScript (web/PWA + mobile + desktop frontend), Vue 3 + Naive UI, vitest, Wails RPC, gorilla/coder WebSocket (Go), browser WebSocket API (TS).

**Spec:** [`docs/superpowers/specs/2026-06-15-connection-health-monitoring-design.md`](../specs/2026-06-15-connection-health-monitoring-design.md)

---

## File Structure

New files:

- `internal/connhealth/connhealth.go` — Go tracker library (RTT ring, EMA, state machine, snapshot DTO).
- `internal/connhealth/connhealth_test.go`
- `desktop/frontend/src/composables/useUplinkHealth.ts` — Vue composable that polls `GetUplinkHealth` via Wails RPC.
- `web/src/shared/connhealth/connhealth.ts` — TS tracker (mirror of Go).
- `web/src/shared/connhealth/connhealth.test.ts`
- `web/src/shared/components/ConnHealthPill.vue`
- `web/src/shared/components/ConnHealthPill.test.ts`
- `web/src/shared/components/ConnHealthDrawer.vue`
- `web/src/shared/components/ConnHealthDrawer.test.ts`
- `desktop/app_conn_health_test.go`

Modified:

- `internal/proto/codec.go` — add `EncodePingTimestamp` / `DecodePingTimestamp` helpers.
- `internal/proto/codec_test.go` — round-trip test for ping payload.
- `internal/relay/uplink_conn.go` — echo PING payload as PONG.
- `internal/relay/uplink_conn_test.go` — assertion.
- `internal/relay/client_conn.go` — echo PING payload as PONG.
- `internal/relay/client_conn_test.go` — assertion.
- `desktop/uplink.go` — instantiate Tracker, send PING every 5 s, record byte/RTT, expose `Health()`.
- `desktop/app.go` — add `GetUplinkHealth()` Wails method.
- `desktop/frontend/src/lib/api.ts` — TS binding.
- `desktop/frontend/src/components/TitleBar.vue` — mount pill.
- `desktop/frontend/src/components/SettingsGeneral.vue` — toggle.
- `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts` — strings.
- `web/src/shared/ws/client-conn.ts` — instantiate Tracker, send PING every 5 s, record byte/RTT, expose `getHealth()` + `onHealthChange()`.
- `web/src/main/App.vue` — mount pill in header.
- `web/src/shared/i18n/messages/en.ts` and `zh-CN.ts` — strings.
- `docs/spec/protocol.md` — document PING/PONG payload semantics.

---

## Tasks

### Task 1: Add ping-timestamp codec helpers

**Files:**
- Modify: `internal/proto/codec.go`
- Modify: `internal/proto/codec_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/proto/codec_test.go`:

```go
func TestPingTimestampRoundTrip(t *testing.T) {
	for _, ts := range []uint64{0, 1, 1234567, 1<<53 - 1, 1<<63 - 1} {
		payload := EncodePingTimestamp(ts)
		if len(payload) != 8 {
			t.Fatalf("EncodePingTimestamp(%d) len = %d, want 8", ts, len(payload))
		}
		got, ok := DecodePingTimestamp(payload)
		if !ok {
			t.Fatalf("DecodePingTimestamp(%v) ok=false", payload)
		}
		if got != ts {
			t.Fatalf("DecodePingTimestamp round-trip = %d, want %d", got, ts)
		}
	}
}

func TestDecodePingTimestamp_WrongLength(t *testing.T) {
	for _, sz := range []int{0, 1, 7, 9, 16} {
		if _, ok := DecodePingTimestamp(make([]byte, sz)); ok {
			t.Fatalf("DecodePingTimestamp(len=%d) ok=true, want false", sz)
		}
	}
}

func TestPingPayloadSurvivesMarshalUnmarshal(t *testing.T) {
	in := Frame{Type: TypePing, Payload: EncodePingTimestamp(0xDEADBEEFCAFEBABE)}
	out, err := Unmarshal(Marshal(in))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Type != TypePing {
		t.Fatalf("Type = 0x%02x, want 0x%02x", out.Type, TypePing)
	}
	got, ok := DecodePingTimestamp(out.Payload)
	if !ok || got != 0xDEADBEEFCAFEBABE {
		t.Fatalf("round-trip ts = %d ok=%v", got, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/proto/ -run TestPingTimestampRoundTrip -v
```

Expected: FAIL with `undefined: EncodePingTimestamp`.

- [ ] **Step 3: Implement helpers**

Append to `internal/proto/codec.go`:

```go
// EncodePingTimestamp returns the 8-byte big-endian encoding of ts, used as
// the payload of a TypePing frame. The relay echoes the payload back inside
// the matching TypePong, letting the sender compute RTT without trusting
// the server's clock.
func EncodePingTimestamp(ts uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, ts)
	return b
}

// DecodePingTimestamp returns the timestamp encoded in a TypePing or TypePong
// payload. ok is false if the payload is not exactly 8 bytes — older clients
// or relays may emit empty payloads, in which case the caller should treat
// the frame as a liveness signal only and not record an RTT sample.
func DecodePingTimestamp(payload []byte) (uint64, bool) {
	if len(payload) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(payload), true
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/proto/ -run 'TestPingTimestamp|TestDecodePingTimestamp|TestPingPayloadSurvives' -v
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/proto/codec.go internal/proto/codec_test.go
git commit -m "feat(proto): add EncodePingTimestamp/DecodePingTimestamp helpers"
```

---

### Task 2: Echo PING payload in relay /uplink

**Files:**
- Modify: `internal/relay/uplink_conn.go` (around line 474, the `case proto.TypePong` block plus add a `case proto.TypePing`).
- Modify: `internal/relay/uplink_conn_test.go` (or create if missing — there is `uplink_webhook_test.go` etc., use the existing test harness pattern in `uplink_disconnect_notification_test.go` or `agent_conn_test.go`).

The uplink reader currently has `case proto.TypePong: // keepalive` and no PING case. We need a PING case that pushes a PONG with the same payload onto the existing `uplinkOut` write channel.

- [ ] **Step 1: Write the failing test**

Open `internal/relay/uplink_conn_test.go`. If the file does not exist, create it. Find any existing test that uses `newTestServer` + `dialUplink` (e.g. `uplink_webhook_test.go`) to copy the harness. Append the following test:

```go
func TestUplink_EchoesPingPayloadAsPong(t *testing.T) {
	srv, baseURL := newTestServer(t)
	defer srv.Close()

	c, sessionToken := dialUplink(t, baseURL, "uplink-rtt-echo")
	defer c.Close(websocket.StatusNormalClosure, "")

	// Send PING with a known 8-byte payload.
	wantTS := uint64(0x0123_4567_89AB_CDEF)
	frame := proto.Frame{Type: proto.TypePing, Payload: proto.EncodePingTimestamp(wantTS)}
	if err := c.Write(context.Background(), websocket.MessageBinary, proto.Marshal(frame)); err != nil {
		t.Fatalf("write PING: %v", err)
	}

	// Read frames until we get the matching PONG (the uplink may interleave
	// other admin frames like AUTH_INFO on connect — skip those).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for PONG echo")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Type != proto.TypePong {
			continue
		}
		got, ok := proto.DecodePingTimestamp(f.Payload)
		if !ok {
			t.Fatalf("PONG payload not 8 bytes: %x", f.Payload)
		}
		if got != wantTS {
			t.Fatalf("PONG ts = 0x%x, want 0x%x", got, wantTS)
		}
		_ = sessionToken
		return
	}
}
```

If `newTestServer` / `dialUplink` helpers don't exist with those exact names, look for the equivalents in the existing relay tests (grep `func.*testServer\|func dial.*Uplink`) and substitute. The behavior we are testing is independent of harness shape.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/relay/ -run TestUplink_EchoesPingPayloadAsPong -v
```

Expected: FAIL (timeout — current code drops PING silently).

- [ ] **Step 3: Implement the echo**

In `internal/relay/uplink_conn.go`, replace:

```go
		case proto.TypePong:
			// keepalive
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
```

with:

```go
		case proto.TypePing:
			// Echo the payload back as PONG. Clients use this to measure
			// application-level RTT (their PING carries an 8B timestamp;
			// the same payload comes back unchanged). Empty payloads are
			// also echoed unchanged so old clients still get a liveness
			// signal.
			pong := proto.Frame{Type: proto.TypePong, Payload: f.Payload}
			select {
			case uplinkOut <- pong:
			case <-connCtx.Done():
				return
			default:
				s.debugf("uplink pong_drop reason=out_full")
			}
		case proto.TypePong:
			// keepalive
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/relay/ -run TestUplink_EchoesPingPayloadAsPong -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_conn_test.go
git commit -m "feat(relay): echo PING payload as PONG on /uplink for RTT probing"
```

---

### Task 3: Echo PING payload in relay /client

**Files:**
- Modify: `internal/relay/client_conn.go` (around line 222, the `case proto.TypePong` block — add a `case proto.TypePing` above it).
- Modify: `internal/relay/client_conn_test.go` (file exists; append).

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/client_conn_test.go`:

```go
func TestClient_EchoesPingPayloadAsPong(t *testing.T) {
	srv, baseURL := newTestServer(t)
	defer srv.Close()

	// Set up a session reachable via /client. Reuse whatever helper the
	// surrounding tests use to register a session and dial /client with a
	// valid session token (e.g. dialClient(t, baseURL, sessionID, ...)).
	sess := registerTestSession(t, srv)
	c := dialClient(t, baseURL, sess.ID.String())
	defer c.Close(websocket.StatusNormalClosure, "")

	// ATTACH first (existing tests show the expected payload shape) — skipped
	// here only because the relay accepts PING before ATTACH; if not, send
	// ATTACH using the same helper as the neighbouring tests.

	wantTS := uint64(0xFEEDFACECAFEBEEF)
	frame := proto.Frame{Type: proto.TypePing, SessionID: sess.ID, Payload: proto.EncodePingTimestamp(wantTS)}
	if err := c.Write(context.Background(), websocket.MessageBinary, proto.Marshal(frame)); err != nil {
		t.Fatalf("write PING: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for PONG echo")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type != proto.TypePong {
			continue
		}
		got, ok := proto.DecodePingTimestamp(f.Payload)
		if !ok || got != wantTS {
			t.Fatalf("PONG payload mismatch: ok=%v got=0x%x want=0x%x", ok, got, wantTS)
		}
		return
	}
}
```

If `registerTestSession` / `dialClient` aren't the exact helper names in the file, grep for the existing client_conn tests' setup helpers and substitute.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/relay/ -run TestClient_EchoesPingPayloadAsPong -v
```

Expected: FAIL.

- [ ] **Step 3: Implement the echo**

In `internal/relay/client_conn.go`, find:

```go
		case proto.TypePong:
			// keepalive response

		default:
			log.Printf("client: unexpected frame type 0x%02x", f.Type)
```

Insert a PING case before TypePong. The /client conn writes to its WS via a helper or a write channel — find the variable used in the file (grep for `c.Write` or for a channel like `outbound` / `sendCh`) and use the same path. If the existing writer is a goroutine reading from a channel, push the pong onto that channel; if writes are direct, do a direct `c.Write`. The new block:

```go
		case proto.TypePing:
			// Echo PING payload as PONG so the client can compute RTT
			// without trusting the server clock.
			pong := proto.Frame{Type: proto.TypePong, SessionID: f.SessionID, Payload: f.Payload}
			if err := sendClientFrame(ctx, c, pong); err != nil {
				s.debugf("client pong_write_failed error=%q", err)
				return
			}
		case proto.TypePong:
			// keepalive response
```

Replace `sendClientFrame` with whatever the file uses to write outbound frames — read the file's top 200 lines and pick the matching helper. Most likely a direct `c.Write(ctx, websocket.MessageBinary, proto.Marshal(pong))` with the existing write deadline pattern.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/relay/ -run TestClient_EchoesPingPayloadAsPong -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/client_conn.go internal/relay/client_conn_test.go
git commit -m "feat(relay): echo PING payload as PONG on /client for RTT probing"
```

---

### Task 4: Create `internal/connhealth` Go library

**Files:**
- Create: `internal/connhealth/connhealth.go`
- Create: `internal/connhealth/connhealth_test.go`

The Tracker is a pure data structure with three input methods (`OnPongRTT`, `OnBytesIn`, `OnBytesOut`, `OnStateChange`, `OnSeqGap`) and one output (`Snapshot`). No I/O, no timers, no goroutines — those are driven by the caller (the uplink/client conn loop). This keeps the test simple and the Tracker reusable in any callsite.

- [ ] **Step 1: Write the failing test**

Create `internal/connhealth/connhealth_test.go`:

```go
package connhealth

import (
	"testing"
	"time"
)

func TestTracker_InitialSnapshot(t *testing.T) {
	tr := New()
	s := tr.Snapshot(time.Unix(0, 0))
	if s.State != StateClosed {
		t.Fatalf("initial state = %q, want %q", s.State, StateClosed)
	}
	if s.RTT.LastMS != nil {
		t.Fatalf("initial RTT non-nil")
	}
	if s.Reconnect.CountLastHour != 0 {
		t.Fatalf("initial reconnect count = %d", s.Reconnect.CountLastHour)
	}
}

func TestTracker_RecordsRTTAndComputesPercentiles(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	for i, ms := range []int{50, 60, 70, 80, 90, 100, 110, 120, 130, 140} {
		tr.OnPongRTT(ms, time.Unix(int64(i), 0))
	}
	s := tr.Snapshot(time.Unix(10, 0))
	if got := *s.RTT.LastMS; got != 140 {
		t.Fatalf("last RTT = %d, want 140", got)
	}
	if got := *s.RTT.P50MS; got < 90 || got > 100 {
		t.Fatalf("p50 = %d, want ~95", got)
	}
	if got := *s.RTT.P95MS; got < 130 || got > 140 {
		t.Fatalf("p95 = %d, want ~135-140", got)
	}
	if len(s.RTTSamples) != 10 {
		t.Fatalf("samples len = %d, want 10", len(s.RTTSamples))
	}
}

func TestTracker_RTTRingEvictsOldest(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	for i := 0; i < RTTRingSize+5; i++ {
		tr.OnPongRTT(i, time.Unix(int64(i), 0))
	}
	s := tr.Snapshot(time.Unix(0, 0))
	if len(s.RTTSamples) != RTTRingSize {
		t.Fatalf("ring size = %d, want %d", len(s.RTTSamples), RTTRingSize)
	}
	// Oldest sample should be i=5 (we wrote 65 items into a 60-slot ring).
	first := s.RTTSamples[0]
	if first.RTTMS != 5 {
		t.Fatalf("ring[0].rtt = %d, want 5", first.RTTMS)
	}
}

func TestTracker_StateTransitionsLogReconnects(t *testing.T) {
	tr := New()
	tr.SetState(StateConnecting, time.Unix(0, 0))
	tr.SetState(StateConnected, time.Unix(1, 0))
	tr.SetState(StateReconnecting, time.Unix(60, 0))
	tr.SetReconnectReason("ws_close_1006", time.Unix(60, 0))
	tr.SetState(StateConnected, time.Unix(62, 0))
	s := tr.Snapshot(time.Unix(120, 0))
	if s.Reconnect.CountLastHour != 1 {
		t.Fatalf("reconnect count = %d, want 1", s.Reconnect.CountLastHour)
	}
	if len(s.Reconnect.History) != 1 {
		t.Fatalf("history len = %d", len(s.Reconnect.History))
	}
	h := s.Reconnect.History[0]
	if h.Reason != "ws_close_1006" {
		t.Fatalf("reason = %q", h.Reason)
	}
	if h.DurationMS != 2000 {
		t.Fatalf("downtime = %d ms, want 2000", h.DurationMS)
	}
}

func TestTracker_ReconnectCountWindowsAtOneHour(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	// Two reconnects two hours ago (3600s window).
	tr.SetState(StateReconnecting, time.Unix(100, 0))
	tr.SetState(StateConnected, time.Unix(101, 0))
	tr.SetState(StateReconnecting, time.Unix(200, 0))
	tr.SetState(StateConnected, time.Unix(201, 0))
	// One inside the window.
	tr.SetState(StateReconnecting, time.Unix(5000, 0))
	tr.SetState(StateConnected, time.Unix(5001, 0))
	s := tr.Snapshot(time.Unix(5500, 0))
	if s.Reconnect.CountLastHour != 1 {
		t.Fatalf("reconnect count = %d, want 1 (5000s is inside, 100/200 are outside the 1h window from t=5500)", s.Reconnect.CountLastHour)
	}
}

func TestTracker_BytesEMA(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	// 1000 bytes/s for 10 s.
	for i := 0; i < 10; i++ {
		tr.OnBytesIn(1000, time.Unix(int64(i), 0))
		tr.OnBytesOut(500, time.Unix(int64(i), 0))
		tr.Tick(time.Unix(int64(i), 0))
	}
	s := tr.Snapshot(time.Unix(10, 0))
	if s.Bytes.InPerSec < 900 || s.Bytes.InPerSec > 1100 {
		t.Fatalf("in_per_sec = %d, want ~1000", s.Bytes.InPerSec)
	}
	if s.Bytes.OutPerSec < 450 || s.Bytes.OutPerSec > 550 {
		t.Fatalf("out_per_sec = %d, want ~500", s.Bytes.OutPerSec)
	}
}

func TestTracker_SeqGapsCounter(t *testing.T) {
	tr := New()
	tr.OnSeqGap()
	tr.OnSeqGap()
	tr.OnSeqGap()
	if got := tr.Snapshot(time.Unix(0, 0)).SeqGaps; got != 3 {
		t.Fatalf("seq_gaps = %d, want 3", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/connhealth/ -v
```

Expected: FAIL with "no Go files in …/internal/connhealth/" or "undefined: New".

- [ ] **Step 3: Implement Tracker**

Create `internal/connhealth/connhealth.go`:

```go
// Package connhealth tracks the health of a single WebSocket connection to
// the relay. A Tracker is fed RTT samples, byte counters, state transitions,
// and seq-gap events from the conn's read/write loops, and exposes an
// immutable Snapshot consumable by UI code (via Wails RPC on desktop, via a
// Vue ref on web/mobile).
//
// The Tracker is intentionally pure: no I/O, no goroutines, no timers. All
// time inputs are passed in so tests can drive it deterministically.
package connhealth

import (
	"sort"
	"sync"
	"time"
)

const (
	// RTTRingSize is the number of RTT samples retained. At one sample
	// every PingPeriodMS, this is a 5-minute sliding window.
	RTTRingSize = 60

	// ReconnectHistorySize is the number of recent reconnect events kept
	// for the drawer's "Recent reconnects" table.
	ReconnectHistorySize = 5

	// ReconnectWindowMS is the rolling window for CountLastHour.
	ReconnectWindowMS = 60 * 60 * 1000

	// bytesEMAAlpha is the smoothing factor for the 1-Hz byte EMAs.
	bytesEMAAlpha = 0.2
)

type State string

const (
	StateClosed       State = "closed"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
)

type RTTSample struct {
	AtMS  int64 `json:"at_ms"`
	RTTMS int   `json:"rtt_ms"`
}

type ReconnectEvent struct {
	AtMS       int64  `json:"at_ms"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"duration_ms"`
}

type Snapshot struct {
	State State `json:"state"`
	RTT   struct {
		LastMS *int `json:"last_ms"`
		P50MS  *int `json:"p50_ms"`
		P95MS  *int `json:"p95_ms"`
	} `json:"rtt"`
	RTTSamples []RTTSample `json:"rtt_samples"`
	Reconnect  struct {
		CountLastHour int              `json:"count_last_hour"`
		LastAtMS      *int64           `json:"last_at_ms"`
		LastReason    string           `json:"last_reason"`
		History       []ReconnectEvent `json:"history"`
	} `json:"reconnect"`
	Bytes struct {
		InPerSec  int64 `json:"in_per_sec"`
		OutPerSec int64 `json:"out_per_sec"`
	} `json:"bytes"`
	SeqGaps int `json:"seq_gaps"`
}

type Tracker struct {
	mu sync.Mutex

	state       State
	stateSince  time.Time
	rttRing     [RTTRingSize]RTTSample
	rttCount    int  // total samples ever written (for ring index)
	rttFilled   bool // becomes true once rttCount >= RTTRingSize

	reconnects        []ReconnectEvent
	pendingReason     string
	pendingReconnect  bool
	reconnectStartAt  time.Time

	bytesInBucket    int64
	bytesOutBucket   int64
	bytesBucketSec   int64
	bytesInPerSec    float64
	bytesOutPerSec   float64

	seqGaps int
}

func New() *Tracker {
	return &Tracker{state: StateClosed}
}

// SetState transitions to s at now. The caller is responsible for calling
// SetReconnectReason BEFORE SetState(StateReconnecting) if a reason is known.
func (t *Tracker) SetState(s State, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == s {
		return
	}
	switch {
	case s == StateReconnecting:
		t.pendingReconnect = true
		t.reconnectStartAt = now
	case t.pendingReconnect && s == StateConnected:
		duration := now.Sub(t.reconnectStartAt)
		ev := ReconnectEvent{
			AtMS:       t.reconnectStartAt.UnixMilli(),
			Reason:     t.pendingReason,
			DurationMS: duration.Milliseconds(),
		}
		t.reconnects = append(t.reconnects, ev)
		if len(t.reconnects) > ReconnectHistorySize {
			t.reconnects = t.reconnects[len(t.reconnects)-ReconnectHistorySize:]
		}
		t.pendingReconnect = false
		t.pendingReason = ""
	}
	t.state = s
	t.stateSince = now
}

// SetReconnectReason records why the next reconnect happened. Call this from
// the WS close handler (with the mapped reason string) before SetState.
func (t *Tracker) SetReconnectReason(reason string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pendingReason = reason
}

// OnPongRTT records an RTT sample (ms). Caller should only call this when
// the matching ping's payload round-tripped successfully.
func (t *Tracker) OnPongRTT(rttMS int, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := t.rttCount % RTTRingSize
	t.rttRing[idx] = RTTSample{AtMS: now.UnixMilli(), RTTMS: rttMS}
	t.rttCount++
	if t.rttCount >= RTTRingSize {
		t.rttFilled = true
	}
}

// OnBytesIn / OnBytesOut accumulate per-second buckets. The caller MUST call
// Tick(now) once per second (or more frequently — Tick is idempotent within
// the same second).
func (t *Tracker) OnBytesIn(n int, now time.Time)  { t.addBytes(int64(n), 0, now) }
func (t *Tracker) OnBytesOut(n int, now time.Time) { t.addBytes(0, int64(n), now) }

func (t *Tracker) addBytes(in, out int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sec := now.Unix()
	if sec != t.bytesBucketSec {
		t.rolloverBuckets(sec)
	}
	t.bytesInBucket += in
	t.bytesOutBucket += out
}

// Tick rolls forward the per-second EMA. Safe to call from a 1 Hz timer.
func (t *Tracker) Tick(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sec := now.Unix()
	if sec != t.bytesBucketSec {
		t.rolloverBuckets(sec)
	}
}

func (t *Tracker) rolloverBuckets(newSec int64) {
	// Apply EMA with the previous bucket's totals, then reset.
	t.bytesInPerSec = bytesEMAAlpha*float64(t.bytesInBucket) + (1-bytesEMAAlpha)*t.bytesInPerSec
	t.bytesOutPerSec = bytesEMAAlpha*float64(t.bytesOutBucket) + (1-bytesEMAAlpha)*t.bytesOutPerSec
	t.bytesInBucket = 0
	t.bytesOutBucket = 0
	t.bytesBucketSec = newSec
}

// OnSeqGap increments the seq-gap counter. The caller observes TypeOut frames
// and increments when the new seq is more than 1 past the last seen.
func (t *Tracker) OnSeqGap() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seqGaps++
}

// Snapshot returns an immutable view at the given wall-clock now.
func (t *Tracker) Snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	var s Snapshot
	s.State = t.state
	s.SeqGaps = t.seqGaps
	s.Bytes.InPerSec = int64(t.bytesInPerSec)
	s.Bytes.OutPerSec = int64(t.bytesOutPerSec)

	samples := t.orderedRTTLocked()
	s.RTTSamples = samples
	if len(samples) > 0 {
		last := samples[len(samples)-1].RTTMS
		s.RTT.LastMS = &last
		sorted := make([]int, len(samples))
		for i, sm := range samples {
			sorted[i] = sm.RTTMS
		}
		sort.Ints(sorted)
		p50 := percentile(sorted, 50)
		p95 := percentile(sorted, 95)
		s.RTT.P50MS = &p50
		s.RTT.P95MS = &p95
	}

	nowMS := now.UnixMilli()
	cutoff := nowMS - ReconnectWindowMS
	count := 0
	for _, ev := range t.reconnects {
		if ev.AtMS >= cutoff {
			count++
		}
	}
	s.Reconnect.CountLastHour = count
	if len(t.reconnects) > 0 {
		s.Reconnect.History = append([]ReconnectEvent(nil), t.reconnects...)
		last := t.reconnects[len(t.reconnects)-1]
		s.Reconnect.LastAtMS = &last.AtMS
		s.Reconnect.LastReason = last.Reason
	}
	return s
}

func (t *Tracker) orderedRTTLocked() []RTTSample {
	if !t.rttFilled {
		return append([]RTTSample(nil), t.rttRing[:t.rttCount]...)
	}
	start := t.rttCount % RTTRingSize
	out := make([]RTTSample, 0, RTTRingSize)
	out = append(out, t.rttRing[start:]...)
	out = append(out, t.rttRing[:start]...)
	return out
}

func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	// Nearest-rank.
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/connhealth/ -v
```

Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/connhealth/
git commit -m "feat(connhealth): add Tracker for WS link RTT/state/byte aggregation"
```

---

### Task 5: Wire Tracker into desktop uplink

**Files:**
- Modify: `desktop/uplink.go`

The uplink already has the `runOnce(ctx)` function with its own reader and writer goroutines. We:

1. Add a `*connhealth.Tracker` field on the `uplink` struct, initialized in `newUplink`.
2. In `runOnce`, after the WS dial succeeds, call `Tracker.SetState(StateConnected, time.Now())`.
3. In the reader, on `proto.TypePong`, decode payload as 8-byte ts and call `Tracker.OnPongRTT`.
4. In the reader, on every successful `Unmarshal`, call `Tracker.OnBytesIn(len(data), time.Now())`.
5. In the reader, on `proto.TypeOut`, compare `seq` to a per-session `lastSeq` and call `Tracker.OnSeqGap()` if the gap > 1. (Reuse the existing per-stream tracking if present — at minimum count gaps on the most recent stream.)
6. In the writer, after each successful `conn.Write`, call `Tracker.OnBytesOut(len(marshalled), time.Now())`.
7. Add a new writer goroutine: every `pingPeriod = 5 * time.Second`, push a `TypePing` frame onto the `out` channel with `EncodePingTimestamp(monotonic_ms())`. Use `time.Since(u.startMono).Milliseconds()` where `u.startMono` is captured in `newUplink`. Skip the send if `Tracker.SnapshotState() != StateConnected` (we'd send into a non-open WS).
8. Add a 1 Hz `Tracker.Tick(time.Now())` ticker (in any of the existing goroutines or a new tiny one).
9. On read or write error / `runOnce` return: `Tracker.SetReconnectReason(reasonString, time.Now())` then `Tracker.SetState(StateReconnecting, ...)`. Reason mapping: WS close code → string ("ws_close_1006", "ws_close_4001", etc.), or `"network_error"` / `"context_canceled"`.
10. Expose `func (u *uplink) Health() connhealth.Snapshot { return u.tracker.Snapshot(time.Now()) }`.

- [ ] **Step 1: Read current uplink.go structure**

```bash
sed -n '30,80p' desktop/uplink.go
```

Confirm `type uplink struct { … }` and `func newUplink(...)` and `func (u *uplink) runOnce(ctx)` exist as expected.

- [ ] **Step 2: Edit uplink.go — add tracker field and constructor wire-up**

In `desktop/uplink.go`, change the `uplink struct` definition to include:

```go
	tracker  *connhealth.Tracker
	startMono time.Time
```

In `newUplink`, after the existing field initialization, add:

```go
	u.tracker = connhealth.New()
	u.startMono = time.Now()
```

Add the import for `"github.com/<repo-go-mod-path>/internal/connhealth"` at the top — discover the module path by running `head -1 go.mod`.

- [ ] **Step 3: Edit uplink.go — instrument reader / writer**

In `runOnce`, replace the existing `proto.TypePong: // keepalive ack from relay` block with:

```go
		case proto.TypePong:
			if ts, ok := proto.DecodePingTimestamp(f.Payload); ok {
				nowMS := time.Since(u.startMono).Milliseconds()
				rtt := int(nowMS - int64(ts))
				if rtt >= 0 && rtt < 60_000 {
					u.tracker.OnPongRTT(rtt, time.Now())
				}
			}
```

In the reader loop, immediately after each successful `proto.Unmarshal`, add:

```go
		u.tracker.OnBytesIn(len(data), time.Now())
```

In the `case proto.TypeOut` block (inside the local-forwarder, not `runOnce` — re-grep to find where uplink consumes OUT frames; if it doesn't consume OUT directly because OUT is what the uplink SENDS, instrument the seq counter inside `forwardLocalSubscriberFrame` instead by adding `u.tracker.OnBytesOut(...)` after the channel push).

In the writer goroutine, after each successful `conn.Write(...)` add:

```go
		u.tracker.OnBytesOut(len(data), time.Now())
```

(where `data` is the `proto.Marshal(f)` value; lift it to a variable.)

- [ ] **Step 4: Edit uplink.go — add the periodic PING sender + Tick**

Inside `runOnce`, after the existing writer goroutine, add:

```go
	// Periodic PING for RTT measurement + 1Hz Tick for byte EMAs.
	go func() {
		pingTicker := time.NewTicker(5 * time.Second)
		tickTicker := time.NewTicker(time.Second)
		defer pingTicker.Stop()
		defer tickTicker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-tickTicker.C:
				u.tracker.Tick(time.Now())
			case <-pingTicker.C:
				ts := uint64(time.Since(u.startMono).Milliseconds())
				frame := proto.Frame{Type: proto.TypePing, Payload: proto.EncodePingTimestamp(ts)}
				select {
				case out <- frame:
				case <-connCtx.Done():
					return
				default:
					// Channel full — skip; next tick will retry.
				}
			}
		}
	}()
```

- [ ] **Step 5: Edit uplink.go — state transitions**

Just after the WS dial succeeds (right before `u.writeAnnounce`):

```go
	u.tracker.SetState(connhealth.StateConnected, time.Now())
```

In the `runOnce` defer / cleanup path (or wherever the function returns with an error or normally), add:

```go
	defer func() {
		reason := "network_error"
		// Mapped reason can be set by handleCloseError; for now record the last seen code.
		u.tracker.SetReconnectReason(reason, time.Now())
		u.tracker.SetState(connhealth.StateReconnecting, time.Now())
	}()
```

Inside `handleCloseError`, after the existing reason-mapping switch, call:

```go
	u.tracker.SetReconnectReason(reason, time.Now())
```

In `Run` (the outer retry loop that calls `runOnce`), at the very top of the loop just before dialing, call:

```go
	u.tracker.SetState(connhealth.StateConnecting, time.Now())
```

- [ ] **Step 6: Add the Health() accessor**

Append to `desktop/uplink.go`:

```go
// Health returns a snapshot of the uplink's link-health metrics. Safe to call
// concurrently with the read/write loops.
func (u *uplink) Health() connhealth.Snapshot {
	return u.tracker.Snapshot(time.Now())
}
```

- [ ] **Step 7: Verify build**

```bash
go vet -tags webkit2_41 ./desktop/...
go build -tags webkit2_41 ./desktop/...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add desktop/uplink.go
git commit -m "feat(desktop): instrument uplink with connhealth Tracker"
```

---

### Task 6: Add `GetUplinkHealth` Wails RPC + DTO

**Files:**
- Modify: `desktop/app.go`
- Create: `desktop/app_conn_health_test.go`
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/app_conn_health_test.go`:

```go
package main

import (
	"testing"
)

func TestGetUplinkHealth_NoUplinkReturnsClosed(t *testing.T) {
	a := &App{}
	s := a.GetUplinkHealth()
	if s.State != "closed" {
		t.Fatalf("state = %q, want closed", s.State)
	}
	if s.RTT.LastMS != nil {
		t.Fatalf("RTT non-nil on closed app")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -tags webkit2_41 ./desktop/ -run TestGetUplinkHealth_NoUplinkReturnsClosed -v
```

Expected: FAIL with `undefined: App.GetUplinkHealth`.

- [ ] **Step 3: Implement the RPC**

Find an unused area near the other relay-related methods in `desktop/app.go` (e.g. right after `SetUplinkPaused`). Add:

```go
// GetUplinkHealth returns a snapshot of the desktop uplink's connection
// health (RTT, reconnect history, byte rates, seq gaps). Surfaced to the
// frontend ConnHealthPill / ConnHealthDrawer.
func (a *App) GetUplinkHealth() connhealth.Snapshot {
	if a.uplink == nil {
		return connhealth.Snapshot{State: connhealth.StateClosed}
	}
	return a.uplink.Health()
}
```

Add the import `"github.com/<module>/internal/connhealth"`.

If the uplink reference on `App` is not named `a.uplink`, grep for it:

```bash
grep -n "uplink" desktop/app.go | head -10
```

and substitute.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -tags webkit2_41 ./desktop/ -run TestGetUplinkHealth_NoUplinkReturnsClosed -v
```

Expected: PASS.

- [ ] **Step 5: Add the TS binding**

In `desktop/frontend/src/lib/api.ts`, find the existing Wails-bound interface (`bindings()` or `App` interface). Add the method signature near the other relay-related methods:

```typescript
  GetUplinkHealth(): Promise<ConnHealthSnapshot>;
```

Below the interface, add the type definition:

```typescript
export interface ConnHealthSnapshot {
  state: 'closed' | 'connecting' | 'connected' | 'reconnecting';
  rtt: {
    last_ms: number | null;
    p50_ms: number | null;
    p95_ms: number | null;
  };
  rtt_samples: Array<{ at_ms: number; rtt_ms: number }>;
  reconnect: {
    count_last_hour: number;
    last_at_ms: number | null;
    last_reason: string;
    history: Array<{ at_ms: number; reason: string; duration_ms: number }>;
  };
  bytes: {
    in_per_sec: number;
    out_per_sec: number;
  };
  seq_gaps: number;
}

export function getUplinkHealth(): Promise<ConnHealthSnapshot> {
  return bindings().GetUplinkHealth();
}
```

- [ ] **Step 6: Regenerate Wails bindings**

```bash
cd desktop && wails generate module
```

This regenerates `desktop/frontend/wailsjs/go/main/App.{js,d.ts}` with the new method. Confirm the new method appears:

```bash
grep GetUplinkHealth desktop/frontend/wailsjs/go/main/App.d.ts
```

Expected: line listing the new method.

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/app_conn_health_test.go desktop/frontend/src/lib/api.ts desktop/frontend/wailsjs/
git commit -m "feat(desktop): expose GetUplinkHealth Wails RPC"
```

---

### Task 7: Create TS connhealth library

**Files:**
- Create: `web/src/shared/connhealth/connhealth.ts`
- Create: `web/src/shared/connhealth/connhealth.test.ts`

Mirror the Go semantics so both clients show the same numbers.

- [ ] **Step 1: Write the failing test**

Create `web/src/shared/connhealth/connhealth.test.ts`:

```typescript
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { Tracker, RTT_RING_SIZE, RECONNECT_HISTORY_SIZE } from './connhealth'

describe('Tracker', () => {
  let tr: Tracker
  beforeEach(() => {
    tr = new Tracker()
  })

  it('starts in the closed state with empty RTT', () => {
    const s = tr.snapshot(0)
    expect(s.state).toBe('closed')
    expect(s.rtt.last_ms).toBeNull()
    expect(s.rtt_samples).toEqual([])
    expect(s.reconnect.count_last_hour).toBe(0)
  })

  it('records RTT samples and computes p50/p95', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < 10; i++) {
      tr.onPongRTT(50 + i * 10, i * 1000)
    }
    const s = tr.snapshot(10_000)
    expect(s.rtt.last_ms).toBe(140)
    expect(s.rtt.p50_ms).toBeGreaterThanOrEqual(90)
    expect(s.rtt.p50_ms).toBeLessThanOrEqual(100)
    expect(s.rtt.p95_ms).toBeGreaterThanOrEqual(130)
    expect(s.rtt.p95_ms).toBeLessThanOrEqual(140)
    expect(s.rtt_samples.length).toBe(10)
  })

  it('evicts oldest RTT samples past RTT_RING_SIZE', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < RTT_RING_SIZE + 5; i++) {
      tr.onPongRTT(i, i * 1000)
    }
    const s = tr.snapshot(0)
    expect(s.rtt_samples.length).toBe(RTT_RING_SIZE)
    expect(s.rtt_samples[0].rtt_ms).toBe(5)
  })

  it('tracks reconnect events with duration and reason', () => {
    tr.setState('connected', 0)
    tr.setState('reconnecting', 60_000)
    tr.setReconnectReason('ws_close_1006', 60_000)
    tr.setState('connected', 62_000)
    const s = tr.snapshot(120_000)
    expect(s.reconnect.count_last_hour).toBe(1)
    expect(s.reconnect.history).toHaveLength(1)
    expect(s.reconnect.history[0].reason).toBe('ws_close_1006')
    expect(s.reconnect.history[0].duration_ms).toBe(2000)
  })

  it('windows reconnect count at one hour', () => {
    tr.setState('connected', 0)
    tr.setState('reconnecting', 100_000)
    tr.setState('connected', 101_000)
    tr.setState('reconnecting', 200_000)
    tr.setState('connected', 201_000)
    tr.setState('reconnecting', 5_000_000)
    tr.setState('connected', 5_001_000)
    const s = tr.snapshot(5_500_000)
    expect(s.reconnect.count_last_hour).toBe(1)
  })

  it('computes byte EMAs after Tick', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < 10; i++) {
      tr.onBytesIn(1000, i * 1000)
      tr.onBytesOut(500, i * 1000)
      tr.tick(i * 1000)
    }
    const s = tr.snapshot(10_000)
    expect(s.bytes.in_per_sec).toBeGreaterThanOrEqual(900)
    expect(s.bytes.in_per_sec).toBeLessThanOrEqual(1100)
    expect(s.bytes.out_per_sec).toBeGreaterThanOrEqual(450)
    expect(s.bytes.out_per_sec).toBeLessThanOrEqual(550)
  })

  it('counts seq gaps via onSeqGap', () => {
    tr.onSeqGap()
    tr.onSeqGap()
    tr.onSeqGap()
    expect(tr.snapshot(0).seq_gaps).toBe(3)
  })

  it('emits a "health-change" event on state transitions', () => {
    const listener = vi.fn()
    tr.onChange(listener)
    tr.setState('connecting', 0)
    tr.setState('connected', 100)
    expect(listener).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/shared/connhealth/connhealth.test.ts
```

Expected: FAIL with "Cannot find module './connhealth'".

- [ ] **Step 3: Implement Tracker**

Create `web/src/shared/connhealth/connhealth.ts`:

```typescript
// Mirror of internal/connhealth/connhealth.go — feeds the ConnHealthPill /
// ConnHealthDrawer for web/PWA/mobile clients. The semantics (RTT ring size,
// reconnect window, EMA alpha) MUST match the Go side so both desktop uplink
// and web/mobile show the same numbers.

export const RTT_RING_SIZE = 60
export const RECONNECT_HISTORY_SIZE = 5
export const RECONNECT_WINDOW_MS = 60 * 60 * 1000
const BYTES_EMA_ALPHA = 0.2

export type ConnState = 'closed' | 'connecting' | 'connected' | 'reconnecting'

export interface RTTSample {
  at_ms: number
  rtt_ms: number
}

export interface ReconnectEvent {
  at_ms: number
  reason: string
  duration_ms: number
}

export interface ConnHealthSnapshot {
  state: ConnState
  rtt: {
    last_ms: number | null
    p50_ms: number | null
    p95_ms: number | null
  }
  rtt_samples: RTTSample[]
  reconnect: {
    count_last_hour: number
    last_at_ms: number | null
    last_reason: string
    history: ReconnectEvent[]
  }
  bytes: {
    in_per_sec: number
    out_per_sec: number
  }
  seq_gaps: number
}

type Listener = () => void

export class Tracker {
  private state: ConnState = 'closed'
  private rttRing: RTTSample[] = []
  private rttHead = 0  // next write index
  private rttFilled = false

  private reconnects: ReconnectEvent[] = []
  private pendingReason = ''
  private pendingReconnect = false
  private reconnectStartAt = 0

  private bytesInBucket = 0
  private bytesOutBucket = 0
  private bytesBucketSec = 0
  private bytesInPerSec = 0
  private bytesOutPerSec = 0

  private seqGaps = 0

  private listeners: Set<Listener> = new Set()

  constructor() {
    for (let i = 0; i < RTT_RING_SIZE; i++) {
      this.rttRing.push({ at_ms: 0, rtt_ms: 0 })
    }
  }

  onChange(fn: Listener): () => void {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  private emit(): void {
    for (const fn of this.listeners) {
      try { fn() } catch { /* swallow */ }
    }
  }

  setState(s: ConnState, nowMS: number): void {
    if (this.state === s) return
    if (s === 'reconnecting') {
      this.pendingReconnect = true
      this.reconnectStartAt = nowMS
    } else if (this.pendingReconnect && s === 'connected') {
      const ev: ReconnectEvent = {
        at_ms: this.reconnectStartAt,
        reason: this.pendingReason,
        duration_ms: nowMS - this.reconnectStartAt,
      }
      this.reconnects.push(ev)
      if (this.reconnects.length > RECONNECT_HISTORY_SIZE) {
        this.reconnects = this.reconnects.slice(-RECONNECT_HISTORY_SIZE)
      }
      this.pendingReconnect = false
      this.pendingReason = ''
    }
    this.state = s
    this.emit()
  }

  setReconnectReason(reason: string, _nowMS: number): void {
    this.pendingReason = reason
  }

  onPongRTT(rttMS: number, nowMS: number): void {
    this.rttRing[this.rttHead] = { at_ms: nowMS, rtt_ms: rttMS }
    this.rttHead = (this.rttHead + 1) % RTT_RING_SIZE
    if (this.rttHead === 0) this.rttFilled = true
  }

  onBytesIn(n: number, nowMS: number): void {
    this.rollover(nowMS)
    this.bytesInBucket += n
  }

  onBytesOut(n: number, nowMS: number): void {
    this.rollover(nowMS)
    this.bytesOutBucket += n
  }

  tick(nowMS: number): void {
    this.rollover(nowMS)
  }

  private rollover(nowMS: number): void {
    const sec = Math.floor(nowMS / 1000)
    if (sec === this.bytesBucketSec) return
    this.bytesInPerSec = BYTES_EMA_ALPHA * this.bytesInBucket + (1 - BYTES_EMA_ALPHA) * this.bytesInPerSec
    this.bytesOutPerSec = BYTES_EMA_ALPHA * this.bytesOutBucket + (1 - BYTES_EMA_ALPHA) * this.bytesOutPerSec
    this.bytesInBucket = 0
    this.bytesOutBucket = 0
    this.bytesBucketSec = sec
  }

  onSeqGap(): void {
    this.seqGaps += 1
  }

  snapshot(nowMS: number): ConnHealthSnapshot {
    const samples = this.orderedRTT()
    const out: ConnHealthSnapshot = {
      state: this.state,
      rtt: { last_ms: null, p50_ms: null, p95_ms: null },
      rtt_samples: samples,
      reconnect: {
        count_last_hour: 0,
        last_at_ms: null,
        last_reason: '',
        history: this.reconnects.slice(),
      },
      bytes: {
        in_per_sec: Math.round(this.bytesInPerSec),
        out_per_sec: Math.round(this.bytesOutPerSec),
      },
      seq_gaps: this.seqGaps,
    }
    if (samples.length > 0) {
      out.rtt.last_ms = samples[samples.length - 1].rtt_ms
      const sorted = samples.map(s => s.rtt_ms).sort((a, b) => a - b)
      out.rtt.p50_ms = sorted[Math.floor(sorted.length * 0.5)]
      out.rtt.p95_ms = sorted[Math.floor(sorted.length * 0.95)] ?? sorted[sorted.length - 1]
    }
    const cutoff = nowMS - RECONNECT_WINDOW_MS
    out.reconnect.count_last_hour = this.reconnects.filter(e => e.at_ms >= cutoff).length
    if (this.reconnects.length > 0) {
      const last = this.reconnects[this.reconnects.length - 1]
      out.reconnect.last_at_ms = last.at_ms
      out.reconnect.last_reason = last.reason
    }
    return out
  }

  private orderedRTT(): RTTSample[] {
    if (!this.rttFilled) {
      return this.rttRing.slice(0, this.rttHead)
    }
    return this.rttRing.slice(this.rttHead).concat(this.rttRing.slice(0, this.rttHead))
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run src/shared/connhealth/connhealth.test.ts
```

Expected: PASS (8 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/connhealth/
git commit -m "feat(web): add TS Tracker mirror of internal/connhealth"
```

---

### Task 8: Wire Tracker into `web/src/shared/ws/client-conn.ts`

**Files:**
- Modify: `web/src/shared/ws/client-conn.ts`

- [ ] **Step 1: Edit `client-conn.ts` — fields and constructor**

In `client-conn.ts`, near the top, add:

```typescript
import { Tracker } from '../connhealth/connhealth'
```

Inside `class SessionConnection`, add a private field:

```typescript
  private readonly health = new Tracker()
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private tickTimer: ReturnType<typeof setInterval> | null = null
  private lastSeqForGap = 0
```

Expose:

```typescript
  getHealth() { return this.health.snapshot(Date.now()) }
  onHealthChange(fn: () => void) { return this.health.onChange(fn) }
```

- [ ] **Step 2: Edit `client-conn.ts` — instrument the lifecycle**

In `openWS`, right after `ws.binaryType = 'arraybuffer'`:

```typescript
    this.health.setState(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting', Date.now())
```

In `onopen`, after the existing `this.handlers.onStatus?.('attached')`:

```typescript
      this.health.setState('connected', Date.now())
      this.startPingLoop(ws)
```

In `onmessage`, after the `decodeFrame` succeeds:

```typescript
      this.health.onBytesIn(f.payload.byteLength + 22, Date.now()) // header + sid + payload
```

In `onmessage` switch, add a PONG case:

```typescript
        case TYPE.PONG: {
          const payload = f.payload
          if (payload.length === 8) {
            const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
            const hi = dv.getUint32(0, false)
            const lo = dv.getUint32(4, false)
            const sentMS = hi * 0x100000000 + lo
            const rtt = Date.now() - sentMS
            if (rtt >= 0 && rtt < 60_000) {
              this.health.onPongRTT(rtt, Date.now())
            }
          }
          break
        }
```

In the OUT case, before `this.lastSeq = seq`, check the gap:

```typescript
          if (this.lastSeqForGap !== 0 && seq > this.lastSeqForGap + 1) {
            this.health.onSeqGap()
          }
          this.lastSeqForGap = seq
```

In `onclose`, immediately add (before the reconnect-banner logic):

```typescript
      this.stopPingLoop()
      // The browser exposes a close code on ev (CloseEvent); use it for reason.
```

Actually `ws.onclose` already takes an event — change the signature to `ws.onclose = (ev: CloseEvent) => { ... }` and add:

```typescript
      const reason = ev.code ? `ws_close_${ev.code}` : 'ws_close'
      this.health.setReconnectReason(reason, Date.now())
      this.health.setState('reconnecting', Date.now())
```

Note: when `detached === true`, set state to `'closed'` instead of `'reconnecting'`.

Add the private methods at the bottom of the class:

```typescript
  private startPingLoop(ws: WebSocket): void {
    this.stopPingLoop()
    // 1 Hz tick for byte EMAs.
    this.tickTimer = setInterval(() => this.health.tick(Date.now()), 1000)
    // 5 s PING for RTT.
    this.pingTimer = setInterval(() => {
      if (ws.readyState !== WebSocket.OPEN) return
      const nowMS = Date.now()
      const payload = new Uint8Array(8)
      const dv = new DataView(payload.buffer)
      dv.setUint32(0, Math.floor(nowMS / 0x100000000), false)
      dv.setUint32(4, nowMS >>> 0, false)
      const frame = encodeFrame(TYPE.PING, this.sidBytes, payload)
      ws.send(frame)
      this.health.onBytesOut(frame.byteLength, Date.now())
    }, 5000)
  }

  private stopPingLoop(): void {
    if (this.pingTimer !== null) { clearInterval(this.pingTimer); this.pingTimer = null }
    if (this.tickTimer !== null) { clearInterval(this.tickTimer); this.tickTimer = null }
  }
```

In `detach()`, add:

```typescript
    this.stopPingLoop()
    this.health.setState('closed', Date.now())
```

Also: every existing `ws.send(...)` in `sendInput`, `sendResize`, `claimDriver`, `sendPasteImage` should count out-bytes:

```typescript
    // Right after each ws.send(frame), add:
    this.health.onBytesOut(frame.byteLength, Date.now())
```

(where `frame` is the existing `encodeFrame(...)` value; lift it to a `const` if not already.)

- [ ] **Step 3: Verify build**

```bash
cd web && npm run build
```

Expected: success.

- [ ] **Step 4: Add a focused test**

Append to `web/src/shared/ws/client-conn.test.ts` (or create a new test file `client-conn.health.test.ts`):

```typescript
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { SessionConnection } from './client-conn'
import { TYPE, encodeFrame, uuidToBytes } from './protocol'

class FakeWS {
  readyState = WebSocket.CONNECTING
  binaryType = 'arraybuffer'
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  sent: Uint8Array[] = []
  send(data: ArrayBuffer | Uint8Array) {
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
    this.sent.push(bytes)
  }
  close() { this.readyState = WebSocket.CLOSED }
}

describe('SessionConnection — connection health', () => {
  let fakeWS: FakeWS
  beforeEach(() => {
    fakeWS = new FakeWS()
    vi.stubGlobal('WebSocket', vi.fn(() => fakeWS))
    vi.useFakeTimers()
  })

  it('records RTT when a PONG echoes the PING timestamp', () => {
    const sid = '11111111-1111-1111-1111-111111111111'
    const conn = new SessionConnection(sid, {})
    conn.attach()
    fakeWS.readyState = WebSocket.OPEN
    fakeWS.onopen?.(new Event('open'))

    // Fast-forward 5 s — the ping timer fires once.
    vi.advanceTimersByTime(5000)
    expect(fakeWS.sent.some((b) => b[1] === TYPE.PING)).toBe(true)

    // Extract the PING payload and echo it back as PONG.
    const ping = fakeWS.sent.find((b) => b[1] === TYPE.PING)!
    const payload = ping.slice(6 + 16)
    const pong = encodeFrame(TYPE.PONG, uuidToBytes(sid), payload)
    fakeWS.onmessage?.({ data: pong.buffer } as MessageEvent)

    const health = conn.getHealth()
    expect(health.rtt.last_ms).not.toBeNull()
    expect(health.rtt.last_ms!).toBeGreaterThanOrEqual(0)
  })
})
```

Run:

```bash
cd web && npx vitest run src/shared/ws/client-conn.health.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/ws/client-conn.ts web/src/shared/ws/client-conn.health.test.ts
git commit -m "feat(web): instrument SessionConnection with connhealth Tracker"
```

---

### Task 9: ConnHealthPill component

**Files:**
- Create: `web/src/shared/components/ConnHealthPill.vue`
- Create: `web/src/shared/components/ConnHealthPill.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/shared/components/ConnHealthPill.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ConnHealthPill from './ConnHealthPill.vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

function snapshot(overrides: Partial<ConnHealthSnapshot> = {}): ConnHealthSnapshot {
  return {
    state: 'connected',
    rtt: { last_ms: 120, p50_ms: 110, p95_ms: 150 },
    rtt_samples: [],
    reconnect: { count_last_hour: 0, last_at_ms: null, last_reason: '', history: [] },
    bytes: { in_per_sec: 0, out_per_sec: 0 },
    seq_gaps: 0,
    ...overrides,
  }
}

describe('ConnHealthPill', () => {
  it('green band for RTT < 150 ms', () => {
    const w = mount(ConnHealthPill, { props: { health: snapshot({ rtt: { last_ms: 80, p50_ms: 80, p95_ms: 100 } }) } })
    expect(w.classes()).toContain('band-green')
    expect(w.text()).toContain('80')
  })
  it('yellow band for RTT 150–500 ms', () => {
    const w = mount(ConnHealthPill, { props: { health: snapshot({ rtt: { last_ms: 340, p50_ms: 300, p95_ms: 400 } }) } })
    expect(w.classes()).toContain('band-yellow')
  })
  it('red band for RTT > 500 ms', () => {
    const w = mount(ConnHealthPill, { props: { health: snapshot({ rtt: { last_ms: 800, p50_ms: 700, p95_ms: 900 } }) } })
    expect(w.classes()).toContain('band-red')
  })
  it('reconnecting state pulses regardless of RTT', () => {
    const w = mount(ConnHealthPill, { props: { health: snapshot({ state: 'reconnecting' }) } })
    expect(w.classes()).toContain('band-reconnecting')
    expect(w.text().toLowerCase()).toContain('reconnect')
  })
  it('closed state is dim and shows "off"', () => {
    const w = mount(ConnHealthPill, { props: { health: snapshot({ state: 'closed' }) } })
    expect(w.classes()).toContain('band-off')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/shared/components/ConnHealthPill.test.ts
```

Expected: FAIL with "Cannot find module './ConnHealthPill.vue'".

- [ ] **Step 3: Implement the component**

Create `web/src/shared/components/ConnHealthPill.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

const props = defineProps<{ health: ConnHealthSnapshot }>()

const band = computed(() => {
  if (props.health.state === 'reconnecting' || props.health.state === 'connecting') return 'band-reconnecting'
  if (props.health.state === 'closed') return 'band-off'
  const rtt = props.health.rtt.last_ms
  if (rtt === null) return 'band-green' // connected but no sample yet
  if (rtt < 150) return 'band-green'
  if (rtt < 500) return 'band-yellow'
  return 'band-red'
})

const label = computed(() => {
  if (props.health.state === 'reconnecting') return 'reconnecting…'
  if (props.health.state === 'connecting') return 'connecting…'
  if (props.health.state === 'closed') return 'off'
  const rtt = props.health.rtt.last_ms
  return rtt === null ? '—' : `${rtt} ms`
})
</script>

<template>
  <button class="conn-health-pill" :class="band" type="button" :aria-label="label">
    <span class="dot">●</span>
    <span class="text">{{ label }}</span>
  </button>
</template>

<style scoped>
.conn-health-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  height: 20px;
  border-radius: 999px;
  border: 1px solid var(--border, #ccc);
  background: transparent;
  color: inherit;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
}
.conn-health-pill .dot { font-size: 8px; line-height: 1; }
.band-green { color: var(--good, #16a34a); border-color: color-mix(in srgb, var(--good, #16a34a) 35%, transparent); }
.band-yellow { color: var(--warn, #d97706); border-color: color-mix(in srgb, var(--warn, #d97706) 35%, transparent); }
.band-red { color: var(--bad, #dc2626); border-color: color-mix(in srgb, var(--bad, #dc2626) 35%, transparent); }
.band-off { color: var(--fg-dim, #888); }
.band-reconnecting { color: var(--warn, #d97706); animation: pulse 1s ease-in-out infinite; }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
@media (prefers-reduced-motion: reduce) {
  .band-reconnecting { animation: none; }
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run src/shared/components/ConnHealthPill.test.ts
```

Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/components/ConnHealthPill.vue web/src/shared/components/ConnHealthPill.test.ts
git commit -m "feat(web): ConnHealthPill — RTT-banded link-health pill"
```

---

### Task 10: ConnHealthDrawer component

**Files:**
- Create: `web/src/shared/components/ConnHealthDrawer.vue`
- Create: `web/src/shared/components/ConnHealthDrawer.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/shared/components/ConnHealthDrawer.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ConnHealthDrawer from './ConnHealthDrawer.vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

function snapshot(): ConnHealthSnapshot {
  return {
    state: 'connected',
    rtt: { last_ms: 142, p50_ms: 120, p95_ms: 200 },
    rtt_samples: Array.from({ length: 30 }, (_, i) => ({ at_ms: i * 5000, rtt_ms: 100 + i })),
    reconnect: {
      count_last_hour: 2,
      last_at_ms: 1_700_000_000_000,
      last_reason: 'ws_close_1006',
      history: [
        { at_ms: 1_700_000_000_000, reason: 'ws_close_1006', duration_ms: 4000 },
      ],
    },
    bytes: { in_per_sec: 1024, out_per_sec: 256 },
    seq_gaps: 0,
  }
}

describe('ConnHealthDrawer', () => {
  it('shows current RTT and p50 / p95', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const txt = w.text()
    expect(txt).toContain('142')
    expect(txt).toContain('120')
    expect(txt).toContain('200')
  })
  it('renders a sparkline path with as many points as samples', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const path = w.find('svg path')
    expect(path.exists()).toBe(true)
  })
  it('shows the reconnect table with reason and downtime', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const txt = w.text()
    expect(txt).toContain('ws_close_1006')
    expect(txt).toContain('4')   // 4s downtime — formatting may vary, accept loose match
  })
  it('hidden when open=false', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: false } })
    expect(w.find('.drawer').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/shared/components/ConnHealthDrawer.test.ts
```

Expected: FAIL.

- [ ] **Step 3: Implement the component**

Create `web/src/shared/components/ConnHealthDrawer.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import type { ConnHealthSnapshot } from '../connhealth/connhealth'

const props = defineProps<{ health: ConnHealthSnapshot; open: boolean }>()
defineEmits<{ (e: 'close'): void }>()

const sparkPath = computed(() => {
  const samples = props.health.rtt_samples
  if (samples.length < 2) return ''
  const w = 240, h = 60, pad = 4
  const xs = (i: number) => pad + (i / (samples.length - 1)) * (w - 2 * pad)
  const minV = Math.min(...samples.map(s => s.rtt_ms))
  const maxV = Math.max(...samples.map(s => s.rtt_ms))
  const span = Math.max(1, maxV - minV)
  const ys = (v: number) => pad + (1 - (v - minV) / span) * (h - 2 * pad)
  return samples.map((s, i) => `${i === 0 ? 'M' : 'L'} ${xs(i).toFixed(1)} ${ys(s.rtt_ms).toFixed(1)}`).join(' ')
})

const fmtKBs = (n: number) => `${(n / 1024).toFixed(1)} KB/s`
const fmtDownTime = (ms: number) => ms < 1000 ? `${ms} ms` : `${Math.round(ms / 1000)} s`
const fmtAt = (ms: number) => new Date(ms).toLocaleTimeString()
</script>

<template>
  <aside v-if="open" class="drawer" role="dialog" aria-label="Connection health">
    <header class="head">
      <span>Connection health</span>
      <button class="close" @click="$emit('close')" aria-label="Close">×</button>
    </header>

    <section class="rtt">
      <div class="row">
        <span class="metric-label">RTT (now)</span>
        <span class="metric-value">{{ health.rtt.last_ms ?? '—' }} ms</span>
      </div>
      <div class="row">
        <span class="metric-label">p50 / p95 (5 min)</span>
        <span class="metric-value">{{ health.rtt.p50_ms ?? '—' }} / {{ health.rtt.p95_ms ?? '—' }} ms</span>
      </div>
      <svg width="240" height="60" class="spark" aria-hidden="true">
        <path :d="sparkPath" fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </section>

    <section class="bytes">
      <div class="row"><span class="metric-label">↓ in</span><span class="metric-value">{{ fmtKBs(health.bytes.in_per_sec) }}</span></div>
      <div class="row"><span class="metric-label">↑ out</span><span class="metric-value">{{ fmtKBs(health.bytes.out_per_sec) }}</span></div>
    </section>

    <section class="recon">
      <div class="row"><span class="metric-label">State</span><span class="metric-value">{{ health.state }}</span></div>
      <div class="row"><span class="metric-label">Reconnects (1 h)</span><span class="metric-value">{{ health.reconnect.count_last_hour }}</span></div>
      <table v-if="health.reconnect.history.length > 0" class="reconn-table">
        <thead><tr><th>time</th><th>reason</th><th>downtime</th></tr></thead>
        <tbody>
          <tr v-for="ev in health.reconnect.history" :key="ev.at_ms">
            <td>{{ fmtAt(ev.at_ms) }}</td>
            <td>{{ ev.reason }}</td>
            <td>{{ fmtDownTime(ev.duration_ms) }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section v-if="health.seq_gaps > 0" class="gaps">
      Seq gaps observed: {{ health.seq_gaps }}
    </section>
  </aside>
</template>

<style scoped>
.drawer {
  position: fixed;
  top: 36px;
  right: 8px;
  z-index: 1100;
  width: 280px;
  background: var(--bg, #fff);
  color: var(--fg, #111);
  border: 1px solid var(--border, #ddd);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.12);
  padding: 10px 12px;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.head { display: flex; align-items: center; justify-content: space-between; font-weight: 600; }
.close { background: transparent; border: 0; cursor: pointer; font-size: 16px; line-height: 1; color: inherit; }
.row { display: flex; justify-content: space-between; padding: 2px 0; }
.metric-label { color: var(--fg-dim, #666); }
.metric-value { font-variant-numeric: tabular-nums; }
.spark { color: var(--good, #16a34a); display: block; }
.reconn-table { width: 100%; border-collapse: collapse; font-size: 11px; }
.reconn-table th, .reconn-table td { text-align: left; padding: 2px 4px; border-top: 1px solid var(--border, #eee); }
.gaps { color: var(--warn, #d97706); }
</style>
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run src/shared/components/ConnHealthDrawer.test.ts
```

Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/components/ConnHealthDrawer.vue web/src/shared/components/ConnHealthDrawer.test.ts
git commit -m "feat(web): ConnHealthDrawer — sparkline + reconnect log"
```

---

### Task 11: Mount pill + drawer in desktop TitleBar

**Files:**
- Create: `desktop/frontend/src/composables/useUplinkHealth.ts`
- Modify: `desktop/frontend/src/components/TitleBar.vue`
- Modify: `desktop/frontend/vite.config.ts` (only if `@shared/` alias is not already mapped to `web/src/shared/`)

The desktop frontend already aliases `@shared/` to the web's shared lib in some places; confirm before importing.

- [ ] **Step 1: Check alias**

```bash
grep -rn '"@shared"\|"@shared/' desktop/frontend/vite.config.ts desktop/frontend/tsconfig.json 2>/dev/null
```

If no `@shared` alias exists, add to `desktop/frontend/vite.config.ts`:

```typescript
resolve: {
  alias: {
    '@shared': path.resolve(__dirname, '../../web/src/shared'),
    // …existing aliases
  },
},
```

and to `desktop/frontend/tsconfig.json` `compilerOptions.paths`:

```json
"@shared/*": ["../../web/src/shared/*"]
```

If both already exist, skip this step.

- [ ] **Step 2: Create the composable**

Create `desktop/frontend/src/composables/useUplinkHealth.ts`:

```typescript
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { getUplinkHealth, type ConnHealthSnapshot } from '../lib/api'

const CLOSED_SNAPSHOT: ConnHealthSnapshot = {
  state: 'closed',
  rtt: { last_ms: null, p50_ms: null, p95_ms: null },
  rtt_samples: [],
  reconnect: { count_last_hour: 0, last_at_ms: null, last_reason: '', history: [] },
  bytes: { in_per_sec: 0, out_per_sec: 0 },
  seq_gaps: 0,
}

// useUplinkHealth polls GetUplinkHealth on a 5 s cadence while idle, 1 s while
// the drawer is open. The poll is cheap (a single Wails RPC), but the snapshot
// itself can be 60 entries × small JSON, so 5 s feels right for the pill.
export function useUplinkHealth(opts: { fast?: () => boolean } = {}) {
  const health = ref<ConnHealthSnapshot>(CLOSED_SNAPSHOT)
  let timer: number | null = null

  async function tick() {
    try { health.value = await getUplinkHealth() }
    catch { /* keep last value */ }
  }

  function schedule() {
    if (timer !== null) window.clearTimeout(timer)
    const delay = opts.fast?.() ? 1000 : 5000
    timer = window.setTimeout(async () => {
      await tick()
      schedule()
    }, delay) as unknown as number
  }

  onMounted(() => { tick(); schedule() })
  onBeforeUnmount(() => { if (timer !== null) window.clearTimeout(timer) })

  return health
}
```

- [ ] **Step 3: Mount in TitleBar.vue**

In `desktop/frontend/src/components/TitleBar.vue`, find a sensible slot in the existing template (probably to the right of the existing identity/relay status). Add:

```vue
<script lang="ts" setup>
// … existing imports
import { ref } from 'vue'
import ConnHealthPill from '@shared/components/ConnHealthPill.vue'
import ConnHealthDrawer from '@shared/components/ConnHealthDrawer.vue'
import { useUplinkHealth } from '../composables/useUplinkHealth'

const drawerOpen = ref(false)
const health = useUplinkHealth({ fast: () => drawerOpen.value })
</script>
```

In the template, just inside the right-side status area:

```vue
<ConnHealthPill :health="health" @click="drawerOpen = !drawerOpen" />
<ConnHealthDrawer :health="health" :open="drawerOpen" @close="drawerOpen = false" />
```

- [ ] **Step 4: Smoke build**

```bash
cd desktop/frontend && npm run build
```

Expected: success.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useUplinkHealth.ts desktop/frontend/src/components/TitleBar.vue desktop/frontend/vite.config.ts desktop/frontend/tsconfig.json
git commit -m "feat(desktop): mount ConnHealthPill + drawer in titlebar"
```

---

### Task 12: Mount pill + drawer in web/PWA header

**Files:**
- Modify: `web/src/main/App.vue` (and the mobile shell if separate — grep for the per-platform header layout).

- [ ] **Step 1: Find the header**

```bash
grep -n "header\|<TopBar\|<header" web/src/main/App.vue | head -10
```

- [ ] **Step 2: Mount the pill**

In `web/src/main/App.vue`, add:

```vue
<script setup lang="ts">
// … existing imports
import { ref, computed } from 'vue'
import ConnHealthPill from '@shared/components/ConnHealthPill.vue'
import ConnHealthDrawer from '@shared/components/ConnHealthDrawer.vue'
// `conn` is the active SessionConnection instance for the current session.
// Get the snapshot reactively:
const drawerOpen = ref(false)
const health = ref(conn.value ? conn.value.getHealth() : null)
let pollTimer: number | null = null
function startPoll() {
  if (pollTimer !== null) window.clearTimeout(pollTimer)
  pollTimer = window.setTimeout(() => {
    if (conn.value) health.value = conn.value.getHealth()
    startPoll()
  }, drawerOpen.value ? 1000 : 5000) as unknown as number
}
startPoll()
</script>
```

In the template, near the existing header content:

```vue
<ConnHealthPill v-if="health" :health="health" @click="drawerOpen = !drawerOpen" />
<ConnHealthDrawer v-if="health" :health="health" :open="drawerOpen" @close="drawerOpen = false" />
```

Replace `conn.value` with whatever the actual reactive ref / Pinia getter is in this file. If multiple sessions can be active concurrently in the web app, pick the currently-focused one (the same one bound to `TerminalView`).

- [ ] **Step 3: Build**

```bash
cd web && npm run build
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add web/src/main/App.vue
git commit -m "feat(web): mount ConnHealthPill + drawer in header"
```

---

### Task 13: Settings → General toggle

**Files:**
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/composables/useUplinkHealth.ts` (read the flag)
- Modify: `web/src/shared/ws/client-conn.ts` (read the flag)
- Modify: `desktop/app.go` (persist the flag, expose via existing prefs API)

The simplest implementation: add a single boolean to the existing prefs store and have both clients read it before starting the ping loop.

- [ ] **Step 1: Add the pref field**

Find the prefs / config struct in `desktop/app.go` (grep `Preferences\|UserPrefs\|appConfig`). Add a field:

```go
ConnHealthMonitoring bool `json:"conn_health_monitoring"`
```

with default `true`. Surface it via the existing `GetPreferences` / `SetPreferences` (or equivalent) — no new RPC needed if a generic prefs object already crosses the bridge.

- [ ] **Step 2: SettingsGeneral.vue toggle**

In `SettingsGeneral.vue`, add a checkbox bound to the prefs ref. Pattern follows the existing toggles in the same file (e.g. webgl, notifications). Add `t('settings.general.connHealth')` etc. labels.

- [ ] **Step 3: Gate PING send on the flag**

In `useUplinkHealth.ts`, if the flag is false: skip polling, return a snapshot whose `state` mirrors connection state but `rtt_samples = []` and `rtt.last_ms = null`. (Achieved by reading the snapshot from the Go side, which independently respects the flag — gate the PING send goroutine in `desktop/uplink.go` on the prefs flag.)

In `web/src/shared/ws/client-conn.ts`, accept an optional `enableHealthMonitoring: boolean` constructor option that defaults to `true`; gate `startPingLoop` on it.

- [ ] **Step 4: Verify**

```bash
go test -tags webkit2_41 ./desktop/ -v
cd desktop/frontend && npm run build
cd ../../web && npm run build && npx vitest run
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/frontend/src/components/SettingsGeneral.vue desktop/frontend/src/composables/useUplinkHealth.ts web/src/shared/ws/client-conn.ts
git commit -m "feat: Settings → General toggle for connection health monitoring"
```

---

### Task 14: i18n strings

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Modify: `web/src/shared/i18n/messages/en.ts`
- Modify: `web/src/shared/i18n/messages/zh-CN.ts`

Add (under `settings.general`):

```typescript
connHealth: 'Connection health monitoring',
connHealthHint: 'Periodically pings the relay to surface RTT and reconnect history in the title bar.',
```

Add a new section `connHealth`:

```typescript
connHealth: {
  pillLabelConnecting: 'connecting…',
  pillLabelReconnecting: 'reconnecting…',
  pillLabelOff: 'off',
  drawerTitle: 'Connection health',
  drawerRttNow: 'RTT (now)',
  drawerRttP50P95: 'p50 / p95 (5 min)',
  drawerBytesIn: '↓ in',
  drawerBytesOut: '↑ out',
  drawerState: 'State',
  drawerReconnectsLastHour: 'Reconnects (1 h)',
  drawerReconnectsTime: 'time',
  drawerReconnectsReason: 'reason',
  drawerReconnectsDowntime: 'downtime',
  drawerSeqGaps: 'Seq gaps observed:',
},
```

zh-CN equivalents:

```typescript
connHealth: '连接质量监控',
connHealthHint: '周期性向 relay 发送 ping,在标题栏显示 RTT 和重连历史。',
```

```typescript
connHealth: {
  pillLabelConnecting: '连接中…',
  pillLabelReconnecting: '重连中…',
  pillLabelOff: '已关闭',
  drawerTitle: '连接质量',
  drawerRttNow: 'RTT(当前)',
  drawerRttP50P95: 'p50 / p95(最近 5 分钟)',
  drawerBytesIn: '↓ 接收',
  drawerBytesOut: '↑ 发送',
  drawerState: '状态',
  drawerReconnectsLastHour: '最近 1 小时重连',
  drawerReconnectsTime: '时间',
  drawerReconnectsReason: '原因',
  drawerReconnectsDowntime: '断开时长',
  drawerSeqGaps: '观察到的序号跳跃:',
},
```

Wire the components to `useI18n()` so string literals are replaced with `t('connHealth.*')` calls.

- [ ] **Step 1: Apply the edits**

(Each file gets the additions above.)

- [ ] **Step 2: Replace literals in components**

In `ConnHealthPill.vue`, replace `'reconnecting…'` with `t('connHealth.pillLabelReconnecting')` etc. Inject `useI18n` from the host project (the shared component cannot assume an i18n provider — accept the localized strings as optional props OR look up via a thin adapter — read how `SettingsDiagnostics.vue` handles cross-app i18n and follow the same pattern).

Pragmatic: have the pill/drawer accept a `labels` prop with all strings, and the desktop/web wrapper passes localized labels.

- [ ] **Step 3: Verify**

```bash
cd desktop/frontend && npm run build
cd ../../web && npm run build
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/i18n/ web/src/shared/i18n/ desktop/frontend/src/components/TitleBar.vue web/src/main/App.vue web/src/shared/components/ConnHealthPill.vue web/src/shared/components/ConnHealthDrawer.vue
git commit -m "feat(i18n): localize connection health pill + drawer (en, zh-CN)"
```

---

### Task 15: Document protocol change in `docs/spec/protocol.md`

**Files:**
- Modify: `docs/spec/protocol.md`

- [ ] **Step 1: Find the frame-type table**

```bash
grep -n "TypePing\|0x20\|0x21\|PING\|PONG" docs/spec/protocol.md
```

- [ ] **Step 2: Update the PING/PONG section**

Replace the existing PING/PONG entry with:

```markdown
### `TypePing (0x20)` / `TypePong (0x21)` — application-level keepalive + RTT

**Direction**: bidirectional (any side may originate PING; the other side echoes the payload as PONG).

**Payload (`TypePing`)**: empty, or 8 bytes big-endian unsigned monotonic milliseconds. The 8-byte form lets the originator compute RTT without trusting the peer's clock.

**Payload (`TypePong`)**: the exact bytes received in the matching `TypePing` payload. A peer that receives a `TypePing` with an empty payload must echo an empty `TypePong`; with an 8-byte payload must echo the same 8 bytes.

Used by clients (`desktop` uplink, `web`/`mobile` per-session WS) to drive the title-bar connection-health pill. Relay does not interpret the payload — it is opaque echo. WebSocket control-frame Ping/Pong (gorilla/coder `Conn.Ping`) is still used for low-level keepalive and is independent of these frames.
```

- [ ] **Step 3: Commit**

```bash
git add docs/spec/protocol.md
git commit -m "docs: document TypePing/TypePong payload echo semantics"
```

---

### Task 16: Full verification

- [ ] **Step 1: Run all Go tests**

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 120s ./...
```

Expected: all green.

- [ ] **Step 2: Run all TS tests + builds**

```bash
cd desktop/frontend && npm test && npm run build
cd ../../web && npm run build && npm test && npm run test:contract
cd ../mobile && npm test
```

Expected: all green.

- [ ] **Step 3: Manual smoke test (local relay + desktop)**

Open three terminals. Terminal 1:

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
```

Terminal 2:

```bash
cd desktop && wails dev
```

In the desktop app:
- Settings → Relay → enter `http://127.0.0.1:8080` → log in.
- Confirm the titlebar shows a green pill with a 2-digit ms reading after ~5 s.
- Click the pill → drawer opens with RTT, byte rates, no reconnects.
- Kill the relay process → pill should turn `reconnecting` (yellow pulse) within 5 s.
- Restart the relay → pill returns to green; reconnect counter increments to 1; drawer shows one entry with reason and downtime.

If any step fails, debug and fix.

- [ ] **Step 4: Commit any fixes**

```bash
git add -p
git commit -m "fix: smoke-test issues uncovered during verification"
```

(Only if there were issues. If everything passed cleanly, skip this step.)

---

### Task 17: Ship the release

Once all tasks above are committed and verification is clean, run the ship-release workflow:

- [ ] **Step 1: Invoke ship-release**

Use the `Skill` tool to invoke `ship-release`. The skill will:

1. Confirm working tree is clean and on the feature branch.
2. Open a PR from `feat/conn-health-monitoring` → `main`.
3. Squash-merge once CI is green.
4. Tag the next patch release.

Do **not** push, merge, or tag manually outside the skill — the skill enforces the right sequence (clean tree → green build → squash-merge → tag bump).

- [ ] **Step 2: Confirm release**

After ship-release exits, verify:

```bash
git log --oneline origin/main | head -3
git tag --sort=-v:refname | head -1
```

The most recent tag should be one minor or patch above the previous, and the squashed commit should be on `origin/main`.

---

## Self-Review Notes

- Spec sections covered:
  - **Protocol**: Task 1 (codec helpers), Task 2 (uplink echo), Task 3 (client echo), Task 15 (doc).
  - **Client tracker (Go)**: Task 4 (lib), Task 5 (wire desktop), Task 6 (Wails RPC).
  - **Client tracker (TS)**: Task 7 (lib), Task 8 (wire client-conn).
  - **UI components**: Task 9 (pill), Task 10 (drawer), Task 11 (desktop mount), Task 12 (web mount).
  - **Settings toggle**: Task 13.
  - **i18n**: Task 14.
  - **Testing**: every implementation task is TDD; Task 16 runs the full suite + manual smoke.
- Frame types referenced (`TypePing 0x20`, `TypePong 0x21`) match `internal/proto/frame.go`.
- Method names are consistent: `OnPongRTT` / `onPongRTT`, `OnBytesIn` / `onBytesIn`, `Snapshot` / `snapshot`, `SetState` / `setState`. Go uses PascalCase, TS uses camelCase, but the field names in `Snapshot`/`ConnHealthSnapshot` are snake_case for JSON wire format (matches the existing Wails-bridged types).
- No "TBD" / "implement later" placeholders. Each step shows the actual code to write.
