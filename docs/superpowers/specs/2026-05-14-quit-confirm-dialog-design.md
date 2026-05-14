# Quit-Confirmation Dialog Design

**Date:** 2026-05-14
**Status:** Approved

## Goal

Add a confirmation dialog that appears when the user tries to close the desktop app while at least one local or remote session is active. If no sessions are active, the app quits silently.

## Motivation

The desktop app currently quits as soon as the user hits the window close button, Cmd+Q, or chooses Quit from the app menu. Local PTY processes are killed and remote attachments are detached without warning. Users have asked for a confirmation step so an accidental Cmd+Q doesn't drop in-flight work.

## Non-Goals

- "Don't ask again" preference. The dialog only shows when there is something to lose, so a permanent suppression is not needed.
- Per-tab or per-pane close confirmation. Those are separate UX surfaces.
- A "save session" or "restore on next launch" feature.
- Intercepting `SIGKILL`, force-quit, OS-driven reboot beyond what Wails surfaces in `OnBeforeClose`, or webview crashes.

## Behavior

1. Any path that goes through Wails' `OnBeforeClose` triggers the flow: window close button, `Cmd+Q` via the macOS app menu, and OS-initiated app quits where the framework cooperates.
2. The Go side emits a `before-close` runtime event to the frontend and returns `true` from `OnBeforeClose`, preventing the quit.
3. The frontend's `before-close` handler counts active panes in `tabs.value`:
   - **Local pane** = `pane.sessionId != null && !pane.remote`.
   - **Remote pane** = `pane.sessionId != null && pane.remote`.
4. If both counts are zero, the frontend calls a new `ConfirmQuit()` binding directly. No dialog.
5. If either count is greater than zero, the frontend renders `ConfirmQuitDialog.vue` with `:local-count` and `:remote-count` props.
   - **Cancel** closes the dialog. The quit was already prevented in step 2; nothing else to do.
   - **Quit** calls `ConfirmQuit()`.
6. `ConfirmQuit()` sets an `quitApproved` flag on the `App` struct, then calls `wailsruntime.Quit(a.ctx)`. The next `OnBeforeClose` returns `false` because the flag is set, so the framework proceeds with shutdown.

The dialog wording follows `ConfirmInstallDialog.vue`:

- "End N local shell session(s) — running processes will be terminated"
- "Detach from N remote session(s) — the remote PTY keeps running on its host"
- A `Save your work first.` warning line appears only when local count > 0.

The primary button is labeled `quit` and uses the existing `.primary.danger` styling when local count > 0; otherwise it uses the plain `.primary` styling. Cancel is the secondary action.

## Architecture

### Components Touched

- **`desktop/main.go`** — register `OnBeforeClose: app.beforeClose` in the Wails options struct.
- **`desktop/app.go`** — add `quitApproved bool` field on `App`; add `beforeClose(ctx context.Context) bool` method; add `ConfirmQuit()` Wails binding.
- **`desktop/app_test.go`** (new file) — unit-test `beforeClose` flag-gating using a local `App` value (no Wails runtime required).
- **`desktop/frontend/src/lib/api.ts`** — add `ConfirmQuit()` to the `AppBindings` interface and export a `confirmQuit()` wrapper.
- **`desktop/frontend/src/components/ConfirmQuitDialog.vue`** (new) — dialog component, props `localCount`, `remoteCount`, emits `confirm` and `cancel`.
- **`desktop/frontend/src/components/ConfirmQuitDialog.test.ts`** (new) — source-level tests asserting the dialog renders the local/remote count props and exposes a `confirm` emit.
- **`desktop/frontend/src/App.vue`** — register a `runtime.EventsOn("before-close", ...)` listener in `onMounted`, compute local/remote pane counts at handler time, either call `confirmQuit()` or open the dialog. Unregister the listener in `onBeforeUnmount`. Wire the dialog's `confirm`/`cancel` emits.
- **`desktop/frontend/src/App.test.ts`** — source-level test that App.vue registers the listener, imports `confirmQuit`, and includes the dialog markup.

### Data Flow

```
window close button / Cmd+Q
  → Wails OnBeforeClose (Go)
    → if quitApproved: return false   →  framework quits
    → else: EventsEmit("before-close") + return true
      → frontend handler
        → if localCount == 0 && remoteCount == 0: ConfirmQuit()  →  quitApproved = true → runtime.Quit() → OnBeforeClose returns false → framework quits
        → else: show <ConfirmQuitDialog/>
          → cancel: hide dialog
          → confirm: ConfirmQuit()  →  quitApproved = true → runtime.Quit() → OnBeforeClose returns false → framework quits
```

### Error Handling

The `before-close` listener is the only path that consumes the event. If the frontend is mid-mount or hot-reloading when the event fires, the event is dropped — the user would see the close be prevented with no dialog, and a second Cmd+Q would land normally. Acceptable; no recovery code needed.

`ConfirmQuit()` calling `runtime.Quit()` cannot fail in a way callers can recover from; we don't propagate an error.

## Testing

**Go (`desktop/app_test.go`):**
- `TestBeforeCloseEmitsAndPreventsByDefault` — first call returns `true` and (via stubbed emitter) records that `before-close` was emitted.
- `TestBeforeCloseAllowsQuitWhenApproved` — after setting `quitApproved = true`, `beforeClose` returns `false` and does not emit.

The test exercises a refactored seam: `beforeClose` accepts an injected emitter function (a `func()`) so the test doesn't need a real Wails runtime. The production wiring passes a closure that calls `wailsruntime.EventsEmit(a.ctx, "before-close")`.

**Frontend source-level tests:**
- `ConfirmQuitDialog.test.ts` — asserts the dialog template references `localCount`, `remoteCount`, has a `Cancel` button, has a primary action wired to a `confirm` emit, and applies `.primary.danger` when local count > 0.
- `App.test.ts` — extends an existing or new describe block, asserts `App.vue` source contains `EventsOn("before-close"` and `confirmQuit(`, imports `ConfirmQuitDialog`, and includes the dialog in its template.

**Manual verification:**
1. Launch desktop app with no sessions, hit Cmd+Q → app quits silently.
2. Open a local shell, hit Cmd+Q → dialog appears with "End 1 local shell session"; cancel keeps app open; quit closes it.
3. Open a remote attach, hit Cmd+Q → dialog appears with "Detach from 1 remote session"; quit closes the app and the remote PTY continues running on its host.
4. Click the window close button → same flow as Cmd+Q.
5. Open multiple tabs with mixed local/remote panes → counts roll up across all tabs.

## Risks

**Low.** The Go change is small and behind a flag; the existing close flow is preserved when `quitApproved` is set. The frontend change is additive (new dialog, new event listener); existing surfaces are unaffected.

The one real risk is that Wails' `OnBeforeClose` event timing varies subtly across platforms — for example, Cmd+Q on macOS routes through the AppKit terminate flow rather than the window-close flow. We rely on Wails' v2 contract that both paths fire `OnBeforeClose`. Manual verification step 2 covers this; if Cmd+Q bypasses the hook on a platform, that's a Wails-side issue we accept and surface at QA, not a design flaw.
