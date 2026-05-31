import { describe, it, expect } from 'vitest'
import { formatDiagnostics } from '../diagnostics'
import type { DiagnosticsPayload } from '../api'

function baseline(): DiagnosticsPayload {
  return {
    generated_at: '2026-06-01T12:00:00Z',
    app_version: 'v0.4.0',
    os: 'darwin', arch: 'arm64', os_version: '14.6.1',
    webview_summary: 'WKWebView (Safari/17.5)',
    user_agent: 'Mozilla/5.0',
    relay_url: 'https://relay.example.com',
    relay_status: 'connected',
    relay_token_redacted: 'atk_AbCdEfGh…',
    allow_insecure_relay: false,
    remote_permission: 'full',
    uplink_paused: false,
    recent_relay_errors: [],
    config: {
      default_shell: '/bin/zsh',
      locale: 'system',
      terminal_theme: 'default',
      notifications_enabled: true,
      shell_integration_enabled: true,
      webgl_renderer_enabled: true,
      logging_enabled: true,
      log_file_path: '/tmp/atterm.log',
      auto_check_updates: true,
      command_notify_threshold_seconds: 10,
    },
  }
}

describe('formatDiagnostics', () => {
  it('renders a header with the generated_at timestamp', () => {
    const out = formatDiagnostics(baseline())
    expect(out.startsWith('atterm desktop diagnostics — 2026-06-01T12:00:00Z')).toBe(true)
  })

  it('emits "(none)" when there are no relay errors', () => {
    expect(formatDiagnostics(baseline())).toContain('(none)')
  })

  it('lists each recent relay error on its own line, newest first', () => {
    const p = baseline()
    p.recent_relay_errors = [
      { timestamp: '2026-06-01T11:59:00Z', message: 'dial failed' },
      { timestamp: '2026-06-01T11:58:00Z', message: 'auth_invalid_token' },
    ]
    const out = formatDiagnostics(p)
    const dialIdx = out.indexOf('dial failed')
    const authIdx = out.indexOf('auth_invalid_token')
    expect(dialIdx).toBeGreaterThan(0)
    expect(authIdx).toBeGreaterThan(dialIdx)
  })

  it('marks unknown WebView as (unknown)', () => {
    const p = baseline()
    p.webview_summary = ''
    expect(formatDiagnostics(p)).toContain('WebView:')
    expect(formatDiagnostics(p)).toContain('(unknown)')
  })

  it('writes the host-only relay URL', () => {
    const out = formatDiagnostics(baseline())
    expect(out).toContain('https://relay.example.com')
    expect(out).not.toContain('?')
  })

  it('marks insecure HTTP as yes when allowed', () => {
    const p = baseline()
    p.allow_insecure_relay = true
    expect(formatDiagnostics(p)).toContain('Allow insecure HTTP:    yes')
  })

  it('writes (not configured) when relay_url is empty', () => {
    const p = baseline()
    p.relay_url = ''
    expect(formatDiagnostics(p)).toContain('(not configured)')
  })
})
