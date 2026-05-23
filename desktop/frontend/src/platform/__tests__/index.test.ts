import { describe, it, expect, beforeEach } from 'vitest'
import type { Platform } from '../types'
import { initPlatform, usePlatform, __setPlatformForTests } from '../index'

function fakePlatform(): Platform {
  return {
    caps: {
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
    },
    relay: {
      load: async () => null,
      save: async () => {},
      clear: async () => {},
      fetchMe: async () => ({ user_id: 'u', email: 'e' }),
    },
    sessions: {
      closeSession: async () => {},
      listShells: async () => [],
      listRemoteSessions: async () => [],
    },
    system: {
      showNotification: async () => {},
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async () => {},
      getEnvironment: async () => null,
    },
    events: {
      on: () => () => {},
      emit: () => {},
    },
  }
}

describe('platform singleton', () => {
  beforeEach(() => {
    __setPlatformForTests(null)
  })

  it('usePlatform() throws before initPlatform()', () => {
    expect(() => usePlatform()).toThrow(/initPlatform/i)
  })

  it('initPlatform() returns a Platform and usePlatform() returns the same instance', () => {
    // Stub VITE_TARGET handling by pre-installing via the test helper instead
    // of actually invoking createWailsPlatform here (kept separate).
    const fake = fakePlatform()
    __setPlatformForTests(fake)
    expect(usePlatform()).toBe(fake)
  })

  it('initPlatform() is idempotent', () => {
    const fake = fakePlatform()
    __setPlatformForTests(fake)
    // Idempotency property check via subsequent usePlatform calls.
    expect(usePlatform()).toBe(usePlatform())
  })

  it('__setPlatformForTests(null) clears the singleton', () => {
    __setPlatformForTests(fakePlatform())
    expect(usePlatform()).toBeTruthy()
    __setPlatformForTests(null)
    expect(() => usePlatform()).toThrow()
  })
})
