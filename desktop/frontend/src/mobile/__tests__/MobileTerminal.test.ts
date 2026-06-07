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

const eventHandlers = new Map<string, (data: unknown) => void>()
const eventsOn = vi.fn((evt: string, h: (data: unknown) => void) => {
  eventHandlers.set(evt, h)
  return () => eventHandlers.delete(evt)
})
vi.mock('../../platform', () => ({
  usePlatform: () => ({
    templates: {
      load: vi.fn().mockResolvedValue([]),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      loadHidden: vi.fn().mockResolvedValue(false),
      saveHidden: vi.fn().mockResolvedValue(undefined),
    },
    auxKeys: {
      load: vi.fn().mockResolvedValue([]),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
    },
    events: {
      on: (evt: string, h: (data: unknown) => void) => eventsOn(evt, h),
      emit: vi.fn(),
    },
  }),
}))

// jsdom has no ResizeObserver; MobileTerminal registers one to re-fit.
vi.stubGlobal('ResizeObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

const termWrite = vi.fn()
const termDispose = vi.fn()
const termFit = vi.fn()
const termResize = vi.fn()
let lastTerm: any = null
vi.mock('xterm', () => ({
  Terminal: class {
    options: Record<string, unknown> = {}
    cols = 80
    rows = 24
    textarea = document.createElement('textarea')
    constructor() { lastTerm = this }
    onData(cb: (s: string) => void) { (this as any)._onData = cb }
    onResize() {}
    open() {}
    write(d: unknown) { termWrite(d) }
    dispose() { termDispose() }
    focus() {}
    loadAddon() {}
    resize(c: number, r: number) { termResize(c, r) }
  },
}))
vi.mock('xterm-addon-fit', () => ({
  FitAddon: class { fit() { termFit() } activate() {} },
}))
vi.mock('xterm-addon-webgl', () => ({
  WebglAddon: class { onContextLoss() {} dispose() {} activate() {} },
}))

const getPhoto = vi.fn()
vi.mock('@capacitor/camera', () => ({
  Camera: { getPhoto: (...a: unknown[]) => getPhoto(...a) },
  CameraSource: { Prompt: 'PROMPT' },
  CameraResultType: { Base64: 'base64' },
}))

const hideKeyboard = vi.fn().mockResolvedValue(undefined)
vi.mock('@capacitor/keyboard', () => ({
  Keyboard: { hide: (...a: unknown[]) => hideKeyboard(...a) },
}))

import MobileTerminal from '../MobileTerminal.vue'
import type { RemoteSession } from '../../platform/types'

const info: RemoteSession = { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24 }

beforeEach(() => { vi.clearAllMocks(); lastHandlers = null; lastArgs = null; eventHandlers.clear() })

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

  it('viewer locks the grid to the PTY cols/rows from META (avoids overflow)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Become a viewer (not driver), then receive a wide PTY size.
    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    termResize.mockClear()
    lastHandlers.onMeta?.({ cols: 213, rows: 50 })
    expect(termResize).toHaveBeenCalledWith(213, 50)
  })

  it('driver does NOT lock to META cols/rows (it fits its own viewport)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Driver by default; a META with a wide size must not resize our grid.
    termResize.mockClear()
    lastHandlers.onMeta?.({ cols: 213, rows: 50 })
    expect(termResize).not.toHaveBeenCalled()
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

  it('image button sends the picked photo via sendPasteImage when in control mode', async () => {
    getPhoto.mockResolvedValue({ base64String: btoa('PNGDATA'), format: 'png' })
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)

    const btn = w.find('[data-testid="mobile-image"]')
    expect(btn.exists()).toBe(true)
    expect(btn.classes()).not.toContain('inert')

    await btn.trigger('click')
    await flushPromises()

    expect(getPhoto).toHaveBeenCalled()
    expect(sendPasteImage).toHaveBeenCalled()
    const [file, name] = (sendPasteImage as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(name).toBe('mobile-image.png')
    expect(file).toBeInstanceOf(File)
  })

  it('image button does not invoke the camera when not in control mode', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    // controlMode is false by default — button is inert and tapping it only
    // flashes the protect banner, never opens the camera.
    expect(w.find('[data-testid="mobile-image"]').classes()).toContain('inert')
    await w.find('[data-testid="mobile-image"]').trigger('click')
    expect(getPhoto).not.toHaveBeenCalled()
  })

  it('image button is inert for view-only sessions', async () => {
    const viewOnly = { ...info, remote_permission: 'view' }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: viewOnly, active: true } })
    await flushPromises()
    const btn = w.find('[data-testid="mobile-image"]')
    if (btn.exists()) {
      expect(btn.classes()).toContain('inert')
    }
  })

  it('renders a mobile control panel with the default aux keys', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    expect(w.find('[data-testid="mobile-control-panel"]').exists()).toBe(true)
    for (const id of ['aux-enter', 'aux-esc', 'aux-tab', 'aux-ctrl-c', 'aux-ctrl-d', 'aux-up', 'aux-down', 'aux-left', 'aux-right']) {
      expect(w.find(`[data-testid="mobile-key-${id}"]`).exists()).toBe(true)
    }
  })

  it('requires explicit control mode before shortcut buttons send input', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\r')
  })

  it('clicking the terminal area blurs the focused xterm textarea and hides the iOS keyboard', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    document.body.appendChild(lastTerm.textarea)
    const blur = vi.spyOn(lastTerm.textarea, 'blur')
    lastTerm.textarea.focus()
    expect(document.activeElement).toBe(lastTerm.textarea)

    await w.find('.term').trigger('pointerdown')

    expect(blur).toHaveBeenCalledOnce()
    expect(hideKeyboard).toHaveBeenCalledOnce()
    expect(sendInput).not.toHaveBeenCalled()
    lastTerm.textarea.remove()
  })

  it('does not hide the keyboard when the xterm textarea is not focused', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })

    await w.find('.term').trigger('pointerdown')

    expect(hideKeyboard).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('forwards non-composition IME insertText (punctuation/space/digit) xterm drops', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true) // canSend
    sendInput.mockClear()
    for (const ch of ['，', ' ', '1']) {
      lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: ch, inputType: 'insertText', isComposing: false } as any))
    }
    expect(sendInput.mock.calls.map((c: unknown[]) => c[0])).toEqual(['，', ' ', '1'])
  })

  it('does NOT hijack composition input (pinyin→Hanzi stays with xterm)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    sendInput.mockClear()
    // composition-in-progress + the committed-composition input must be ignored
    lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: 'ni', inputType: 'insertCompositionText', isComposing: true } as any))
    lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: '你', inputType: 'insertCompositionText', isComposing: false } as any))
    expect(sendInput).not.toHaveBeenCalled()
  })

  it('sends Ctrl-D through the control panel', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-aux-ctrl-d"]').trigger('click')
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
    await flushPromises()
    expect(w.find('[data-testid="mobile-view-only"]').exists()).toBe(true)
    expect(w.find('[data-testid="mobile-key-aux-enter"]').classes()).toContain('inert')
    expect((w.find('[data-testid="mobile-control-toggle"]').element as HTMLInputElement).disabled).toBe(true)
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-take-control"]').exists()).toBe(false)
  })

  it('mounts the template bar with templates loaded from platform.templates', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    // The defaults seed includes default-yes; mock platform returns [] which falls back to DEFAULT_TEMPLATES.
    expect(w.find('[data-testid="template-bar"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-btn-default-yes"]').exists()).toBe(true)
  })

  it('clicking a template button sends the text and a standalone Enter one tick later (no preview)', async () => {
    vi.useFakeTimers()
    try {
      const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      await flushPromises()
      await w.find('[data-testid="template-btn-default-yes"]').trigger('click')
      expect(w.find('[data-testid="template-preview"]').exists()).toBe(false)
      // Text first, no trailing CR — bundling them would make Codex read the
      // payload as a paste and the CR would become a literal newline.
      expect(sendInput).toHaveBeenCalledTimes(1)
      expect(sendInput).toHaveBeenLastCalledWith('yes')
      vi.runOnlyPendingTimers()
      expect(sendInput).toHaveBeenCalledTimes(2)
      expect(sendInput).toHaveBeenLastCalledWith('\r')
    } finally {
      vi.useRealTimers()
    }
  })

  it('subscribes to shortcutsChanged to live-reload bars, and unsubscribes on unmount', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    expect(eventsOn).toHaveBeenCalledWith('mobile:shortcutsChanged', expect.any(Function))
    // Firing the event reloads bars without a tab close/reopen.
    eventHandlers.get('mobile:shortcutsChanged')?.(null)
    await flushPromises()
    expect(w.find('[data-testid="template-bar"]').exists()).toBe(true)
    w.unmount()
    expect(eventHandlers.has('mobile:shortcutsChanged')).toBe(false)
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
    await flushPromises()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).not.toContain('shaking')
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when an inert template button is tapped', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="template-btn-default-yes"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when the user taps the terminal area', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('.term').trigger('pointerdown')
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })
})
