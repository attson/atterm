export type RendezvousMode = 'disabled' | 'official' | 'custom'

export type RendezvousPreflightCode =
  | 'disabled'
  | 'insecure_context'
  | 'webcrypto_unavailable'
  | 'webrtc_unavailable'
  | 'ready'

export interface RendezvousCapabilities {
  secureContext: boolean
  webCrypto: boolean
  webRTC: boolean
}

export interface RendezvousPreflightResult {
  ready: boolean
  code: RendezvousPreflightCode
}

export function browserRendezvousCapabilities(): RendezvousCapabilities {
  return {
    secureContext: globalThis.isSecureContext === true,
    webCrypto: typeof globalThis.crypto?.subtle !== 'undefined',
    webRTC: typeof globalThis.RTCPeerConnection === 'function',
  }
}

// This gate must run before creating identity keys or RTCPeerConnection. It
// turns browser capability failures into stable UI states instead of throws.
export function checkRendezvousPreflight(
  mode: RendezvousMode,
  capabilities: RendezvousCapabilities = browserRendezvousCapabilities(),
): RendezvousPreflightResult {
  if (mode === 'disabled') return { ready: false, code: 'disabled' }
  if (!capabilities.secureContext) return { ready: false, code: 'insecure_context' }
  if (!capabilities.webCrypto) return { ready: false, code: 'webcrypto_unavailable' }
  if (!capabilities.webRTC) return { ready: false, code: 'webrtc_unavailable' }
  return { ready: true, code: 'ready' }
}
