import type { Platform } from '../platform/types'

export interface CapacitorPrefsSyncLike {
  pull(): Promise<void>
}

export interface CapacitorAppStateSource {
  addListener(event: 'appStateChange', handler: (state: { isActive: boolean }) => void): unknown
}

export function bindCapacitorPrefsSync(
  platform: Pick<Platform, 'events'>,
  prefsSync: CapacitorPrefsSyncLike,
  appStateSource: CapacitorAppStateSource,
): void {
  const pullAndNotify = () => {
    void prefsSync.pull()
      .then(() => platform.events.emit('prefs:changed', undefined))
      .catch(() => {})
  }

  pullAndNotify()
  appStateSource.addListener('appStateChange', (state) => {
    if (state.isActive) pullAndNotify()
  })
  platform.events.on('relay:auth-restored', pullAndNotify)
  platform.events.on('prefs:remote-changed', pullAndNotify)
}
