import { beforeEach } from 'vitest'
import { initI18n, resetI18nForTest } from '@shared/i18n'

export function installI18nTestHooks(): void {
  beforeEach(() => {
    localStorage.clear()
    resetI18nForTest()
    initI18n({
      getLanguages: () => ['en-US'],
      listenLanguageChange: () => () => undefined,
    })
  })
}
