import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const attach = vi.fn()
const detach = vi.fn()
const sendInput = vi.fn()
const sendResize = vi.fn()
const claimDriver = vi.fn()
const sendPasteImage = vi.fn().mockResolvedValue(true)
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
    sendPasteImage(blob: Blob, filename: string) { return sendPasteImage(blob, filename) }
  },
  pasteImageBlockReason: vi.fn().mockReturnValue(null),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => ({
    templates: {
      load: vi.fn().mockResolvedValue([]),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
    },
  }),
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

    // Taking control auto-enables control mode so the next tap is usable.
    const toggle = w.find('[data-testid="mobile-control-toggle"]').element as HTMLInputElement
    expect(toggle.checked).toBe(true)
  })

  it('blocks pointer events on .term while viewing so iOS taps cannot fall through to xterm', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Driver by default → no blocking.
    expect(w.find('.term').classes()).not.toContain('inert')

    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    // Viewing now → .term must be pointer-inert so taps land on the overlay button.
    expect(w.find('.term').classes()).toContain('inert')

    lastHandlers.onDriverChange?.('me', true, '')
    await w.vm.$nextTick()
    // Back to driver → interactive again.
    expect(w.find('.term').classes()).not.toContain('inert')
  })

  it('image button delegates the chosen file to sendPasteImage when controlMode is on', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // canSend gate: must be driver (default) + controlMode on. Toggle it on.
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)

    const btn = w.find('[data-testid="mobile-image"]')
    expect(btn.exists()).toBe(true)
    expect(btn.classes()).not.toContain('inert')

    const fileInput = w.find('[data-testid="mobile-image-input"]')
    expect(fileInput.exists()).toBe(true)
    expect(fileInput.attributes('accept')).toContain('image/')

    // Simulate the native picker returning a PNG.
    const file = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'snap.png', { type: 'image/png' })
    const inputEl = fileInput.element as HTMLInputElement
    Object.defineProperty(inputEl, 'files', { value: [file], configurable: true })
    await fileInput.trigger('change')

    expect(sendPasteImage).toHaveBeenCalledWith(file, 'snap.png')
  })

  it('image button is inert when not in control mode', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // controlMode is false by default — image button must be inert (still
    // rendered + tap-able so we can flash the protect banner).
    expect(w.find('[data-testid="mobile-image"]').classes()).toContain('inert')
  })

  it('image button is inert for view-only sessions', () => {
    const viewOnly = { ...info, remote_permission: 'view' }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: viewOnly, active: true } })
    const btn = w.find('[data-testid="mobile-image"]')
    if (btn.exists()) {
      expect(btn.classes()).toContain('inert')
    }
  })

  it('renders a mobile control panel with required keys', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-control-panel"]').exists()).toBe(true)
    for (const id of ['enter', 'esc', 'tab', 'ctrl-c', 'ctrl-d', 'arrow-up', 'arrow-down', 'arrow-left', 'arrow-right']) {
      expect(w.find(`[data-testid="mobile-key-${id}"]`).exists()).toBe(true)
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

  it('sends Ctrl-D through the control panel', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-ctrl-d"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\x04')
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

  it('marks control buttons inert and hides take-control for view-only sessions', async () => {
    const viewInfo = { ...info, remote_permission: 'view' as const }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: viewInfo, active: true } })
    expect(w.find('[data-testid="mobile-view-only"]').exists()).toBe(true)
    expect(w.find('[data-testid="mobile-key-enter"]').classes()).toContain('inert')
    expect((w.find('[data-testid="mobile-control-toggle"]').element as HTMLInputElement).disabled).toBe(true)
    await w.find('[data-testid="mobile-key-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(false)
  })

  it('mounts the template bar with templates loaded from platform.templates', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    // The defaults seed includes default-y; mock platform returns [] which falls back to DEFAULT_TEMPLATES.
    expect(w.find('[data-testid="template-bar"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-btn-default-y"]').exists()).toBe(true)
  })

  it('opens the preview dialog when a template button is clicked', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await flushPromises()
    await w.find('[data-testid="template-btn-default-y"]').trigger('click')
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-preview"]').text()).toContain('y')
  })

  it('sends template text plus CR when the preview Send button is clicked', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await flushPromises()
    await w.find('[data-testid="template-btn-default-yes"]').trigger('click')
    await w.find('[data-testid="template-preview-confirm"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('yes\r')
  })

  it('does not render the legacy QUICK_TEXTS row', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-quick-y"]').exists()).toBe(false)
    expect(w.find('[data-testid="mobile-quick-continue"]').exists()).toBe(false)
  })

  it('renders the protect-mode banner when controlMode is off and the user is an eligible driver', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Default: driver + canControl + controlMode === false → banner shown.
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(true)
  })

  it('hides the protect-mode banner once controlMode is enabled', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('does not render the protect-mode banner for view-only sessions (view-only banner covers it)', () => {
    const viewInfo = { ...info, remote_permission: 'view' as const }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: viewInfo, active: true } })
    expect(w.find('[data-testid="mobile-view-only"]').exists()).toBe(true)
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('does not render the protect-mode banner while the user is a viewer (viewer overlay covers it)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('shakes the protect-mode banner when an inert aux key is tapped', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).not.toContain('shaking')
    await w.find('[data-testid="mobile-key-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when an inert template button is tapped', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="template-btn-default-y"]').trigger('click')
    // Preview dialog must NOT open while protect mode blocks input.
    expect(w.find('[data-testid="template-preview"]').exists()).toBe(false)
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when the user taps the terminal area', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('.term').trigger('pointerdown')
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })
})
