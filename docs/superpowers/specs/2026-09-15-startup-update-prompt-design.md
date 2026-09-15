# Startup Update Prompt — Design

Date: 2026-09-15
Status: Approved for planning

## Problem

The auto-update backend is complete (`desktop/updater.go` + `desktop/app_updater.go`):
it checks GitHub ~2s after boot and every 24h, and can download/verify/install.
But the **only UI** for updates lives in Settings → Updates
(`SettingsUpdates.vue`), reachable by clicking the ⚙ gear in the TabBar.

On desktop boot the app immediately enters a session (auto `startNewTab()` or the
`RecoveryDialog`), so an available update is surfaced only as a small red dot on
the ⚙ gear. A user who wants to update must first be inside a running session,
then open Settings → Updates. There is no update path *at startup, before
entering a session*.

## Goal

On desktop startup, before entering the first session, if an update is detected,
present a dedicated update dialog offering download + install. If there is no
update (or the check times out, or the user disabled auto-check), boot proceeds
silently as it does today.

## Scope

- Desktop (Wails) only: gated on `caps.autoUpdate && caps.wailsBindings`.
- Web / Capacitor builds are unchanged — they have no bundled updater
  (`caps.autoUpdate === false`).
- **No Go backend changes.** All required bindings already exist:
  `checkUpdate`, `getUpdateState`, `startDownload`, `cancelDownload`,
  `installUpdate`, `getAutoCheckUpdates`.

## Hard Invariant — never pop during normal use

The startup update dialog opens **exactly once, at the boot gate, before the
first session**. It MUST NOT appear while the app is in normal use.

- The only code path that opens it is the one-time boot auto-start block in
  `App.vue`, guarded by the existing `autoStarted` flag and `tabs.length === 0`.
- The existing 5s `getUpdateState()` poll continues to **only** toggle the ⚙
  badge dot (`updateBadge`); it never opens the dialog.
- The background 24h check likewise only feeds the ⚙ badge.
- Once the user dismisses the dialog ("Later"), it does not re-open for the rest
  of that run — only the ⚙ badge remains.

## Design

### 1. New component: `StartupUpdateDialog.vue`

Modeled on `RecoveryDialog.vue` (same modal backdrop / card look), reusing the
same update bindings as `SettingsUpdates.vue`. Deliberately leaner than the
Settings tab — no auto-check toggle, no GH-proxy field, no multi-version-line
selector, no force-redownload. Those advanced controls stay in Settings →
Updates. This dialog does only the "latest version" happy path.

- **State:** polls `getUpdateState()` every 2s (same cadence as
  `SettingsUpdates.vue`); local refs for in-flight/cancelling.
- **Displays:** current → latest version, a status line
  (available / downloading N% / ready / error), and a collapsible release-notes
  section (`state.notes`).
- **Buttons:**
  - Primary "Download & Install" → `startDownload()`.
    - While `state.downloading`: button becomes "Cancel (N%)" → `cancelDownload()`.
    - When `state.ready` becomes true: button becomes "Install & Restart" →
      `installUpdate()` (app quits, helper replaces install, relaunches).
    - **Ready does NOT auto-install** — the user must click "Install & Restart".
      This avoids an unexpected restart interrupting the user.
  - Secondary "Later" → emits `dismiss`; boot resumes. ⚙ badge remains so the
    user can still update from Settings later.
- **Errors:** `state.error` shown inline; "Later" is always available so a failed
  download never traps the user at boot.

Emits: `dismiss`. (Install is handled internally via `installUpdate()`, which
quits the app, so no parent action is needed for the install path.)

### 2. `App.vue` boot refactor

Extract the current recovery-or-startNewTab branch (today at App.vue ~1722–1735)
into a `resumeBoot()` function that performs exactly the existing logic:

```
function resumeBoot() {
  const hasRecovery = recoveryEnabled && (recoverySnap?.tabs?.length ?? 0) > 0;
  if (hasRecovery) {
    recoveryDialogState.value = { open: true, snapshot: recoverySnap };
  } else if (caps.localPty) {
    startNewTab();
  }
}
```

