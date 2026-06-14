import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ConnHealthDrawer from '@shared/components/ConnHealthDrawer.vue'
import type { ConnHealthSnapshot } from '@shared/connhealth/connhealth'

function snapshot(): ConnHealthSnapshot {
  return {
    state: 'connected',
    rtt: { last_ms: 142, p50_ms: 120, p95_ms: 200 },
    rtt_samples: Array.from({ length: 30 }, (_, i) => ({ at_ms: i * 5000, rtt_ms: 100 + i })),
    reconnect: {
      count_last_hour: 2,
      last_at_ms: 1_700_000_000_000,
      last_reason: 'ws_close_1006',
      history: [
        { at_ms: 1_700_000_000_000, reason: 'ws_close_1006', duration_ms: 4000 },
      ],
    },
    bytes: { in_per_sec: 1024, out_per_sec: 256 },
    seq_gaps: 0,
  }
}

describe('ConnHealthDrawer', () => {
  it('shows current RTT and p50/p95', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const txt = w.text()
    expect(txt).toContain('142')
    expect(txt).toContain('120')
    expect(txt).toContain('200')
  })
  it('renders a sparkline path with multiple points', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const path = w.find('svg path')
    expect(path.exists()).toBe(true)
    const d = path.attributes('d') ?? ''
    expect(d.startsWith('M')).toBe(true)
    expect(d.split('L').length).toBeGreaterThan(2)
  })
  it('shows the reconnect table with reason and downtime', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: true } })
    const txt = w.text()
    expect(txt).toContain('ws_close_1006')
    expect(txt).toContain('4 s')
  })
  it('hidden when open=false', () => {
    const w = mount(ConnHealthDrawer, { props: { health: snapshot(), open: false } })
    expect(w.find('.drawer').exists()).toBe(false)
  })
  it('seq_gaps section visible only when > 0', () => {
    const noGaps = snapshot()
    noGaps.seq_gaps = 0
    const wNoGaps = mount(ConnHealthDrawer, { props: { health: noGaps, open: true } })
    expect(wNoGaps.find('.gaps').exists()).toBe(false)

    const withGaps = snapshot()
    withGaps.seq_gaps = 3
    const wGaps = mount(ConnHealthDrawer, { props: { health: withGaps, open: true } })
    expect(wGaps.find('.gaps').exists()).toBe(true)
    expect(wGaps.text()).toContain('3')
  })
})
