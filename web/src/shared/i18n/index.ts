import { computed, ref } from 'vue';
import { en, type Messages } from './messages/en';
import { zhCN } from './messages/zh-CN';

export type LocalePreference = 'system' | 'en' | 'zh-CN';
export type ResolvedLocale = 'en' | 'zh-CN';
type LanguageChangeUnsubscribe = () => void;
type InitI18nOptions = {
  getLanguages?: () => readonly string[];
  listenLanguageChange?: (handler: () => void) => LanguageChangeUnsubscribe;
};

const storageKey = 'atterm.locale';
const dictionaries: Record<ResolvedLocale, Messages> = {
  en,
  'zh-CN': zhCN,
};

export const localePreference = ref<LocalePreference>('system');
export const resolvedLocale = ref<ResolvedLocale>('en');
export const languageOptions = computed<Array<{ label: string; value: LocalePreference }>>(() => [
  { label: t('common.system'), value: 'system' },
  { label: t('common.english'), value: 'en' },
  { label: t('common.simplifiedChinese'), value: 'zh-CN' },
]);

let getLanguages: () => readonly string[] = defaultGetLanguages;
let unsubscribeLanguageChange: LanguageChangeUnsubscribe | undefined;

export function initI18n(options: InitI18nOptions = {}): void {
  unsubscribeLanguageChange?.();
  unsubscribeLanguageChange = undefined;
  getLanguages = options.getLanguages ?? defaultGetLanguages;

  const storedPreference = readStoredPreference();
  localePreference.value = storedPreference;
  updateResolvedLocale();

  const listenLanguageChange = options.listenLanguageChange ?? defaultListenLanguageChange;
  unsubscribeLanguageChange = listenLanguageChange(() => {
    updateResolvedLocale();
  });
}

export function setLocalePreference(preference: LocalePreference): void {
  localePreference.value = preference;
  updateResolvedLocale();
  try {
    window.localStorage.setItem(storageKey, preference);
  } catch {
    // Browser storage can be unavailable; runtime state still updates.
  }
}

export function resolveLocalePreference(
  preference: LocalePreference,
  languages: readonly string[] = defaultGetLanguages(),
): ResolvedLocale {
  if (preference === 'en' || preference === 'zh-CN') {
    return preference;
  }
  const primaryLanguage = languages.find((language) => language.trim() !== '');
  if (primaryLanguage === undefined) {
    return 'en';
  }
  const normalized = primaryLanguage.trim().toLowerCase();
  return normalized === 'zh' || normalized.startsWith('zh-') ? 'zh-CN' : 'en';
}

export function t(key: string, params: Record<string, string | number> = {}): string {
  const message = lookupMessage(dictionaries[resolvedLocale.value], key)
    ?? lookupMessage(dictionaries.en, key)
    ?? key;
  return interpolate(message, params);
}

export function resetI18nForTest(): void {
  unsubscribeLanguageChange?.();
  unsubscribeLanguageChange = undefined;
  getLanguages = defaultGetLanguages;
  localePreference.value = 'system';
  setResolvedLocale('en');
}

function readStoredPreference(): LocalePreference {
  try {
    const stored = window.localStorage.getItem(storageKey);
    return isLocalePreference(stored) ? stored : 'system';
  } catch {
    return 'system';
  }
}

function isLocalePreference(value: unknown): value is LocalePreference {
  return value === 'system' || value === 'en' || value === 'zh-CN';
}

function updateResolvedLocale(): void {
  setResolvedLocale(resolveLocalePreference(localePreference.value, getLanguages()));
}

function setResolvedLocale(locale: ResolvedLocale): void {
  resolvedLocale.value = locale;
  syncDocumentLanguage(locale);
}

function syncDocumentLanguage(locale: ResolvedLocale): void {
  if (typeof document === 'undefined') {
    return;
  }
  document.documentElement.lang = locale;
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

function defaultGetLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') {
    return [];
  }
  if (navigator.languages.length > 0) {
    return navigator.languages;
  }
  return navigator.language ? [navigator.language] : [];
}

function defaultListenLanguageChange(handler: () => void): LanguageChangeUnsubscribe {
  if (typeof window === 'undefined') {
    return () => undefined;
  }
  window.addEventListener('languagechange', handler);
  return () => window.removeEventListener('languagechange', handler);
}
