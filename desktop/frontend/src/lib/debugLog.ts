// debugLog is an emergency on-screen logger for diagnosing the iOS
// "stuck on 配对中…/保存配置… 0.1s" issue where Vue reactivity / JS
// timers appear frozen but event handlers still fire.
//
// Writes go to:
//   1. console.log (Safari Web Inspector if connected)
//   2. A fixed <pre> at the bottom of <body>, mutated via direct DOM
//      assignment (textContent =) — bypasses Vue's update queue, so a
//      stalled reactive scheduler can't hide the log.
//
// Toggle visibility by tapping the dim "log" handle at the bottom-left.

const startTs = Date.now()

let panel: HTMLPreElement | null = null
let visible = true

function ensurePanel(): HTMLPreElement | null {
  if (typeof document === 'undefined') return null
  if (panel) return panel
  const pre = document.createElement('pre')
  pre.id = 'atterm-debug-log'
  pre.style.cssText = [
    'position:fixed',
    'bottom:0',
    'left:0',
    'right:0',
    'max-height:35vh',
    'overflow-y:auto',
    'background:rgba(0,0,0,0.88)',
    'color:#0f9',
    'font-size:10px',
    'padding:6px 8px',
    'margin:0',
    'z-index:99999',
    'font-family:ui-monospace,SFMono-Regular,monospace',
    'line-height:1.35',
    'white-space:pre-wrap',
    'pointer-events:auto',
  ].join(';')
  document.body.appendChild(pre)

  // Toggle handle. Small dim square at bottom-left corner.
  const toggle = document.createElement('button')
  toggle.id = 'atterm-debug-toggle'
  toggle.textContent = 'log'
  toggle.style.cssText = [
    'position:fixed',
    'bottom:0',
    'left:0',
    'z-index:100000',
    'background:rgba(0,0,0,0.6)',
    'color:#999',
    'border:none',
    'padding:6px 10px',
    'font-size:10px',
    'font-family:ui-monospace,SFMono-Regular,monospace',
  ].join(';')
  toggle.onclick = () => {
    visible = !visible
    if (panel) panel.style.display = visible ? 'block' : 'none'
  }
  document.body.appendChild(toggle)

  panel = pre
  return panel
}

export function debugLog(msg: string): void {
  const elapsed = Date.now() - startTs
  const tag = `[${String(elapsed).padStart(6, ' ')}ms]`
  // eslint-disable-next-line no-console
  console.log('[AT Term debug]', tag, msg)
  const p = ensurePanel()
  if (!p) return
  // Direct DOM mutation — synchronous, never queued via Vue.
  p.textContent = (p.textContent || '') + tag + ' ' + msg + '\n'
  // Keep latest visible.
  p.scrollTop = p.scrollHeight
}

export function debugReset(): void {
  if (panel) panel.textContent = ''
}
