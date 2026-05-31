import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import SettingsDiagnostics from '../SettingsDiagnostics.vue'
import * as api from '../../lib/api'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

function fakePayload(): api.DiagnosticsPayload {
  return {
    generated_at: '2026-06-01T12:00:00Z',
    app_version: 'v0.4.0',
    os: 'darwin', arch: 'arm64', os_version: '14.6.1',
    webview_summary: 'WKWebView (Safari/17.5)',
    user_agent: 'mock-ua',
    relay_url: 'https://relay.example.com',
    relay_status: 'connected',
    relay_token_redacted: 'atk_AbCdEfGh…',
    allow_insecure_relay: false,
    remote_permission: 'full',
    uplink_paused: false,
    recent_relay_errors: [],
    config: {
      default_shell: '/bin/zsh', locale: 'system', terminal_theme: 'default',
      notifications_enabled: true, shell_integration_enabled: true,
      webgl_renderer_enabled: true, logging_enabled: true,
      log_file_path: '/tmp/atterm.log', auto_check_updates: true,
      command_notify_threshold_seconds: 10,
    },
  }
}

describe('SettingsDiagnostics', () => {
  beforeEach(() => {
    vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    vi.spyOn(api, 'exportDiagnostics').mockResolvedValue('/tmp/diag.txt')
  })

  it('renders the formatted text after mount', async () => {
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    expect(w.find('[data-testid="diag-preview"]').text()).toContain('App version:')
    expect(w.find('[data-testid="diag-preview"]').text()).toContain('v0.4.0')
  })

  it('passes navigator.userAgent to getDiagnostics on mount', async () => {
    const spy = vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    mount(SettingsDiagnostics)
    await flushPromises()
    expect(spy).toHaveBeenCalledWith(expect.any(String))
  })

  it('Copy button writes the formatted text to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText }, userAgent: 'test-ua' })
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    await w.find('[data-testid="diag-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledOnce()
    expect(writeText.mock.calls[0][0]).toContain('App version:')
  })

  it('Export button calls exportDiagnostics with the formatted text', async () => {
    const exportSpy = vi.spyOn(api, 'exportDiagnostics').mockResolvedValue('/tmp/diag.txt')
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    await w.find('[data-testid="diag-export"]').trigger('click')
    await flushPromises()
    expect(exportSpy).toHaveBeenCalledOnce()
    expect(exportSpy.mock.calls[0][0]).toContain('App version:')
  })

  it('Refresh button re-fetches the payload', async () => {
    const getSpy = vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    expect(getSpy).toHaveBeenCalledTimes(1)
    await w.find('[data-testid="diag-refresh"]').trigger('click')
    await flushPromises()
    expect(getSpy).toHaveBeenCalledTimes(2)
  })
})