Insert a one-time update gate before it, inside the existing
`if (!autoStarted && tabs.value.length === 0) { ... !webTabs.tryRestoreOnBoot() }`
block:

```
if (caps.wailsBindings && caps.autoUpdate && (await getAutoCheckUpdates())) {
  const available = await checkForUpdateBounded(BOOT_UPDATE_CHECK_TIMEOUT_MS);
  if (available) {
    startupUpdateOpen.value = true;   // dialog's `dismiss` handler calls resumeBoot()
    return;                           // defer resumeBoot until user dismisses
  }
}
resumeBoot();
```

`checkForUpdateBounded(ms)`:
- Runs `await Promise.race([checkUpdate(), timeout(ms)])`, then reads
  `getUpdateState()` and returns `!!state.available`.
- On any error / timeout, returns `false` (boot proceeds; the background 24h
  loop + ⚙ badge still cover the late-arriving case).
- `BOOT_UPDATE_CHECK_TIMEOUT_MS` default: **3000ms**.

Ordering when both an update and a recovery snapshot exist: the update dialog
shows first; on "Later" → `resumeBoot()` shows the recovery dialog / startNewTab.
(If the user installs, the app restarts and the recovery snapshot — still on
disk — surfaces on the next boot.)

Dialog wiring in template:
```
<StartupUpdateDialog
  v-if="startupUpdateOpen"
  @dismiss="onStartupUpdateDismiss"
/>
```
```
function onStartupUpdateDismiss() {
  startupUpdateOpen.value = false;
  resumeBoot();
}
```

The one-time-ness is enforced by `autoStarted` (already set true at the top of
the block) plus the fact that nothing outside this block ever sets
`startupUpdateOpen`.

### 3. i18n

Add a `startupUpdate.*` message group to both
`desktop/frontend/src/i18n/messages/en.ts` and `zh.ts` (English + Chinese kept
in sync per repo convention). Keys (indicative): `title`, `subtitle`,
`currentToLatest`, `releaseNotes`, `downloadInstall`, `cancel`, `installRestart`,
`later`, plus status strings for downloading/ready (or reuse existing
`settings.updates.*` where wording matches).

## Data flow

```
boot (App.vue onMounted, Wails branch)
  └─ auto-start block (once, tabs==0, !autoStarted)
       └─ tryRestoreOnBoot? no
            └─ update gate (caps.autoUpdate && autoCheck)
                 ├─ checkForUpdateBounded(3s) == true
                 │     └─ open StartupUpdateDialog
                 │          ├─ Download & Install → startDownload → (ready) → installUpdate → quit/relaunch
                 │          └─ Later → resumeBoot()
                 └─ false/timeout → resumeBoot()
                                       ├─ recovery snapshot → RecoveryDialog
                                       └─ else localPty → startNewTab()
```

## Testing

- `StartupUpdateDialog.test.ts`: renders current/latest version; "Download &
  Install" calls `startDownload`; shows "Install & Restart" when `ready`, which
  calls `installUpdate`; "Later" emits `dismiss`; error string renders.
- `App.test.ts` boot branches (mock `checkUpdate` / `getUpdateState` /
  `getAutoCheckUpdates`):
  - update available → `startupUpdateOpen` true, recovery/startNewTab deferred
    until `dismiss`.
  - no update / timeout → normal boot (recovery or startNewTab), dialog never
    opens.
  - `autoCheck` disabled → gate skipped, normal boot.
  - Regression: the 5s poll toggling the badge does not open the dialog.
- Keep existing `SettingsUpdates` / `SettingsDialog` tests green (unchanged).

## Files touched

- `desktop/frontend/src/components/StartupUpdateDialog.vue` (new)
- `desktop/frontend/src/App.vue` (extract `resumeBoot()`, add gate + dialog
  state/handlers + render)
- `desktop/frontend/src/i18n/messages/en.ts`, `zh.ts`
- Tests: `StartupUpdateDialog.test.ts` (new), `App.test.ts` (boot branches)

## Out of scope / YAGNI

- No "skip this version" persistence.
- No auto-install without user click.
- No multi-version-line selection or force-redownload in the startup dialog
  (those remain in Settings → Updates).
- No changes to web/mobile.
