import {
  languageOptions,
  localePreference,
  resolvedLocale,
  setLocalePreference,
  t,
} from "./index";

export function useI18n() {
  return {
    t,
    languageOptions,
    localePreference,
    resolvedLocale,
    setLocalePreference,
  };
}
