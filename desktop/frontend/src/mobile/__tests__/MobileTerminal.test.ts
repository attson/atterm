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
const termScrollToBottom = vi.fn()
const termSelect = vi.fn()
const termSelectLines = vi.fn()
const termClearSelection = vi.fn()
const termGetSelection = vi.fn().mockReturnValue('')
const termGetSelectionPosition = vi.fn().mockReturnValue(undefined as any)
const termScrollLines = vi.fn()
let selectionChangeCb: (() => void) | null = null
let bufferLineText = ''
let lastTerm: any = null

vi.mock('xterm', () => ({
  Terminal: class {
    options: Record<string, unknown> = {}
    cols = 80
    rows = 24
    textarea = document.createElement('textarea')
    buffer = {
      active: {
        getLine: (_row: number) => ({ translateToString: () => bufferLineText }),
      },
    }
    constructor() { lastTerm = this }
    onData(cb: (s: string) => void) { (this as any)._onData = cb }
    onResize() {}
    onSelectionChange(cb: () => void) { selectionChangeCb = cb; return { dispose() {} } }
    open() {}
    write(d: unknown, cb?: () => void) { termWrite(d); cb?.() }
    dispose() { termDispose() }
    focus() {}
    loadAddon() {}
    resize(c: number, r: number) { termResize(c, r) }
    scrollToBottom() { termScrollToBottom() }
    select(c: number, r: number, len: number) { termSelect(c, r, len) }
    selectLines(s: number, e: number) { termSelectLines(s, e) }
    clearSelection() { termClearSelection() }
    hasSelection() { return Boolean(termGetSelection()) }
    getSelection() { return termGetSelection() }
    getSelectionPosition() { return termGetSelectionPosition() }
    scrollLines(n: number) { termScrollLines(n) }
  },
}))

// setBufferLine lets a test stage what term.buffer.active.getLine(row) returns.
function setBufferLine(text: string) { bufferLineText = text }
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
import { pasteImageBus } from '../../lib/pasteImageBus'

const info: RemoteSession = { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24 }

beforeEach(() => {
  vi.clearAllMocks()
  lastHandlers = null
  lastArgs = null
  eventHandlers.clear()
  selectionChangeCb = null
  bufferLineText = ''
  termGetSelection.mockReturnValue('')
  termGetSelectionPosition.mockReturnValue(undefined)
})

