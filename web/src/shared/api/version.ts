import { apiFetch, ApiError } from './client'
import type { VersionResponse } from './types'
import { t } from '@shared/i18n'

type Translate = (key: string, params?: Record<string, string | number>) => string

export function formatVersionLabel(version: string, translate: Translate = t): string {
  return translate('common.versionLabel', { version: version || 'dev' })
}

// Version display is best-effort UI, not security-critical.
export async function fetchVersion(): Promise<string> {
  try {
    const { data } = await apiFetch<VersionResponse>('/api/version')
    return data.version
  } catch (e) {
    if (!(e instanceof ApiError)) throw e
    return 'dev'
  }
}

export async function fetchVersionLabel(translate: Translate = t): Promise<string> {
  return formatVersionLabel(await fetchVersion(), translate)
}
