import { describe, it, expect, vi, beforeEach } from 'vitest'

const registerSW = vi.fn().mockReturnValue(() => {})

vi.mock('virtual:pwa-register', () => ({
  registerSW: (opts: unknown) => registerSW(opts),
}))

describe('shared/pwa', () => {
  beforeEach(() => {
    registerSW.mockClear()
  })

  it('calls registerSW({ immediate: true }) on import when SW is available', async () => {
    Object.defineProperty(navigator, 'serviceWorker', {
      value: { ready: Promise.resolve() } as unknown as ServiceWorkerContainer,
      configurable: true,
    })
    vi.resetModules()
    await import('@shared/pwa')
    expect(registerSW).toHaveBeenCalledTimes(1)
    expect(registerSW.mock.calls[0]![0]).toMatchObject({ immediate: true })
  })

  it('skips registration when serviceWorker is unavailable', async () => {
    delete (navigator as { serviceWorker?: unknown }).serviceWorker
    vi.resetModules()
    await import('@shared/pwa')
    expect(registerSW).not.toHaveBeenCalled()
  })
})