describe('MobileTerminal', () => {
  it('creates SessionConnection with endpoint+sessionId and attaches on mount', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(lastArgs).toEqual({ endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1' })
    expect(attach).toHaveBeenCalledOnce()
  })

  it('writes incoming output to the terminal', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onOutput?.(new Uint8Array([104, 105]))
    expect(termWrite).toHaveBeenCalled()
  })

  it('scrolls to newest output when initial replay finishes', async () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onReplayProgress?.({ phase: 'start', bytes: 0, total_bytes: 100, seq: 0 })
    lastHandlers.onOutput?.(new Uint8Array([104, 105]))
    expect(termScrollToBottom).not.toHaveBeenCalled()

    lastHandlers.onReplayProgress?.({ phase: 'end', bytes: 100, total_bytes: 100, seq: 1 })

    expect(termScrollToBottom).toHaveBeenCalledOnce()
  })

  it('emits ended on CLOSE', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onClose?.({ exit_code: 0 })
    expect(w.emitted('ended')).toBeTruthy()
  })

  it('emits tokenInvalid on error status', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onStatus?.('error')
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })

  it('forwards cwd/title META updates via the meta event', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onMeta?.({ cwd: '/Users/me/proj', title: 'vim' })
    expect(w.emitted('meta')![0]).toEqual([{ cwd: '/Users/me/proj', title: 'vim' }])
  })

  it('viewer locks the grid to the PTY cols/rows from META (avoids overflow)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Become a viewer (not driver), then receive a wide PTY size.
    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    termResize.mockClear()
    lastHandlers.onMeta?.({ cols: 213, rows: 50 })
    expect(termResize).toHaveBeenCalledWith(213, 50)
  })

  it('driver does NOT lock to META cols/rows (it fits its own viewport)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Driver by default; a META with a wide size must not resize our grid.
    termResize.mockClear()
    lastHandlers.onMeta?.({ cols: 213, rows: 50 })
    expect(termResize).not.toHaveBeenCalled()
  })

  it('detaches + disposes on unmount', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    w.unmount()
    expect(detach).toHaveBeenCalledOnce()
    expect(termDispose).toHaveBeenCalledOnce()
  })

  it('pushes its size to the PTY when it becomes the driver', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    sendResize.mockClear() // ignore the mount-time fit resize
    lastHandlers.onDriverChange?.('me', true, '')
    expect(sendResize).toHaveBeenCalledWith(80, 24)
  })

  it('shows viewer overlay when not driver; take-control calls claimDriver', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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
    const emitSpy = vi.spyOn(pasteImageBus, 'emit')
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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

    expect(emitSpy).toHaveBeenCalledTimes(1)
    const [emittedBlob, emittedName] = emitSpy.mock.calls[0]
    expect(emittedBlob).toBe(file)
    expect(emittedName).toBe('mobile-image.png')
    emitSpy.mockRestore()
  })

  it('image button does not invoke the camera when not in control mode', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    // controlMode is false by default — button is inert and tapping it only
    // flashes the protect banner, never opens the camera.
    expect(w.find('[data-testid="mobile-image"]').classes()).toContain('inert')
    await w.find('[data-testid="mobile-image"]').trigger('click')
    expect(getPhoto).not.toHaveBeenCalled()
  })

  it('image button is inert for view-only sessions', async () => {
    const viewOnly = { ...info, remote_permission: 'view' }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info: viewOnly, active: true } })
    await flushPromises()
    const btn = w.find('[data-testid="mobile-image"]')
    if (btn.exists()) {
      expect(btn.classes()).toContain('inert')
    }
  })

  it('renders a mobile control panel with the default aux keys', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    expect(w.find('[data-testid="mobile-control-panel"]').exists()).toBe(true)
    for (const id of ['aux-enter', 'aux-esc', 'aux-tab', 'aux-ctrl-c', 'aux-ctrl-d', 'aux-up', 'aux-down', 'aux-left', 'aux-right']) {
      expect(w.find(`[data-testid="mobile-key-${id}"]`).exists()).toBe(true)
    }
  })

  it('requires explicit control mode before shortcut buttons send input', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()

    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\r')
  })

  it('clicking the terminal area blurs the focused xterm textarea and hides the iOS keyboard', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })

    await w.find('.term').trigger('pointerdown')

    expect(hideKeyboard).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('forwards non-composition IME insertText (punctuation/space/digit) xterm drops', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true) // canSend
    sendInput.mockClear()
    for (const ch of ['，', ' ', '1']) {
      lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: ch, inputType: 'insertText', isComposing: false } as any))
    }
    expect(sendInput.mock.calls.map((c: unknown[]) => c[0])).toEqual(['，', ' ', '1'])
  })

  it('does NOT hijack composition input (pinyin→Hanzi stays with xterm)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    sendInput.mockClear()
    // composition-in-progress + the committed-composition input must be ignored
    lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: 'ni', inputType: 'insertCompositionText', isComposing: true } as any))
    lastTerm.textarea.dispatchEvent(new InputEvent('input', { data: '你', inputType: 'insertCompositionText', isComposing: false } as any))
    expect(sendInput).not.toHaveBeenCalled()
  })

  it('sends Ctrl-D through the control panel', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-key-aux-ctrl-d"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('\x04')
  })

  it('asks for paste confirmation before sending clipboard text', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    await w.find('[data-testid="mobile-paste"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    await w.find('[data-testid="mobile-paste-confirm-panel"] textarea').setValue('paste me')
    await w.find('[data-testid="mobile-paste-confirm"]').trigger('click')
    expect(sendInput).toHaveBeenCalledWith('paste me')
  })

  it('marks control buttons inert and hides take-control for view-only sessions', async () => {
    const viewInfo = { ...info, remote_permission: 'view' as const }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info: viewInfo, active: true } })
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
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    // The defaults seed includes default-yes; mock platform returns [] which falls back to DEFAULT_TEMPLATES.
    expect(w.find('[data-testid="template-bar"]').exists()).toBe(true)
    expect(w.find('[data-testid="template-btn-default-yes"]').exists()).toBe(true)
  })

  it('clicking a template button sends the text and a standalone Enter one tick later (no preview)', async () => {
    vi.useFakeTimers()
    try {
      const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
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
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(w.find('[data-testid="mobile-quick-y"]').exists()).toBe(false)
    expect(w.find('[data-testid="mobile-quick-continue"]').exists()).toBe(false)
  })

  it('renders the protect-mode banner when controlMode is off and the user is an eligible driver', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    // Default: driver + canControl + controlMode === false → banner shown.
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(true)
  })

  it('hides the protect-mode banner once controlMode is enabled', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('does not render the protect-mode banner for view-only sessions (view-only banner covers it)', () => {
    const viewInfo = { ...info, remote_permission: 'view' as const }
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info: viewInfo, active: true } })
    expect(w.find('[data-testid="mobile-view-only"]').exists()).toBe(true)
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('does not render the protect-mode banner while the user is a viewer (viewer overlay covers it)', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onDriverChange?.('owner-A', false, 'mac-mini')
    await w.vm.$nextTick()
    expect(w.find('[data-testid="mobile-protect-banner"]').exists()).toBe(false)
  })

  it('shakes the protect-mode banner when an inert aux key is tapped', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).not.toContain('shaking')
    await w.find('[data-testid="mobile-key-aux-enter"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when an inert template button is tapped', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="template-btn-default-yes"]').trigger('click')
    expect(sendInput).not.toHaveBeenCalled()
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  it('shakes the protect-mode banner when the user taps the terminal area', async () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await w.find('.term').trigger('pointerdown')
    expect(w.find('[data-testid="mobile-protect-banner"]').classes()).toContain('shaking')
  })

  describe('long-press selection', () => {
    function info(over: Partial<RemoteSession> = {}): RemoteSession {
      return { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24, remote_permission: 'full', ...over }
    }

    async function mountReady(extra: Partial<RemoteSession> = {}) {
      const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info: info(extra), active: true } })
      await flushPromises()
      return w
    }

    function viewportEl(w: ReturnType<typeof mount>) {
      // jsdom returns null for querySelector on inserted children of mocked xterm.
      // For these tests we attach our own div with the xterm-viewport class and
      // dispatch events on it.
      const root = w.element as HTMLElement
      let vp = root.querySelector('.xterm-viewport') as HTMLElement | null
      if (!vp) {
        vp = document.createElement('div')
        vp.className = 'xterm-viewport'
        Object.defineProperty(vp, 'getBoundingClientRect', { value: () => ({ left: 0, top: 0, right: 800, bottom: 600, width: 800, height: 600, x: 0, y: 0, toJSON() { return this } }) })
        Object.defineProperty(vp, 'scrollTop', { value: 0, configurable: true })
        ;(root.querySelector('.term') as HTMLElement).appendChild(vp)
      }
      return vp
    }

    function pointerEvent(type: string, x: number, y: number): PointerEvent {
      // jsdom lacks PointerEvent; build a minimal stand-in via MouseEvent + extra props.
      const ev = new MouseEvent(type, { clientX: x, clientY: y, bubbles: true })
      Object.defineProperty(ev, 'pointerId', { value: 1 })
      return ev as unknown as PointerEvent
    }

    it('does not select when canSend is false (control mode off)', async () => {
      const w = await mountReady()
      // controlMode is false by default; canSend is therefore false.
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 100, 80))
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).not.toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('selects the word at the press position after 500 ms when canSend is true', async () => {
      const w = await mountReady()
      // Engage control mode (this also requires being driver — true by default in mock).
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')              // cell (2,0) is the 't' of "git"
      // cellW/Hour derived from fallback: fontSize=12 → cellW=7.2, cellH=12.
      // For col=2 row=0, clientX=15 (15/7.2 ≈ 2.08 → col 2), clientY=8 (8/12 = 0).
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(0, 0, 3)  // selects "git"
      // Popover should now be visible (mock getSelectionPosition to return a non-null bbox so updatePopoverFromSelection produces a valid result)
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)
    })

    it('exits selection when the cancel button is tapped', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-cancel"]').trigger('click')
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('exits selection when control mode is turned off mid-selection', async () => {
      const w = await mountReady()
      const toggle = w.find('[data-testid="mobile-control-toggle"]')
      await toggle.setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)

      await toggle.setValue(false)
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('exits selection on viewport scroll', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      vp.dispatchEvent(new Event('scroll', { bubbles: true }))
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
    })

    it('exits selection on outside (document) pointerdown', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)

      document.dispatchEvent(pointerEvent('pointerdown', 500, 500))
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('does NOT enter selection on whitespace (wordBoundary len=0)', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git   status')             // whitespace at cols 3,4,5
      vp.dispatchEvent(pointerEvent('pointerdown', 32, 8))  // col 4 with cellW≈7.2 → whitespace
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).not.toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('cancel button click still works when a real pointerdown precedes it (capture-phase doc handler does not eat it)', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)

      // Dispatch a REAL pointerdown on the cancel button (not a synthetic click).
      // The capture-phase document handler MUST recognize the button as
      // popover-internal and not pre-emptively exit the selection.
      const cancelBtn = w.find('[data-testid="selection-popover-cancel"]').element as HTMLElement
      cancelBtn.dispatchEvent(pointerEvent('pointerdown', 100, 50))
      await flushPromises()
      // Popover should still be present (document handler must NOT have exited).
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)
      // Now the click follows the pointerdown — synthesize it to complete the gesture.
      await w.find('[data-testid="selection-popover-cancel"]').trigger('click')
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('tap inside viewport during selecting clears the old selection (no zombie popover)', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)
      termClearSelection.mockClear()

      // User taps inside viewport again (short tap, no long press). The
      // previous selection should clear immediately on pointerdown, not
      // wait for an outside tap.
      vp.dispatchEvent(pointerEvent('pointerdown', 200, 80))
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('extends the selection within a row on pointermove > 4 px', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status -v')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))    // col 2 of "git"
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(0, 0, 3)         // selects "git"
      termSelect.mockClear()

      // Drag 60 px to the right → about 8 cells over.
      vp.dispatchEvent(pointerEvent('pointermove', 75, 8))
      // Single-row drag uses term.select; multi-row uses term.selectLines.
      expect(termSelect).toHaveBeenCalled()
      const lastCall = termSelect.mock.calls[termSelect.mock.calls.length - 1]
      expect(lastCall[0]).toBe(0)      // start col stays at anchor word start
      expect(lastCall[1]).toBe(0)      // single row
      expect(lastCall[2]).toBeGreaterThan(3)  // grew past "git"
    })

    it('drag within the original word keeps the word selected (does not shrink)', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))    // col 2 of "git"
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(0, 0, 3)         // selects "git"
      termSelect.mockClear()

      // Drag to col 1 (still inside "git"). Selection should stay at cells 0-2.
      vp.dispatchEvent(pointerEvent('pointermove', 8, 8))
      const calls = termSelect.mock.calls
      // The implementation may call term.select multiple times during the move.
      // What matters is that the final state still spans the original word.
      const last = calls[calls.length - 1]
      expect(last[0]).toBe(0)              // start col stays at word start
      expect(last[1]).toBe(0)              // single row
      expect(last[2]).toBeGreaterThanOrEqual(3)  // covers at least the full word
    })

    it('drag LEFT past the word start extends leftward, preserving the word', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      // Press on "bar" of "foo bar" (col 4–6); long-press at col 5
      setBufferLine('foo bar')
      vp.dispatchEvent(pointerEvent('pointerdown', 36, 8))    // 36/cellW → col 5 for cellW≈7.2
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(4, 0, 3)         // "bar"
      termSelect.mockClear()

      // Drag well past the word's left edge (col 0). Selection should grow leftward:
      // c0 = min(4, cur.col), c1 = max(6, cur.col) where cur.col == 0,
      // giving select(0, 0, 7).
      vp.dispatchEvent(pointerEvent('pointermove', 0, 8))
      const last = termSelect.mock.calls[termSelect.mock.calls.length - 1]
      expect(last[0]).toBe(0)              // start col went left of original word
      expect(last[1]).toBe(0)              // single row
      expect(last[2]).toBeGreaterThanOrEqual(7)  // covers original word + leftward
    })

    it('pointercancel from pressing state cancels timer and returns to idle', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      // We're now in 'pressing' state with a 500ms timer running. Fire cancel BEFORE the timer.
      vp.dispatchEvent(pointerEvent('pointercancel', 15, 8))
      // Let the (cancelled) timer's nominal time pass.
      await new Promise((r) => setTimeout(r, 600))
      // No selection should have been created.
      expect(termSelect).not.toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('uses selectLines when the drag crosses rows', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))    // row 0
      await new Promise((r) => setTimeout(r, 600))
      termSelectLines.mockClear()

      // Drag down past one row (cellH≈12 → y=50 is row 4)
      vp.dispatchEvent(pointerEvent('pointermove', 60, 50))
      expect(termSelectLines).toHaveBeenCalled()
      const lastCall = termSelectLines.mock.calls[termSelectLines.mock.calls.length - 1]
      expect(lastCall[0]).toBe(0)      // start row
      expect(lastCall[1]).toBeGreaterThan(0)  // end row > start row
    })

    it('auto-scrolls when the drag is near the top edge', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        termScrollLines.mockClear()

        // Drag into top 24 px zone.
        vp.dispatchEvent(pointerEvent('pointermove', 100, 10))
        await vi.advanceTimersByTimeAsync(120)
        expect(termScrollLines).toHaveBeenCalledWith(-3)
      } finally {
        vi.useRealTimers()
      }
    })

    it('auto-scrolls when the drag is near the bottom edge', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        termScrollLines.mockClear()

        // Drag into bottom 24 px zone (viewport bottom is 600).
        vp.dispatchEvent(pointerEvent('pointermove', 100, 590))
        await vi.advanceTimersByTimeAsync(120)
        expect(termScrollLines).toHaveBeenCalledWith(3)
      } finally {
        vi.useRealTimers()
      }
    })

    it('stops auto-scrolling on pointerup', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        vp.dispatchEvent(pointerEvent('pointermove', 100, 10))
        await vi.advanceTimersByTimeAsync(120)
        termScrollLines.mockClear()
        vp.dispatchEvent(pointerEvent('pointerup', 100, 10))
        await vi.advanceTimersByTimeAsync(300)
        expect(termScrollLines).not.toHaveBeenCalled()
      } finally {
        vi.useRealTimers()
      }
    })

    it('copy: writes the selection to the clipboard and shows a toast', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined)
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('git')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-copy"]').trigger('click')
      await flushPromises()
      expect(writeText).toHaveBeenCalledWith('git')
      expect(w.find('[data-testid="mobile-selection-toast"]').exists()).toBe(true)
      expect(w.find('[data-testid="mobile-selection-toast"]').text()).toBe('Copied to clipboard')
      expect(termClearSelection).toHaveBeenCalled()
    })

    it('copy: shows a failure toast when clipboard write rejects', async () => {
      const writeText = vi.fn().mockRejectedValue(new Error('denied'))
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('git')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-copy"]').trigger('click')
      await flushPromises()
      expect(w.find('[data-testid="mobile-selection-toast"]').text()).toBe('Copy failed')
    })

    it('copy: silently exits when selection is empty (no toast)', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined)
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      // Selection appears (selectionChangeCb fires with a bbox), popover up.
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      // But by the time the user taps Copy, the selection has been cleared
      // (e.g. by some other event). getSelection now returns ''.
      termGetSelection.mockReturnValue('')

      await w.find('[data-testid="selection-popover-copy"]').trigger('click')
      await flushPromises()
      expect(writeText).not.toHaveBeenCalled()
      expect(w.find('[data-testid="mobile-selection-toast"]').exists()).toBe(false)
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('send: prepares payload and forwards to conn.sendInput, then exits selection', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('ls -la')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('ls -la')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 6, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-send"]').trigger('click')
      expect(sendInput).toHaveBeenCalledWith('ls -la\r')
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('send: silently no-ops when prepareSendPayload returns null', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('foo')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      // Now stage getSelection to return CR-only — prepareSendPayload normalizes
      // \r and strips trailing \r, leaving '' → returns null.
      termGetSelection.mockReturnValue('\r')

      sendInput.mockClear()
      await w.find('[data-testid="selection-popover-send"]').trigger('click')
      expect(sendInput).not.toHaveBeenCalled()
      // The selection still exits even though no send happened.
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })
  })
})
