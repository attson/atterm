// CACHE must contain the 8-hex prefix of sha256 over (path + content) for
// every entry in ASSETS, in order. web/sw-cache-bump.test.mjs enforces this
// and prints the expected hash on mismatch — paste it in. Without the bump
// the install-event re-fetches the same names but cache-first wins, so
// clients keep serving the old file (see PR #34 incident).
const CACHE = "at-term-web-803ed9bd";
const ASSETS = [
  "./",
  "./admin/admin-invitations.js",
  "./admin/admin-users.js",
  "./admin/admin.js",
  "./app-core.js",
  "./app.js",
  "./layout.js",
  "./style.css",
  "./vendor/xterm/xterm.css",
  "./vendor/xterm/xterm.js",
  "./vendor/xterm-addon-fit/xterm-addon-fit.js",
  "./manifest.webmanifest",
  "./icon.png",
  "./icon.svg",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE)
      .then((cache) => cache.addAll(ASSETS))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== "GET" || url.origin !== location.origin || url.pathname.startsWith("/api/")) {
    return;
  }
  if (req.mode === "navigate") {
    event.respondWith(fetch(req).catch(() => caches.match("./")));
    return;
  }
  event.respondWith(caches.match(req, { ignoreSearch: true }).then((cached) => cached || fetch(req)));
});

self.addEventListener("push", (event) => {
  event.waitUntil((async () => {
    let payload = { title: "AT Term", body: "Command finished." };
    try {
      if (event.data) {
        payload = { ...payload, ...event.data.json() };
      }
    } catch (_err) {
      // keep fallback
    }
    const { title, body, tag, data } = payload;
    const options = {};
    if (body !== undefined) options.body = body;
    if (tag !== undefined) options.tag = tag;
    if (data !== undefined) options.data = data;
    await self.registration.showNotification(title, options);
  })());
});
