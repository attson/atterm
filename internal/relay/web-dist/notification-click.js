self.addEventListener('push', (event) => {
  let payload = {}
  try {
    payload = event.data ? event.data.json() : {}
  } catch {
    payload = {}
  }

  const title = payload.title || 'AT Term'
  const options = {
    body: payload.body || '',
    tag: payload.tag,
    data: payload.data || {},
  }

  event.waitUntil(self.registration.showNotification(title, options))
})

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
