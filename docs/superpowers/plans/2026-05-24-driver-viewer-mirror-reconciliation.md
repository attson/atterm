# Driver/Viewer Mirror Reconciliation Implementation Plan (SP1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the central relay's mirror session reflect the upstream PTY driver so remote clients (desktop B / web / mobile) render the viewer overlay and take-control works end-to-end.

**Architecture:** The desktop's LOCAL session stays the source of truth for the PTY driver (first subscriber = driver; commit #4's solo fix preserved). The MIRROR session is gated into a new "driver-from-upstream" mode: it does not self-assign a driver and instead adopts `driver_client_id` from the upstream META it receives over the uplink, rebroadcasting it to remote subscribers. Take-control still flows via the existing `CLAIM_DRIVER` → uplink → `ClaimLocalDriver` path; the resulting local driver change propagates back up as META and reconciles every client.

**Tech Stack:** Go 1.23 (`internal/session`, `internal/relay`), Vue 3 + TypeScript + xterm (`web/`, `desktop/frontend`), Vitest + @vue/test-utils, Go `testing`.

**Branch:** `fix/driver-viewer-mirror-reconciliation` (already created off `main`).

**Build/test env note:** Go needs `PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH` and the `-tags webkit2_41` tag is NOT needed for `internal/...` packages. Frontend uses Node 20 (`desktop/frontend` and `web` each have their own `node_modules`).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/session/session.go` | Session model + driver state | Add `driverFromUpstream` flag + setter; `Subscribe` skips self-promote under flag; `UpdateMeta` adopts upstream driver under flag |
| `internal/session/session_test.go` | Session unit tests | Add mirror-mode tests; keep #4 test green |
| `internal/relay/uplink_conn.go` | Mirror session lifecycle | New `newMirrorSession` helper sets the flag; use it at mirror creation; fix stale comment |
| `internal/relay/uplink_conn_test.go` | Relay unit tests | Test `newMirrorSession` behavior |
| `web/src/shared/ws/client-conn.ts` | Web /client WS | Add `client_id`/`client_name` to ATTACH, `claimDriver()`, `onDriverChange` via META |
| `web/src/shared/ws/client-conn.test.ts` (or existing spec) | Web WS tests | Assert ATTACH carries client_id; claimDriver emits CLAIM_DRIVER; onDriverChange fires |
| `web/src/main/components/TerminalView.vue` | Web terminal UI | `isDriver` + viewer overlay + Take-control button + `disableStdin` |
| `web/src/main/components/TerminalView.test.ts` (new) | Web UI test | Overlay renders when not driver; button calls claimDriver |
| `desktop/frontend/src/components/TerminalView.vue` | Desktop terminal UI | Add Take-control button to existing viewer-overlay |
| `desktop/frontend/src/components/TerminalView.test.ts` | Desktop UI test | Assert take-control button calls claimDriver |
| `desktop/frontend/src/mobile/MobileTerminal.vue` | Mobile terminal UI | Wire `onDriverChange`, viewer overlay, Take-control button, `disableStdin` |
| `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts` | Mobile UI test | Overlay renders when not driver; button calls claimDriver |

Tasks 1→2 are the relay core (must land first). Tasks 3→6 are independent client UIs that each consume the now-correct META; they can be done in any order after Task 2.

---

## Task 1: Session driver-from-upstream mode

**Files:**
- Modify: `internal/session/session.go` (struct ~line 50; `New` ~line 81; `UpdateMeta` ~line 113; `Subscribe` promote block ~line 297)
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/session_test.go`:

```go
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

func TestMirrorLateSubscriberSeesAdoptedUpstreamDriver(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.SetDriverFromUpstream(true)
	s.UpdateMeta(proto.MetaPayload{DriverClientID: "owner-A", DriverClientName: "mac-mini"})

	sub, _ := s.Subscribe(0, "client-remote", "remote-host")
	defer s.Unsubscribe(sub)

	f := readFrameForTest(t, sub) // snapshot META on attach
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
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./internal/session/ -run 'TestMirror' -v
```
Expected: FAIL — `s.SetDriverFromUpstream undefined`.

