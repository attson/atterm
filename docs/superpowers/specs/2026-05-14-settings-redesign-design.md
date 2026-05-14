# Settings Dialog Redesign

**Date:** 2026-05-14
**Status:** Approved

## Goal

Restructure the desktop settings dialog from a single 600-line flat column into a sidebar + tab layout, and polish the visual treatment. Each tab is a focused subcomponent of ~100-150 LOC.

## Motivation

The current `SettingsDialog.vue` mixes four unrelated concerns (terminal theme, relay, logging, updates) under a misleading "relay settings" header. It is one ~600-line file with two extra `<h2>`s mid-page, a fixed 460px width that feels cramped on update content, and an inconsistent save model — relay needs an explicit button while logging and updates auto-save, but the unified Save button at the bottom obscures that. Users find the dialog hard to scan and the dev cost of changes is high because every tweak touches the same large file.

## Non-Goals

- A search / filter input over settings.
- Changes to the settings persistence format or backing config store.
- Adding new settings beyond what already exists (BEL-notifications toggle and similar belong to their own features).
- A standalone "About" tab — version stays in Updates.

## Layout

A modal centered on a 60%-opacity backdrop, identical to the current dialog framing but bigger and split:

```
┌────────────────────────────────────────────────────────────┐
│ SETTINGS                                                 x │
├──────────────┬─────────────────────────────────────────────┤
│ ▸ General    │  <active tab content>                       │
│ ▸ Relay      │                                             │
│   Logging    │                                             │
│   Updates    │                                             │
├──────────────┴─────────────────────────────────────────────┤
│                                <pinned-bottom action row>  │
└────────────────────────────────────────────────────────────┘
```

- Dialog: 720×540, fixed; capped at `calc(100vw - 32px)` / `calc(100vh - 32px)` to remain reachable on small screens.
- Sidebar: 160px, full-height, separated by a 1px `--border` divider. Active item uses `--accent` background tint; non-active items use `--fg-dim` on hover-tinted background.
- Right pane scrolls vertically; the sidebar and action row stay pinned.
- Action row only appears on tabs that need explicit save (Relay). Other tabs render no action row.

## Tabs

### General

- Terminal theme: existing `<select>` of `TERMINAL_THEMES`. Auto-saves on change via `setTerminalThemePreference`, emits `terminal-theme-changed` to parent. Existing behavior preserved verbatim.

This tab is intentionally small. It is also the natural home for app-wide preferences added later (quit-confirm toggle, BEL-notifications toggle).

### Relay

- Relay URL: text input.
- Token: password input.
- Remote session permissions: `view` / `control` / `full` select.
- Allow insecure ws://: checkbox; warning paragraph appears when checked.
- Connection status pill at the top of the tab: green-dot "uplink running" or grey-dot "uplink stopped". Always visible — does not scroll out of view.
- Pinned action row at the dialog footer: `Cancel`, `Disconnect` (when connected), `Save & Connect` (primary). The button labels and disable rules match the current implementation.
- Dirty-state warning: if the user edits any field and clicks a different tab in the sidebar, a confirm dialog appears: "Discard unsaved relay changes?" with `Discard` and `Stay`. No warning if the fields are unchanged.

### Logging

- "Write logs to file" checkbox: auto-saves via `setLoggingConfig`.
- Current log file path: monospace, truncated with tooltip.
- Three buttons: `Change location`, `Reset default`, `View logs`. Same behavior as current code.
- No pinned action row — all changes auto-save.

### Updates

- Current version + status line (existing `updateStatusLine` computed).
- Auto-check toggle.
- Release notes `<details>` (closed by default).
- Action buttons inline in the tab (not pinned): `Check now`, `Download {latest}`, `Force install & restart`. Visibility rules match current code.
- The `ConfirmInstallDialog` still mounts at the dialog root for force-install confirmation.

## Components

