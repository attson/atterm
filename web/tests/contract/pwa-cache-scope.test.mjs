import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const SW_PATH = path.resolve(__dirname, '../../dist/sw.js')
const ALLOWED_EXT = ['js', 'css', 'html', 'svg', 'png', 'ico', 'woff2', 'webmanifest']
const ALLOWED_RE = new RegExp('\\.(?:' + ALLOWED_EXT.join('|') + ')$')

test('dist/sw.js exists after a build', () => {
  assert.ok(existsSync(SW_PATH), 'expected web/dist/sw.js to be present — run `npm run build` first')
})

test('precache manifest only lists static asset extensions', () => {
  const sw = readFileSync(SW_PATH, 'utf8')
  // Extract every `url:"…"` substring from the precacheAndRoute call.
  const urls = []
  const re = /url:\s*"([^"]+)"/g
  let m
  while ((m = re.exec(sw)) !== null) {
    urls.push(m[1])
  }
  assert.ok(urls.length > 0, 'expected at least one precache entry')
  for (const url of urls) {
    assert.match(url, ALLOWED_RE, `precache entry ${url} has an extension outside Sec-5 globPatterns`)
  }
})

test('no runtimeCaching directives in the SW', () => {
  const sw = readFileSync(SW_PATH, 'utf8')
  assert.doesNotMatch(sw, /registerRoute\s*\(/, 'runtimeCaching must be empty per Sec-5')
  assert.doesNotMatch(sw, /workbox\.routing\.registerRoute/, 'runtimeCaching must be empty per Sec-5')
})

test('no navigateFallback configured', () => {
  const sw = readFileSync(SW_PATH, 'utf8')
  // MPA — navigateFallback must be null. Workbox uses NavigationRoute when set.
  assert.doesNotMatch(sw, /NavigationRoute/, 'navigateFallback must be null per Sec-5 (MPA, no SPA shell)')
})
