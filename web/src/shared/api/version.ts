import { apiFetch, ApiError } from './client'
import type { VersionResponse } from './types'

// fetchVersionLabel returns the display string for the page footer.
// Falls back to "version dev" on any failure — version display is
// best-effort UI, not security-critical.
export async function fetchVersionLabel(): Promise<string> {
  try {
    const { data } = await apiFetch<VersionResponse>('/api/version')
    return `version ${data.version}`
  } catch (e) {
    if (!(e instanceof ApiError)) throw e
    return 'version dev'
  }
}
