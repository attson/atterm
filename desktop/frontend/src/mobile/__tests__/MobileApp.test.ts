import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileApp from '../MobileApp.vue'
import type { RemoteSession } from '../../platform/types'

const mk = (id: string, host = 'box'): RemoteSession =>
  ({ session_id: id, host_id: 'h', host, user: 'me', title: id, cols: 80, rows: 24 })

const stubs = {
  MobileSetup: { name: 'MobileSetup', template: '<button data-testid="stub-connect" @click="$emit(\'connected\')"></button>' },
  MobileSessionList: {
    name: 'MobileSessionList',
    props: ['openSessionIds'],
    template: '<div data-testid="stub-list" :data-open="openSessionIds.join(\',\')"></div>',
  },
  MobileTerminalHost: {
    name: 'MobileTerminalHost',
    props: ['openTerminals', 'activeSessionId'],
    template: '<div data-testid="stub-host" :data-active="activeSessionId" :data-count="openTerminals.length"></div>',
  },
}

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false }
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

async function mountWith(loaded: boolean) {
  ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue(
    loaded ? { url: 'https://r', token: 'atk_t', allow_insecure_relay: false, remote_permission: 'full', connected: false } : null,
  )
  const w = mount(MobileApp, { global: { stubs } })
  await flushPromises()
  return w
}

describe('MobileApp navigation + keepalive', () => {
  it('boots to setup when no relay config', async () => {
    const w = await mountWith(false)
    expect(w.find('[data-testid="stub-connect"]').exists()).toBe(true)
  })

  it('boots to list when relay config present', async () => {
    const w = await mountWith(true)
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
  })

  it('connected event navigates setup → list', async () => {
    const w = await mountWith(false)
    await w.find('[data-testid="stub-connect"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
  })

  it('opening a session adds it to the keepalive registry and shows terminal host', async () => {
    const w = await mountWith(true)
    ;(w.findComponent({ name: 'MobileSessionList' }) as any).vm.$emit('open', mk('a'))
    await flushPromises()
    const host = w.find('[data-testid="stub-host"]')
    expect(host.exists()).toBe(true)
    expect(host.attributes('data-count')).toBe('1')
    expect(host.attributes('data-active')).toBe('a')
  })

  it('back returns to list but keeps terminals open', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    list().vm.$emit('open', mk('a'))
    await flushPromises()
    ;(w.findComponent({ name: 'MobileTerminalHost' }) as any).vm.$emit('back')
    await flushPromises()
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
    expect(w.find('[data-testid="stub-list"]').attributes('data-open')).toBe('a')
  })

  it('keeps the terminal host mounted on the list view so connections/clientID survive', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    list().vm.$emit('open', mk('a'))
    await flushPromises()
    ;(w.findComponent({ name: 'MobileTerminalHost' }) as any).vm.$emit('back')
    await flushPromises()
    // Host is hidden (v-show) but NOT unmounted — its SessionConnection stays
    // alive, so re-entering does not mint a new clientID and deadlock the driver.
    expect(w.findComponent({ name: 'MobileTerminalHost' }).exists()).toBe(true)
    expect(w.find('[data-testid="stub-host"]').attributes('data-count')).toBe('1')
  })

  it('LRU caps at 4 open terminals — opening a 5th evicts the oldest', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    const host = () => w.findComponent({ name: 'MobileTerminalHost' }) as any
    for (const id of ['a', 'b', 'c', 'd']) {
      list().vm.$emit('open', mk(id))
      await flushPromises()
      host().vm.$emit('back')
      await flushPromises()
    }
    list().vm.$emit('open', mk('e'))
    await flushPromises()
    expect(w.find('[data-testid="stub-host"]').attributes('data-count')).toBe('4')
  })

  it('switching tabs does not reorder the tab strip (active follows, order stays)', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    const host = () => w.findComponent({ name: 'MobileTerminalHost' }) as any
    for (const id of ['a', 'b', 'c']) {
      list().vm.$emit('open', mk(id))
      await flushPromises()
      host().vm.$emit('back')
      await flushPromises()
    }
    // open order is a,b,c; switch back to the first one
    list().vm.$emit('open', mk('a'))           // re-opening an existing session = switch
    await flushPromises()
    expect(w.find('[data-testid="stub-host"]').attributes('data-active')).toBe('a')
    host().vm.$emit('back')
    await flushPromises()
    // order must be preserved (old LRU code would have produced 'b,c,a')
    expect(w.find('[data-testid="stub-list"]').attributes('data-open')).toBe('a,b,c')
  })

  it('tokenInvalid resets to setup and clears open terminals', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    list().vm.$emit('open', mk('a'))
    await flushPromises()
    ;(w.findComponent({ name: 'MobileTerminalHost' }) as any).vm.$emit('tokenInvalid')
    await flushPromises()
    expect(w.find('[data-testid="stub-connect"]').exists()).toBe(true)
  })
})
