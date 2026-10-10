import { describe, expect, it } from 'vitest'
import { checkRendezvousPreflight } from './rendezvousPreflight'

describe('checkRendezvousPreflight', () => {
  it('does not require browser capabilities when Rendezvous is disabled', () => {
    expect(checkRendezvousPreflight('disabled', {
      secureContext: false,
      webCrypto: false,
      webRTC: false,
    })).toEqual({ ready: false, code: 'disabled' })
  })

  it.each([
    [{ secureContext: false, webCrypto: true, webRTC: true }, 'insecure_context'],
    [{ secureContext: true, webCrypto: false, webRTC: true }, 'webcrypto_unavailable'],
    [{ secureContext: true, webCrypto: true, webRTC: false }, 'webrtc_unavailable'],
  ] as const)('fails before dialing for %o', (capabilities, code) => {
    expect(checkRendezvousPreflight('official', capabilities)).toEqual({ ready: false, code })
  })

  it('allows official and custom modes only when all browser capabilities exist', () => {
    const capabilities = { secureContext: true, webCrypto: true, webRTC: true }
    expect(checkRendezvousPreflight('official', capabilities)).toEqual({ ready: true, code: 'ready' })
    expect(checkRendezvousPreflight('custom', capabilities)).toEqual({ ready: true, code: 'ready' })
  })
})
