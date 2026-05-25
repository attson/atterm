import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  initI18n,
  languageOptions,
  localePreference,
  resetI18nForTest,
  resolveLocalePreference,
  resolvedLocale,
  setLocalePreference,
  t,
} from "./index";
import { useI18n } from "./useI18n";

beforeEach(() => resetI18nForTest());

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
    const save = vi.fn<["system" | "en" | "zh-CN"], Promise<void>>().mockResolvedValue(undefined);

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

  test("rolls back when savePreference fails", async () => {
    await initI18n({
      loadPreference: async () => "en",
      savePreference: async () => {
        throw new Error("blocked");
      },
      getLanguages: () => ["en-US"],
      listenLanguageChange: () => () => undefined,
    });

    await expect(setLocalePreference("zh-CN")).rejects.toThrow(/blocked/);

    expect(localePreference.value).toBe("en");
    expect(resolvedLocale.value).toBe("en");
  });

  test("interpolates named parameters and leaves missing parameters visible", async () => {
    await initI18n({
      getLanguages: () => ["en-US"],
      listenLanguageChange: () => () => undefined,
    });

    expect(t("test.interpolated", { count: 3 })).toBe("3 sessions");
    expect(t("test.interpolated")).toBe("{count} sessions");
  });

  test("returns the key when no locale contains a translation", async () => {
    await initI18n({
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    expect(t("test.missing" as never)).toBe("test.missing");
  });

  test("updates system locale when languagechange fires", async () => {
    let onLanguageChange: (() => void) | undefined;
    let languages = ["en-US"];

    await initI18n({
      getLanguages: () => languages,
      listenLanguageChange: (listener) => {
        onLanguageChange = listener;
        return () => undefined;
      },
    });

    expect(resolvedLocale.value).toBe("en");

    languages = ["zh-CN"];
    onLanguageChange?.();

    expect(localePreference.value).toBe("system");
    expect(resolvedLocale.value).toBe("zh-CN");
  });

  test("useI18n exposes runtime state and actions", async () => {
    await initI18n({
      getLanguages: () => ["en-US"],
      listenLanguageChange: () => () => undefined,
    });

    const i18n = useI18n();

    expect(i18n.t("common.language")).toBe("Language");
    expect(i18n.languageOptions).toBe(languageOptions);
    expect(i18n.localePreference.value).toBe("system");
    expect(i18n.resolvedLocale.value).toBe("en");

    await i18n.setLocalePreference("zh-CN");

    expect(i18n.localePreference.value).toBe("zh-CN");
    expect(i18n.resolvedLocale.value).toBe("zh-CN");
  });
});
