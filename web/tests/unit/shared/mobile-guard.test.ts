import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  saveRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import { applyMobileEntryGuard, type EntryPage } from '@shared/mobile-guard'

interface Case {
  name: string
  mobile: boolean
  hasConfig: boolean
  page: EntryPage
  expectRedirect: string | null
  expectReturn: boolean
}

const CASES: Case[] = [
  { name: 'browser any page → no-op',          mobile: false, hasConfig: false, page: 'home',     expectRedirect: null,           expectReturn: false },
  { name: 'mobile no config + setup → render', mobile: true,  hasConfig: false, page: 'setup',    expectRedirect: null,           expectReturn: false },
  { name: 'mobile no config + home → setup',   mobile: true,  hasConfig: false, page: 'home',     expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile no config + login → setup',  mobile: true,  hasConfig: false, page: 'login',    expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile no config + admin → setup',  mobile: true,  hasConfig: false, page: 'admin',    expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile no config + signup → setup',   mobile: true,  hasConfig: false, page: 'signup',   expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile + config + setup → home',    mobile: true,  hasConfig: true,  page: 'setup',    expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + login → home',    mobile: true,  hasConfig: true,  page: 'login',    expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + signup → home',   mobile: true,  hasConfig: true,  page: 'signup',   expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + home → render',   mobile: true,  hasConfig: true,  page: 'home',     expectRedirect: null,           expectReturn: false },
  { name: 'mobile + config + admin → render',  mobile: true,  hasConfig: true,  page: 'admin',    expectRedirect: null,           expectReturn: false },
]

describe('applyMobileEntryGuard decision table', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  for (const c of CASES) {
    it(c.name, () => {
      if (c.mobile) (globalThis as any).Capacitor = { isNativePlatform: () => true }
      if (c.hasConfig) saveRelayConfig({
        baseURL: 'https://r.example.com',
        sessionToken: 'ses_t',
        expiresAt: null,
        allowInsecure: false,
      })

      const returned = applyMobileEntryGuard(c.page)

      expect(returned).toBe(c.expectReturn)
      if (c.expectRedirect === null) {
        expect(replaceMock).not.toHaveBeenCalled()
      } else {
        expect(replaceMock).toHaveBeenCalledWith(c.expectRedirect)
      }
    })
  }
})
