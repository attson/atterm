# Configuration export / import — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write the user's configuration to a plain JSON file on disk and read one back, never through the relay, and never carrying a credential.

**Architecture:** A pure encode/decode layer over the synced preference set, plus a merge-with-preview import that writes through the existing `App` setters so imported values sync like any other edit. Export reads host and key *records* from config and never assembles the credential maps that `sealSSHHosts` bundles for the wire.

**Tech Stack:** Go 1.23.12, Wails v2.12.0, Vue 3, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-20-config-export-import-design.md`

## Global Constraints

- **Redline #5:** `internal/` must not import `desktop/`.
- **Redline #4:** no protocol frame payload changes. This item adds none — it is Wails-binding only and never touches the relay.
- **Redline #2:** `SetSubscriberLifecycle` / uplink subscriber count untouched.
- Go: pinned toolchain **go1.23.12**. Node: **20**. `gofmt -l $(git ls-files '*.go')` must be empty — CI gates it.
- Any change under `desktop/frontend/src` requires rebuilding `internal/relay/web-dist/` via `./scripts/build-web.sh`.
- i18n parity is compile-enforced by `satisfies Messages` in `zh-CN.ts`.
- **Shared components ship to web and iOS** (`web/vite.config.ts` aliases the web build's `@` to `desktop/frontend/src`; Capacitor mounts the same shell). Anything desktop-only must self-gate on `platform.caps.wailsBindings` AND have a test mounting it under both shapes. Items 29 and 30 both needed a fix round for this; do not make it three.

## THE constraint

**Export must never write a credential.** `sealSSHHosts` (`desktop/prefssync_adapter.go:108`) bundles four things: `SSHHost` records, a `map[hostID]sshCredential` loaded from the OS keyring, `SSHKey` records, and a `map[keyID]sshKeySecret` — the private keys. That is correct for syncing between two desktops the same user controls. Piping it into a plaintext file would write every stored SSH password and private key to disk in the clear.

The tempting implementation — "unseal the two encrypted keys, dump the result" — is one line and leaks everything. Export reads `c.SSHHosts` and `c.SSHKeys` from config directly and never calls the credential loaders at all. `SSHHost` (`desktop/ssh_hosts_store.go:36`) and `SSHKey` (`desktop/ssh_keys_store.go:14`, holding only `ID`/`Name`/`KeyType`) contain no secret.

---

### Task 1: The export encoder

**Files:**
- Create: `desktop/config_export.go`
- Test: `desktop/config_export_test.go`

**Interfaces:**
- Consumes: `a.cfgStore`, `prefssync.SyncedKeys()`, `stripUnsyncedEnv` (`desktop/profiles.go:47`).
- Produces:
  ```go
  const configExportVersion = 1

  type ConfigExport struct {
      Version     int                        `json:"atterm_export"`
      ExportedAt  string                     `json:"exported_at"`
      AppVersion  string                     `json:"app_version"`
      Preferences map[string]json.RawMessage `json:"preferences"`
  }

  func (a *App) BuildConfigExport(includeLocalEnv bool) (ConfigExport, error)
  func (a *App) ExportConfig(includeLocalEnv bool) (string, error) // save dialog + write, mirrors ExportDiagnostics
  ```

**Behaviour the tests must pin:**
- **No credential ever appears in the output.** Seed the config with hosts and keys, seed the keyring slots with a password and a private key, export, and assert the serialized bytes contain neither. Assert on the BYTES, not on a struct field — a future field addition must fail this test.
- The two sealed keys are written under their unsealed names (`ssh_hosts`, `profiles`), not `ssh_hosts_encrypted` / `profiles_encrypted`.
- `Env` is stripped for profiles with `SyncEnv: false` by calling `stripUnsyncedEnv`, not a reimplementation. With `includeLocalEnv: true`, `Env` is present.
- Every key in `prefssync.SyncedKeys()` that has a value appears; keys with no value are absent rather than `null`.
- `ExportConfig` returns `("", nil)` when the user cancels the dialog — mirror `ExportDiagnostics` (`desktop/app.go:1881`) exactly, including the injectable `a.writeFile` seam it uses for tests.

- [ ] Steps: failing tests → run → implement → run → mutation check (make the credential map get included; confirm the byte-level test fails) → `gofmt -w` → commit.

---

### Task 2: The import decoder and preview

**Files:**
- Create: `desktop/config_import.go`
- Test: `desktop/config_import_test.go`

**Interfaces:**
  ```go
  type ImportChange struct {
      Key    string `json:"key"`
      Action string `json:"action"` // "add" | "replace" | "unchanged"
      Detail string `json:"detail,omitempty"`
  }
  type ImportPreview struct {
      Changes []ImportChange `json:"changes"`
      Skipped []string       `json:"skipped"` // malformed entries, with a reason
  }
  func (a *App) PreviewConfigImport(jsonText string) (ImportPreview, error)
  ```

**Behaviour the tests must pin:**
- `PreviewConfigImport` changes NOTHING. Assert the config store is byte-identical before and after.
- An unknown `atterm_export` version is refused outright with a clear error. Guessing at a future format is how an import silently drops what it did not understand.
- Lists with stable IDs (hosts, profiles) merge by ID: same ID → `replace`, new ID → `add`, and **a local entry absent from the file is KEPT** — assert that explicitly, it is the property that makes import non-destructive.
- A malformed individual entry is skipped and counted, and the rest still import. Same rule `Pull` already follows per key, for the same reason.
- Preview is deterministic in order.

- [ ] Steps as above, with a mutation proving that removing the version check lets an unknown version through.

---

### Task 3: Apply, through the setters

**Files:**
- Modify: `desktop/config_import.go`
- Test: `desktop/config_import_test.go`

**Interfaces:**
  ```go
  type ImportReport struct {
      Applied []ImportChange `json:"applied"`
      Skipped []string       `json:"skipped"`
  }
  func (a *App) ApplyConfigImport(jsonText string, includeLocalEnv bool) (ImportReport, error)
  ```

**Behaviour the tests must pin:**
- Every imported key goes through the existing `App` setter for that key, so it lands in the config store, marks dirty, and syncs. Assert the sync meta is dirty afterwards — writing `configStore` directly would leave imported values invisible to every other device until something else happened to touch the same key.
- `Apply` re-parses the raw text rather than taking a handle from `Preview`. A test should show that the same input produces the same decisions.
- Applying an import enqueues exactly one sync push, not one per key (item 30's loop coalesces; verify the coalescing is actually relied on rather than bypassed).

- [ ] Steps as above, with a mutation writing straight to `cfgStore` and confirming the dirty-meta assertion fails.

---

### Task 4: Bindings, i18n, and the UI

**Files:**
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`, a new `desktop/frontend/src/lib/api/configio.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`, `zh-CN.ts`
- Create: a settings panel section + test

**Behaviour the tests must pin:**
- The export checkbox for local env defaults OFF, and its label says what it means — env vars frequently hold API tokens, and the person who set `SyncEnv: false` already stated their intent once.
- Import shows the preview and applies only after an explicit confirm.
- The panel is desktop-gated and tested under both platform shapes.
- Listeners (if any) are removed on unmount.

---

### Task 5: Roadmap + embed

- [ ] Tick item 31 in `docs/roadmap.md` in the same register as items 26-30: state plainly that credentials are never exported and why the obvious implementation would have leaked them; that import merges and never wipes; that there is no encrypted export format.
- [ ] Rebuild the embed with the pinned toolchain.
- [ ] `go build ./... && go test ./... -race && gofmt -l $(git ls-files '*.go')` empty, and `npx vue-tsc --noEmit && npm test -- --run` clean.
