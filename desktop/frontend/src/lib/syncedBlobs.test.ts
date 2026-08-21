// The Go and TS envelope implementations are written independently. Only a
// fixture produced by desktop/synced_blob_vectors_test.go
// (TestSyncedBlobVectors) and opened here proves the two agree on
// session-key derivation, AAD layout, and envelope framing — the same
// contract fsCrypto.vectors.test.ts holds for the FS request/response
// envelopes. Never hand-edit desktop/testdata/synced_blob_vectors.json;
// regenerate it with `go test ./desktop/ -run TestSyncedBlobVectors -update`
// after any change to sealProfiles/sealSSHHosts on the Go side.
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { b64ToBytes } from './opaque'
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

const accountKey = b64ToBytes(fixture.account_key_b64)

// The three CANARY strings sealed into the fixture's ssh_hosts_value by
// desktop/synced_blob_vectors_test.go's fixtureSSHCreds/fixtureSSHKeySecrets.
// A denylist of forbidden *key names* (the previous version of this test)
// only catches a leak that keeps using the same field name a reviewer
// already knows to look for — it does nothing against a differently-named
// field (`savedAuth`, `material`, ...) that forwards the same secret value.
// Asserting the CANARY substrings are literally absent from the serialized
// output catches the leak regardless of what the leaking field is called.
const CANARIES = ['CANARY-password-must-not-leak', 'CANARY-private-key-must-not-leak', 'CANARY-passphrase-must-not-leak']

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
    // entry (desktop/ssh_sync.go sshSyncHost/sshSyncKey). Three assertions,
    // each catching a different way this could regress:
    const { hosts, keys } = openSSHHostsBlob(accountKey, fixture.ssh_hosts_value)

    // 1. A length guard of its own — without this, the assertions below
    //    pass vacuously against a reader that returns `{ hosts: [], keys: [] }`.
    //    Don't rely on the neighbouring 'opens the Go-sealed blob' test to
    //    catch that; weaken that test and this one must still fail on its own.
    expect(hosts.length).toBeGreaterThan(0)
    expect(keys.length).toBeGreaterThan(0)

    // 2. CANARY-substring check on the full serialized output. A denylist of
    //    forbidden key *names* is defeated by renaming the leaking field
    //    (e.g. `savedAuth`, `material`); grepping the actual secret value out
    //    of the serialized objects catches the leak under any field name.
    const serializedHosts = JSON.stringify(hosts)
    const serializedKeys = JSON.stringify(keys)
    for (const canary of CANARIES) {
      expect(serializedHosts).not.toContain(canary)
      expect(serializedKeys).not.toContain(canary)
    }

    // 3. Exact key ALLOWLIST on one decoded host and one decoded key: a new
    //    field must be added to this list deliberately, rather than being
    //    inherited silently from a destructure of the raw wire entry.
    const h1 = hosts.find((h) => h.id === 'h1')!
    expect(Object.keys(h1).sort()).toEqual(
      ['alias', 'authKind', 'hasJumpChain', 'host', 'id', 'isProxyCommandHost', 'keyId', 'note', 'port', 'tags', 'user'].sort(),
    )
    const k1 = keys.find((k) => k.id === 'k1')!
    expect(Object.keys(k1).sort()).toEqual(['id', 'keyType', 'name'].sort())
  })

  it('fails cleanly on a wrong account key', () => {
    const wrongKey = new Uint8Array(32).fill(0xff)
    expect(() => openSSHHostsBlob(wrongKey, fixture.ssh_hosts_value)).toThrow()
  })
})
