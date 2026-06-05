# Mobile settings page + customizable shortcuts (design)

Date: 2026-06-06
Status: Draft (design phase); pending user review then implementation plan

## 1. Goal

Three related mobile UX improvements, shipped as one batch:

1. **Customizable aux-key bar** — the bottom row of fixed control keys
   (`enter` / `esc` / `tab` / `⌃C` / `⌃D` / arrows) becomes a
   user-editable list. `paste` / `image` stay as fixed function buttons.
2. **Mobile template management** — the quick-template row (`y` / `n` /
   `continue` / …), today read-only-defaults on mobile, gains the same
   add / edit / reorder / reset editor desktop already has, surfaced on
   the new settings page (like the desktop quick-input plugin).
3. **Dedicated settings page** — the `gear` button currently drops the
   user straight onto the connect/setup screen. Instead it opens a real
   settings page with: language switch, aux-key editor, template editor,
   and logout. Logout returns to the connect page **without clearing the
   saved config** (config stays pre-filled).

Also folded in (separate, smaller): **image-picker menu i18n** — replace
the native `<input type=file>` system sheet (whose "Photo Library /
Choose File" text follows the iOS system language, not the app) with
`@capacitor/camera`, whose prompt labels we can localize.

### Out of scope

- Desktop changes (desktop already has the template editor; aux keys are
  a mobile-only concept — desktop has a real keyboard).
- Syncing aux-keys / templates across devices (each end keeps its own
  localStorage list, same as templates today).
- Per-key icons / colors for aux keys (label text only).
- Camera capture/recording features beyond picking one image.

## 2. Data model

### 2.1 AuxKey (new)

`desktop/frontend/src/lib/auxKeys.ts` (new):

```ts
export interface AuxKey {
  id: string     // crypto.randomUUID() at creation; stable thereafter
  label: string  // button text, e.g. "esc", "⌃C", "↑"
  seq: string     // raw bytes sent verbatim on tap (NO trailing \r)
}

// DEFAULT_AUX_KEYS mirrors the current hardcoded AUX_KEYS in
// MobileTerminal.vue. Stable string ids so re-seeding after reset is churn-free.
export const DEFAULT_AUX_KEYS: AuxKey[] = [
  { id: 'aux-enter', label: 'enter', seq: '\r' },
  { id: 'aux-esc',   label: 'esc',   seq: '\x1b' },
  { id: 'aux-tab',   label: 'tab',   seq: '\t' },
  { id: 'aux-ctrl-c', label: '⌃C',   seq: '\x03' },
  { id: 'aux-ctrl-d', label: '⌃D',   seq: '\x04' },
  { id: 'aux-up',    label: '↑',     seq: '\x1b[A' },
  { id: 'aux-down',  label: '↓',     seq: '\x1b[B' },
  { id: 'aux-left',  label: '←',     seq: '\x1b[D' },
  { id: 'aux-right', label: '→',     seq: '\x1b[C' },
]

// Same seed-on-empty contract as effectiveTemplates: empty stored list →
// defaults; storage stays empty until the user edits, so "reset" = clear.
export async function effectiveAuxKeys(
  bridge: { load: () => Promise<AuxKey[]> },
): Promise<AuxKey[]> {
  const stored = await bridge.load()
  return stored.length > 0 ? stored : DEFAULT_AUX_KEYS
}
```

**Key difference from QuickTemplate:** `seq` is sent raw via
`conn.sendInput(seq)` — no `\r` appended, no preview dialog. Aux keys are
instant control-key presses. Templates append `\r` and show a preview
(unchanged).

The editor's "send text" field accepts escape notation so users can enter
control bytes: the editor parses `\r \n \t \e \xNN` and `^X` (caret →
control char) into real bytes before storing. Stored `seq` is the decoded
raw string. A small `parseSeq(input): string` / `displaySeq(seq): string`
pair in `auxKeys.ts` handles the round-trip so the editor shows `\x1b`
rather than an invisible ESC.

### 2.2 QuickTemplate (reused as-is)

No model change. Mobile just gains the editor UI; the `platform.templates`
bridge already exists and works on capacitor (localStorage).

## 3. Platform bridge

`platform/types.ts` gains an `AuxKeyBridge`, mirroring `TemplateBridge`:

```ts
export interface AuxKeyBridge {
  load(): Promise<AuxKey[]>
  save(list: AuxKey[]): Promise<void>
  clear(): Promise<void>
}

export interface Platform {
  // …existing…
  auxKeys: AuxKeyBridge
}
```

- **capacitor.ts** — localStorage key `atterm.auxkeys`, byte-for-byte copy
  of the templates impl (parse/guard JSON, `[]` on miss).
- **wails.ts** — desktop never renders the aux bar, but the bridge must
  exist to satisfy the `Platform` type. Back it with the same config.json
  pattern as templates (`GetAuxKeys` / `SetAuxKeys` bindings + appConfig
  field) for symmetry, OR a localStorage-less no-op returning `[]`. Defer
  to implementer; the cheap correct option is the config.json pair so the
  shape matches templates exactly.
- **_fakePlatform.ts** — in-memory list for tests.

Persistence note: localStorage on iOS WKWebView **is** durable across app
restarts (the relay-config loss was a Keychain-plugin registration bug,
not a localStorage problem — see the v0.2.38 fix). Aux keys / templates
are non-sensitive UI prefs, so localStorage is the right store; no
Keychain needed.

## 4. Navigation

`MobileApp.vue` gains a `'settings'` view:

```
type View = 'setup' | 'list' | 'terminal' | 'settings'
```

- `MobileSessionList` `gear` button: change emit from `editRelay` →
  `openSettings`. `MobileApp.onOpenSettings()` sets `view = 'settings'`.
- `MobileSettings` emits:
  - `back` → `view = 'list'`
  - `logout` → return to `setup` **without** `platform.relay.clear()`.
    The config stays in Keychain; `MobileSetup.onMounted` re-fills url +
    token from it. (Per user decision: "只要退出登录，不清空配置".)
    `MobileApp` also tears down open terminals + recency on logout, same
    as `onTokenInvalid`, so we don't leave WS connections dangling on a
    screen the user logically left.

> **Open question for review:** the label says "退出登录" (logout) but we
> deliberately do NOT clear the token. Confirm the intended behavior is
> "return to the connect screen with config preserved." If a true
> credential wipe is wanted later, that's a separate `clear()` call.

Setup screen keeps its own language selector (a first-time user has no
settings page yet), so language lives in both places, both driving the
same `setLocalePreference`.

## 5. UI components

### 5.1 MobileSettings.vue (new)

A full-screen page (same shell style as `MobileSetup`). Sections:

```
┌─ 设置 ────────────────────────────── [← 返回] ┐
│                                                │
│  语言            [ 跟随系统 ▾ ]                 │
│                                                │
│  快捷模板                                       │
│   ┌──────────────────────────────────────┐    │
│   │ y    │ y         │ ↑ ↓ 编辑 删除 │    │     │
│   │ …                                    │    │
│   └──────────────────────────────────────┘    │
│   [ + 新增 ]            [ 恢复默认 ]            │
│                                                │
│  快捷按键（控制键）                             │
│   ┌──────────────────────────────────────┐    │
│   │ esc  │ \x1b      │ ↑ ↓ 编辑 删除 │    │     │
│   │ …                                    │    │
│   └──────────────────────────────────────┘    │
│   [ + 新增 ]            [ 恢复默认 ]            │
│                                                │
│  [ 退出登录 ]                                   │
└────────────────────────────────────────────────┘
```

- Two editor lists share one inner component
  `MobileListEditor.vue` (generic over `{id,label,value}` rows with
  add/edit/reorder/delete/reset), so templates and aux keys don't
  duplicate the row/edit/reorder logic. The aux editor passes
  parse/display fns for the `seq` field; the template editor passes
  identity fns for `text`.
- Reuses the desktop `SettingsTemplates.vue` *logic* (reload/persist/
  move/edit/reset) but as a touch-sized component (42px rows, larger tap
  targets), not the desktop dialog styling.
- Language selector = the same native `<select>` MobileSetup uses.

### 5.2 MobileTerminal.vue (modified)

- Replace the hardcoded `AUX_KEYS` const with a reactive `auxKeys` ref
  loaded via `effectiveAuxKeys(platform.auxKeys)` in `onMounted`
  (mirrors how templates load today).
- The aux bar renders `auxKeys` + the two fixed function buttons
  (`paste`, `image`) appended at the end. Function buttons are NOT part
  of the editable list.
- `sendAux(seq)` unchanged (still `sendRaw(seq)`, no `\r`).
- The template bar already loads via `effectiveTemplates`; no change
  beyond picking up edits next mount (lists reload on mount; live refresh
  across the settings round-trip is acceptable since leaving the terminal
  to open settings remounts the bar on return).

### 5.3 Image picker → @capacitor/camera (#5)

- Add dependency `@capacitor/camera` (official SPM plugin; `cap sync`
  registers it — no manual pbxproj surgery like the custom Keychain one).
- `openImagePicker()` becomes:

  ```ts
  const photo = await Camera.getPhoto({
    source: CameraSource.Prompt,
    resultType: CameraResultType.Base64,
    promptLabelHeader: t('mobile.image.prompt'),
    promptLabelPhoto: t('mobile.image.fromLibrary'),
    promptLabelPicture: t('mobile.image.takePhoto'),
    promptLabelCancel: t('common.cancel'),
  })
  // base64 → Blob/File → existing conn.sendPasteImage(file, name)
  ```
- Keep the `canSend` / `nudgeProtect` gate.
- iOS `Info.plist` needs `NSCameraUsageDescription` +
  `NSPhotoLibraryUsageDescription` (Camera plugin requirement).
- The hidden `<input type=file>` and `onImagePicked` are removed.

## 6. i18n (en + zh-CN)

| key | en | zh-CN |
|---|---|---|
| `mobile.settings.title` | "Settings" | "设置" |
| `mobile.settings.language` | "Language" | "语言" |
| `mobile.settings.templates` | "Quick templates" | "快捷模板" |
| `mobile.settings.auxKeys` | "Shortcut keys" | "快捷按键" |
| `mobile.settings.logout` | "Log out" | "退出登录" |
| `mobile.settings.back` | "Back" | "返回" |
| `mobile.settings.add` | "Add" | "新增" |
| `mobile.settings.edit` | "Edit" | "编辑" |
| `mobile.settings.delete` | "Delete" | "删除" |
| `mobile.settings.reset` | "Reset to defaults" | "恢复默认" |
| `mobile.settings.label` | "Label" | "标签" |
| `mobile.settings.seq` | "Send bytes (\\r \\n \\t \\e \\xNN ^X)" | "发送字节（\\r \\n \\t \\e \\xNN ^X）" |
| `mobile.settings.text` | "Send text" | "发送文本" |
| `mobile.image.prompt` | "Add image" | "添加图片" |
| `mobile.image.fromLibrary` | "Photo Library" | "从相册选择" |
| `mobile.image.takePhoto` | "Take Photo" | "拍照" |

`common.cancel` already exists.

## 7. Testing

- `lib/__tests__/auxKeys.test.ts`: `effectiveAuxKeys` defaults-on-empty;
  `parseSeq`/`displaySeq` round-trip for `\r \n \t \e \xNN ^X`; default
  list has stable unique ids.
- `platform/__tests__/capacitor.test.ts`: auxKeys load/save/clear
  round-trip (extend, mirroring the templates cases).
- `mobile/__tests__/MobileSettings.test.ts`: renders language + both
  editors + logout; add/edit/reorder/delete calls the right bridge save;
  reset clears + reseeds; logout emits `logout` and does NOT call
  `relay.clear`.
- `mobile/__tests__/MobileTerminal.test.ts` (extend): aux bar renders the
  loaded list; editing persists; `paste`/`image` always present; tapping
  a custom aux key sends its raw seq with no `\r`.
- `mobile/__tests__/MobileApp.test.ts` (extend): `gear`→settings view;
  `logout`→setup view with config still loadable.
- Image: unit-test the base64→File conversion helper; the Camera plugin
  call itself is mocked (native path verified on device).

## 8. Rollout

- One new dependency (`@capacitor/camera`); `cd mobile && npm run ios:sync`
  picks it up. Requires the two Info.plist usage strings.
- localStorage keys are additive; absent → defaults. No migration.
- No relay/protocol changes. No desktop user-visible change (aux bridge is
  inert there).
- CI: web/desktop frontend tests cover the TS; iOS native (Camera perms,
  plugin registration) is device-verified since CI doesn't build iOS.

## 9. Open questions (resolve in review)

1. **Logout semantics** (§4): preserve config vs true wipe. Spec assumes
   preserve.
2. **Desktop auxKeys bridge** (§3): config.json pair (symmetric) vs no-op
   `[]`. Spec leans symmetric.
3. **Live refresh**: editing in settings then returning to an already-open
   terminal — spec relies on remount-on-return. If a terminal stays
   mounted (it does, via `v-show`), the bar won't pick up edits until the
   next mount. Acceptable for v1, or add a focus-time reload. Spec leans
   focus-time reload of both bars to avoid a stale bar surprise.
