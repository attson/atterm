# Remote Viewer Count Implementation Plan (SP2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the desktop owner a per-session "👁 N" badge counting how many remote clients are attached, by propagating the central relay's mirror subscriber count down the uplink.

**Architecture:** New downlink frame `TypeViewers` (0x36). The mirror `Session` fires a subscriber-count hook on every attach/detach; `uplink_conn.go` enqueues a `TypeViewers{session_id,count}` frame to the desktop; `desktop/uplink.go` emits a `relay:viewers` Wails event; `App.vue` stores a reactive `Record<sessionId,count>` and threads it into `PaneGrid`, which renders a top-right badge on local panes. Orthogonal to SP1's driver/viewer overlay.

**Tech Stack:** Go 1.23 (`internal/proto`, `internal/session`, `internal/relay`, `desktop/`), Vue 3 + TS (`desktop/frontend`), Go `testing`, Vitest.

**Branch:** `feat/remote-viewer-count` (already created off `main`).

**Build/test env:** Go needs `PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH`. `internal/...` needs no build tag; `./desktop/` e2e tests run without the webkit tag. Frontend: `desktop/frontend` (Node, `npx vitest`).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/proto/frame.go` | wire frame types/payloads | add `TypeViewers` 0x36 + `ViewersPayload` |
| `internal/proto/frame_test.go` | proto tests | round-trip + uniqueness |
| `internal/session/session.go` | session model | add count hook + fire on add/remove |
| `internal/session/session_test.go` | session tests | hook fires with correct counts |
| `internal/relay/uplink_conn.go` | mirror lifecycle | register hook → enqueue downlink |
| `internal/relay/uplink_conn_test.go` | relay tests | mirror count hook delivers counts |
| `desktop/uplink.go` | uplink inbound | `case TypeViewers` → emit `relay:viewers` |
| `desktop/uplink_e2e_test.go` | uplink e2e | frame → `relay:viewers` emit |
| `desktop/frontend/src/App.vue` | session/event state | `relay:viewers` listener + `viewerCounts` + `viewerCountFor` |
| `desktop/frontend/src/components/PaneGrid.vue` | pane rendering | top-right viewers badge on local panes + `avoid-top-right-badge` |
| `desktop/frontend/src/components/PaneGrid.test.ts` | UI test | badge markup gated on count + local pane |
| `docs/spec/protocol.md` | protocol spec | document 0x36 |

Tasks 1→4 are the Go pipeline (do in order). Task 5 is the frontend. Task 6 is docs.

---

## Task 1: proto TypeViewers frame

**Files:**
- Modify: `internal/proto/frame.go` (Type block ~line 40; payload structs section)
- Test: `internal/proto/frame_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/proto/frame_test.go`:

```go
func TestViewersPayloadRoundTrip(t *testing.T) {
	if TypeViewers != 0x36 {
		t.Fatalf("TypeViewers = 0x%02x; want 0x36", TypeViewers)
	}
	in := ViewersPayload{SessionID: "11111111-2222-3333-4444-555555555555", Count: 3}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ViewersPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round-trip = %+v; want %+v", out, in)
	}
}
```

(If `frame_test.go` lacks `encoding/json` in imports, add it.)

- [ ] **Step 2: Run it to verify it fails**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./internal/proto/ -run TestViewersPayload -v
```
Expected: FAIL — `undefined: TypeViewers` / `ViewersPayload`.

- [ ] **Step 3: Add the type + payload**

In `internal/proto/frame.go`, add the constant after `TypeCommandEvent` (~line 41):

```go
	TypeViewers       Type = 0x36 // relay -> uplink; mirror's remote subscriber count
```

Add the payload struct near the other payloads (e.g., after `ClaimDriverPayload`):

```go
// ViewersPayload is the JSON body of a TypeViewers frame: the count of remote
// /client subscribers currently attached to a session's mirror on the relay.
type ViewersPayload struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}
```

- [ ] **Step 4: Run the test**

```bash
go test ./internal/proto/ -run TestViewersPayload -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proto/frame.go internal/proto/frame_test.go
git commit -m "feat(proto): add TypeViewers (0x36) + ViewersPayload"
```

---

## Task 2: Session subscriber-count hook

**Files:**
- Modify: `internal/session/session.go` (struct ~line 57; `SetSubscriberLifecycle` ~line 106; `Subscribe` unlock path ~line 308; `removeSubscriber` ~line 600)
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/session_test.go`:

```go
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

	// Hooks fire asynchronously; give them a moment to drain.
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
```

(`sync` and `time` are already imported in `session_test.go`.)

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./internal/session/ -run TestSubscriberCountHook -v
```
Expected: FAIL — `s.SetSubscriberCountHook undefined`.

- [ ] **Step 3: Add the field + setter**

