import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const pairs = Object.entries(params).map(([kk, vv]) => `${kk}=${vv}`).join(',')
      return `${k}[${pairs}]`
    },
  }),
}))

const { fakePlatform } = vi.hoisted(() => ({
  fakePlatform: {
    relay: {
      fetchMe: vi.fn(),
    },
  },
}))

vi.mock('../platform', () => ({
  usePlatform: () => fakePlatform,
}))

vi.mock('@shared/api/me', () => ({
  changePassword: vi.fn(),
  deleteMe: vi.fn(),
}))

import SettingsAccount from './SettingsAccount.vue'
import { changePassword, deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

const changePasswordMock = changePassword as unknown as ReturnType<typeof vi.fn>
const deleteMeMock = deleteMe as unknown as ReturnType<typeof vi.fn>

beforeEach(() => {
  changePasswordMock.mockReset()
  deleteMeMock.mockReset()
  fakePlatform.relay.fetchMe = vi.fn().mockResolvedValue({ user_id: 'u1', email: 'me@example.com' })
})

afterEach(() => {
  vi.restoreAllMocks()
})

async function mountReady() {
  const w = mount(SettingsAccount)
  await flushPromises()
  return w
}

async function fillChangePasswordForm(w: Awaited<ReturnType<typeof mountReady>>, old: string, next: string, confirm: string) {
  await w.get('[data-testid="old-password-input"]').setValue(old)
  await w.get('[data-testid="new-password-input"]').setValue(next)
  await w.get('[data-testid="confirm-password-input"]').setValue(confirm)
}

describe('SettingsAccount', () => {
  it('renders me.email fetched via platform.relay.fetchMe', async () => {
    const w = await mountReady()
    expect(fakePlatform.relay.fetchMe).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid="account-email"]').text()).toBe('me@example.com')
  })

  describe('change password', () => {
    it('submit is disabled until old password set, new password >= 8 chars, and confirm matches', async () => {
      const w = await mountReady()
      const submit = w.get('[data-testid="change-password-submit"]')
      expect((submit.element as HTMLButtonElement).disabled).toBe(true)

      await fillChangePasswordForm(w, 'current-pw', 'short', 'short')
      expect((submit.element as HTMLButtonElement).disabled).toBe(true) // too short

      await fillChangePasswordForm(w, 'current-pw', 'longenough1', 'mismatched1')
      expect((submit.element as HTMLButtonElement).disabled).toBe(true) // mismatch

      await fillChangePasswordForm(w, 'current-pw', 'longenough1', 'longenough1')
      expect((submit.element as HTMLButtonElement).disabled).toBe(false)
    })

    it('submits changePassword(current, new), shows success message, clears fields', async () => {
      changePasswordMock.mockResolvedValue(undefined)
      const w = await mountReady()
      await fillChangePasswordForm(w, 'current-pw', 'longenough1', 'longenough1')
      await w.get('[data-testid="change-password-submit"]').trigger('click')
      await flushPromises()

      expect(changePasswordMock).toHaveBeenCalledWith('current-pw', 'longenough1')
      expect(w.text()).toContain('settings.account.changePassword.successToast')
      expect((w.get('[data-testid="old-password-input"]').element as HTMLInputElement).value).toBe('')
      expect((w.get('[data-testid="new-password-input"]').element as HTMLInputElement).value).toBe('')
      expect((w.get('[data-testid="confirm-password-input"]').element as HTMLInputElement).value).toBe('')
    })

    it('maps a 401 response to the wrong-password error message', async () => {
      changePasswordMock.mockRejectedValue(new ApiError(401, 'http_error', null))
      const w = await mountReady()
      await fillChangePasswordForm(w, 'wrong-pw', 'longenough1', 'longenough1')
      await w.get('[data-testid="change-password-submit"]').trigger('click')
      await flushPromises()
      expect(w.text()).toContain('settings.account.errors.wrongPassword')
    })

    it('maps a 429 response to the rate-limited error message', async () => {
      changePasswordMock.mockRejectedValue(new ApiError(429, 'http_error', null))
      const w = await mountReady()
      await fillChangePasswordForm(w, 'current-pw', 'longenough1', 'longenough1')
      await w.get('[data-testid="change-password-submit"]').trigger('click')
      await flushPromises()
      expect(w.text()).toContain('settings.account.errors.rateLimited')
    })

    it('maps a network error (status 0) to the network error message', async () => {
      changePasswordMock.mockRejectedValue(new ApiError(0, 'network_error', null))
      const w = await mountReady()
      await fillChangePasswordForm(w, 'current-pw', 'longenough1', 'longenough1')
      await w.get('[data-testid="change-password-submit"]').trigger('click')
      await flushPromises()
      expect(w.text()).toContain('settings.account.errors.network')
    })

    it('does not call changePassword when the form is invalid', async () => {
      const w = await mountReady()
      await fillChangePasswordForm(w, 'current-pw', 'short', 'short')
      await w.get('[data-testid="change-password-submit"]').trigger('click')
      await flushPromises()
      expect(changePasswordMock).not.toHaveBeenCalled()
    })
  })

  describe('danger zone', () => {
    it('starts collapsed and expands on toggle to show the two confirmation inputs', async () => {
      const w = await mountReady()
      expect(w.find('[data-testid="danger-current-password"]').exists()).toBe(false)
      expect(w.find('[data-testid="danger-confirm-email"]').exists()).toBe(false)

      await w.get('[data-testid="danger-zone-toggle"]').trigger('click')

      expect(w.find('[data-testid="danger-current-password"]').exists()).toBe(true)
      expect(w.find('[data-testid="danger-confirm-email"]').exists()).toBe(true)
    })

    it('delete button is disabled until password is non-empty and email matches exactly', async () => {
      const w = await mountReady()
      await w.get('[data-testid="danger-zone-toggle"]').trigger('click')
      const del = w.get('[data-testid="danger-delete-button"]')
      expect((del.element as HTMLButtonElement).disabled).toBe(true)

      await w.get('[data-testid="danger-current-password"]').setValue('pw')
      await w.get('[data-testid="danger-confirm-email"]').setValue('wrong@example.com')
      expect((del.element as HTMLButtonElement).disabled).toBe(true)

      await w.get('[data-testid="danger-confirm-email"]').setValue('me@example.com')
      expect((del.element as HTMLButtonElement).disabled).toBe(false)
    })

    it('clicking delete prompts window.confirm; declining skips deleteMe', async () => {
      const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
      const w = await mountReady()
      await w.get('[data-testid="danger-zone-toggle"]').trigger('click')
      await w.get('[data-testid="danger-current-password"]').setValue('pw')
      await w.get('[data-testid="danger-confirm-email"]').setValue('me@example.com')
      await w.get('[data-testid="danger-delete-button"]').trigger('click')
      await flushPromises()

      expect(confirmSpy).toHaveBeenCalledTimes(1)
      expect(deleteMeMock).not.toHaveBeenCalled()
    })

    it('confirmed delete calls deleteMe(email, password) and redirects to /login.html', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true)
      deleteMeMock.mockResolvedValue(undefined)
      const assign = vi.fn()
      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: { ...originalLocation, assign },
        writable: true,
      })

      const w = await mountReady()
      await w.get('[data-testid="danger-zone-toggle"]').trigger('click')
      await w.get('[data-testid="danger-current-password"]').setValue('pw-1234')
      await w.get('[data-testid="danger-confirm-email"]').setValue('me@example.com')
      await w.get('[data-testid="danger-delete-button"]').trigger('click')
      await flushPromises()

      expect(deleteMeMock).toHaveBeenCalledWith('me@example.com', 'pw-1234')
      expect(assign).toHaveBeenCalledWith('/login.html')

      Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    })

    it('failed delete shows an inline error and does not redirect', async () => {
      vi.spyOn(window, 'confirm').mockReturnValue(true)
      deleteMeMock.mockRejectedValue(new Error('invalid credentials'))
      const w = await mountReady()
      await w.get('[data-testid="danger-zone-toggle"]').trigger('click')
      await w.get('[data-testid="danger-current-password"]').setValue('wrong-pw')
      await w.get('[data-testid="danger-confirm-email"]').setValue('me@example.com')
      await w.get('[data-testid="danger-delete-button"]').trigger('click')
      await flushPromises()

      expect(w.text()).toContain('settings.account.errors.wrongPassword')
    })
  })
})
