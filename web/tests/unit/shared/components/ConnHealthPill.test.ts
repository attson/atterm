import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ConnHealthPill from '@shared/components/ConnHealthPill.vue'
import type { ConnHealthSnapshot } from '@shared/connhealth/connhealth'

function snapshot(overrides: Partial<ConnHealthSnapshot> = {}): ConnHealthSnapshot {
  return {
    state: 'connected',
    rtt: { last_ms: 120, p50_ms: 110, p95_ms: 150 },
    rtt_samples: [],
    reconnect: {
      count_last_hour: 0,
      last_at_ms: null,
      last_reason: '',
      history: [],
    },
    bytes: { in_per_sec: 0, out_per_sec: 0 },
    seq_gaps: 0,
    ...overrides,
  }
}

describe('ConnHealthPill', () => {
  it('green band for RTT < 150 ms', () => {
    const w = mount(ConnHealthPill, {
      props: {
        health: snapshot({ rtt: { last_ms: 80, p50_ms: 80, p95_ms: 100 } }),
      },
    })
    expect(w.classes()).toContain('band-green')
    expect(w.text()).toContain('80')
  })
  it('yellow band for RTT 150–500 ms', () => {
    const w = mount(ConnHealthPill, {
      props: {
        health: snapshot({ rtt: { last_ms: 340, p50_ms: 300, p95_ms: 400 } }),
      },
    })
    expect(w.classes()).toContain('band-yellow')
  })
  it('red band for RTT > 500 ms', () => {
    const w = mount(ConnHealthPill, {
      props: {
        health: snapshot({ rtt: { last_ms: 800, p50_ms: 700, p95_ms: 900 } }),
      },
    })
    expect(w.classes()).toContain('band-red')
  })
  it('reconnecting state pulses regardless of RTT', () => {
    const w = mount(ConnHealthPill, {
      props: { health: snapshot({ state: 'reconnecting' }) },
    })
    expect(w.classes()).toContain('band-reconnecting')
    expect(w.text().toLowerCase()).toContain('reconnect')
  })
  it('closed state is dim', () => {
    const w = mount(ConnHealthPill, {
      props: { health: snapshot({ state: 'closed' }) },
    })
    expect(w.classes()).toContain('band-off')
  })
  it('falls back to label strings when none provided', () => {
    const w = mount(ConnHealthPill, {
      props: { health: snapshot({ state: 'connecting' }) },
    })
    expect(w.text()).toMatch(/connecting/i)
  })
  it('uses provided labels for state strings', () => {
    const w = mount(ConnHealthPill, {
      props: {
        health: snapshot({ state: 'reconnecting' }),
        labels: { reconnecting: '重连中…' },
      },
    })
    expect(w.text()).toContain('重连中…')
  })
})
