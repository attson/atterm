import type { DiagnosticsPayload } from './api'

/**
 * Renders a DiagnosticsPayload as a multi-line plaintext block suitable
 * for pasting into an issue or chat. Single space between colon and
 * value; label column padded to 24 chars.
 */
export function formatDiagnostics(p: DiagnosticsPayload): string {
  const pad = (k: string) => k.padEnd(24, ' ')
  const lines: string[] = [
    `atterm desktop diagnostics — ${p.generated_at}`,
    '--------------------------------------------------',
    pad('App version:') + p.app_version,
    pad('OS:') + `${p.os} ${p.os_version} (${p.arch})`.replace(/\s+/g, ' ').trim(),
    pad('WebView:') + (p.webview_summary || '(unknown)'),
    pad('Relay URL:') + (p.relay_url || '(not configured)'),
    pad('Relay status:') + p.relay_status,
    pad('Relay token:') + (p.relay_token_redacted || '(none)'),
    pad('Allow insecure HTTP:') + (p.allow_insecure_relay ? 'yes' : 'no'),
    pad('Remote permission:') + p.remote_permission,
    pad('Uplink paused:') + (p.uplink_paused ? 'yes' : 'no'),
    '',
    'Recent relay errors (most recent first):',
    ...(p.recent_relay_errors.length === 0
      ? ['  (none)']
      : p.recent_relay_errors.map(e => `  ${e.timestamp}  ${e.message}`)),
    '',
    'Config summary:',
    pad('  Default shell:') + p.config.default_shell,
    pad('  Locale:') + p.config.locale,
    pad('  Theme:') + p.config.terminal_theme,
    pad('  Notifications:') + (p.config.notifications_enabled ? 'enabled' : 'disabled'),
    pad('  Shell integration:') + (p.config.shell_integration_enabled ? 'enabled' : 'disabled'),
    pad('  WebGL renderer:') + (p.config.webgl_renderer_enabled ? 'enabled' : 'disabled'),
    pad('  Logging:') + (p.config.logging_enabled
      ? `enabled (${p.config.log_file_path})`
      : 'disabled'),
    pad('  Auto-check updates:') + (p.config.auto_check_updates ? 'enabled' : 'disabled'),
    pad('  Command notify:') + `${p.config.command_notify_threshold_seconds}s`,
  ]
  return lines.join('\n')
}
