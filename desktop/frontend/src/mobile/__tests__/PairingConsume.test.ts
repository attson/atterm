import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import PairingConsume from '../PairingConsume.vue'

const consumePairing = vi.fn()
const save = vi.fn()

vi.mock('../../platform', () => ({
  usePlatform: () => ({
    relay: { consumePairing, save, load: vi.fn() },
  }),
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('PairingConsume', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('rejects URL with missing t param', async () => {
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
    expect(consumePairing).not.toHaveBeenCalled()
  })

  it('rejects http URL when allowInsecure is false', async () => {
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'http://relay.example.com/pair?t=pair_X', allowInsecure: false },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
  })

  it('happy path: calls consumePairing, saves config, emits connected', async () => {
    consumePairing.mockResolvedValue({
      relay_url: 'https://relay.example.com',
      api_token: 'atk_NEW',
      user: { id: 'u1', email: 'alice@example.com' },
    })
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair?t=pair_VALID' },
    })
    await flushPromises()
    expect(consumePairing).toHaveBeenCalledWith('https://relay.example.com', 'pair_VALID')
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      url: 'https://relay.example.com',
      token: 'atk_NEW',
    }))
    expect(wrapper.emitted('connected')).toBeTruthy()
  })

  it('renders pair_invalid error when consume rejects', async () => {
    consumePairing.mockRejectedValue(new Error('pair_invalid'))
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair?t=pair_BAD' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
    expect(save).not.toHaveBeenCalled()
  })
})
