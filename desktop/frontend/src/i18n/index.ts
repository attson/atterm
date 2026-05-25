import { ref } from "vue";
import { en, type Messages } from "./messages/en";
import { zhCN } from "./messages/zh-CN";

export type LocalePreference = "system" | "en" | "zh-CN";
export type ResolvedLocale = "en" | "zh-CN";

type PrimitiveMessage = string;
type DotPrefix<Prefix extends string, Key extends string> = `${Prefix}.${Key}`;
type MessageKeyOf<T> = {
  [Key in keyof T & string]: T[Key] extends PrimitiveMessage
    ? Key
    : T[Key] extends Record<string, unknown>
      ? DotPrefix<Key, MessageKeyOf<T[Key]>>
      : never;
}[keyof T & string];

type TranslationParams = Record<string, string | number>;
type LanguageChangeUnsubscribe = () => void;

type InitI18nOptions = {
  loadPreference?: () => Promise<unknown>;
  savePreference?: (preference: LocalePreference) => Promise<void>;
  getLanguages?: () => readonly string[];
  listenLanguageChange?: (listener: () => void) => LanguageChangeUnsubscribe;
};

export type MessageKey = MessageKeyOf<Messages>;

const messages: Record<ResolvedLocale, Messages> = {
  en,
  "zh-CN": zhCN,
};

export const languageOptions: readonly { labelKey: MessageKey; value: LocalePreference }[] = [
  { labelKey: "common.system", value: "system" },
  { labelKey: "common.english", value: "en" },
  { labelKey: "common.simplifiedChinese", value: "zh-CN" },
];

export const localePreference = ref<LocalePreference>("system");
export const resolvedLocale = ref<ResolvedLocale>("en");

let savePreference: ((preference: LocalePreference) => Promise<void>) | undefined;
let getLanguages: () => readonly string[] = defaultGetLanguages;
let unsubscribeLanguageChange: LanguageChangeUnsubscribe | undefined;

export function resolveLocalePreference(
  preference: LocalePreference,
  languages: readonly string[],
): ResolvedLocale {
  if (preference !== "system") {
    return preference;
  }

  return languages.some((language) => language.toLowerCase().startsWith("zh")) ? "zh-CN" : "en";
}

export async function initI18n(options: InitI18nOptions = {}): Promise<void> {
  unsubscribeLanguageChange?.();
  unsubscribeLanguageChange = undefined;

  savePreference = options.savePreference;
  getLanguages = options.getLanguages ?? defaultGetLanguages;

  const loadedPreference = options.loadPreference ? await options.loadPreference() : "system";
  localePreference.value = normalizeLocalePreference(loadedPreference);
  updateResolvedLocale();

  const listenLanguageChange = options.listenLanguageChange ?? defaultListenLanguageChange;
  unsubscribeLanguageChange = listenLanguageChange(() => {
    updateResolvedLocale();
  });
}

export async function setLocalePreference(preference: LocalePreference): Promise<void> {
  const previousPreference = localePreference.value;
  const previousResolvedLocale = resolvedLocale.value;

  localePreference.value = preference;
  updateResolvedLocale();

  try {
    await savePreference?.(preference);
  } catch (error) {
    localePreference.value = previousPreference;
    resolvedLocale.value = previousResolvedLocale;
    throw error;
  }
}

export function t(key: MessageKey, params: TranslationParams = {}): string {
  const message = lookupMessage(messages[resolvedLocale.value], key) ?? lookupMessage(messages.en, key);

  if (message === undefined) {
    return key;
  }

  return interpolate(message, params);
}

export function resetI18nForTest(): void {
  unsubscribeLanguageChange?.();
  unsubscribeLanguageChange = undefined;
  savePreference = undefined;
  getLanguages = defaultGetLanguages;
  localePreference.value = "system";
  resolvedLocale.value = "en";
}

function updateResolvedLocale(): void {
  resolvedLocale.value = resolveLocalePreference(localePreference.value, getLanguages());
}

function normalizeLocalePreference(value: unknown): LocalePreference {
  return value === "en" || value === "zh-CN" || value === "system" ? value : "system";
}

function lookupMessage(dictionary: Messages, key: string): string | undefined {
  let current: unknown = dictionary;

  for (const part of key.split(".")) {
    if (!isRecord(current) || !(part in current)) {
      return undefined;
    }
    current = current[part];
  }

  return typeof current === "string" ? current : undefined;
}

function interpolate(message: string, params: TranslationParams): string {
  return message.replace(/\{([A-Za-z0-9_]+)\}/g, (placeholder, name: string) => {
    const value = params[name];
    return value === undefined ? placeholder : String(value);
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function defaultGetLanguages(): readonly string[] {
  if (typeof navigator === "undefined") {
    return [];
  }
  return navigator.languages.length > 0 ? navigator.languages : [navigator.language];
}

function defaultListenLanguageChange(listener: () => void): LanguageChangeUnsubscribe {
  if (typeof window === "undefined") {
    return () => undefined;
  }

  window.addEventListener("languagechange", listener);
  return () => window.removeEventListener("languagechange", listener);
}
