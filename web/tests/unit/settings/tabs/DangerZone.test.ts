import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  deleteMe: vi.fn(),
}))

import DangerZone from '@/settings/tabs/DangerZone.vue'
import { deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

async function clickConfirm() {
  // Naive UI's <n-popconfirm> teleports the popup to body; clicking
  // the trigger only opens the popup, so we then click the primary
  // confirm button inside it.
  const confirmBtn = document.querySelector(
    '.n-popconfirm .n-button--primary-type',
  ) as HTMLButtonElement | null
  confirmBtn?.click()
  await flushPromises()
  // performDelete's promise rejection resolves on the next microtask
  // tick after the click; a second flush lets the catch+render run.
  await flushPromises()
}

async function fillAndSubmit(wrapper: ReturnType<typeof mount>, email: string, password: string) {
  await wrapper.find('input[type="email"]').setValue(email)
  await wrapper.find('input[autocomplete="current-password"]').setValue(password)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
  // open popconfirm by clicking the trigger button (the submit button itself)
  await wrapper.find('[data-testid="delete-account-trigger"]').trigger('click')
  await flushPromises()
  await clickConfirm()
}

describe('DangerZone.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    // Naive UI teleports the popconfirm popup to document.body and does
    // not clean it up between mounts; without this reset, document.querySelector
    // in clickConfirm() picks the stale popup from the previous test.
    document.body.innerHTML = ''
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '#danger', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('submits email + password and redirects to /login.html on success', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(DangerZone, { attachTo: document.body })

    await fillAndSubmit(wrapper, 'a@b.example', 'password-1234')

    expect(deleteMe).toHaveBeenCalledWith('a@b.example', 'password-1234')
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('shows the email-mismatch message on 400', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'email_mismatch', null),
    )
    const wrapper = mount(DangerZone, { attachTo: document.body })

    await fillAndSubmit(wrapper, 'wrong@b.example', 'password-1234')

    expect(wrapper.text()).toContain("Email doesn't match")
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the password-incorrect message on 401', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'password_incorrect', null),
    )
    const wrapper = mount(DangerZone, { attachTo: document.body })

    await fillAndSubmit(wrapper, 'a@b.example', 'wrong')

    expect(wrapper.text()).toContain('Password is incorrect.')
  })

  it('shows the last-admin guard message on 409', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'last_admin', null),
    )
    const wrapper = mount(DangerZone, { attachTo: document.body })

    await fillAndSubmit(wrapper, 'a@b.example', 'password-1234')

    expect(wrapper.text()).toContain('last admin')
  })
})