In `internal/session/session.go`, add to the `Session` struct near the lifecycle hooks (~line 58):

```go
	// onSubscriberCount, if set, fires with the new subscriber count whenever
	// a subscriber is added or removed. Used by mirror sessions to report the
	// remote viewer count downstream. Fired async (lock released).
	onSubscriberCount func(int)
```

Add the setter next to `SetSubscriberLifecycle` (~line 111):

```go
// SetSubscriberCountHook registers a callback fired (async) with the new
// subscriber count on every add/remove. Replaces any previous hook.
func (s *Session) SetSubscriberCountHook(fn func(int)) {
	s.mu.Lock()
	s.onSubscriberCount = fn
	s.mu.Unlock()
}
```

- [ ] **Step 4: Fire on attach**

In `Subscribe`, the snapshot block under lock (~line 286-307) captures state then unlocks at ~line 308. Capture the count there. Change the captured-vars block to also grab the count and hook; add the fire after the existing post-unlock hooks.

Locate (just before `s.mu.Unlock()` at ~line 308) where `firstHook := s.onFirstSubscribe` is read, and add alongside it:

```go
	firstHook := s.onFirstSubscribe
	countHook := s.onSubscriberCount
	subCount := len(s.subs)
	s.mu.Unlock()
```

Then after the existing `if wasEmpty && firstHook != nil { go firstHook() }` (~line 319-321), add:

```go
	if added && countHook != nil {
		go countHook(subCount)
	}
```

- [ ] **Step 5: Fire on detach**

In `removeSubscriber` (~line 600), capture the count + hook under lock and fire after unlock. Change the captured block (~line 612-615):

```go
	nowEmpty := len(s.subs) == 0
	lastHook := s.onLastUnsubscribe
	countHook := s.onSubscriberCount
	subCount := len(s.subs)
	metaCopy := s.meta
	s.mu.Unlock()
```

After the existing `if was && nowEmpty && lastHook != nil { go lastHook() }` (~line 620-622), add:

```go
	if was && countHook != nil {
		go countHook(subCount)
	}
```

- [ ] **Step 6: Run the test + full session suite**

```bash
go test ./internal/session/ -v
```
Expected: PASS (new test + all existing, including SP1's mirror tests + #4 guard).

- [ ] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): subscriber-count hook for viewer reporting"
```

---

## Task 3: Wire the count hook on mirror creation

**Files:**
- Modify: `internal/relay/uplink_conn.go` (announce handler, after `newMirrorSession` ~line 188; `enqueue` closure is in scope at ~line 101)
- Test: `internal/relay/uplink_conn_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/uplink_conn_test.go`:

```go
func TestMirrorSessionCountHookReportsViewers(t *testing.T) {
	sess := newMirrorSession(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24}, "owner")
	// Wire a capturing hook exactly as handleUplink does (sans enqueue).
	counts := make(chan int, 8)
	sess.SetSubscriberCountHook(func(n int) { counts <- n })

	a, _ := sess.Subscribe(0, "a", "ha")
	if got := <-counts; got != 1 {
		t.Fatalf("after 1 attach: count=%d; want 1", got)
	}
	b, _ := sess.Subscribe(0, "b", "hb")
	if got := <-counts; got != 2 {
		t.Fatalf("after 2 attach: count=%d; want 2", got)
	}
	sess.Unsubscribe(a)
	if got := <-counts; got != 1 {
		t.Fatalf("after 1 detach: count=%d; want 1", got)
	}
	sess.Unsubscribe(b)
	if got := <-counts; got != 0 {
		t.Fatalf("after all detach: count=%d; want 0", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./internal/relay/ -run TestMirrorSessionCountHook -v
```
Expected: FAIL — currently no hook is registered, but `SetSubscriberCountHook` exists (Task 2), so this test actually exercises the session hook directly and should PASS once Task 2 is in. It locks the contract the relay wiring depends on. If it fails, Task 2 is incomplete.

(Note: this test validates the hook contract; Step 3 adds the real `enqueue` wiring, which is covered end-to-end by the existing `TestUplinkE2E` staying green + the desktop emit test in Task 4.)

- [ ] **Step 3: Register the enqueue hook on mirror creation**

In `internal/relay/uplink_conn.go`, in the announce handler right after `sess := newMirrorSession(id, info, ownerUserID)` (~line 188), and before/near the existing `sess.SetSubscriberLifecycle(...)`, add:

```go
			sess.SetSubscriberCountHook(func(n int) {
				payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid.String(), Count: n})
				enqueue(proto.Frame{Type: proto.TypeViewers, SessionID: sid, Payload: payload})
			})
