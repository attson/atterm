// The Go and TS envelope implementations are written independently. Only a
// fixture produced by desktop/synced_blob_vectors_test.go
// (TestGenerateSyncedBlobVectors) and opened here proves the two agree on
// session-key derivation, AAD layout, and envelope framing — the same
// contract fsCrypto.vectors.test.ts holds for the FS request/response
// envelopes. Never hand-edit desktop/testdata/synced_blob_vectors.json;
// regenerate it with `go test ./desktop/... -run TestGenerateSyncedBlobVectors`
// after any change to sealProfiles/sealSSHHosts on the Go side.
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { openProfilesBlob, openSSHHostsBlob } from './syncedBlobs'

interface Fixture {
  account_key_b64: string
  profiles_value: string
  ssh_hosts_value: string
}

// Avoid the `new URL('literal', import.meta.url)` spelling: Vite treats
// that exact pattern as a static asset reference and rewrites it, which
// breaks resolution of a path outside the project root (desktop/testdata is
// outside desktop/frontend). Resolving in two steps opts out of that.
const thisFile = fileURLToPath(import.meta.url)
const fixturePath = resolve(dirname(thisFile), '../../../testdata/synced_blob_vectors.json')
const fixture: Fixture = JSON.parse(readFileSync(fixturePath, 'utf-8'))

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

const accountKey = b64ToBytes(fixture.account_key_b64)

describe('openProfilesBlob — Go-generated vector', () => {
  it('opens the Go-sealed blob and decodes both profiles', () => {
    const { profiles, defaultProfileId } = openProfilesBlob(accountKey, fixture.profiles_value)
    expect(defaultProfileId).toBe('p-synced')
    expect(profiles).toHaveLength(2)

    const synced = profiles.find((p) => p.id === 'p-synced')
    expect(synced).toBeDefined()
    expect(synced!.name).toBe('Synced Profile')
    expect(synced!.shell).toBe('/bin/zsh')
    expect(synced!.cwd).toBe('/home/u/work')
    expect(synced!.startupCmd).toBe('tmux attach')
    expect(synced!.syncEnv).toBe(true)
    expect(synced!.env).toEqual({ FOO: 'bar' })

    const unsynced = profiles.find((p) => p.id === 'p-unsynced')
    expect(unsynced).toBeDefined()
    expect(unsynced!.syncEnv).toBe(false)
  })

  it('distinguishes "SyncEnv=false" from "no env vars set"', () => {
    // stripUnsyncedEnv (desktop/profiles.go) clears Env unconditionally
    // before sealing for any profile with SyncEnv==false, so the sealed
    // payload never carries env for p-unsynced at all. The view's `env`
    // must therefore be undefined here — a caller checks `syncEnv` to know
    // *why*, rather than reading an empty object and concluding "the user
    // configured zero env vars".
    const { profiles } = openProfilesBlob(accountKey, fixture.profiles_value)
    const unsynced = profiles.find((p) => p.id === 'p-unsynced')!
    expect(unsynced.syncEnv).toBe(false)
    expect(unsynced.env).toBeUndefined()

    const synced = profiles.find((p) => p.id === 'p-synced')!
    expect(synced.syncEnv).toBe(true)
    expect(synced.env).toEqual({ FOO: 'bar' })
  })

  it('fails cleanly on a wrong account key', () => {
    const wrongKey = new Uint8Array(32).fill(0xff)
    expect(() => openProfilesBlob(wrongKey, fixture.profiles_value)).toThrow()
  })
})

describe('openSSHHostsBlob — Go-generated vector', () => {
  it('opens the Go-sealed blob and decodes hosts + keys', () => {
    const { hosts, keys } = openSSHHostsBlob(accountKey, fixture.ssh_hosts_value)
    expect(hosts).toHaveLength(2)
    expect(keys).toHaveLength(1)

    const h1 = hosts.find((h) => h.id === 'h1')
    expect(h1).toBeDefined()
    expect(h1!.alias).toBe('box1')
    expect(h1!.host).toBe('box1.example.com')
    expect(h1!.port).toBe('22')
    expect(h1!.user).toBe('root')
    expect(h1!.authKind).toBe('password')
    expect(h1!.tags).toEqual(['prod'])
    expect(h1!.note).toBe('primary')
    expect(h1!.hasJumpChain).toBe(false)
    expect(h1!.isProxyCommandHost).toBe(false)

    const h2 = hosts.find((h) => h.id === 'h2')
    expect(h2).toBeDefined()
    expect(h2!.keyId).toBe('k1')
    expect(h2!.hasJumpChain).toBe(true) // proxy_jump: 'box1'

    const k1 = keys[0]
    expect(k1.name).toBe('deploy-key')
    expect(k1.keyType).toBe('ED25519')
  })

  it('never surfaces a credential field on the decoded hosts or keys', () => {
    // The sealed payload bundles sshCredential (password) into every host
    // entry and sshKeySecret (private_key, passphrase) into every key
    // entry (desktop/ssh_sync.go sshSyncHost/sshSyncKey). Assert on the
    // decoded objects' own keys — not on what a renderer happens to show —
    // so a future field added to the view type that leaks a secret fails
    // this test even before anything renders it.
    const { hosts, keys } = openSSHHostsBlob(accountKey, fixture.ssh_hosts_value)
    const forbidden = ['cred', 'password', 'private_key', 'privateKey', 'passphrase', 'keySecret', 'secret']

    for (const h of hosts as unknown as Record<string, unknown>[]) {
      const ownKeys = Object.keys(h)
      for (const f of forbidden) expect(ownKeys).not.toContain(f)
    }
    for (const k of keys as unknown as Record<string, unknown>[]) {
      const ownKeys = Object.keys(k)
      for (const f of forbidden) expect(ownKeys).not.toContain(f)
    }
  })

  it('fails cleanly on a wrong account key', () => {
    const wrongKey = new Uint8Array(32).fill(0xff)
    expect(() => openSSHHostsBlob(wrongKey, fixture.ssh_hosts_value)).toThrow()
  })
})
