import { mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import InsecureBanner from '../InsecureBanner.vue'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('InsecureBanner', () => {
  it('renders nothing when relayUrl is https', () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'https://relay.example.com' } })
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(false)
  })

  it('renders the collapsed banner when relayUrl is http', () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'http://relay.example.com' } })
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(true)
    expect(w.find('[data-testid="insecure-body"]').exists()).toBe(false)
  })

  it('expands body on tap and emits dismiss on Dismiss click', async () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'http://relay.example.com' } })
    await w.find('[data-testid="insecure-banner"]').trigger('click')
    expect(w.find('[data-testid="insecure-body"]').exists()).toBe(true)
    await w.find('[data-testid="insecure-dismiss"]').trigger('click')
    expect(w.emitted('dismiss')).toBeTruthy()
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(false)
  })
})