- [ ] **Step 3: Add the flag + setter**

In `internal/session/session.go`, add to the `Session` struct (after the `driverClientName string` field, ~line 52):

```go
	// driverFromUpstream marks a MIRROR session: its driver is dictated by the
	// upstream META forwarded over the uplink (the real PTY driver on the host
	// relay), not by its own first subscriber. Local sessions leave this false
	// and keep first-subscriber-wins + commit #4's solo behavior.
	driverFromUpstream bool
```

Add the setter after `New` (~line 90):

```go
// SetDriverFromUpstream marks this session as a mirror whose driver is adopted
// from upstream META rather than self-assigned. Call once right after New.
func (s *Session) SetDriverFromUpstream(v bool) {
	s.mu.Lock()
	s.driverFromUpstream = v
	s.mu.Unlock()
}
```

- [ ] **Step 4: Skip self-promote under the flag**

In `Subscribe` (~line 297), wrap the promotion block:

```go
		if !s.driverFromUpstream && s.driverSubscriber == nil {
			s.driverSubscriber = sub
			s.driverClientID = sub.clientID
			s.driverClientName = sub.clientName
			promotedToDriver = true
		}
```

- [ ] **Step 5: Adopt upstream driver in UpdateMeta**

Replace the body of `UpdateMeta` (~line 113) with:

```go
func (s *Session) UpdateMeta(m proto.MetaPayload) {
	s.mu.Lock()
	changed := false
	if m.Cwd != "" && s.meta.Cwd != m.Cwd {
		s.meta.Cwd = m.Cwd
		changed = true
	}
	if m.Title != "" && s.meta.Title != m.Title {
		s.meta.Title = m.Title
		changed = true
	}
	// Mirror sessions adopt the upstream's authoritative driver. Local
	// sessions never take driver fields from a META (preserves commit #4:
	// a cwd-only update must not clobber driver state).
	if s.driverFromUpstream &&
		(s.driverClientID != m.DriverClientID || s.driverClientName != m.DriverClientName) {
		s.driverClientID = m.DriverClientID
		s.driverClientName = m.DriverClientName
		changed = true
	}
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverID, driverName)
	}
}
```

