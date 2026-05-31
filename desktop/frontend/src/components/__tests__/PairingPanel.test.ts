import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import PairingPanel from '../PairingPanel.vue'
import * as api from '../../lib/api'

vi.mock('qrcode', () => ({
  default: {
    toDataURL: vi.fn(async (url: string) =>
      'data:image/png;base64,FAKE_' + Buffer.from(url).toString('base64').slice(0, 16))
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('PairingPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.spyOn(api, 'createPairingToken').mockResolvedValue({
      token: 'pair_TESTVAL',
      expires_at: Math.floor(Date.now() / 1000) + 300,
      qr_url: 'https://relay.test/pair?t=pair_TESTVAL',
    })
  })

  it('shows idle state with a Generate button by default', () => {
    const wrapper = mount(PairingPanel)
    expect(wrapper.find('[data-testid="pair-generate"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pair-qr"]').exists()).toBe(false)
  })

  it('renders the QR image and countdown after Generate clicked', async () => {
    const wrapper = mount(PairingPanel)
    await wrapper.find('[data-testid="pair-generate"]').trigger('click')
    await flushPromises()
    const img = wrapper.find('[data-testid="pair-qr"]')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toMatch(/^data:image\/png/)
    expect(wrapper.text()).toContain('5:00')
  })

  it('shows expired state when countdown reaches zero', async () => {
    const wrapper = mount(PairingPanel)
    await wrapper.find('[data-testid="pair-generate"]').trigger('click')
    await flushPromises()
    vi.advanceTimersByTime(301 * 1000)
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-expired"]').exists()).toBe(true)
  })
})