```

(`sid := id` is already declared just below `newMirrorSession`; move the `sid := id` line above this hook, or reuse the existing capture. Ensure `sid` is in scope before the hook closure.)

- [ ] **Step 4: Run relay suite + vet**

```bash
go test ./internal/relay/ -run 'TestMirrorSession|TestUplink' -v
go vet ./internal/...
```
Expected: PASS; vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_conn_test.go
git commit -m "feat(relay): mirror enqueues TypeViewers count downlink on subscriber change"
```

---

## Task 4: Desktop emits relay:viewers

**Files:**
- Modify: `desktop/uplink.go` (inbound switch, near `case proto.TypeClaimDriver:` ~line 378)
- Test: `desktop/uplink_e2e_test.go`

- [ ] **Step 1: Write the failing test**

Append to `desktop/uplink_e2e_test.go` (mirrors `TestUplink_AuthInfo_EmitsUserID`):

```go
func TestUplink_Viewers_EmitsCount(t *testing.T) {
	const sid = "11111111-2222-3333-4444-555555555555"
	payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid, Count: 2})

	ready := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/uplink", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_, _, _ = c.Read(r.Context()) // drain ANNOUNCE
		_ = c.Write(r.Context(), websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeViewers, Payload: payload}))
		close(ready)
		<-r.Context().Done()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	type emitCall struct {
		name string
		data interface{}
	}
	var (
		mu    sync.Mutex
		calls []emitCall
	)
	stubEmit := func(_ context.Context, name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		var d interface{}
		if len(data) > 0 {
			d = data[0]
		}
		calls = append(calls, emitCall{name: name, data: d})
	}

	host, err := startRelayHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u := newUplink("ws://"+ln.Addr().String(), "tok", proto.RemotePermissionFull, host)
	u.eventsEmit = stubEmit

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("relay never sent TypeViewers")
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var found *emitCall
	for i := range calls {
		if calls[i].name == "relay:viewers" {
			found = &calls[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("relay:viewers not emitted; got: %+v", calls)
	}
	m, ok := found.data.(map[string]any)
	if !ok {
		t.Fatalf("relay:viewers data = %T; want map[string]any", found.data)
	}
	if m["session_id"] != sid {
		t.Errorf("session_id = %v; want %s", m["session_id"], sid)
	}
	if m["count"] != 2 {
		t.Errorf("count = %v; want 2", m["count"])
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./desktop/ -run TestUplink_Viewers_EmitsCount -v
```
Expected: FAIL — no `relay:viewers` emit.

- [ ] **Step 3: Handle the frame**

In `desktop/uplink.go`, add a case before `case proto.TypeAuthInfo:` (~line 390):

```go
		case proto.TypeViewers:
			var p proto.ViewersPayload
			if err := json.Unmarshal(f.Payload, &p); err != nil {
				continue
			}
			if u.eventsEmit != nil {
				u.eventsEmit(ctx, "relay:viewers", map[string]any{"session_id": p.SessionID, "count": p.Count})
			}
```

- [ ] **Step 4: Run the test + desktop suite**