- `SettingsDialog.vue` — owns the backdrop, the sidebar nav, the active-tab state, the action row slot, and the close behavior. Hosts `ConfirmInstallDialog` and `LogViewerDialog`. No settings-domain logic.
- `SettingsGeneral.vue` — terminal theme select. Props: `terminalThemeId`. Emits: `terminal-theme-changed`.
- `SettingsRelay.vue` — relay form, connection status, save/disconnect handlers. Props: none required (fetches own state on mount). Emits: `relay-config-changed`, `dirty`, `save-requested`. The action-row buttons live in the parent and call back into the tab via `defineExpose`.
- `SettingsLogging.vue` — logging toggle + path + buttons. Props: none. Emits: `open-log-viewer` so the parent can mount `LogViewerDialog`.
- `SettingsUpdates.vue` — version, auto-check, release notes, action buttons. Props: `localSessionCount`, `remoteSessionCount` (forwarded to `ConfirmInstallDialog` via parent). Emits: `request-install` so the parent mounts the confirm dialog.

Each subcomponent fetches its own backing state via the existing `lib/api` wrappers in `onMounted`. They do not share state via the parent.

## Style Tokens (CSS)

Reuse existing CSS variables: `--panel`, `--border`, `--fg`, `--fg-dim`, `--accent`, `--bad`, `--good`. No new variables.

- Form-field group spacing: 14px between control groups; 6px between label and its control.
- Input height: 32px; padding 6px 10px; focus ring `0 0 0 2px var(--accent)`.
- Buttons: 32px height, consistent padding 6px 14px.
- Sidebar item: 36px height, 12px horizontal padding, 4px vertical padding.
- Section heading inside tab: 13px uppercase letterspace 0.05em color `--fg-dim`, matching existing header style.

## Data Flow

```
SettingsDialog.vue
  │  active tab: ref<"general"|"relay"|"logging"|"updates">
  │  dirty:      ref<boolean>     ← bubbled up from SettingsRelay
  │
  ├─ <aside class="settings-nav">  (renders 4 buttons, click → set active tab)
  ├─ <section class="settings-pane">
  │    └─ <component :is="activeTabComponent" v-bind="…" @… />
  └─ <footer v-if="activeTab === 'relay'">
       <button @click="cancel">Cancel</button>
       <button v-if="connected" @click="disconnect">Disconnect</button>
       <button class="primary" @click="save">Save &amp; Connect</button>
     </footer>
```

The Relay tab exposes its `save()` / `disconnect()` / `connected` / `url` via `defineExpose`, and the parent calls into them through a `ref<InstanceType<typeof SettingsRelay>>`.

## Error Handling

Each subcomponent owns its own `error` ref and renders an inline `.error` paragraph at the bottom of its content. The parent does not aggregate errors — errors stay local to the tab that produced them, and the user sees them in context.

## Testing

The existing `SettingsDialog.test.ts` (6 source-level tests) is restructured rather than rewritten wholesale:

- Tests that asserted on relay form markup move to a new `SettingsRelay.test.ts`.
- Tests for theme select move to `SettingsGeneral.test.ts`.
- Tests for logging toggle/path move to `SettingsLogging.test.ts`.
- Tests for updates / auto-check / release notes move to `SettingsUpdates.test.ts`.
- `SettingsDialog.test.ts` shrinks to assert: sidebar renders the four labels; clicking a sidebar button activates the corresponding tab; the footer action row only renders for the relay tab; closing the dialog emits `close`.

All assertions remain source-level (`import source from "./X.vue?raw"`) — the existing test pattern.

## Risks

- The Relay tab's dirty-confirm flow is a small UX addition. If the parent and child don't coordinate the `dirty` ref correctly, the user might lose unsaved changes silently. Mitigation: a source-level test asserts `SettingsRelay.vue` emits `dirty` whenever any input changes, and that the parent listens for it.
- Splitting one 600-line file into five subcomponents touches every existing settings test. Mitigation: do the test-file moves in the same commit as the subcomponent extraction so the diff is reviewable.
- The pinned action row design assumes the active tab name is enough to decide what's in the footer. If we later add a tab that also needs explicit save (e.g., a hypothetical "Backups" tab), the dialog needs to grow per-tab footer slots. We accept this risk — the current set is fixed.
