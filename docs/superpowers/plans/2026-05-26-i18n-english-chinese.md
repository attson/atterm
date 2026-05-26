# I18n English Chinese Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full English/Simplified Chinese UI internationalization to the desktop Wails frontend, the Capacitor/mobile frontend, and the browser web client, defaulting to system language with per-client local persistence.

**Architecture:** Implement a tiny in-repo i18n runtime in each frontend package (`desktop/frontend/src/i18n` and `web/src/shared/i18n`) with typed message dictionaries, reactive `localePreference`/`resolvedLocale`, named interpolation, safe fallbacks, and `languagechange` handling. Desktop Wails persists preference through existing Go config and bindings; mobile and browser persist through localStorage. UI integration then converts every visible string to `t()` and verifies coverage through tests, builds, and literal-string audits.

**Tech Stack:** Go config/Wails bindings, Vue 3 Composition API, TypeScript strict mode, Vitest, @vue/test-utils, Vite, no new frontend dependencies.

**Spec:** `docs/superpowers/specs/2026-05-26-i18n-english-chinese-design.md`

---

## Grounded Facts

- Desktop Wails settings live in `desktop/config.go` and are exposed through methods on `desktop/app.go`; frontend wrappers live in `desktop/frontend/src/lib/api.ts`.
- Desktop and Capacitor share `desktop/frontend/src`, but entrypoints differ: `desktop/frontend/src/main.ts` mounts `App.vue`; `desktop/frontend/src/main.capacitor.ts` mounts `mobile/MobileApp.vue`.
- Capacitor/mobile already uses localStorage for relay config in `desktop/frontend/src/platform/capacitor.ts` under key `atterm.relay`.
- Browser web has separate Vue entrypoints: `web/src/login/main.ts`, `web/src/signup/main.ts`, `web/src/setup/main.ts`, `web/src/main/main.ts`, `web/src/settings/main.ts`, and `web/src/admin/main.ts`.
- Browser settings currently chooses mobile tabs with `isMobileApp()` in `web/src/settings/App.vue`; adding a language tab/section must not hide relay configuration for mobile app settings.
- Existing tests often combine source-string checks (`*.vue?raw`) with targeted component tests. New i18n runtime behavior should be tested with executable Vitest tests, not only source checks.
- Do not touch the unrelated `.gitignore` modification unless the user explicitly asks.

## File Structure

### Desktop Wails Go

- Modify `desktop/config.go` — store, validate, and default `LocalePreference`.
- Modify `desktop/app.go` — expose `GetLocalePreference()` and `SetLocalePreference(preference string)`.
- Modify or create `desktop/config_test.go` — verify default and validation behavior if no existing test file covers config defaults.

### Desktop/Mobile Frontend

- Create `desktop/frontend/src/i18n/messages/en.ts` — English source dictionary.
- Create `desktop/frontend/src/i18n/messages/zh-CN.ts` — Simplified Chinese dictionary with `satisfies typeof en`.
- Create `desktop/frontend/src/i18n/index.ts` — runtime state, locale resolution, interpolation, persistence initialization, fallback logic.
- Create `desktop/frontend/src/i18n/useI18n.ts` — Vue composable returning `t`, locale refs, and language options.
- Create `desktop/frontend/src/i18n/i18n.test.ts` — runtime tests.
- Modify `desktop/frontend/src/main.ts` — initialize i18n with Wails persistence before mount.
- Modify `desktop/frontend/src/main.capacitor.ts` — initialize i18n with localStorage persistence before mount.
- Modify `desktop/frontend/src/lib/api.ts` — add Wails binding interface/wrapper for locale preference.
- Modify `desktop/frontend/src/components/SettingsGeneral.vue` and `desktop/frontend/src/components/SettingsGeneral.test.ts` — add language selector.
- Modify all user-facing desktop/mobile Vue files in `desktop/frontend/src/App.vue`, `desktop/frontend/src/components/`, `desktop/frontend/src/mobile/`, and `desktop/frontend/src/plugins/` — replace visible strings with dictionary keys.

### Browser Web Frontend

- Create `web/src/shared/i18n/messages/en.ts` — English source dictionary for browser client.
- Create `web/src/shared/i18n/messages/zh-CN.ts` — Simplified Chinese dictionary with `satisfies typeof en`.
- Create `web/src/shared/i18n/index.ts` — browser runtime state, localStorage persistence, locale resolution, interpolation, fallback logic.
- Create `web/src/shared/i18n/useI18n.ts` — Vue composable.
- Create `web/src/shared/i18n/i18n.test.ts` — runtime tests.
- Modify all six `web/src/*/main.ts` entrypoints — call `initI18n()` before mounting.
- Create `web/src/shared/components/LanguageSelect.vue` — compact language selector reused by unauthenticated entrypoints and settings.
- Modify `web/src/login/App.vue`, `web/src/signup/App.vue`, and `web/src/setup/App.vue` — add pre-auth language selector and translate strings.
- Modify `web/src/settings/App.vue` plus all `web/src/settings/tabs/*.vue` — add language settings and translate strings.
- Modify `web/src/admin/App.vue` and `web/src/admin/tabs/*.vue` — translate admin UI strings.
- Modify `web/src/main/App.vue` and `web/src/main/components/*.vue` — translate session/terminal UI strings.
- Modify `web/src/shared/components/Topbar.vue` — translate navigation and sign-out strings.

---

### Task 1: Desktop Go locale preference config and bindings

**Files:**
- Modify: `desktop/config.go`
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/src/lib/api.ts`
- Test: `desktop/config_test.go`

- [ ] **Step 1: Write failing Go config tests**

Create `desktop/config_test.go` if it does not exist, or append these tests if it exists:

```go
package main

import "testing"

func TestLocalePreferenceOrDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  appConfig
		want string
	}{
		{name: "empty defaults to system", cfg: appConfig{}, want: localePreferenceSystem},
		{name: "system allowed", cfg: appConfig{LocalePreference: localePreferenceSystem}, want: localePreferenceSystem},
		{name: "english allowed", cfg: appConfig{LocalePreference: localePreferenceEnglish}, want: localePreferenceEnglish},
		{name: "simplified chinese allowed", cfg: appConfig{LocalePreference: localePreferenceChineseSimplified}, want: localePreferenceChineseSimplified},
		{name: "unknown falls back to system", cfg: appConfig{LocalePreference: "fr"}, want: localePreferenceSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.LocalePreferenceOrDefault(); got != tt.want {
				t.Fatalf("LocalePreferenceOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -tags webkit2_41 ./desktop/ -run TestLocalePreferenceOrDefault -count=1
```

Expected: FAIL with undefined `LocalePreferenceOrDefault` or undefined locale constants.

- [ ] **Step 3: Implement config constants, field, and default helper**

In `desktop/config.go`, add constants near the existing config constants:

```go
const (
	localePreferenceSystem            = "system"
	localePreferenceEnglish           = "en"
	localePreferenceChineseSimplified = "zh-CN"
)
```

Add the persisted field to `appConfig` near other user-facing preferences:

```go
	// LocalePreference controls UI language. Empty means "system" so older
	// configs keep following the OS/browser language after upgrade.
	LocalePreference string `json:"locale_preference,omitempty"`
```

Add this method near the other `appConfig` default helpers:

```go
func (c appConfig) LocalePreferenceOrDefault() string {
	switch c.LocalePreference {
	case localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified:
		return c.LocalePreference
	default:
		return localePreferenceSystem
	}
}
```

- [ ] **Step 4: Implement Wails app bindings**

In `desktop/app.go`, add these methods near `GetTerminalTheme`/`SetTerminalTheme`:

```go
// GetLocalePreference returns the user's persisted UI language preference.
func (a *App) GetLocalePreference() string {
	if a == nil || a.cfgStore == nil {
		return localePreferenceSystem
	}
	return a.cfgStore.Get().LocalePreferenceOrDefault()
}

// SetLocalePreference persists the user's UI language preference.
func (a *App) SetLocalePreference(preference string) error {
	if a == nil || a.cfgStore == nil {
		return errors.New("app not initialized")
	}
	cfg := a.cfgStore.Get()
	switch preference {
	case localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified:
		cfg.LocalePreference = preference
	default:
		return errors.New("unsupported locale preference")
	}
	return a.cfgStore.Set(cfg)
}
```

`desktop/app.go` already imports `errors`; if an implementation branch has changed that, add `errors` to the import list.

- [ ] **Step 5: Add TypeScript API wrappers**

In `desktop/frontend/src/lib/api.ts`, add:

```ts
export type LocalePreference = "system" | "en" | "zh-CN";
```

Extend the `AppBindings` interface with:

```ts
  GetLocalePreference(): Promise<LocalePreference>;
  SetLocalePreference(preference: LocalePreference): Promise<void>;
```

Add wrappers near other preference wrappers:

```ts
export function getLocalePreference(): Promise<LocalePreference> {
  return bindings().GetLocalePreference();
}

export function setLocalePreference(preference: LocalePreference): Promise<void> {
  return bindings().SetLocalePreference(preference);
}
```

- [ ] **Step 6: Run tests**

```bash
go test -tags webkit2_41 ./desktop/ -run TestLocalePreferenceOrDefault -count=1
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: PASS for Go test and typecheck.

- [ ] **Step 7: Commit**

```bash
git add desktop/config.go desktop/app.go desktop/config_test.go desktop/frontend/src/lib/api.ts
git commit -m "add desktop locale preference binding"
```

---

### Task 2: Desktop/mobile i18n runtime

**Files:**
- Create: `desktop/frontend/src/i18n/messages/en.ts`
- Create: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Create: `desktop/frontend/src/i18n/index.ts`
- Create: `desktop/frontend/src/i18n/useI18n.ts`
- Create: `desktop/frontend/src/i18n/i18n.test.ts`

- [ ] **Step 1: Write failing runtime tests**

Create `desktop/frontend/src/i18n/i18n.test.ts`:

```ts
import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  initI18n,
  localePreference,
  resolvedLocale,
  resolveLocalePreference,
  setLocalePreference,
  t,
  resetI18nForTest,
} from "./index";

beforeEach(() => {
  resetI18nForTest();
});

describe("desktop i18n locale resolution", () => {
  test("resolves Chinese system tags to zh-CN", () => {
    expect(resolveLocalePreference("system", ["zh-CN", "en-US"])).toBe("zh-CN");
    expect(resolveLocalePreference("system", ["zh-Hans-US"])).toBe("zh-CN");
    expect(resolveLocalePreference("system", ["zh"])).toBe("zh-CN");
  });

  test("resolves non-Chinese system tags to en", () => {
    expect(resolveLocalePreference("system", ["ja-JP", "en-US"])).toBe("en");
    expect(resolveLocalePreference("system", [])).toBe("en");
    expect(resolveLocalePreference("system", [""])).toBe("en");
  });

  test("explicit preference overrides system languages", () => {
    expect(resolveLocalePreference("en", ["zh-CN"])).toBe("en");
    expect(resolveLocalePreference("zh-CN", ["en-US"])).toBe("zh-CN");
  });
});

describe("desktop i18n runtime", () => {
  test("initializes from loader and persists setLocalePreference", async () => {
    const save = vi.fn<[("system" | "en" | "zh-CN")], Promise<void>>().mockResolvedValue(undefined);
    await initI18n({
      loadPreference: async () => "zh-CN",
      savePreference: save,
      getLanguages: () => ["en-US"],
      listenLanguageChange: () => () => undefined,
    });
    expect(localePreference.value).toBe("zh-CN");
    expect(resolvedLocale.value).toBe("zh-CN");

    await setLocalePreference("en");
    expect(localePreference.value).toBe("en");
    expect(resolvedLocale.value).toBe("en");
    expect(save).toHaveBeenCalledWith("en");
  });

  test("falls back to system for invalid loaded preference", async () => {
    await initI18n({
      loadPreference: async () => "fr" as "en",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });
    expect(localePreference.value).toBe("system");
    expect(resolvedLocale.value).toBe("zh-CN");
  });

  test("interpolates named parameters and leaves missing parameters visible", async () => {
    await initI18n({ getLanguages: () => ["en-US"], listenLanguageChange: () => () => undefined });
    expect(t("test.interpolated", { count: 3 })).toBe("3 sessions");
    expect(t("test.interpolated")).toBe("{count} sessions");
  });

  test("returns the key when no locale contains a translation", async () => {
    await initI18n({ getLanguages: () => ["zh-CN"], listenLanguageChange: () => () => undefined });
    expect(t("test.missing" as never)).toBe("test.missing");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/i18n/i18n.test.ts
```

Expected: FAIL because `src/i18n/index.ts` does not exist.

- [ ] **Step 3: Add starter dictionaries**

Create `desktop/frontend/src/i18n/messages/en.ts`:

```ts
export const en = {
  common: {
    language: "Language",
    system: "System",
    english: "English",
    simplifiedChinese: "Simplified Chinese",
    save: "Save",
    cancel: "Cancel",
    close: "Close",
    refresh: "Refresh",
    copy: "Copy",
    paste: "Paste",
    send: "Send",
    disconnect: "Disconnect",
    connect: "Connect",
    settings: "Settings",
  },
  settings: {
    general: {
      languageLabel: "Language",
      languageHint: "Default follows your system language. This setting is stored on this device.",
    },
  },
  test: {
    interpolated: "{count} sessions",
    englishOnly: "English fallback",
  },
} as const;

export type Messages = typeof en;
```

Create `desktop/frontend/src/i18n/messages/zh-CN.ts`:

```ts
import type { Messages } from "./en";

export const zhCN = {
  common: {
    language: "语言",
    system: "跟随系统",
    english: "English",
    simplifiedChinese: "简体中文",
    save: "保存",
    cancel: "取消",
    close: "关闭",
    refresh: "刷新",
    copy: "复制",
    paste: "粘贴",
    send: "发送",
    disconnect: "断开连接",
    connect: "连接",
    settings: "设置",
  },
  settings: {
    general: {
      languageLabel: "语言",
      languageHint: "默认跟随系统语言。此设置仅保存在本设备。",
    },
  },
  test: {
    interpolated: "{count} 个会话",
    englishOnly: "英文兜底",
  },
} satisfies Messages;
```

- [ ] **Step 4: Implement runtime**

Create `desktop/frontend/src/i18n/index.ts`:

```ts
import { computed, ref, type ComputedRef, type Ref } from "vue";
import { en, type Messages } from "./messages/en";
import { zhCN } from "./messages/zh-CN";

export type LocalePreference = "system" | "en" | "zh-CN";
export type ResolvedLocale = "en" | "zh-CN";
export type TranslationParams = Record<string, string | number | boolean | null | undefined>;
export type MessageKey = LeafKeys<Messages>;

type LeafKeys<T, Prefix extends string = ""> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : T[K] extends Record<string, unknown>
      ? LeafKeys<T[K], `${Prefix}${K}.`>
      : never;
}[keyof T & string];

interface I18nOptions {
  loadPreference?: () => Promise<LocalePreference | string | null | undefined>;
  savePreference?: (preference: LocalePreference) => Promise<void> | void;
  getLanguages?: () => readonly string[];
  listenLanguageChange?: (handler: () => void) => () => void;
}

const messages = { en, "zh-CN": zhCN } as const;
let savePreference: I18nOptions["savePreference"];
let getLanguages: () => readonly string[] = defaultLanguages;
let removeLanguageListener: (() => void) | undefined;

export const localePreference: Ref<LocalePreference> = ref("system");
export const resolvedLocale: ComputedRef<ResolvedLocale> = computed(() =>
  resolveLocalePreference(localePreference.value, getLanguages()),
);

export const languageOptions = computed(() => [
  { value: "system" as const, label: t("common.system") },
  { value: "en" as const, label: t("common.english") },
  { value: "zh-CN" as const, label: t("common.simplifiedChinese") },
]);

export async function initI18n(options: I18nOptions = {}): Promise<void> {
  removeLanguageListener?.();
  savePreference = options.savePreference;
  getLanguages = options.getLanguages ?? defaultLanguages;
  const loaded = await options.loadPreference?.().catch(() => null);
  localePreference.value = normalizePreference(loaded);
  removeLanguageListener = (options.listenLanguageChange ?? defaultListenLanguageChange)(() => {
    if (localePreference.value === "system") {
      localePreference.value = "system";
    }
  });
}

export async function setLocalePreference(preference: LocalePreference): Promise<void> {
  const normalized = normalizePreference(preference);
  const previous = localePreference.value;
  localePreference.value = normalized;
  try {
    await savePreference?.(normalized);
  } catch (error) {
    localePreference.value = previous;
    throw error;
  }
}

export function resolveLocalePreference(preference: LocalePreference, languages: readonly string[]): ResolvedLocale {
  if (preference === "en" || preference === "zh-CN") return preference;
  const first = languages.find((lang) => lang.trim() !== "")?.toLowerCase() ?? "";
  return first === "zh" || first.startsWith("zh-") ? "zh-CN" : "en";
}

export function t(key: MessageKey, params: TranslationParams = {}): string {
  const value = lookup(messages[resolvedLocale.value], key) ?? lookup(en, key) ?? key;
  return interpolate(value, params);
}

export function resetI18nForTest(): void {
  removeLanguageListener?.();
  removeLanguageListener = undefined;
  savePreference = undefined;
  getLanguages = defaultLanguages;
  localePreference.value = "system";
}

function normalizePreference(value: LocalePreference | string | null | undefined): LocalePreference {
  return value === "en" || value === "zh-CN" || value === "system" ? value : "system";
}

function lookup(source: unknown, key: string): string | undefined {
  let current: unknown = source;
  for (const part of key.split(".")) {
    if (!current || typeof current !== "object" || !(part in current)) return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return typeof current === "string" ? current : undefined;
}

function interpolate(template: string, params: TranslationParams): string {
  return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (match, name: string) => {
    const value = params[name];
    return value === undefined || value === null ? match : String(value);
  });
}

function defaultLanguages(): readonly string[] {
  if (typeof navigator === "undefined") return [];
  return navigator.languages?.length ? navigator.languages : [navigator.language].filter(Boolean);
}

function defaultListenLanguageChange(handler: () => void): () => void {
  if (typeof window === "undefined") return () => undefined;
  window.addEventListener("languagechange", handler);
  return () => window.removeEventListener("languagechange", handler);
}
```

- [ ] **Step 5: Add composable**

Create `desktop/frontend/src/i18n/useI18n.ts`:

```ts
import { languageOptions, localePreference, resolvedLocale, setLocalePreference, t } from "./index";

export function useI18n() {
  return {
    t,
    languageOptions,
    localePreference,
    resolvedLocale,
    setLocalePreference,
  };
}
```

- [ ] **Step 6: Run tests and typecheck**

```bash
cd desktop/frontend && npx vitest run src/i18n/i18n.test.ts
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/i18n
git commit -m "add desktop i18n runtime"
```

---

### Task 3: Desktop/mobile i18n initialization and settings selector

**Files:**
- Modify: `desktop/frontend/src/main.ts`
- Modify: `desktop/frontend/src/main.capacitor.ts`
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/components/SettingsGeneral.test.ts`
- Modify: `desktop/frontend/src/mobile/MobileSetup.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts`

- [ ] **Step 1: Add failing source tests for settings selector**

Append to `desktop/frontend/src/components/SettingsGeneral.test.ts`:

```ts
describe("SettingsGeneral language preference", () => {
  test("imports i18n and locale preference bindings", () => {
    expect(source).toContain("useI18n");
    expect(source).toContain("getLocalePreference");
    expect(source).toContain("setLocalePreference");
  });

  test("renders language selector with translated label and hint", () => {
    expect(source).toContain("settings.general.languageLabel");
    expect(source).toContain("settings.general.languageHint");
    expect(source).toContain("languageOptions");
  });
});
```

- [ ] **Step 2: Add failing mobile setup test**

Append to `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts`:

```ts
it("renders a language selector before relay connection", () => {
  const wrapper = mount(MobileSetup, { props: { reason: null } });
  expect(wrapper.text()).toContain("Language");
  expect(wrapper.find('[data-testid="mobile-language"]').exists()).toBe(true);
});
```

If the file does not already import `mount`, `MobileSetup`, and platform test setup, follow the imports already used in that test file and append only the `it(...)` block.

- [ ] **Step 3: Run tests to verify failures**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsGeneral.test.ts src/mobile/__tests__/MobileSetup.test.ts
```

Expected: FAIL because the new imports/selectors are not present.

- [ ] **Step 4: Initialize i18n in Wails entrypoint**

Change `desktop/frontend/src/main.ts` to initialize i18n before mount:

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
import { getLocalePreference, setLocalePreference } from './lib/api'
import { initPlatform } from './platform'
import { createWailsPlatform } from './platform/wails'
import './style.css'

async function bootstrap() {
  await initI18n({
    loadPreference: getLocalePreference,
    savePreference: setLocalePreference,
  })

  const platform = initPlatform(createWailsPlatform)
  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
```

- [ ] **Step 5: Initialize i18n in Capacitor entrypoint**

Change `desktop/frontend/src/main.capacitor.ts` to use localStorage:

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import MobileApp from './mobile/MobileApp.vue'
import { initI18n, type LocalePreference } from './i18n'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const LOCALE_STORAGE_KEY = 'atterm.locale'

async function bootstrap() {
  await initI18n({
    loadPreference: async () => localStorage.getItem(LOCALE_STORAGE_KEY) as LocalePreference | null,
    savePreference: async (preference) => localStorage.setItem(LOCALE_STORAGE_KEY, preference),
  })

  const platform = initPlatform(createCapacitorPlatform)
  const app = createApp(MobileApp)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
```

If `localStorage` can be unavailable in a test runtime, wrap `localStorage.getItem`/`setItem` in `typeof localStorage === 'undefined'` guards.

- [ ] **Step 6: Add desktop SettingsGeneral language selector**

In `desktop/frontend/src/components/SettingsGeneral.vue`, import i18n and locale API:

```ts
import { getLocalePreference, setLocalePreference, type LocalePreference } from "../lib/api";
import { useI18n } from "../i18n/useI18n";
```

If `getLocalePreference` import conflicts with the existing grouped import from `../lib/api`, add the new names to that grouped import instead of creating a second import.

Add state in `<script setup>`:

```ts
const { t, languageOptions, setLocalePreference: setRuntimeLocalePreference } = useI18n();
const selectedLocale = ref<LocalePreference>("system");
const persistedLocale = ref<LocalePreference>("system");
const localeSaving = ref(false);
```

In `onMounted`, add a loading block:

```ts
  try {
    const preference = await getLocalePreference();
    selectedLocale.value = preference;
    persistedLocale.value = preference;
    await setRuntimeLocalePreference(preference);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
```

Add change handler:

```ts
async function onLocaleChange() {
  const previous = persistedLocale.value;
  const next = selectedLocale.value;
  localeSaving.value = true;
  error.value = "";
  try {
    await setLocalePreference(next);
    await setRuntimeLocalePreference(next);
    persistedLocale.value = next;
  } catch (e: any) {
    selectedLocale.value = previous;
    await setRuntimeLocalePreference(previous);
    error.value = e?.message ?? String(e);
  } finally {
    localeSaving.value = false;
  }
}
```

Add template block near the top of general settings:

```vue
<section class="setting-section">
  <label class="setting-label" for="locale-preference">{{ t("settings.general.languageLabel") }}</label>
  <SelectDropdown
    id="locale-preference"
    v-model="selectedLocale"
    :options="languageOptions"
    :disabled="localeSaving"
    data-testid="language-select"
    @update:modelValue="onLocaleChange"
  />
  <p class="setting-hint">{{ t("settings.general.languageHint") }}</p>
</section>
```

Adjust class names to the existing `SettingsGeneral.vue` structure if it uses different classes; keep `data-testid="language-select"`.

- [ ] **Step 7: Add mobile setup language selector**

In `desktop/frontend/src/mobile/MobileSetup.vue`, import and use i18n:

```ts
import { useI18n } from '../i18n/useI18n'

const { t, languageOptions, localePreference, setLocalePreference } = useI18n()
```

Add template block above the relay URL field:

```vue
<label class="field">
  <span>{{ t('common.language') }}</span>
  <select
    data-testid="mobile-language"
    :value="localePreference"
    :disabled="submitting"
    @change="setLocalePreference(($event.target as HTMLSelectElement).value as 'system' | 'en' | 'zh-CN')"
  >
    <option v-for="option in languageOptions" :key="option.value" :value="option.value">
      {{ option.label }}
    </option>
  </select>
</label>
```

Add CSS so the select matches inputs:

```css
.field select { width: 100%; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; }
```

- [ ] **Step 8: Run targeted tests**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsGeneral.test.ts src/mobile/__tests__/MobileSetup.test.ts src/i18n/i18n.test.ts
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/main.ts desktop/frontend/src/main.capacitor.ts desktop/frontend/src/components/SettingsGeneral.vue desktop/frontend/src/components/SettingsGeneral.test.ts desktop/frontend/src/mobile/MobileSetup.vue desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts
git commit -m "wire desktop language preference UI"
```

---

### Task 4: Desktop/mobile full string migration

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Modify: all user-facing Vue files in `desktop/frontend/src/App.vue`, `desktop/frontend/src/components/*.vue`, `desktop/frontend/src/mobile/*.vue`, `desktop/frontend/src/plugins/**/*.vue`, and user-facing message helpers in `desktop/frontend/src/lib/*.ts` and `desktop/frontend/src/plugins/**/*.ts` where they produce UI text.
- Test: existing desktop/frontend tests affected by migrated source strings.

- [ ] **Step 1: Capture current user-visible string inventory**

Run:

```bash
cd desktop/frontend
rg -n '>[A-Za-z][^<]{1,}<|aria-label="[^"]+"|title="[^"]+"|placeholder="[^"]+"|\b(alert|confirm)\(|message\.(success|error|warning|info)\(|error\.value\s*=\s*"|warning\.value\s*=\s*"|return\s+"[A-Za-z]' src/App.vue src/components src/mobile src/plugins src/lib > /tmp/atterm-desktop-i18n-before.txt
wc -l /tmp/atterm-desktop-i18n-before.txt
```

Expected: command succeeds and writes the migration inventory. Review `/tmp/atterm-desktop-i18n-before.txt` before editing so strings from every directory are covered.

- [ ] **Step 2: Expand desktop English dictionary by feature**

Edit `desktop/frontend/src/i18n/messages/en.ts` so it contains complete groups for desktop/mobile/plugin UI. Use this shape and add keys discovered from the inventory:

```ts
export const en = {
  common: {
    language: "Language",
    system: "System",
    english: "English",
    simplifiedChinese: "Simplified Chinese",
    save: "Save",
    cancel: "Cancel",
    close: "Close",
    refresh: "Refresh",
    copy: "Copy",
    paste: "Paste",
    send: "Send",
    disconnect: "Disconnect",
    connect: "Connect",
    settings: "Settings",
    loading: "Loading...",
    enabled: "Enabled",
    disabled: "Disabled",
  },
  app: {},
  terminal: {},
  sessions: {},
  settings: {
    title: "Settings",
    tabs: {},
    general: {
      languageLabel: "Language",
      languageHint: "Default follows your system language. This setting is stored on this device.",
    },
    relay: {},
    logging: {},
    updates: {},
    plugins: {},
    shortcuts: {},
  },
  mobile: {},
  plugins: {
    quickInput: {},
    fileExplorer: {},
    translate: {},
  },
} as const;

export type Messages = typeof en;
```

Populate the empty objects with concrete keys and English values from `/tmp/atterm-desktop-i18n-before.txt`. Use stable semantic names, for example `settings.relay.allowInsecureLabel`, not names tied to line numbers.

- [ ] **Step 3: Expand Simplified Chinese dictionary**

Edit `desktop/frontend/src/i18n/messages/zh-CN.ts` so it satisfies the full English dictionary:

```ts
import type { Messages } from "./en";

export const zhCN = {
  common: {
    language: "语言",
    system: "跟随系统",
    english: "English",
    simplifiedChinese: "简体中文",
    save: "保存",
    cancel: "取消",
    close: "关闭",
    refresh: "刷新",
    copy: "复制",
    paste: "粘贴",
    send: "发送",
    disconnect: "断开连接",
    connect: "连接",
    settings: "设置",
    loading: "加载中...",
    enabled: "已启用",
    disabled: "已禁用",
  },
  app: {},
  terminal: {},
  sessions: {},
  settings: {
    title: "设置",
    tabs: {},
    general: {
      languageLabel: "语言",
      languageHint: "默认跟随系统语言。此设置仅保存在本设备。",
    },
    relay: {},
    logging: {},
    updates: {},
    plugins: {},
    shortcuts: {},
  },
  mobile: {},
  plugins: {
    quickInput: {},
    fileExplorer: {},
    translate: {},
  },
} satisfies Messages;
```

Replace the empty objects with Chinese translations for every English key. - [ ] **Step 4: Migrate desktop shell components**

For each file below, add `const { t } = useI18n();` in `<script setup>` and replace visible strings in the template/script with `t(...)`:

```text
desktop/frontend/src/App.vue
desktop/frontend/src/components/ConfirmInstallDialog.vue
desktop/frontend/src/components/ConfirmQuitDialog.vue
desktop/frontend/src/components/HotkeyCaptureCell.vue
desktop/frontend/src/components/LogViewerDialog.vue
desktop/frontend/src/components/PaneGrid.vue
desktop/frontend/src/components/RemoteSessionsDialog.vue
desktop/frontend/src/components/SelectDropdown.vue
desktop/frontend/src/components/SessionPickerDialog.vue
desktop/frontend/src/components/SettingsDialog.vue
desktop/frontend/src/components/SettingsGeneral.vue
desktop/frontend/src/components/SettingsLogging.vue
desktop/frontend/src/components/SettingsPlugins.vue
desktop/frontend/src/components/SettingsRelay.vue
desktop/frontend/src/components/SettingsShortcuts.vue
desktop/frontend/src/components/SettingsUpdates.vue
desktop/frontend/src/components/ShortcutHints.vue
desktop/frontend/src/components/TabBar.vue
desktop/frontend/src/components/TerminalView.vue
desktop/frontend/src/components/TitleBar.vue
desktop/frontend/src/components/WindowControls.vue
```

Use this pattern for dynamic messages:

```ts
error.value = t("settings.relay.invalidToken")
statusText.value = t("terminal.replayProgress", { percent: pct() })
```

Use this pattern for attributes:

```vue
<button :aria-label="t('terminal.backToSessions')">...</button>
<input :placeholder="t('settings.relay.urlPlaceholder')" />
```

- [ ] **Step 5: Migrate mobile components**

For each file below, add `useI18n()` and replace visible strings with keys under `mobile`, `sessions`, `terminal`, or `settings.relay`:

```text
desktop/frontend/src/mobile/MobileApp.vue
desktop/frontend/src/mobile/MobileSessionList.vue
desktop/frontend/src/mobile/MobileSetup.vue
desktop/frontend/src/mobile/MobileTerminal.vue
desktop/frontend/src/mobile/MobileTerminalHost.vue
```

Keep protocol values unchanged, for example `remote_permission: 'full'`, view names, and test IDs.

- [ ] **Step 6: Migrate plugin UI**

For each file below, add `useI18n()` and replace visible strings with plugin keys:

```text
desktop/frontend/src/plugins/PluginHost.vue
desktop/frontend/src/plugins/fileExplorer/FileEditor.vue
desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
desktop/frontend/src/plugins/fileExplorer/FileTabs.vue
desktop/frontend/src/plugins/fileExplorer/FileTree.vue
desktop/frontend/src/plugins/fileExplorer/FileTreeNode.vue
desktop/frontend/src/plugins/quickInput/QuickInputBar.vue
desktop/frontend/src/plugins/quickInput/QuickInputSettings.vue
desktop/frontend/src/plugins/translate/TranslatePanel.vue
desktop/frontend/src/plugins/translate/TranslatePanelHost.vue
desktop/frontend/src/plugins/translate/TranslateSettings.vue
```

For TypeScript files that return labels or validation messages, import `t` directly from `../i18n` or pass labels from Vue components instead of hard-coding English:

```text
desktop/frontend/src/plugins/quickInput/defaults.ts
desktop/frontend/src/plugins/quickInput/hotkeyConflict.ts
desktop/frontend/src/plugins/translate/index.ts
desktop/frontend/src/plugins/translate/panelStore.ts
```

If a TypeScript module is pure configuration and should stay non-reactive, store translation keys in the config and resolve them at render time.

- [ ] **Step 7: Update tests that asserted English literals**

Run:

```bash
cd desktop/frontend
rg -n 'Show system notifications|Enable shell integration|typing lag|Settings|Relay|Updates|Copy|Paste|Default target language|Connect to your relay' src/**/*.test.ts
```

For each match, change the assertion from hard-coded UI text to a key/import assertion or mount with i18n initialized. Example source assertion update:

```ts
expect(source).toContain('t("settings.general.notificationsLabel")')
```

Example component mount setup:

```ts
import { initI18n, resetI18nForTest } from "../i18n";

beforeEach(async () => {
  resetI18nForTest();
  await initI18n({ getLanguages: () => ["en-US"], listenLanguageChange: () => () => undefined });
});
```

- [ ] **Step 8: Run desktop literal audit**

Run:

```bash
cd desktop/frontend
rg -n '>[A-Za-z][^<]{1,}<|aria-label="[^"]+"|title="[^"]+"|placeholder="[^"]+"|\b(alert|confirm)\(|message\.(success|error|warning|info)\(|error\.value\s*=\s*"[A-Za-z]|warning\.value\s*=\s*"[A-Za-z]' src/App.vue src/components src/mobile src/plugins src/lib
```

Expected: no matches for user-visible English. Acceptable matches are protocol constants, CSS class names, test IDs, xterm shortcut key names such as `Esc`, and provider prompts that are intentionally sent to an API rather than shown as UI. Document accepted matches in the commit message body if any remain.

- [ ] **Step 9: Run desktop tests/build**

```bash
cd desktop/frontend && npm run test
cd desktop/frontend && npm run build
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add desktop/frontend/src
git commit -m "localize desktop and mobile UI"
```

---

### Task 5: Browser web i18n runtime

**Files:**
- Create: `web/src/shared/i18n/messages/en.ts`
- Create: `web/src/shared/i18n/messages/zh-CN.ts`
- Create: `web/src/shared/i18n/index.ts`
- Create: `web/src/shared/i18n/useI18n.ts`
- Create: `web/src/shared/i18n/i18n.test.ts`

- [ ] **Step 1: Write failing browser runtime tests**

Create `web/src/shared/i18n/i18n.test.ts`:

```ts
import { beforeEach, describe, expect, test, vi } from 'vitest'
import {
  initI18n,
  localePreference,
  resetI18nForTest,
  resolveLocalePreference,
  resolvedLocale,
  setLocalePreference,
  t,
} from './index'

beforeEach(() => {
  resetI18nForTest()
  localStorage.clear()
})

describe('web i18n locale resolution', () => {
  test('resolves Chinese browser languages to zh-CN', () => {
    expect(resolveLocalePreference('system', ['zh-CN'])).toBe('zh-CN')
    expect(resolveLocalePreference('system', ['zh-Hans-US'])).toBe('zh-CN')
    expect(resolveLocalePreference('system', ['zh'])).toBe('zh-CN')
  })

  test('falls back to English for other browser languages', () => {
    expect(resolveLocalePreference('system', ['fr-FR'])).toBe('en')
    expect(resolveLocalePreference('system', [])).toBe('en')
  })
})

describe('web i18n persistence and translation', () => {
  test('loads and saves localStorage preference', async () => {
    localStorage.setItem('atterm.locale', 'zh-CN')
    await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
    expect(localePreference.value).toBe('zh-CN')
    expect(resolvedLocale.value).toBe('zh-CN')

    await setLocalePreference('en')
    expect(localStorage.getItem('atterm.locale')).toBe('en')
    expect(resolvedLocale.value).toBe('en')
  })

  test('ignores storage failures and keeps runtime preference', async () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
    await expect(setLocalePreference('zh-CN')).resolves.toBeUndefined()
    expect(localePreference.value).toBe('zh-CN')
    spy.mockRestore()
  })

  test('interpolates parameters', async () => {
    await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
    expect(t('test.interpolated', { count: 2 })).toBe('2 sessions')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd web && npx vitest run src/shared/i18n/i18n.test.ts
```

Expected: FAIL because the i18n module does not exist.

- [ ] **Step 3: Add browser dictionaries**

Create `web/src/shared/i18n/messages/en.ts`:

```ts
export const en = {
  common: {
    language: 'Language',
    system: 'System',
    english: 'English',
    simplifiedChinese: 'Simplified Chinese',
    save: 'Save',
    cancel: 'Cancel',
    close: 'Close',
    refresh: 'Refresh',
    copy: 'Copy',
    paste: 'Paste',
    send: 'Send',
    connect: 'Connect',
    disconnect: 'Disconnect',
    loading: 'Loading...',
  },
  topbar: {},
  auth: {},
  setup: {},
  main: {},
  terminal: {},
  sessions: {},
  settings: {},
  admin: {},
  test: {
    interpolated: '{count} sessions',
  },
} as const

export type Messages = typeof en
```

Create `web/src/shared/i18n/messages/zh-CN.ts`:

```ts
import type { Messages } from './en'

export const zhCN = {
  common: {
    language: '语言',
    system: '跟随系统',
    english: 'English',
    simplifiedChinese: '简体中文',
    save: '保存',
    cancel: '取消',
    close: '关闭',
    refresh: '刷新',
    copy: '复制',
    paste: '粘贴',
    send: '发送',
    connect: '连接',
    disconnect: '断开连接',
    loading: '加载中...',
  },
  topbar: {},
  auth: {},
  setup: {},
  main: {},
  terminal: {},
  sessions: {},
  settings: {},
  admin: {},
  test: {
    interpolated: '{count} 个会话',
  },
} satisfies Messages
```

- [ ] **Step 4: Implement browser runtime**

Create `web/src/shared/i18n/index.ts`:

```ts
import { computed, ref, type ComputedRef, type Ref } from 'vue'
import { en, type Messages } from './messages/en'
import { zhCN } from './messages/zh-CN'

export type LocalePreference = 'system' | 'en' | 'zh-CN'
export type ResolvedLocale = 'en' | 'zh-CN'
export type TranslationParams = Record<string, string | number | boolean | null | undefined>
export type MessageKey = LeafKeys<Messages>

type LeafKeys<T, Prefix extends string = ''> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : T[K] extends Record<string, unknown>
      ? LeafKeys<T[K], `${Prefix}${K}.`>
      : never
}[keyof T & string]

const STORAGE_KEY = 'atterm.locale'
const messages = { en, 'zh-CN': zhCN } as const
let getLanguages: () => readonly string[] = defaultLanguages
let removeLanguageListener: (() => void) | undefined

export const localePreference: Ref<LocalePreference> = ref('system')
export const resolvedLocale: ComputedRef<ResolvedLocale> = computed(() =>
  resolveLocalePreference(localePreference.value, getLanguages()),
)

export const languageOptions = computed(() => [
  { value: 'system' as const, label: t('common.system') },
  { value: 'en' as const, label: t('common.english') },
  { value: 'zh-CN' as const, label: t('common.simplifiedChinese') },
])

interface InitOptions {
  getLanguages?: () => readonly string[]
  listenLanguageChange?: (handler: () => void) => () => void
}

export async function initI18n(options: InitOptions = {}): Promise<void> {
  removeLanguageListener?.()
  getLanguages = options.getLanguages ?? defaultLanguages
  localePreference.value = normalizePreference(readStoredPreference())
  removeLanguageListener = (options.listenLanguageChange ?? defaultListenLanguageChange)(() => {
    if (localePreference.value === 'system') localePreference.value = 'system'
  })
}

export async function setLocalePreference(preference: LocalePreference): Promise<void> {
  const normalized = normalizePreference(preference)
  localePreference.value = normalized
  try {
    localStorage.setItem(STORAGE_KEY, normalized)
  } catch {
    // Restricted browser contexts may block storage; keep in-memory language.
  }
}

export function resolveLocalePreference(preference: LocalePreference, languages: readonly string[]): ResolvedLocale {
  if (preference === 'en' || preference === 'zh-CN') return preference
  const first = languages.find((lang) => lang.trim() !== '')?.toLowerCase() ?? ''
  return first === 'zh' || first.startsWith('zh-') ? 'zh-CN' : 'en'
}

export function t(key: MessageKey, params: TranslationParams = {}): string {
  const value = lookup(messages[resolvedLocale.value], key) ?? lookup(en, key) ?? key
  return interpolate(value, params)
}

export function resetI18nForTest(): void {
  removeLanguageListener?.()
  removeLanguageListener = undefined
  getLanguages = defaultLanguages
  localePreference.value = 'system'
}

function readStoredPreference(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function normalizePreference(value: string | null | undefined): LocalePreference {
  return value === 'en' || value === 'zh-CN' || value === 'system' ? value : 'system'
}

function lookup(source: unknown, key: string): string | undefined {
  let current: unknown = source
  for (const part of key.split('.')) {
    if (!current || typeof current !== 'object' || !(part in current)) return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return typeof current === 'string' ? current : undefined
}

function interpolate(template: string, params: TranslationParams): string {
  return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (match, name: string) => {
    const value = params[name]
    return value === undefined || value === null ? match : String(value)
  })
}

function defaultLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages?.length ? navigator.languages : [navigator.language].filter(Boolean)
}

function defaultListenLanguageChange(handler: () => void): () => void {
  if (typeof window === 'undefined') return () => undefined
  window.addEventListener('languagechange', handler)
  return () => window.removeEventListener('languagechange', handler)
}
```

Create `web/src/shared/i18n/useI18n.ts`:

```ts
import { languageOptions, localePreference, resolvedLocale, setLocalePreference, t } from './index'

export function useI18n() {
  return {
    t,
    languageOptions,
    localePreference,
    resolvedLocale,
    setLocalePreference,
  }
}
```

- [ ] **Step 5: Run tests and typecheck**

```bash
cd web && npx vitest run src/shared/i18n/i18n.test.ts
cd web && npx vue-tsc --noEmit
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/shared/i18n
git commit -m "add web i18n runtime"
```

---

### Task 6: Browser entrypoint initialization and language selectors

**Files:**
- Modify: `web/src/login/main.ts`
- Modify: `web/src/signup/main.ts`
- Modify: `web/src/setup/main.ts`
- Modify: `web/src/main/main.ts`
- Modify: `web/src/settings/main.ts`
- Modify: `web/src/admin/main.ts`
- Create: `web/src/shared/components/LanguageSelect.vue`
- Create: `web/src/shared/components/LanguageSelect.test.ts`
- Modify: `web/src/login/App.vue`
- Modify: `web/src/signup/App.vue`
- Modify: `web/src/setup/App.vue`
- Modify: `web/src/settings/App.vue`

- [ ] **Step 1: Write failing component test for shared language selector**

Create `web/src/shared/components/LanguageSelect.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, test } from 'vitest'
import { initI18n, resetI18nForTest } from '@shared/i18n'
import LanguageSelect from './LanguageSelect.vue'

describe('LanguageSelect', () => {
  beforeEach(async () => {
    resetI18nForTest()
    localStorage.clear()
    await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
  })

  test('renders language options and persists selection', async () => {
    const wrapper = mount(LanguageSelect)
    const select = wrapper.get('[data-testid="language-select"]')
    expect(wrapper.text()).toContain('Language')
    await select.setValue('zh-CN')
    expect(localStorage.getItem('atterm.locale')).toBe('zh-CN')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd web && npx vitest run src/shared/components/LanguageSelect.test.ts
```

Expected: FAIL because `LanguageSelect.vue` does not exist.

- [ ] **Step 3: Create LanguageSelect component**

Create `web/src/shared/components/LanguageSelect.vue`:

```vue
<script setup lang="ts">
import { useI18n } from '@shared/i18n/useI18n'
import type { LocalePreference } from '@shared/i18n'

const { t, languageOptions, localePreference, setLocalePreference } = useI18n()

async function onChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value as LocalePreference
  await setLocalePreference(value)
}
</script>

<template>
  <label class="language-select">
    <span>{{ t('common.language') }}</span>
    <select data-testid="language-select" :value="localePreference" @change="onChange">
      <option v-for="option in languageOptions" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  </label>
</template>

<style scoped>
.language-select {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--muted, #9ca3af);
  font-size: 0.85rem;
}
.language-select select {
  border: 1px solid var(--line, #273043);
  border-radius: 999px;
  background: var(--panel, #111827);
  color: var(--fg, #f9fafb);
  padding: 0.25rem 0.6rem;
}
</style>
```

- [ ] **Step 4: Initialize i18n in every web entrypoint**

Apply this pattern to each entrypoint. Example for `web/src/login/main.ts`:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { initI18n } from '@shared/i18n'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

async function bootstrap() {
  await initI18n()
  if (!applyMobileEntryGuard('login')) {
    createApp(App).mount('#app')
  }
}

void bootstrap()
```

Use the same `bootstrap` shape in:

```text
web/src/signup/main.ts
web/src/setup/main.ts
web/src/main/main.ts
web/src/settings/main.ts
web/src/admin/main.ts
```

Keep the existing entry name passed to `applyMobileEntryGuard()` in each file.

- [ ] **Step 5: Add selectors to unauthenticated pages**

In `web/src/login/App.vue`, `web/src/signup/App.vue`, and `web/src/setup/App.vue`, import and render `LanguageSelect` near the auth/setup card footer or header:

```ts
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { useI18n } from '@shared/i18n/useI18n'

const { t } = useI18n()
```

Template pattern:

```vue
<div class="language-row">
  <LanguageSelect />
</div>
```

CSS pattern:

```css
.language-row {
  display: flex;
  justify-content: center;
  margin-top: 1rem;
}
```

- [ ] **Step 6: Add language settings section**

In `web/src/settings/App.vue`, import `LanguageSelect` and `useI18n`:

```ts
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { useI18n } from '@shared/i18n/useI18n'

const { t } = useI18n()
```

Add a settings block above the tabs:

```vue
<section class="settings-language">
  <LanguageSelect />
  <p>{{ t('settings.languageHint') }}</p>
</section>
```

Add CSS:

```css
.settings-language {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: rgba(15, 23, 42, 0.72);
}
.settings-language p {
  margin: 0;
  color: var(--muted);
  font-size: 0.85rem;
}
```

Add dictionary keys:

```ts
settings: {
  languageHint: 'Default follows your browser language. This setting is stored in this browser.',
}
```

```ts
settings: {
  languageHint: '默认跟随浏览器语言。此设置仅保存在当前浏览器。',
}
```

- [ ] **Step 7: Run tests/typecheck**

```bash
cd web && npx vitest run src/shared/components/LanguageSelect.test.ts src/shared/i18n/i18n.test.ts
cd web && npx vue-tsc --noEmit
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/login/main.ts web/src/signup/main.ts web/src/setup/main.ts web/src/main/main.ts web/src/settings/main.ts web/src/admin/main.ts web/src/shared/components/LanguageSelect.vue web/src/shared/components/LanguageSelect.test.ts web/src/login/App.vue web/src/signup/App.vue web/src/setup/App.vue web/src/settings/App.vue web/src/shared/i18n/messages/en.ts web/src/shared/i18n/messages/zh-CN.ts
git commit -m "wire web language preference UI"
```

---

### Task 7: Browser web full string migration

**Files:**
- Modify: `web/src/shared/i18n/messages/en.ts`
- Modify: `web/src/shared/i18n/messages/zh-CN.ts`
- Modify: `web/src/login/App.vue`
- Modify: `web/src/signup/App.vue`
- Modify: `web/src/setup/App.vue`
- Modify: `web/src/main/App.vue`
- Modify: `web/src/main/components/*.vue`
- Modify: `web/src/shared/components/Topbar.vue`
- Modify: `web/src/settings/App.vue`
- Modify: `web/src/settings/tabs/*.vue`
- Modify: `web/src/admin/App.vue`
- Modify: `web/src/admin/tabs/*.vue`
- Modify: user-facing helper strings in `web/src/shared/**/*.ts` where they are displayed to users.

- [ ] **Step 1: Capture current web string inventory**

Run:

```bash
cd web
rg -n '>[A-Za-z][^<]{1,}<|aria-label="[^"]+"|title="[^"]+"|placeholder="[^"]+"|message\.(success|error|warning|info)\(|error\.value\s*=\s*' src > /tmp/atterm-web-i18n-before.txt
wc -l /tmp/atterm-web-i18n-before.txt
```

Expected: command succeeds and writes the migration inventory. Review `/tmp/atterm-web-i18n-before.txt` before editing.

- [ ] **Step 2: Expand web dictionaries by feature**

Edit `web/src/shared/i18n/messages/en.ts` and `web/src/shared/i18n/messages/zh-CN.ts` so all empty groups from Task 5 are fully populated. Use these group names:

```ts
topbar: {
  home: 'Home',
  settings: 'Settings',
  admin: 'Admin',
  signOut: 'Sign out',
  primaryNav: 'Primary',
},
auth: {
  signIn: 'sign in',
  signUp: 'sign up',
  email: 'Email',
  password: 'Password',
  inviteCode: 'Invite code',
},
setup: {},
main: {},
terminal: {},
sessions: {},
settings: {},
admin: {},
```

Populate the empty groups with keys from the inventory. The Chinese dictionary must use `satisfies Messages`, not a partial type.

- [ ] **Step 3: Migrate auth/setup pages**

In these files, import `useI18n`, add `const { t } = useI18n()`, and replace all visible strings, labels, placeholders, link text, and client-side errors:

```text
web/src/login/App.vue
web/src/signup/App.vue
web/src/setup/App.vue
```

Use this pattern for Naive UI labels:

```vue
<n-form-item :label="t('auth.email')" :show-feedback="false">
```

Use this pattern for string assignments:

```ts
error.value = t('setup.invalidToken')
```

- [ ] **Step 4: Migrate topbar and main terminal pages**

Migrate these files:

```text
web/src/shared/components/Topbar.vue
web/src/main/App.vue
web/src/main/components/InstallHint.vue
web/src/main/components/PasteFallback.vue
web/src/main/components/SessionList.vue
web/src/main/components/ShortcutBar.vue
web/src/main/components/TerminalView.vue
```

Keep terminal shortcut labels like `Esc`, `Tab`, `Ctrl-C`, and `Ctrl-D` unchanged unless they are explanatory text. Translate action labels such as copy/paste/send/cancel and overlays such as driver/viewer state.

- [ ] **Step 5: Migrate settings pages**

Migrate these files:

```text
web/src/settings/App.vue
web/src/settings/tabs/ApiTokens.vue
web/src/settings/tabs/ChangePassword.vue
web/src/settings/tabs/DangerZone.vue
web/src/settings/tabs/Notifications.vue
web/src/settings/tabs/Relay.vue
web/src/settings/tabs/Sessions.vue
web/src/settings/tabs/Webhooks.vue
```

Translate tab labels, card titles, form labels, placeholders, empty states, success/error messages, and validation errors. Keep API token values, webhook URLs, email confirmations, and other user data unchanged.

- [ ] **Step 6: Migrate admin pages**

Migrate these files:

```text
web/src/admin/App.vue
web/src/admin/tabs/Config.vue
web/src/admin/tabs/Invitations.vue
web/src/admin/tabs/Users.vue
```

Translate tab labels, card titles, table column titles, button labels, status labels, and messages. Keep role names and API field names unchanged when they are part of payloads; translate only display labels.

- [ ] **Step 7: Update web tests that asserted English literals**

Run:

```bash
cd web
rg -n 'Sign in|Settings|Relay|API token|Notifications|No active sessions|No live sessions|Users|Invitations|Runtime limits' src tests || true
```

For every test hit, either initialize i18n in the test and assert rendered English, or assert translation keys in source when that is the local pattern. Use this setup for mounted Vue tests:

```ts
import { initI18n, resetI18nForTest } from '@shared/i18n'

beforeEach(async () => {
  resetI18nForTest()
  localStorage.clear()
  await initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined })
})
```

- [ ] **Step 8: Run web literal audit**

Run:

```bash
cd web
rg -n '>[A-Za-z][^<]{1,}<|aria-label="[^"]+"|title="[^"]+"|placeholder="[^"]+"|message\.(success|error|warning|info)\(|error\.value\s*=\s*"[A-Za-z]' src
```

Expected: no matches for user-visible English. Acceptable matches are brand text `AT Term`, keyboard key labels, API constants, URLs, examples such as `https://relay.example.com`, token prefixes such as `atk_`, and code values. Document accepted matches in the commit message body if any remain.

- [ ] **Step 9: Run web tests/build**

```bash
cd web && npm run test
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add web/src
git commit -m "localize browser web UI"
```

---

### Task 8: Cross-client verification and cleanup

**Files:**
- Modify: any frontend tests failing after full migration.
- Modify: dictionaries if literal audits find missed UI strings.
- No changes to protocol docs are expected because this is UI-only.

- [ ] **Step 1: Run full frontend and Go verification**

```bash
go test -tags webkit2_41 ./desktop/
cd desktop/frontend && npm run test
cd desktop/frontend && npm run build
cd web && npm run test
cd web && npm run build
node --test web/tests/contract/*.test.mjs
```

Expected: all commands PASS.

- [ ] **Step 2: Run repository-wide i18n audits**

```bash
rg -n '>[A-Za-z][^<]{1,}<|aria-label="[^"]+"|title="[^"]+"|placeholder="[^"]+"|message\.(success|error|warning|info)\(|error\.value\s*=\s*"[A-Za-z]' desktop/frontend/src web/src
rg -n 'TODO|TBD|FIXME|translate later|i18n later' desktop/frontend/src/i18n web/src/shared/i18n desktop/frontend/src web/src
```

Expected: first command has only accepted non-UI/code-value matches; second command has no matches introduced by this work.

- [ ] **Step 3: Manually verify locale behavior in browser unit environment**

Run this smoke script from each package to prove fresh/system behavior and explicit override:

```bash
cd desktop/frontend && npx vitest run src/i18n/i18n.test.ts
cd web && npx vitest run src/shared/i18n/i18n.test.ts
```

Expected: tests cover Chinese system tags resolving to `zh-CN`, non-Chinese resolving to `en`, explicit language overriding system, persistence, and interpolation.

- [ ] **Step 4: Inspect git diff for forbidden scope changes**

```bash
git diff --stat HEAD~8..HEAD
git diff HEAD~8..HEAD -- internal cmd web/vendor docs/spec || true
git status --short
```

Expected: no protocol/internal relay/vendor changes; `.gitignore` remains only the pre-existing unrelated modification unless the user explicitly approved touching it.

- [ ] **Step 5: Final commit if cleanup was needed**

If Task 8 made fixes, commit them:

```bash
git add desktop/frontend/src web/src desktop/config.go desktop/app.go desktop/config_test.go desktop/frontend/src/lib/api.ts
git commit -m "finish i18n verification fixes"
```

If Task 8 made no fixes, do not create an empty commit.
