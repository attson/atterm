# I18n English Chinese Design

## Goal

Add full UI internationalization for AT Term's desktop, mobile, and browser clients, with English and Simplified Chinese available everywhere and the default language following the user's system/browser language.

## Scope

- Cover all current user-visible UI strings in the desktop Wails frontend, Capacitor/mobile frontend, and browser web client.
- Support three persisted language preferences: `system`, `en`, and `zh-CN`.
- Default fresh installs and fresh browser profiles to `system`.
- Persist the language preference locally per device/client, not in relay account state.
- Keep terminal/PTY bytes, user content, protocol fields, logs, API machine codes, and developer-only text untranslated.
- Do not add new frontend dependencies.

## Locale Model

`system` is a preference, not a render locale. The i18n runtime resolves it to one of the supported render locales:

- `zh-CN` when the first available system/browser language is Chinese, including `zh`, `zh-CN`, `zh-Hans`, and `zh-SG` style tags.
- `en` for all other languages and as the fallback for malformed or unavailable language information.

Explicit preferences `en` and `zh-CN` render exactly that locale. When the preference is `system`, clients listen for the browser `languagechange` event and recompute the resolved locale. Explicit preferences ignore system language changes.

The runtime exposes these types:

```ts
export type LocalePreference = "system" | "en" | "zh-CN"
export type ResolvedLocale = "en" | "zh-CN"
```

## Persistence

Language preference is local to each client.

Desktop Wails:

- Add `LocalePreference string json:"locale_preference,omitempty"` to `desktop/appConfig`.
- Add `LocalePreferenceOrDefault()` that returns `system` unless the stored value is `en` or `zh-CN`.
- Expose get/set bindings from `desktop/app.go` and wrap them in `desktop/frontend/src/lib/api.ts`.
- Store the value in the existing `~/.config/atterm/config.json` file.

Desktop mobile/Capacitor:

- Store the preference through the frontend local storage path used by the mobile shell. This keeps mobile language independent from the desktop config even when source files are shared.
- The mobile platform adapter does not need relay/userstore changes.

Browser web/PWA:

- Store the preference in localStorage under a dedicated key, for example `atterm.locale`.
- The setting applies across the browser client's multiple entrypoints (`login`, `signup`, `setup`, `main`, `settings`, and `admin`) in the same origin.

## Runtime Architecture

Use a small in-repo i18n runtime instead of `vue-i18n`.

Desktop/mobile frontend files:

- `desktop/frontend/src/i18n/messages/en.ts`
- `desktop/frontend/src/i18n/messages/zh-CN.ts`
- `desktop/frontend/src/i18n/index.ts`
- `desktop/frontend/src/i18n/useI18n.ts`

Browser web files:

- `web/src/shared/i18n/messages/en.ts`
- `web/src/shared/i18n/messages/zh-CN.ts`
- `web/src/shared/i18n/index.ts`
- `web/src/shared/i18n/useI18n.ts`

Each runtime provides:

- `initI18n(loadPreference, savePreference?)` for entrypoint startup.
- `t(key, params?)` for translating string keys.
- `localePreference`, `resolvedLocale`, and `setLocalePreference()` as reactive state.
- `languageOptions()` or shared constants for language picker labels.
- `resolveLocalePreference(preference, languages)` as a pure, unit-tested helper.

The implementation can be duplicated between the two packages initially, because desktop/mobile and web have separate package roots and build pipelines. Shared behavior is kept aligned by using the same type names, key naming rules, and tests in both packages.

## Dictionary Structure

English is the source dictionary and Simplified Chinese must match its structure.

```ts
export const en = {
  common: {
    save: "Save",
    cancel: "Cancel",
  },
  settings: {
    title: "Settings",
  },
} as const

export const zhCN = {
  common: {
    save: "保存",
    cancel: "取消",
  },
  settings: {
    title: "设置",
  },
} satisfies typeof en
```

Keys are grouped by feature/page, for example:

- `common`
- `auth`
- `setup`
- `topbar`
- `terminal`
- `sessions`
- `settings.general`
- `settings.relay`
- `settings.updates`
- `settings.shortcuts`
- `settings.webhooks`
- `admin.users`
- `admin.invitations`
- `admin.config`
- `mobile`
- `plugins.quickInput`
- `plugins.fileExplorer`
- `plugins.translate`

Interpolation uses simple named placeholders: `"Signed out {count} other devices."`. The runtime replaces `{name}` with stringified parameter values. If a parameter is missing, the placeholder remains visible so missing data is obvious during testing.

## UI Integration

Desktop settings:

- Add a language selector to `SettingsGeneral.vue`.
- Options are `System`, `English`, and `简体中文` in English UI, and `跟随系统`, `English`, `简体中文` in Chinese UI.
- Changing language applies immediately and persists through the Wails binding.

Mobile:

- Add the same preference to the mobile setup/settings surface. The current mobile shell has setup and session-list surfaces; the language selector should be reachable before a valid relay token exists so users can switch language during first setup.

Browser web:

- Add language selection to the settings page.
- Add a compact language selector to unauthenticated entrypoints (`login`, `signup`, and `setup`) so users can switch before sign-in or relay setup.
- The existing top-level pages initialize i18n before mount so initial render uses the persisted or system-resolved language.

All visible strings move behind `t()`, including:

- Text nodes, button labels, tab labels, headings, empty states, warnings, and hints.
- Form labels, placeholders, feedback, confirmation text, and aria labels.
- Toast/message strings and client-side validation errors.
- Plugin UI strings under `desktop/frontend/src/plugins/`.

## Error Handling

The runtime falls back safely:

- Unknown stored preference resolves to `system` and is overwritten only when the user saves a new value.
- Missing translation keys return the English value when available; if not available, return the key string.
- Failed desktop get/set binding calls surface the existing error handling in Settings and keep the previous in-memory preference.
- Browser localStorage errors are ignored so private/restricted contexts still render using `system`.

API/server error codes are not translated generically. UI code maps known codes to translated user-facing messages where it already does that today; unknown errors keep their raw message for diagnosis.

## Testing

Desktop/mobile frontend:

- Unit-test locale resolution, storage adapter behavior, interpolation, and fallback behavior.
- Component-test `SettingsGeneral.vue` language selector persistence and immediate rerender.
- Component-test mobile setup language selector availability before relay connection.
- Build with `cd desktop/frontend && npm run build`.

Browser web:

- Unit-test locale resolution, localStorage persistence, interpolation, and fallback behavior.
- Component-test settings language selector and at least one unauthenticated language selector.
- Build with `cd web && npm run build`.

Go desktop:

- Unit-test config defaulting and validation for `LocalePreference` if existing config tests provide a nearby pattern.
- Run `go test -tags webkit2_41 ./desktop/` if Go bindings/config code changes are included in the implementation branch.

Manual verification:

- Fresh profile starts in Chinese when browser/system language is Chinese, otherwise English.
- Explicit English and Simplified Chinese selections override system language.
- Desktop, mobile, and browser choices persist independently.
- Terminal output and PTY input remain untouched.
