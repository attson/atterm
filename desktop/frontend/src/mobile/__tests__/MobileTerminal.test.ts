import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

const attach = vi.fn()
const detach = vi.fn()
const sendInput = vi.fn()
const sendResize = vi.fn()
const claimDriver = vi.fn()
let lastHandlers: any = null
let lastArgs: any = null

vi.mock('../../lib/connection', () => ({
  SessionConnection: class {
    constructor(endpoint: any, sessionId: string, handlers: any) {
      lastArgs = { endpoint, sessionId }
      lastHandlers = handlers
    }
    attach() { attach() }
    detach() { detach() }
    sendInput(s: string) { sendInput(s) }
    sendResize(c: number, r: number) { sendResize(c, r) }
    claimDriver() { claimDriver() }
  },
}))

const termWrite = vi.fn()
const termDispose = vi.fn()
const termFit = vi.fn()
vi.mock('xterm', () => ({
  Terminal: class {
    options: Record<string, unknown> = {}
    cols = 80
    rows = 24
    onData(cb: (s: string) => void) { (this as any)._onData = cb }
    onResize() {}
    open() {}
    write(d: unknown) { termWrite(d) }
    dispose() { termDispose() }
    focus() {}
    loadAddon() {}
  },
}))
vi.mock('xterm-addon-fit', () => ({
  FitAddon: class { fit() { termFit() } activate() {} },
}))
vi.mock('xterm-addon-webgl', () => ({
  WebglAddon: class { onContextLoss() {} dispose() {} activate() {} },
}))

import MobileTerminal from '../MobileTerminal.vue'
import type { RemoteSession } from '../../platform/types'

const info: RemoteSession = { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24 }

beforeEach(() => { vi.clearAllMocks(); lastHandlers = null; lastArgs = null })

describe('MobileTerminal', () => {
  it('creates SessionConnection with endpoint+sessionId and attaches on mount', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(lastArgs).toEqual({ endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1' })
    expect(attach).toHaveBeenCalledOnce()
  })

  it('writes incoming output to the terminal', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onOutput?.(new Uint8Array([104, 105]))
    expect(termWrite).toHaveBeenCalled()
  })

  it('emits ended on CLOSE', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onClose?.({ exit_code: 0 })
    expect(w.emitted('ended')).toBeTruthy()
  })

  it('emits tokenInvalid on error status', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onStatus?.('error')
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })

  it('forwards cwd/title META updates via the meta event', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onMeta?.({ cwd: '/Users/me/proj', title: 'vim' })
    expect(w.emitted('meta')![0]).toEqual([{ cwd: '/Users/me/proj', title: 'vim' }])
  })

  it('detaches + disposes on unmount', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    w.unmount()
    expect(detach).toHaveBeenCalledOnce()
    expect(termDispose).toHaveBeenCalledOnce()
  })

  it('pushes its size to the PTY when it becomes the driver', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    sendResize.mockClear() // ignore the mount-time fit resize
    lastHandlers.onDriverChange?.('me', true, '')
    expect(sendResize).toHaveBeenCalledWith(80, 24)
  })

  it('shows viewer overlay when not driver; take-control calls claimDriver', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(false)

    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(true)

    await w.find('[data-testid="mobile-take-control"]').trigger('click')
    expect(claimDriver).toHaveBeenCalled()

    lastHandlers.onDriverChange?.('me', true, '')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(false)
  })

  it('renders a mobile control panel with required keys and quick text buttons', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-control-panel"]').exists()).toBe(true)
    for (const id of ['enter', 'esc', 'tab', 'ctrl-c', 'ctrl-d', 'arrow-up', 'arrow-down', 'arrow-left', 'arrow-right']) {
      expect(w.find(`[data-testid="mobile-key-${id}"]`).exists()).toBe(true)
    }
    for (const text of ['y', 'n', 'yes', 'no', 'continue']) {
      expect(w.find(`[data-testid="mobile-quick-${text}"]`).exists()).toBe(true)
    }
  })

  it('requires explicit control mode before shortcut buttons send input', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-key-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-enter"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\r')
  })

  it('sends Ctrl-D and quick text through the control panel', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-ctrl-d"]').trigger('click')
    await w.find('[data-testid="mobile-quick-continue"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\x04')
    expect(sendInput).toHaveBeenCalledWith('continue\r')
  })

  it('asks for paste confirmation before sending clipboard text', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-paste"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    await w.find('[data-testid="mobile-paste-confirm-panel"] textarea').setValue('paste me')
    await w.find('[data-testid="mobile-paste-confirm"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('paste me')
  })

  it('disables control buttons and take-control for view-only sessions', async () => {
    const viewInfo = { ...info, remote_permission: 'view' as const }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: viewInfo, active: true } })
    expect(w.find('[data-testid="mobile-view-only"]').exists()).toBe(true)
    expect(w.find('[data-testid="mobile-key-enter"]').attributes('disabled')).toBeDefined()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(false)
  })
})
