import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobilePlaceholder from '../MobilePlaceholder.vue'

describe('MobilePlaceholder', () => {
  it('renders the AT Term app name', () => {
    expect(mount(MobilePlaceholder).text()).toMatch(/AT Term/i)
  })

  it('mentions that setup/relay config lands in PR-C', () => {
    expect(mount(MobilePlaceholder).text()).toMatch(/PR-C|setup|configure|relay/i)
  })

  it('exposes a data-testid for smoke targeting', () => {
    expect(mount(MobilePlaceholder).find('[data-testid="mobile-placeholder"]').exists()).toBe(true)
  })
})