```bash
go test ./desktop/ -run TestUplink_Viewers_EmitsCount -v
go test -timeout 120s ./desktop/
```
Expected: PASS (new test + existing e2e incl. SP1's CLAIM_DRIVER round-trip).

- [ ] **Step 5: Commit**

```bash
git add desktop/uplink.go desktop/uplink_e2e_test.go
git commit -m "feat(desktop): emit relay:viewers on TypeViewers frame"
```

---

## Task 5: Frontend viewer-count badge

**Files:**
- Modify: `desktop/frontend/src/App.vue` (relay listener ~line 694; PaneGrid props ~line 786)
- Modify: `desktop/frontend/src/components/PaneGrid.vue` (props; `cell-controls` ~line 76; `avoid-top-right-badge` ~line 67)
- Test: `desktop/frontend/src/components/PaneGrid.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/PaneGrid.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import source from "./PaneGrid.vue?raw";

describe("PaneGrid viewers badge", () => {
  test("accepts a viewerCountFor prop", () => {
    expect(source).toMatch(/viewerCountFor/);
  });
  test("renders a viewers badge on local panes when count > 0", () => {
    expect(source).toMatch(/class="viewers-badge"/);
    // gated on a local (non-remote) pane and a positive count
    expect(source).toMatch(/!pane\.remote/);
    expect(source).toMatch(/viewerCountFor\?\.\(pane\.sessionId\)/);
  });
  test("lets the SP1 overlay dodge the viewers badge too", () => {
    expect(source).toMatch(/avoid-top-right-badge="pane\.remote \|\|/);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/components/PaneGrid.test.ts
```
Expected: FAIL.

- [ ] **Step 3: Add the prop to PaneGrid**

In `desktop/frontend/src/components/PaneGrid.vue` `<script setup>`, add `viewerCountFor` to the `defineProps` block (alongside `sessionInfoFor`):

```ts
  viewerCountFor?: (sessionId: string) => number;
```

- [ ] **Step 4: Render the badge + update avoid-top-right-badge**

In the template, change the `avoid-top-right-badge` binding (~line 67):

```vue
          :avoid-top-right-badge="pane.remote || (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
```

In `cell-controls` (~line 76), add before the remote-badge block:

```vue
        <div
          v-if="pane.sessionId && !pane.remote && (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          class="viewers-badge"
          :title="`${viewerCountFor!(pane.sessionId)} remote viewer(s) watching`"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <span>{{ viewerCountFor!(pane.sessionId) }}</span>
        </div>
```

Add scoped styles (next to `.remote-badge`):

```css
.viewers-badge {
  position: absolute;
  top: 4px;
  right: 4px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.55);
  color: var(--fg);
  font-size: 11px;
  pointer-events: none;
}
.viewers-badge svg { display: block; }
```

- [ ] **Step 5: Wire the App.vue listener + state + prop**

In `desktop/frontend/src/App.vue` `<script setup>`, add reactive state (near other refs) — `reactive` is already imported in this file (verify; if not, add it to the `vue` import):

```ts
const viewerCounts = reactive<Record<string, number>>({});
function viewerCountFor(sessionId: string): number {
  return viewerCounts[sessionId] ?? 0;
}
```

Register the listener next to the `relay:auth-error` handler (~line 694):

```ts
  $platform.events.on('relay:viewers', (data) => {
    const d = data as { session_id: string; count: number };
    if (d && typeof d.session_id === 'string') {
      viewerCounts[d.session_id] = d.count ?? 0;
    }
  });
```

Pass the prop to `PaneGrid` (~line 786 block), alongside `:session-info-for`:

```vue
            :viewer-count-for="viewerCountFor"
```

- [ ] **Step 6: Run the test + type-check**

```bash
cd desktop/frontend && npx vitest run src/components/PaneGrid.test.ts && npm run build:wails
```
Expected: PaneGrid test PASS; vue-tsc + vite build clean.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/components/PaneGrid.vue desktop/frontend/src/components/PaneGrid.test.ts
git commit -m "feat(desktop): top-right viewers badge on local panes"
```

---

## Task 6: Document the frame in the protocol spec

**Files:**
- Modify: `docs/spec/protocol.md`

- [ ] **Step 1: Add the frame entry**

Find the frame-type table/list in `docs/spec/protocol.md` (where `0x35` / CommandEvent is documented) and add a row/entry:

```
0x36  VIEWERS        relay → uplink   JSON {session_id, count}: number of remote /client
                                      subscribers attached to the session's mirror. Sent on
                                      every attach/detach. Owner-side "N watching" badge.
```

(Match the surrounding formatting — if it's a markdown table, add a table row with the same columns; if it's a prose list, mirror the adjacent entry's style.)

- [ ] **Step 2: Commit**

```bash
git add docs/spec/protocol.md
git commit -m "docs(protocol): document TypeViewers (0x36) frame"
```

---

## Final verification (after all tasks)

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./internal/... ./desktop/ && go test ./internal/proto/ ./internal/session/ ./internal/relay/ ./desktop/
cd desktop/frontend && npm run build:wails && npx vitest run
```

Manual smoke (desktop env): start a local session on desktop A; attach a web/mobile/second-desktop client via the relay → A's terminal shows "👁 1" top-right; attach another → "👁 2"; detach all → badge disappears.

---

## Self-Review

**Spec coverage:**
- New `TypeViewers` 0x36 + `ViewersPayload` → Task 1. ✓
- Mirror reports count (all remote subscribers, driver included) → Task 2 (hook) + Task 3 (wiring; count = `len(subs)` with no driver exclusion). ✓
- Downlink propagation → Task 3 (enqueue). ✓
- Desktop emits event → Task 4. ✓
- Top-right badge on owner's terminal, N>0 only → Task 5 (gated `(viewerCountFor?.(…) ?? 0) > 0`, on `!pane.remote`). ✓
- Coexist with SP1 overlay → Task 5 updates `avoid-top-right-badge`. ✓
- protocol.md doc → Task 6. ✓
- Owner-side only, no web/mobile display → no client tasks. ✓

**Placeholder scan:** No TBD/TODO. Each code step has complete code; Task 6 instructs matching the existing doc format (the only "match surrounding style" step, unavoidable without the file's exact layout, but the content to add is fully specified).

**Type consistency:** `ViewersPayload{SessionID string, Count int}` consistent across Tasks 1/3/4. Event name `relay:viewers` and payload keys `session_id`/`count` consistent across Task 4 (emit) and Task 5 (listener). `viewerCountFor(sessionId: string): number` consistent across App.vue (Task 5) and PaneGrid prop + template (Task 5). `SetSubscriberCountHook(func(int))` consistent across Tasks 2/3. `TypeViewers` 0x36 consistent across Tasks 1/3/4/6.
