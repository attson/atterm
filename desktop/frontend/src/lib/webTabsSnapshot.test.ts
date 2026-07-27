import { beforeEach, describe, expect, it } from 'vitest'
import { getWindowId, loadSnapshot, saveSnapshot, parseHashSid, formatHash } from './webTabsSnapshot'

beforeEach(() => { localStorage.clear(); sessionStorage.clear() })

describe('windowId', () => {
  it('is stable within the same session', () => {
    const a = getWindowId()
    const b = getWindowId()
    expect(a).toBe(b)
    expect(a).toMatch(/^[0-9a-f-]{36}$/)
  })
})

describe('snapshot roundtrip', () => {
  it('empty when no snapshot stored', () => {
    expect(loadSnapshot()).toBeNull()
  })
  it('persists per-window', () => {
    const snap = {
      tabs: [{ id: 't1', layout: 'single', active_pane_idx: 0,
               panes: [{ slot: 0, session_id: 'sid-a' }] }],
      active_tab_id: 't1',
    }
    saveSnapshot(snap)
    expect(loadSnapshot()).toEqual(snap)
    // Different window → separate storage
    const otherWin = 'other-window-uuid'
    localStorage.setItem(`atterm.web.tabs.v1.${otherWin}`, JSON.stringify({ tabs: [], active_tab_id: '' }))
    expect(loadSnapshot()).toEqual(snap)  // still ours
  })
})

describe('hash routing', () => {
  it('parse #/session/sid-a', () => {
    expect(parseHashSid('#/session/sid-a')).toEqual({ sid: 'sid-a', focus: undefined, permission: undefined })
  })
  it('parse #/session/sid-a?focus=input&permission=view', () => {
    expect(parseHashSid('#/session/sid-a?focus=input&permission=view')).toEqual({
      sid: 'sid-a', focus: 'input', permission: 'view',
    })
  })
  it('empty hash returns null sid', () => {
    expect(parseHashSid('')).toEqual({ sid: null, focus: undefined, permission: undefined })
    expect(parseHashSid('#/')).toEqual({ sid: null, focus: undefined, permission: undefined })
  })
  it('formatHash roundtrips', () => {
    expect(formatHash('sid-a')).toBe('#/session/sid-a')
    expect(formatHash('sid-a', { focus: 'input' })).toBe('#/session/sid-a?focus=input')
    expect(formatHash('sid-a', { permission: 'view' })).toBe('#/session/sid-a?permission=view')
  })
})
