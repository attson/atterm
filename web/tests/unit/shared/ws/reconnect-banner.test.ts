import { describe, it, expect } from 'vitest'
import { shouldShowReconnectBanner } from '@shared/ws/client-conn'

describe('shouldShowReconnectBanner', () => {
  it('returns false when there has been no failure yet', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 0, firstFailureAt: null, now: 1000 })).toBe(false)
  })

  it('returns false with 4 failures and only 10s elapsed', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 4, firstFailureAt: 1000, now: 11_000 })).toBe(false)
  })

  it('returns true at 5 failures even if only 1s elapsed', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 5, firstFailureAt: 1000, now: 2000 })).toBe(true)
  })

  it('returns true after 30s elapsed even with only 2 failures', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 2, firstFailureAt: 1000, now: 31_001 })).toBe(true)
  })

  it('returns false exactly at threshold-minus-one of both axes', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 4, firstFailureAt: 1000, now: 30_999 })).toBe(false)
  })
})
