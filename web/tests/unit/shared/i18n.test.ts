import { beforeEach, describe, expect, test, vi } from 'vitest';
import {
  initI18n,
  localePreference,
  resetI18nForTest,
  resolveLocalePreference,
  resolvedLocale,
  setLocalePreference,
  t,
} from '@shared/i18n';

describe('browser i18n runtime', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetI18nForTest();
  });

  test('resolves Chinese system language tags and rejects zhx regression', () => {
    expect(resolveLocalePreference('system', ['zh-CN'])).toBe('zh-CN');
    expect(resolveLocalePreference('system', ['zh-Hans-US'])).toBe('zh-CN');
    expect(resolveLocalePreference('system', ['zh'])).toBe('zh-CN');
    expect(resolveLocalePreference('system', ['zhx'])).toBe('en');
    expect(resolveLocalePreference('system', ['fr'])).toBe('en');
    expect(resolveLocalePreference('system', [''])).toBe('en');
    expect(resolveLocalePreference('system', [])).toBe('en');
    expect(resolveLocalePreference('zh-CN', ['en-US'])).toBe('zh-CN');
    expect(resolveLocalePreference('en', ['zh-CN'])).toBe('en');
  });

  test('loads and saves locale preference in localStorage', () => {
    localStorage.setItem('atterm.locale', 'zh-CN');

    initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined });

    expect(localePreference.value).toBe('zh-CN');
    expect(resolvedLocale.value).toBe('zh-CN');

    setLocalePreference('en');

    expect(localePreference.value).toBe('en');
    expect(resolvedLocale.value).toBe('en');
    expect(localStorage.getItem('atterm.locale')).toBe('en');
  });

  test('falls back to system when stored preference is invalid', () => {
    localStorage.setItem('atterm.locale', 'zhx');

    initI18n({ getLanguages: () => ['zh-CN'], listenLanguageChange: () => () => undefined });

    expect(localePreference.value).toBe('system');
    expect(resolvedLocale.value).toBe('zh-CN');
  });

  test('keeps in-memory preference when localStorage setItem fails', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => 'system',
      setItem: () => {
        throw new Error('quota exceeded');
      },
    });
    initI18n({ getLanguages: () => ['en-US'], listenLanguageChange: () => () => undefined });

    expect(() => setLocalePreference('zh-CN')).not.toThrow();

    expect(localePreference.value).toBe('zh-CN');
    expect(resolvedLocale.value).toBe('zh-CN');
  });


  test('recomputes system locale when languagechange fires', () => {
    let onLanguageChange: (() => void) | undefined;
    let languages = ['en-US'];

    initI18n({
      getLanguages: () => languages,
      listenLanguageChange: (handler) => {
        onLanguageChange = handler;
        return () => undefined;
      },
    });

    expect(localePreference.value).toBe('system');
    expect(resolvedLocale.value).toBe('en');

    languages = ['zh-CN'];
    onLanguageChange?.();

    expect(localePreference.value).toBe('system');
    expect(resolvedLocale.value).toBe('zh-CN');
  });

  test('translates starter dictionary keys with interpolation', () => {
    setLocalePreference('en');
    expect(t('common.loading')).toBe('Loading...');
    expect(t('test.interpolation', { count: 3 })).toBe('3 sessions');

    setLocalePreference('zh-CN');
    expect(t('common.loading')).toBe('正在加载...');
    expect(t('test.interpolation', { count: 3 })).toBe('3 个会话');
  });
});
