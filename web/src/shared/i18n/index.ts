import { ref } from 'vue';
import { en, type Messages } from './messages/en';
import { zhCN } from './messages/zh-CN';

export type LocalePreference = 'system' | 'en' | 'zh-CN';
export type ResolvedLocale = 'en' | 'zh-CN';

const storageKey = 'atterm.locale';
const dictionaries: Record<ResolvedLocale, Messages> = {
  en,
  'zh-CN': zhCN,
};

export const languageOptions = [
  { label: 'System', value: 'system' },
  { label: 'English', value: 'en' },
  { label: '简体中文', value: 'zh-CN' },
] satisfies Array<{ label: string; value: LocalePreference }>;

export const localePreference = ref<LocalePreference>('system');
export const resolvedLocale = ref<ResolvedLocale>(resolveLocalePreference('system'));

export function initI18n(systemLanguage = readSystemLanguage()): void {
  const storedPreference = readStoredPreference();
  localePreference.value = storedPreference;
  resolvedLocale.value = resolveLocalePreference(storedPreference, systemLanguage);
}

export function setLocalePreference(preference: LocalePreference, systemLanguage = readSystemLanguage()): void {
  localePreference.value = preference;
  resolvedLocale.value = resolveLocalePreference(preference, systemLanguage);
  try {
    window.localStorage.setItem(storageKey, preference);
  } catch {
    // Browser storage can be unavailable; runtime state still updates.
  }
}

export function resolveLocalePreference(
  preference: LocalePreference,
  systemLanguage = readSystemLanguage(),
): ResolvedLocale {
  if (preference === 'en' || preference === 'zh-CN') {
    return preference;
  }
  return systemLanguage === 'zh' || systemLanguage.startsWith('zh-') ? 'zh-CN' : 'en';
}

export function t(key: string, params: Record<string, string | number> = {}): string {
  const message = lookupMessage(dictionaries[resolvedLocale.value], key)
    ?? lookupMessage(dictionaries.en, key)
    ?? key;
  return interpolate(message, params);
}

export function resetI18nForTest(): void {
  localePreference.value = 'system';
  resolvedLocale.value = resolveLocalePreference('system', 'en-US');
}

function readStoredPreference(): LocalePreference {
  try {
    const stored = window.localStorage.getItem(storageKey);
    return isLocalePreference(stored) ? stored : 'system';
  } catch {
    return 'system';
  }
}

function readSystemLanguage(): string {
  return typeof navigator === 'undefined' ? '' : navigator.language;
}

function isLocalePreference(value: unknown): value is LocalePreference {
  return value === 'system' || value === 'en' || value === 'zh-CN';
}

function lookupMessage(messages: Messages, key: string): string | undefined {
  let cursor: unknown = messages;
  for (const segment of key.split('.')) {
    if (!isRecord(cursor)) {
      return undefined;
    }
    cursor = cursor[segment];
  }
  return typeof cursor === 'string' ? cursor : undefined;
}

function interpolate(template: string, params: Record<string, string | number>): string {
  return template.replace(/\{([^{}]+)\}/g, (match, name: string) => {
    const value = params[name];
    return value === undefined ? match : String(value);
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

