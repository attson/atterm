// notification-click.js — service-worker importScripts target. The
// relay's plaintext title/body is always present as a fallback so
// browsers without the M6-sw client update (or with no client open
// to answer the decrypt request) still get a usable push.
//
// When the payload carries `sealedBody` (M6-foundation, v0.2.108)
// the SW asks any visible page client to decrypt with the unlocked
// account_key and renders the parsed `{label, exit_code, elapsed_ms}`
// as a rich title/body. Decryption is best-effort; on timeout or
// no-client we silently keep the relay-composed plaintext.
//
// Keep this script dependency-free — vite-plugin-pwa importScripts
// it directly into the generated workbox SW, no bundler runs over it.

const ATTERM_DECRYPT_REQUEST_TYPE = 'atterm-decrypt-sealed-body'
const ATTERM_DECRYPT_TIMEOUT_MS = 800

self.addEventListener('push', (event) => {
  event.waitUntil(handlePush(event))
})

async function handlePush(event) {
  let payload = {}
  try {
    payload = event.data ? event.data.json() : {}
  } catch {
    payload = {}
  }

  let title = payload.title || 'AT Term'
  let body = payload.body || ''

  const sealedBody = payload.sealedBody
  const sessionId = payload.data && payload.data.sessionId
  if (typeof sealedBody === 'string' && sealedBody.length > 0 && typeof sessionId === 'string') {
    const fields = await tryDecryptSealedBody(sealedBody, sessionId)
    if (fields) {
      const label = fields.label || 'session'
      title = `AT Term · ${label}`
      const exit = typeof fields.exit_code === 'number' ? fields.exit_code : 0
      body = `Command finished · exit ${exit} · ${formatElapsed(fields.elapsed_ms)}`
    }
  }

  const options = {
    body,
    tag: payload.tag,
    data: payload.data || {},
  }

  await self.registration.showNotification(title, options)
}

async function tryDecryptSealedBody(sealedBody, sessionId) {
  let allClients = []
  try {
    allClients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  } catch {
    return null
  }
  for (const c of allClients) {
    const fields = await askClient(c, sealedBody, sessionId)
    if (fields) return fields
  }
  return null
}

function askClient(client, sealedBody, sessionId) {
  return new Promise((resolve) => {
    let settled = false
    const finish = (v) => {
      if (settled) return
      settled = true
      resolve(v)
    }
    const channel = new MessageChannel()
    const timer = setTimeout(() => finish(null), ATTERM_DECRYPT_TIMEOUT_MS)
    channel.port1.onmessage = (e) => {
      clearTimeout(timer)
      const data = e && e.data
      finish(data && data.fields ? data.fields : null)
    }
    try {
      client.postMessage(
        { type: ATTERM_DECRYPT_REQUEST_TYPE, sealedBody, sessionId },
        [channel.port2],
      )
    } catch {
      clearTimeout(timer)
      finish(null)
    }
  })
}

function formatElapsed(ms) {
  const safe = Math.max(0, (ms | 0))
  const sec = Math.floor(safe / 1000)
  if (sec < 60) return `${sec}s`
  return `${Math.floor(sec / 60)}m${sec % 60}s`
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()

  const data = event.notification.data || {}
  const target = data.clickUrl || (data.sessionId ? `/#/s/${data.sessionId}` : '/')
  const targetURL = new URL(target, self.location.origin).href

  event.waitUntil((async () => {
    const windows = await clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const client of windows) {
      if (!('focus' in client)) continue
      await client.focus()
      if ('navigate' in client) {
        return client.navigate(targetURL)
      }
      return undefined
    }
    return clients.openWindow(targetURL)
  })())
})
