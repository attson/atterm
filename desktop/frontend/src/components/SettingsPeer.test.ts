import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (!params) return key
      return `${key}[${Object.entries(params).map(([name, value]) => `${name}=${value}`).join(',')}]`
    },
  }),
}))

const { fakePlatform, qrScanner } = vi.hoisted(() => ({
  fakePlatform: {
    caps: { capacitor: false },
    peer: {
      status: vi.fn(),
      previewConnectionBundle: vi.fn(),
      joinSpace: vi.fn(),
    },
  },
  qrScanner: {
    requestPermissions: vi.fn(),
    scan: vi.fn(),
  },
}))

vi.mock('../platform', () => ({
  usePlatform: () => fakePlatform,
}))

vi.mock('../platform/qrScanner', () => ({
  QRScanner: qrScanner,
}))

import SettingsPeer from './SettingsPeer.vue'

const emptyStatus = {
  configured: false,
  open_invitations: 0,
  used_invitations: 0,
  revoked_invitations: 0,
  expired_invitations: 0,
}

const configuredStatus = {
  configured: true,
  peer_id: 'peer_local_123',
  space_id: 'space_family_456',
  genesis_hash: 'abcdef0123456789',
  created_at: 1_797_897_600,
  open_invitations: 3,
  used_invitations: 2,
  revoked_invitations: 1,
  expired_invitations: 4,
}

const preview = {
  space_id: 'space_family_456',
  fingerprint: 'SHA256:ABCDEF0123456789',
  issuer_peer_id: 'peer_host_789',
  permission: 'control',
  allowed_session_ids: ['session-one', 'session-two'],
  can_invite: true,
  can_sync_secrets: false,
  invitation_expires_at: 1_798_156_800,
  bundle_expires_at: 1_797_984_000,
  route_kind: 'quick_tunnel',
  route_url: 'https://sample.trycloudflare.com',
}

beforeEach(() => {
  vi.clearAllMocks()
  fakePlatform.caps.capacitor = false
  fakePlatform.peer.status.mockResolvedValue(emptyStatus)
  fakePlatform.peer.previewConnectionBundle.mockResolvedValue(preview)
  fakePlatform.peer.joinSpace.mockResolvedValue(configuredStatus)
  qrScanner.requestPermissions.mockResolvedValue({ camera: 'granted' })
  qrScanner.scan.mockResolvedValue({ cancelled: false, rawValue: 'atc1.scanned-token' })
})

afterEach(() => {
  vi.restoreAllMocks()
})

async function mountReady() {
  const wrapper = mount(SettingsPeer)
  await flushPromises()
  return wrapper
}

describe('SettingsPeer', () => {
  it('previews pasted deep links and shows the authenticated trust details', async () => {
    const wrapper = await mountReady()
    const deepLink = 'https://app.example/connect#atc1.bundle-token'

    await wrapper.get('[data-testid="peer-bundle-input"]').setValue(deepLink)
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.previewConnectionBundle).toHaveBeenCalledWith(deepLink)
    expect(wrapper.get('[data-testid="peer-preview"]').text()).toContain(preview.fingerprint)
    expect(wrapper.get('[data-testid="peer-preview"]').text()).toContain(preview.route_url)
    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('session-one')
    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('session-two')
    expect(wrapper.text()).toContain('settings.peer.permission.control')
    expect(wrapper.text()).toContain('settings.peer.yes')
    expect(wrapper.text()).toContain('settings.peer.no')
    expect(fakePlatform.peer.joinSpace).not.toHaveBeenCalled()
  })

  it('requires an explicit confirmation and binds it to the previewed fingerprint', async () => {
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.raw-token')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.joinSpace).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="peer-join-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.joinSpace).toHaveBeenCalledWith({
      connection_bundle: 'atc1.raw-token',
      expected_fingerprint: preview.fingerprint,
    })
    expect(wrapper.find('[data-testid="peer-bundle-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.peer_id)
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(`SHA256:${configuredStatus.genesis_hash}`)
  })

  it('shows all-session scope when the invitation does not restrict session ids', async () => {
    fakePlatform.peer.previewConnectionBundle.mockResolvedValue({ ...preview, allowed_session_ids: [] })
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.all-sessions')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('settings.peer.scopeAll')
  })

  it('maps rejected and unavailable joins to safe messages without rendering the token', async () => {
    fakePlatform.peer.joinSpace.mockRejectedValue(new Error('Peer Space join rejected: dial tcp: connection refused atc1.secret'))
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.secret')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="peer-join-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="peer-error"]').text()).toBe('settings.peer.errors.hostUnavailable')
    expect(wrapper.text()).not.toContain('atc1.secret')
  })

  it('shows QR scanning only on Capacitor and previews the scanned bundle', async () => {
    const desktop = await mountReady()
    expect(desktop.find('[data-testid="peer-scan-button"]').exists()).toBe(false)
    desktop.unmount()

    fakePlatform.caps.capacitor = true
    const mobile = await mountReady()
    await mobile.get('[data-testid="peer-scan-button"]').trigger('click')
    await flushPromises()

    expect(qrScanner.requestPermissions).toHaveBeenCalledTimes(1)
    expect(qrScanner.scan).toHaveBeenCalledTimes(1)
    expect(fakePlatform.peer.previewConnectionBundle).toHaveBeenCalledWith('atc1.scanned-token')
    expect(mobile.find('[data-testid="peer-preview"]').exists()).toBe(true)
  })

  it('shows an existing Space without offering a replacement join form', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.space_id)
    expect(wrapper.find('[data-testid="peer-bundle-input"]').exists()).toBe(false)
  })
})
