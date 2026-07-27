import type { Platform, Capabilities } from './types'
import { apiFetch } from '@webshared/api/client'
// ^ resolved via the '@webshared' vite alias to web/src/shared/api/client
// (desktop/frontend/vite.config.ts). Unused until tasks 4.2/4.3 flesh out
// the bridges below.
void apiFetch

const CAPS: Capabilities = {
  localPty: false,
  autoUpdate: false,
  pluginHost: false,
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: true,
}

export function createWebPlatform(): Platform {
  // Populated in subsequent tasks.
  return {
    caps: CAPS,
    relay: {} as any,
    sessions: {} as any,
    system: {} as any,
    events: {} as any,
    templates: {} as any,
    auxKeys: {} as any,
  }
}