- [ ] **Step 6: Run the new tests + the full session suite (regression incl. #4)**

```bash
go test ./internal/session/ -v
```
Expected: PASS — including `TestUpdateMetaBroadcastsPreservesDriverState` (the #4 guard) and `TestSubscribeAutoPromotesFirstSubscriberToDriver` (local mode unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): mirror sessions adopt driver from upstream META"
```

---

## Task 2: Wire mirror sessions into driver-from-upstream mode

**Files:**
- Modify: `internal/relay/uplink_conn.go` (mirror creation ~line 178; META case comment ~line 314)
- Test: `internal/relay/uplink_conn_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/uplink_conn_test.go`:

```go
func TestNewMirrorSessionAdoptsUpstreamDriver(t *testing.T) {
	id := uuid.New()
	sess := newMirrorSession(id, proto.SessionInfo{Cols: 80, Rows: 24}, "owner-user")

	// A mirror must not self-promote its first subscriber.
	sub, _ := sess.Subscribe(0, "remote-client", "remote-host")
	defer sess.Unsubscribe(sub)
	if sess.DriverClientID() != "" {
		t.Fatalf("mirror self-promoted a driver: %q", sess.DriverClientID())
	}
	if sess.OwnerUserID != "owner-user" {
		t.Fatalf("OwnerUserID = %q; want owner-user", sess.OwnerUserID)
	}

	// Upstream META sets the real driver.
	sess.UpdateMeta(proto.MetaPayload{DriverClientID: "owner-A", DriverClientName: "mac-mini"})
	if got := sess.DriverClientID(); got != "owner-A" {
		t.Fatalf("mirror driver after upstream META = %q; want owner-A", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test ./internal/relay/ -run 'TestNewMirrorSession' -v
```
Expected: FAIL — `undefined: newMirrorSession`.

- [ ] **Step 3: Add the `newMirrorSession` helper**

In `internal/relay/uplink_conn.go`, add near the top of the file (after imports / before `handleUplink`):

```go
// newMirrorSession builds a mirror Session: driver state is adopted from the
// upstream host relay's META (driverFromUpstream), never self-assigned to a
// local subscriber. OwnerUserID is stamped for the registry's owner check.
func newMirrorSession(id uuid.UUID, info proto.SessionInfo, ownerUserID string) *session.Session {
	sess := session.New(id, info)
	sess.OwnerUserID = ownerUserID
	sess.SetDriverFromUpstream(true)
	return sess
}
```

- [ ] **Step 4: Use the helper at mirror creation**

In `handleUplink`, replace the mirror-creation lines (~178-180):

```go
			sess := session.New(id, info)
			sess.OwnerUserID = ownerUserID
```
with:
```go
			sess := newMirrorSession(id, info, ownerUserID)
```

- [ ] **Step 5: Fix the stale comment on the META case**

Replace the comment block above `ms.sess.UpdateMeta(m)` (~314-320) with:

```go
				// Mirror sessions adopt the upstream's driver_client_id from
				// this META (SetDriverFromUpstream), so remote /client
				// subscribers see the real PTY driver (the host desktop) and
				// render the viewer overlay. UpdateMeta is the single broadcast
				// point; the raw upstream frame is not re-broadcast separately.
```

- [ ] **Step 6: Run the relay suite**

```bash
go test ./internal/relay/ -run 'TestNewMirrorSession|TestUplink' -v
go vet ./internal/...
```
Expected: PASS; vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_conn_test.go
git commit -m "feat(relay): mirror sessions adopt upstream driver via newMirrorSession"
```

---

## Task 3: Web client connection — client_id, claimDriver, onDriverChange

**Files:**
- Modify: `web/src/shared/ws/client-conn.ts`
- Test: `web/src/shared/ws/client-conn.test.ts` (create if absent; otherwise append)

- [ ] **Step 1: Write the failing test**

Create `web/src/shared/ws/client-conn.test.ts` (source-level asserts mirror the desktop `lib/connection.test.ts` pattern; the live WS is not exercised):

```ts
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const source = readFileSync(
  fileURLToPath(new URL('./client-conn.ts', import.meta.url)),
  'utf8',
)

describe('client-conn driver/viewer support', () => {
  it('generates a client_id and sends it in ATTACH', () => {
    expect(source).toMatch(/crypto\.randomUUID\(\)/)
    expect(source).toMatch(/client_id:\s*this\.clientID/)
  })
  it('exposes claimDriver() sending a CLAIM_DRIVER frame', () => {
    expect(source).toMatch(/claimDriver\s*\(/)
    expect(source).toMatch(/TYPE\.CLAIM_DRIVER/)
  })
  it('fires onDriverChange from META driver_client_id', () => {
    expect(source).toMatch(/onDriverChange/)
    expect(source).toMatch(/driver_client_id/)
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/shared/ws/client-conn.test.ts
```
Expected: FAIL — patterns not found.

- [ ] **Step 3: Add client identity + handler type**

In `web/src/shared/ws/client-conn.ts`, extend the handler interface (after `onStatus`, ~line 29):

```ts
  onDriverChange?: (driverClientID: string, isMe: boolean, driverName: string) => void
```

Add fields to the class (after `lastSentRows`, ~line 48):

```ts
  private readonly clientID = crypto.randomUUID()
  private readonly clientName = 'web'
  private currentDriverClientID = ''
```

- [ ] **Step 4: Send client_id/client_name in ATTACH**

In `openWS` `ws.onopen` (~line 128), replace the `attachPayload`:

```ts
      const attachPayload = new TextEncoder().encode(JSON.stringify({
        session_id: this.sessionId,
        since_seq: this.lastSeq,
        client_id: this.clientID,
        client_name: this.clientName,
      }))
```

- [ ] **Step 5: Add claimDriver()**

Add a method after `sendResize` (~line 97):

```ts
  claimDriver(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    const payload = new TextEncoder().encode(
      JSON.stringify({ client_id: this.clientID, client_name: this.clientName }),
    )
    this.ws.send(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload))
  }
```

- [ ] **Step 6: Fire onDriverChange from META**

In the `case TYPE.META:` block (~line 156), after `this.handlers.onMeta?.(meta)`:

```ts
            const newDriver = String((meta as { driver_client_id?: unknown }).driver_client_id ?? '')
            const newDriverName = String((meta as { driver_client_name?: unknown }).driver_client_name ?? '')
            if (newDriver !== this.currentDriverClientID) {
              this.currentDriverClientID = newDriver
              this.handlers.onDriverChange?.(newDriver, newDriver !== '' && newDriver === this.clientID, newDriverName)
            }
```

- [ ] **Step 7: Run the test**

```bash
cd web && npx vitest run src/shared/ws/client-conn.test.ts
```
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/shared/ws/client-conn.ts web/src/shared/ws/client-conn.test.ts
git commit -m "feat(web): client_id + claimDriver + onDriverChange in client-conn"
```

---

## Task 4: Web terminal viewer overlay + take-control

**Files:**
- Modify: `web/src/main/components/TerminalView.vue`
- Test: `web/src/main/components/TerminalView.test.ts` (create)

- [ ] **Step 1: Write the failing test**

Create `web/src/main/components/TerminalView.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const source = readFileSync(
  fileURLToPath(new URL('./TerminalView.vue', import.meta.url)),
  'utf8',
)

describe('web TerminalView viewer overlay', () => {
  it('tracks isDriver and toggles disableStdin', () => {
    expect(source).toMatch(/isDriver/)
    expect(source).toMatch(/disableStdin/)
    expect(source).toMatch(/onDriverChange/)
  })
  it('renders a viewer overlay with a take-control button when not driver', () => {
    expect(source).toMatch(/v-if="!isDriver"/)
    expect(source).toContain('remote has taken control')
    expect(source).toMatch(/takeControl/)
    expect(source).toMatch(/claimDriver/)
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/main/components/TerminalView.test.ts
```
Expected: FAIL.

- [ ] **Step 3: Add isDriver state + driver handling**

In `web/src/main/components/TerminalView.vue` `<script setup>`, add a ref (after `replay`, ~line 19):

```ts
const isDriver = ref(true)
```

In `buildConn`'s handlers object (~line 70), add `onDriverChange` and a `takeControl` function above `onMounted`:

```ts
    onDriverChange: (_id, isMe) => {
      isDriver.value = isMe
      if (term) term.options.disableStdin = !isMe
    },
```

Add after `buildConn` (~line 96):

```ts
function takeControl() {
  conn?.claimDriver()
}
```

- [ ] **Step 4: Render the viewer overlay**

In the `<template>`, inside `.term-wrap` (after the replay overlay block, ~line 164):

```vue
      <div v-if="!isDriver" class="viewer-overlay" data-testid="viewer-overlay">
        <div class="viewer-card">
          <div class="viewer-title">remote has taken control</div>
          <button class="take-control" data-testid="take-control" @click="takeControl">Take control</button>
        </div>
      </div>
```

Add scoped styles (after `.replay-fill`, ~line 199):

```css
.viewer-overlay {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  background: rgba(0, 0, 0, 0.6);
}
.viewer-card { display: flex; flex-direction: column; gap: 0.75rem; align-items: center; }
.viewer-title { font-size: 0.9rem; color: var(--fg); }
.take-control {
  padding: 0.5rem 1rem; border: none; border-radius: 8px;
  background: var(--accent, #3b82f6); color: #fff; font-weight: 600;
}
```

- [ ] **Step 5: Run the test**

```bash
cd web && npx vitest run src/main/components/TerminalView.test.ts
```
Expected: PASS.

- [ ] **Step 6: Type-check + build the web bundle**

```bash
cd web && npm run build
```
Expected: vue-tsc + vite build succeed.

- [ ] **Step 7: Commit**

```bash
git add web/src/main/components/TerminalView.vue web/src/main/components/TerminalView.test.ts
git commit -m "feat(web): viewer overlay + take-control button in TerminalView"
```

---

## Task 5: Desktop take-control button

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue` (viewer-overlay ~line 528; `handleViewerKeydown` ~line 104)
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TerminalView.test.ts`:

```ts
test("viewer overlay has a take-control button wired to claimDriver", () => {
  const source = readFileSync(
    fileURLToPath(new URL("./TerminalView.vue", import.meta.url)),
    "utf8",
  );
  expect(source).toMatch(/data-testid="take-control"/);
  expect(source).toMatch(/takeControl/);
  // takeControl calls claimDriver (same conn API as the Space shortcut)
  expect(source).toMatch(/function takeControl[\s\S]*claimDriver/);
});
```

(If `readFileSync`/`fileURLToPath` aren't already imported in this test file, add `import { readFileSync } from "node:fs";` and `import { fileURLToPath } from "node:url";` at the top.)

- [ ] **Step 2: Run it to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts -t "take-control"
```
Expected: FAIL.

- [ ] **Step 3: Add takeControl + the button**

In `desktop/frontend/src/components/TerminalView.vue` `<script setup>`, add near `handleViewerKeydown` (~line 114):

```ts
function takeControl() {
  conn?.claimDriver();
}
```

In the `viewer-overlay` template (~line 528-534), add the button inside `.viewer-overlay-card`, after the hint line:

```vue
        <button class="viewer-overlay-btn" data-testid="take-control" @click="takeControl">Take control</button>
```

Add a scoped style for `.viewer-overlay-btn` (next to the other `.viewer-overlay-*` rules):

```css
.viewer-overlay-btn {
  margin-top: 8px; padding: 6px 14px; border: none; border-radius: 8px;
  background: #3b82f6; color: #fff; font-weight: 600; cursor: pointer;
}
```

- [ ] **Step 4: Run the test + the desktop frontend suite**

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts
```
Expected: PASS (existing Space-intercept + overlay tests still green).

- [ ] **Step 5: Type-check**

```bash
cd desktop/frontend && npm run build:wails
```
Expected: vue-tsc + vite build succeed.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(desktop): take-control button in viewer overlay"
```

---

## Task 6: Mobile viewer overlay + take-control

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue` (handlers ~line 46; template ~line 75)
- Test: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`:

```ts
test("renders a viewer overlay with take-control when not driver", () => {
  const source = readFileSync(
    fileURLToPath(new URL("../MobileTerminal.vue", import.meta.url)),
    "utf8",
  );
  expect(source).toMatch(/onDriverChange/);
  expect(source).toMatch(/isDriver/);
  expect(source).toMatch(/disableStdin/);
  expect(source).toMatch(/data-testid="mobile-take-control"/);
  expect(source).toMatch(/claimDriver/);
});
```

(Add `import { readFileSync } from "node:fs";` and `import { fileURLToPath } from "node:url";` to the test file's imports if not present.)

- [ ] **Step 2: Run it to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts -t "viewer overlay"
```
Expected: FAIL.

- [ ] **Step 3: Add isDriver state + driver handling**

In `desktop/frontend/src/mobile/MobileTerminal.vue` `<script setup>`, add a ref (after `const conn` declarations, ~line 20):

```ts
const isDriver = ref(true)
```

Add `onDriverChange` to the `SessionConnection` handlers object (~line 46, alongside `onOutput`/`onClose`/`onStatus`):

```ts
    onDriverChange: (_id, isMe) => {
      isDriver.value = isMe
      if (term) term.options.disableStdin = !isMe
    },
```

Add a `takeControl` function (after `sendAux`, ~line 35):

```ts
function takeControl() { conn?.claimDriver() }
```

- [ ] **Step 4: Render the viewer overlay**

In the `<template>`, inside `.mobile-term` (after the `.term` div, before `.kbbar`, ~line 77):

```vue
    <div v-if="!isDriver" class="viewer-overlay">
      <div class="viewer-card">
        <div class="viewer-title">remote has control</div>
        <button class="take-control" data-testid="mobile-take-control" @click="takeControl">Take control</button>
      </div>
    </div>
```

Add scoped styles:

```css
.viewer-overlay { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,.55); }
.viewer-card { display: flex; flex-direction: column; align-items: center; gap: 10px; }
.viewer-title { color: #e6e7ea; font-size: 0.9rem; }
.take-control { padding: 8px 16px; border: none; border-radius: 8px; background: #3b82f6; color: #fff; font-weight: 600; }
```

Make `.mobile-term` a positioned container so the overlay anchors to it — change `.mobile-term` to include `position: relative;` (current rule: `.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; }`):

```css
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; position: relative; }
```

- [ ] **Step 5: Run the test + the mobile suite**

```bash
cd desktop/frontend && npx vitest run src/mobile
```
Expected: PASS (all existing mobile tests stay green).

- [ ] **Step 6: Type-check both targets**

```bash
cd desktop/frontend && npm run build:capacitor && npm run build:wails
```
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/mobile/MobileTerminal.vue desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "feat(mobile): viewer overlay + take-control in MobileTerminal"
```

---

## Final verification (after all tasks)

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./internal/... && go test ./internal/session/ ./internal/relay/
cd desktop/frontend && npm run build:wails && npx vitest run
cd ../../web && npm run build && npx vitest run
```

Then manual two-client smoke (needs desktop env): open a session on desktop A, attach desktop B (and a browser) → each remote client shows the viewer overlay; tapping **Take control** flips A into the "remote has taken control" overlay and lets the remote type; A presses Space / Take control to reclaim.

---

## Self-Review

**Spec coverage:**
- Mirror reflects upstream driver → Task 1 (`UpdateMeta` adopt) + Task 2 (flag wired at creation). ✓
- Remote attachers render as viewers → Tasks 1/2 (correct META) consumed by Tasks 4/6 (overlay). ✓
- Take-control end-to-end → existing CLAIM_DRIVER→ClaimLocalDriver path (unchanged) + new `claimDriver()` callers (Tasks 3/4/5/6). ✓
- Unified Take-control button on all clients → Tasks 4 (web), 5 (desktop), 6 (mobile); desktop keeps Space. ✓
- Web gains full driver/viewer → Tasks 3 + 4. ✓
- Preserve #4 solo fix → Task 1 keeps local-session path untouched; `TestUpdateMetaBroadcastsPreservesDriverState` rerun in Step 6. ✓
- No wire change → only existing fields/frames used. ✓
- SP2 (N-viewers indicator) excluded → not in any task. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code and exact commands/expected output.

**Type consistency:** `onDriverChange(driverClientID, isMe, driverName)` signature is identical across web `client-conn.ts` (Task 3), web `TerminalView.vue` (Task 4), and matches the desktop `lib/connection.ts` already consumed by mobile (Task 6). `claimDriver()` is parameterless everywhere. `SetDriverFromUpstream(bool)` / `newMirrorSession(id, info, ownerUserID)` signatures match between Task 1 and Task 2. `data-testid` values (`take-control`, `mobile-take-control`, `viewer-overlay`) are unique per client.
