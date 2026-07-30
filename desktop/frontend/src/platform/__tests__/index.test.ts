import { describe, it, expect, beforeEach } from 'vitest'
import type { Platform } from '../types'
import { initPlatform, usePlatform, __setPlatformForTests } from '../index'

function fakePlatform(): Platform {
  return {
    caps: {
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
      wailsBindings: true, capacitor: false,
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
      getPins: async () => [],
      setPins: async () => {},
    },
    system: {
      showNotification: async () => {},
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async () => {},
      getEnvironment: async () => null,
      getAppVersion: async () => 'dev',
    },
    events: {
      on: () => () => {},
      emit: () => {},
    },
    templates: {
      load: async () => [],
      save: async () => {},
      clear: async () => {},
      loadHidden: async () => false,
      saveHidden: async () => {},
    },
    auxKeys: {
      load: async () => [],
      save: async () => {},
      clear: async () => {},
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

  it('initPlatform(factory) returns the factory result and usePlatform() returns the same instance', () => {
    const fake = fakePlatform()
    const returned = initPlatform(() => fake)
    expect(returned).toBe(fake)
    expect(usePlatform()).toBe(fake)
  })

  it('initPlatform(factory) is idempotent — subsequent calls return the first instance and do not re-invoke the factory', () => {
    const fake1 = fakePlatform()
    const fake2 = fakePlatform()
    const factory1 = () => fake1
    const factory2 = () => fake2
    const first = initPlatform(factory1)
    const second = initPlatform(factory2)
    expect(first).toBe(fake1)
    expect(second).toBe(fake1)  // not fake2 — factory not re-invoked
  })

  it('__setPlatformForTests(null) clears the singleton', () => {
    __setPlatformForTests(fakePlatform())
    expect(usePlatform()).toBeTruthy()
    __setPlatformForTests(null)
    expect(() => usePlatform()).toThrow()
  })
})
