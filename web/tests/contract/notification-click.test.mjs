import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const PUBLIC_SCRIPT_PATH = path.resolve(__dirname, '../../public/notification-click.js')
const DIST_SW_PATH = path.resolve(__dirname, '../../dist/sw.js')

test('notification click worker extension routes push clicks to session deep links', () => {
  assert.ok(existsSync(PUBLIC_SCRIPT_PATH), 'expected web/public/notification-click.js to exist')
  const script = readFileSync(PUBLIC_SCRIPT_PATH, 'utf8')
  assert.match(script, /addEventListener\(['"]push['"]/, 'script must render incoming push payloads')
  assert.match(script, /addEventListener\(['"]notificationclick['"]/, 'script must handle notification clicks')
  assert.match(script, /clickUrl/, 'script must prefer payload data.clickUrl')
  assert.match(script, /sessionId/, 'script must fall back to payload data.sessionId')
  assert.match(script, /clients\.matchAll/, 'script must focus an existing window when possible')
  assert.match(script, /clients\.openWindow/, 'script must open a session route when no window exists')
})

test('generated service worker imports notification click worker extension', () => {
  assert.ok(existsSync(DIST_SW_PATH), 'expected web/dist/sw.js to be present - run `npm run build` first')
  const sw = readFileSync(DIST_SW_PATH, 'utf8')
  assert.match(sw, /notification-click\.js/, 'dist service worker must import notification-click.js')
})
