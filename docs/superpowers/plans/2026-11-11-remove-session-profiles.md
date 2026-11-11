# Remove Session Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove Session Profiles and profile-based remote shell creation from every current client while retaining only the released protocol, AAD, config-sync, and Relay preference compatibility tombstones.

**Architecture:** Remove the feature from the outside in so each commit remains buildable: first remove all new-client UI and initiation APIs, then replace host-side execution with an immediate `feature_removed` response, then retire active persistence/sync paths, and finally delete the local Profile model and session-spawn branches. Released identifiers remain registered and old preference writes remain accepted, but no new build can create or execute a profile.

**Tech Stack:** Go 1.23, Vue 3, TypeScript strict mode, Vitest, Wails v2, Capacitor 8, Vite, repository-native protocol/config-sync packages.

**Spec:** `docs/superpowers/specs/2026-11-11-remove-session-profiles-design.md`

## Global Constraints

- `internal/proto.Version` remains `1`; `SESSION_CREATE=0x3b` and `SESSION_CREATED=0x3c` stay reserved with their existing payload shape.
- Peer record kinds `RecordSessionCreate=0x10` and `RecordSessionCreated=0x11` stay reserved and carry only the same compatibility request/rejection exchange.
- AAD `0xF1` remains registered forever and is renamed/documented as retired; it must never be reused.
- `profiles_encrypted` leaves every active client sync list but remains an accepted Relay preference key of JSON-string kind.
- New desktop, Web, and Capacitor clients expose no Profile UI, Profile API, or remote-create initiation path.
- A released old client talking to a new desktop gets one `SESSION_CREATED` error with the same `request_id`, nil session id, and `error="feature_removed"`; no PTY is forked.
- No fallback opens a default shell for an old request.
- Legacy `profiles`, `profile_config`, and `profile_env` records may remain in the generic encrypted replica but never project into `appConfig` or export back to Relay.
- Existing Relay ciphertext is not bulk-deleted.
- AI recovery's Go-side `SetOnFirstPrompt` resume injection must remain unchanged after Profile startup-command injection is removed.
- Do not add frontend dependencies. Keep desktop/Web/Capacitor on the shared `desktop/frontend/src/App.vue` implementation.
- Web source changes require Node 20 and a deterministic `./scripts/build-web.sh` rebuild committed under `internal/relay/web-dist/`.
- Execute in an isolated worktree created with `superpowers:using-git-worktrees`; preserve unrelated user changes if execution is instead explicitly approved in the current tree.

---

### Task 1: Remove current-client Profile surfaces and initiation APIs

**Files:**
- Delete: `desktop/frontend/src/components/SettingsProfiles.vue`
- Delete: `desktop/frontend/src/components/SettingsProfiles.test.ts`
- Delete: `desktop/frontend/src/components/SettingsProfilesMobile.vue`
- Delete: `desktop/frontend/src/components/SettingsProfilesMobile.test.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.test.ts`
- Modify: `desktop/frontend/src/components/SettingsResponsiveLayout.test.ts`
- Modify: `desktop/frontend/src/components/SettingsConfigIO.vue`
- Modify: `desktop/frontend/src/components/SettingsConfigIO.test.ts`
- Modify: `desktop/frontend/src/lib/api/configio.ts`
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/TabBar.test.ts`
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`
- Modify: `desktop/frontend/src/lib/proto.ts`
- Modify: `desktop/frontend/src/lib/peerSessionConnection.ts`
- Modify: `desktop/frontend/src/lib/peerSessionConnection.test.ts`
- Modify: `desktop/frontend/src/lib/syncedBlobs.ts`
- Modify: `desktop/frontend/src/lib/syncedBlobs.test.ts`
- Modify: `desktop/frontend/src/lib/syncedBlobSource.ts`
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/platform/__tests__/wails.test.ts`
- Modify: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`
- Modify: `desktop/frontend/src/components/SyncStatusIndicator.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Rebuild: `internal/relay/web-dist/**`

**Interfaces:**
- Consumes: current `Platform.sessions`, `TabBar`, Settings tabs, Profile blob reader, and Wails/Capacitor bridge APIs.
- Produces: a current-client surface with no `SessionProfile`, `createSessionWithProfile`, `listRemoteHosts`, `SESSION_CREATE`, `SESSION_CREATED`, Profile Settings tabs, or `profile_id` in local spawn requests. Go bindings may still exist temporarily but have no current frontend caller.

- [ ] **Step 1: Replace feature-positive frontend tests with retirement assertions**

Delete the Profile component suites and replace the relevant sections of the shared tests with assertions using the existing `mountDialog`, `navLabels`, and `mountAppWithActiveLocalCwd` helpers:

```ts
it('does not expose session profile settings on desktop or Capacitor', async () => {
  const wrapper = mountDialog()
  expect(navLabels(wrapper)).not.toContain('Session profiles')
  expect(wrapper.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(false)
})

it('does not expose remote profile creation on current platforms', () => {
  expect(createWailsPlatform().sessions).not.toHaveProperty('createSessionWithProfile')
  expect(createCapacitorPlatform().sessions).not.toHaveProperty('createSessionWithProfile')
})

// Append this assertion to the existing new-tab and split tests inside the
// describe block that owns mountAppWithActiveLocalCwd.
const request = newSessionMock.mock.calls.at(-1)?.[0] as Record<string, unknown>
expect(request).not.toHaveProperty('profile_id')
```

In `syncedBlobs.test.ts`, keep the SSH golden-vector coverage and replace the Profile-vector suite with a module-surface assertion:

```ts
import * as syncedBlobs from './syncedBlobs'

expect(syncedBlobs).not.toHaveProperty('openProfilesBlob')
```

- [ ] **Step 2: Run the focused tests and confirm they fail against the current feature**

Run:

```bash
cd desktop/frontend
npx vitest run src/components/SettingsDialog.test.ts src/components/TabBar.test.ts src/App.test.ts src/lib/syncedBlobs.test.ts src/platform/__tests__/wails.test.ts src/platform/__tests__/capacitor.test.ts
```

Expected: FAIL because Profile tabs, platform methods, blob reader, selector, and `profile_id` wiring still exist.

- [ ] **Step 3: Remove the Profile UI and App/TabBar state**

Delete both Profile components and their dedicated tests. In `SettingsDialog.vue`, remove the `profiles` and `mobile-profiles` tab ids, nav metadata/icons, imports, events, hidden-tab logic, and component mounts. In `TabBar.vue`, remove `SessionProfile`, the `profiles`/`selectedProfileId` props, `select-profile` emit, and `<select class="profile-picker">` markup.

In `App.vue`, remove the Profile imports, `profiles`, `selectedProfileId`, `loadProfiles`, `onProfilesChanged`, `onMobileSessionCreated`, related Settings events, and TabBar props/events. Local creation becomes:

```ts
const sid = await spawnLocalShell(spawnCwd, predictCellDims('single'))
// split path uses the same two-argument shape with result.layout
```

Remove the corresponding Profile sections from `SettingsResponsiveLayout.test.ts`, `SettingsDialog.test.ts`, `TabBar.test.ts`, and `App.test.ts`; keep unrelated layout, Settings, tab, and spawn coverage intact.

In `SettingsConfigIO.vue`, remove the `includeLocalEnv` ref, checkbox, hint, and Profile-env copy. Change the current frontend wrapper to a no-argument API while the still-old Go binding is called with a fixed compatibility value:

```ts
export function exportConfig(): Promise<string> {
  return bindings().ExportConfig(false)
}
```

Update `SettingsConfigIO.test.ts` to assert that export is a single command with no Profile-env option and that the wrapper passes the fixed `false` only at the temporary Wails boundary.

- [ ] **Step 4: Remove current TypeScript Profile and remote-create APIs**

Remove `SessionProfile` and its Get/Set wrappers from `lib/api.ts` and `_bindings.ts`. Remove Profile shapes and `openProfilesBlob` from `syncedBlobs.ts`, leaving SSH blob handling and its `AAD_TAG_SSH_HOSTS` path untouched.

Remove from `SessionBridge`:

```ts
listRemoteHosts?(): Promise<RemoteHost[]>
createSessionWithProfile?(hostID: string, profileID: string): Promise<string>
```

Then remove their Wails and Capacitor implementations, the `CreatePeerSessionWithProfile` import, the Peer connection request/pending-response state, and TypeScript `TYPE.SESSION_CREATE/SESSION_CREATED`. Keep the Go/protocol constants; only current TypeScript initiation is removed here.

Remove Profile sync-status labels and all Profile/mobile-Profile copy from both locale files. Update comments in `syncedBlobSource.ts` so it names only active sealed blobs.

- [ ] **Step 5: Run the full shared frontend tests and builds**

Run with Node 20:

```bash
source "$HOME/.nvm/nvm.sh"
nvm use 20
cd desktop/frontend
npm test
npm run build
npm run build:capacitor
cd ../../web
npm test
npm run test:contract
cd ..
./scripts/build-web.sh
cd mobile
npm test
```

Expected: all commands PASS; `internal/relay/web-dist/` is rebuilt deterministically and contains no Profile UI strings.

- [ ] **Step 6: Commit the current-client removal and embedded web output**

```bash
git add desktop/frontend/src internal/relay/web-dist
git commit -m "refactor(frontend): remove session profile surfaces"
```

### Task 2: Replace remote Profile execution with compatibility rejection

**Files:**
- Delete: `desktop/session_create_handler.go`
- Delete: `desktop/session_create_handler_test.go`
- Create: `desktop/session_create_retired.go`
- Create: `desktop/session_create_retired_test.go`
- Modify: `desktop/uplink.go`
- Modify: `desktop/uplink_session_create_test.go`
- Modify: `desktop/peer_session_control.go`
- Modify: `desktop/peer_session_control_test.go`
- Modify: `desktop/peer_quicktunnel_host.go`
- Modify: `desktop/peer_quicktunnel_host_test.go`
- Modify: `internal/proto/frame.go`
- Test unchanged compatibility router: `internal/relay/session_create_router_test.go`

**Interfaces:**
- Consumes: released `proto.SessionCreatePayload` and `proto.SessionCreatedPayload`, existing Relay routing, authenticated Peer control/terminal routes.
- Produces: `retiredSessionCreateFrame(requestID string) proto.Frame`, which always returns `TypeSessionCreated`, nil session id, `OK=false`, and `Error="feature_removed"`; Relay frame bytes `0x3b/0x3c` and Peer record bytes `0x10/0x11` remain reserved, and no function in this task calls `relayHost.NewSession` for a retired request.

- [ ] **Step 1: Write the retired-response tests before deleting the fork handler**

Create `desktop/session_create_retired_test.go` with the core contract:

```go
func TestRetiredSessionCreateFrame(t *testing.T) {
	frame := retiredSessionCreateFrame("request-1")
	if frame.Type != proto.TypeSessionCreated || frame.SessionID != uuid.Nil {
		t.Fatalf("frame=%+v", frame)
	}
	var payload proto.SessionCreatedPayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestID != "request-1" || payload.OK || payload.SessionID != "" || payload.Error != "feature_removed" {
		t.Fatalf("payload=%+v", payload)
	}
}
```

Rewrite the uplink integration test so a real `TypeSessionCreate` gets `feature_removed` and the registry/session snapshot count does not increase. Rewrite one attached Peer-route test and one zero-session host-control test to assert the same response and matching request id.

- [ ] **Step 2: Run the focused Go tests and confirm the new symbol/behavior is absent**

Run:

```bash
go test -tags webkit2_41 ./desktop/ -run 'RetiredSessionCreate|Uplink.*SessionCreate|Peer.*SessionCreate' -count=1
```

Expected: FAIL because `retiredSessionCreateFrame` is undefined and current handlers still fork or return old errors.

- [ ] **Step 3: Implement the pure compatibility response and wire the Relay uplink**

Create `desktop/session_create_retired.go`:

```go
package main

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

const sessionCreateErrFeatureRemoved = "feature_removed"

func retiredSessionCreateFrame(requestID string) proto.Frame {
	payload, _ := json.Marshal(proto.SessionCreatedPayload{
		RequestID: requestID,
		Error:     sessionCreateErrFeatureRemoved,
	})
	return proto.Frame{Type: proto.TypeSessionCreated, Payload: payload}
}
```

In `uplink.go`, delete `sessionCreateExec`, `newSessionCreateHandler`, its concurrency worker, and all fork hooks. Keep strict request decoding; for a non-empty `request_id`, enqueue `retiredSessionCreateFrame(req.RequestID)` without calling `NewSession`. A missing request id is logged and dropped because no response can be correlated.

- [ ] **Step 4: Retire Peer host handling and remove Peer initiation**

Reduce `peer_session_control.go` to authenticated host-side compatibility handling. Remove `CreatePeerSessionWithProfile`, `peerSessionCreateExchange`, direct/Quick Tunnel client dial helpers, timeout/fallback mapping, host list renderer bindings used only by the deleted UI, and their tests.

Both zero-session host control and attached Peer route must validate the released payload shape, then send only:

```go
response := retiredSessionCreateFrame(request.RequestID)
return channel.SendConfigMessage(ctx, peertransport.RecordSessionCreated, response.Payload)
```

In `peer_quicktunnel_host.go`, delete the session-create worker/channel fields and stream goroutine. Keep authenticated route/permission/lease validation and answer a valid old request directly. Invalid/unauthorized Peer traffic remains rejected before a response is emitted.

- [ ] **Step 5: Mark proto types as retired without changing their shape**

Update `internal/proto/frame.go` comments to state that `0x3b/0x3c` are compatibility tombstones. Keep exactly these three request fields and four response fields:

```go
type SessionCreatePayload struct {
	RequestID string `json:"request_id"`
	HostID    string `json:"host_id"`
	ProfileID string `json:"profile_id"`
}

type SessionCreatedPayload struct {
	RequestID string `json:"request_id"`
	OK        bool   `json:"ok"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}
```

Add `feature_removed` to the documented error values in the Go comment. Do not change Relay routing or reclaim either byte.

- [ ] **Step 6: Run desktop, proto, and Relay compatibility tests**

```bash
gofmt -w desktop/session_create_retired.go desktop/session_create_retired_test.go desktop/uplink.go desktop/uplink_session_create_test.go desktop/peer_session_control.go desktop/peer_session_control_test.go desktop/peer_quicktunnel_host.go desktop/peer_quicktunnel_host_test.go internal/proto/frame.go
go test -tags webkit2_41 -timeout 60s ./desktop/
go test ./internal/proto/ ./internal/relay/
```

Expected: PASS, including existing Relay request correlation/routing tests; no retired request creates a PTY.

- [ ] **Step 7: Commit the remote execution retirement**

```bash
git add desktop internal/proto/frame.go
git commit -m "refactor(session): retire remote profile creation"
```

### Task 3: Retire active Profile persistence, preference sync, import/export, and Peer projection

**Files:**
- Modify: `internal/prefssync/sync.go`
- Modify: `internal/prefssync/sync_test.go`
- Modify: `internal/userstore/preferences.go`
- Modify: `internal/userstore/preferences_synced_keys_test.go`
- Modify: `internal/userstore/preferences_test.go`
- Modify: `internal/configsync/schema.go`
- Modify: `internal/configsync/schema_test.go`
- Modify: `internal/configsync/relay_contract_test.go`
- Modify: `internal/configsync/materialize_test.go`
- Modify: `desktop/prefssync_adapter.go`
- Modify: `desktop/prefssync_adapter_test.go`
- Modify: `desktop/config_export.go`
- Modify: `desktop/config_export_test.go`
- Modify: `desktop/config_import.go`
- Modify: `desktop/config_import_test.go`
- Modify: `desktop/frontend/src/lib/api/configio.ts`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`
- Modify: `desktop/configsync_relay_codec.go`
- Modify: `desktop/configsync_relay_codec_test.go`
- Modify: `desktop/peer_config_bootstrap.go`
- Modify: `desktop/peer_config_projection.go`
- Modify: `desktop/peer_config_replica.go`
- Modify: `desktop/peer_space_test.go`
- Modify: `desktop/synced_blob_vectors_test.go`
- Modify: `desktop/testdata/synced_blob_vectors.json`

**Interfaces:**
- Consumes: current `prefssync.SyncedKeys`, Relay whitelist, `configsync.RelayKeySpec`, plaintext config import/export, Peer canonical replica.
- Produces: `RelayKeySpec.Retired bool`, `configsync.ActiveRelayKeys() []string`, an active sync list without `profiles_encrypted`, and a legacy import result `profiles: retired preference` that does not block unrelated changes.

- [ ] **Step 1: Write tests that distinguish active keys from tombstones**

Update the canonical contract tests to assert:

```go
func TestCanonicalActiveSchemaMatchesDesktopSync(t *testing.T) {
	if got, want := configsync.ActiveRelayKeys(), prefssync.SyncedKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("active configsync keys=%v; prefssync keys=%v", got, want)
	}
	spec, ok := configsync.RelaySpec("profiles_encrypted")
	if !ok || !spec.Retired {
		t.Fatalf("profiles tombstone=%+v ok=%t", spec, ok)
	}
}
```

Add a real userstore test that sends `profiles_encrypted` together with one active key through `SetUserPreferences` and asserts both are accepted/stored. Update `preferences_synced_keys_test.go` so Profile is absent from `expectedSyncedKeyKinds` but separately asserted as a retired whitelist entry of `preferenceKindString`.

Add an import regression:

```go
func TestApplyConfigImport_RetiredProfilesSkippedRestApplied(t *testing.T) {
	// Export contains both "profiles" and "terminal_theme".
	// Assert report.Skipped contains "profiles: retired preference",
	// terminal_theme is applied, and no Profile change is reported.
}
```

- [ ] **Step 2: Run focused tests and confirm current active handling fails them**

```bash
go test ./internal/prefssync/ ./internal/userstore/ ./internal/configsync/ -count=1
go test -tags webkit2_41 ./desktop/ -run 'Prefs|ConfigImport|ConfigExport|ConfigSync|PeerSpace|SyncedBlob' -count=1
```

Expected: FAIL because Profile is still active, `Retired`/`ActiveRelayKeys` do not exist, and legacy imports still apply Profile data.

- [ ] **Step 3: Split active config-sync keys from retired schema entries**

Extend the schema type and active-key helper:

```go
type RelayKeySpec struct {
	Key         string
	Mode        RelayValueMode
	Collections []string
	KeyClasses  []KeyClass
	Retired     bool
	shape       relayJSONShape
}

func ActiveRelayKeys() []string {
	var keys []string
	for _, spec := range relayKeySpecs {
		if !spec.Retired {
			keys = append(keys, spec.Key)
		}
	}
	return keys
}
```

Set `Retired: true` only on `profiles_encrypted`. Keep `RelayKeys`, `RelaySpec`, the three Profile collection constants, base64 shape validation, and record validation as tombstones. Make every desktop loop over `RelayKeySpecs()` skip `spec.Retired` before materialization, projection, bootstrap, local append, or Relay export. Generic replica storage may retain old records.

- [ ] **Step 4: Remove Profile from active preference sync but keep Relay acceptance**

Remove `profiles_encrypted` from `internal/prefssync.syncedKeys`, `appConfigAdapter.ReadValue/WriteValue`, `isPrefCustomized`, dirty tracking, and Profile adapter tests. Leave this Relay entry in `allowedPreferenceKeys` with an explicit tombstone comment:

```go
// Compatibility tombstone: released clients may include this opaque string
// in a batch PUT. Accept it so unrelated active preferences are not rejected.
"profiles_encrypted": preferenceKindString,
```

New desktop pulls now ignore stored Profile rows because the key is absent from `SyncedKeys`.

- [ ] **Step 5: Remove Profile config import/export and sealed codecs**

Remove `profilesExportPayload`, Profile export assembly, Profile preview/merge/apply functions, and Profile changes from config I/O tests. Change the internal builder to `BuildConfigExport() (ConfigExport, error)`. Keep only the exported Wails boundary parameter as an ignored compatibility input so generated IPC does not need an unrelated signature migration:

```diff
-func (a *App) BuildConfigExport(includeLocalEnv bool) (ConfigExport, error) {
+func (a *App) BuildConfigExport() (ConfigExport, error) {
```

Delete the `cfg.Profiles`/`stripUnsyncedEnv`/`profilesExportPayload` block from that builder. Keep the Wails method's save behavior exactly as follows, with only its now-ignored compatibility argument and builder call changed:

```go
func (a *App) ExportConfig(_ bool) (string, error) {
	export, err := a.BuildConfigExport()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", err
	}

	defaultName := "atterm-config-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ".json"
	sd := a.saveDialog
	if sd == nil {
		sd = wailsruntime.SaveFileDialog
	}
	path, err := sd(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export config",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	wf := a.writeFile
	if wf == nil {
		wf = os.WriteFile
	}
	if err := wf(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
```

Update every Go caller/test to call `BuildConfigExport()` without a boolean. The frontend `exportConfig()` remains no-argument and continues passing `false` only to the compatibility Wails binding; update its comments and `_bindings.ts` comment so neither mentions Profile env.

Treat the legacy plaintext key explicitly:

```go
case "profiles":
	preview.Skipped = append(preview.Skipped, "profiles: retired preference")
	continue
```

Keep `profiles` in the import recognizer solely so it receives that stable retired reason rather than a generic unknown-key error. Remove Profile sealed Relay codec cases, Profile Peer bootstrap/projection, and Profile vector generation. Leave SSH sealed blobs and their vectors unchanged.

- [ ] **Step 6: Run sync/config suites and check active code has no Profile data path**

```bash
gofmt -w internal/prefssync internal/userstore/preferences.go internal/userstore/preferences_synced_keys_test.go internal/userstore/preferences_test.go internal/configsync desktop/prefssync_adapter.go desktop/prefssync_adapter_test.go desktop/config_export.go desktop/config_export_test.go desktop/config_import.go desktop/config_import_test.go desktop/configsync_relay_codec.go desktop/configsync_relay_codec_test.go desktop/peer_config_bootstrap.go desktop/peer_config_projection.go desktop/peer_config_replica.go desktop/peer_space_test.go desktop/synced_blob_vectors_test.go
go test ./internal/prefssync/ ./internal/userstore/ ./internal/configsync/
go test -tags webkit2_41 -timeout 60s ./desktop/
rg -n 'sealProfiles|openProfiles|projectProfiles|profiles_encrypted' desktop internal/prefssync internal/configsync internal/userstore
```

Expected: tests PASS. Search results are limited to the still-temporary local Profile model/functions awaiting Task 4, schema/whitelist tombstones, tests for tombstones, and historical comments that will be cleaned in later tasks; no active adapter, exporter, importer, bootstrap, projection, or Relay export handles Profile data.

- [ ] **Step 7: Commit active data-path retirement**

```bash
git add internal/prefssync internal/userstore internal/configsync desktop
git commit -m "refactor(sync): retire session profile data"
```

### Task 4: Delete the local Profile model and session-spawn behavior

**Files:**
- Delete: `desktop/profiles.go`
- Delete: `desktop/profiles_test.go`
- Delete: `desktop/relay_host_profile_test.go`
- Delete: `desktop/app_profiles_bindings_test.go`
- Modify: `desktop/app_nil_slice_test.go`
- Modify: `desktop/app.go`
- Modify: `desktop/config.go`
- Modify: `desktop/config_test.go`
- Modify: `desktop/relay_host.go`
- Verify: `desktop/relay_host_recovery_test.go`
- Modify generated: `desktop/frontend/wailsjs/go/main/App.js`
- Modify generated: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify generated: `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: the now-unreferenced local `SessionProfile` model, `NewSessionReq.ProfileID`, Profile Get/Set bindings, and Profile branches in `relayHost.NewSession`.
- Produces: `NewSessionReq` without `ProfileID`; `relayHost.NewSession` uses only `req.Command`, normal `req.Cwd`/HOME resolution, terminal environment, shell integration, and independent AI recovery.

- [ ] **Step 1: Add a legacy-config drop regression before removing fields**

Add to `desktop/config_test.go`:

```go
func TestRetiredProfileFieldsLoadAndDropOnSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { t.Fatal(err) }
	raw := []byte(`{"locale_preference":"en","profiles":[{"id":"p1","name":"old"}],"default_profile_id":"p1"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil { t.Fatal(err) }
	store := loadConfig()
	if store.Get().LocalePreference != "en" { t.Fatal("active field did not load") }
	if err := store.Set(store.Get()); err != nil { t.Fatal(err) }
	data, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(data, &persisted); err != nil { t.Fatal(err) }
	if _, ok := persisted["profiles"]; ok { t.Fatal("retired profiles survived rewrite") }
	if _, ok := persisted["default_profile_id"]; ok { t.Fatal("retired default_profile_id survived rewrite") }
}
```

- [ ] **Step 2: Run the config regression and confirm it fails while fields are typed**

```bash
go test -tags webkit2_41 ./desktop/ -run TestRetiredProfileFieldsLoadAndDropOnSave -count=1
```

Expected: FAIL because `appConfig` still preserves and rewrites both fields.

- [ ] **Step 3: Remove the model, bindings, and config fields**

Delete `profiles.go` and its tests. Remove `Profiles`, `DefaultProfileID`, and Profile deep-copy logic from `appConfig`/`detachMaps`. Remove `GetProfiles`, `SetProfiles`, `GetDefaultProfileID`, and `SetDefaultProfileID` from `App`, plus their tests and nil-slice assertions.

Remove this exact block from `NewSessionReq`:

```diff
-	// ProfileID explicitly selects a SessionProfile for this session,
-	// overriding the configured default profile.
-	ProfileID string `json:"profile_id,omitempty"`
```

- [ ] **Step 4: Simplify `relayHost.NewSession` without disturbing AI recovery**

Delete `resolveSessionProfile`, `applyProfileEnv`, Profile shell/cwd selection, Profile-specific error wrapping, Profile environment merge, Profile logging, `resumeClaimed`, and the Profile startup-command block.

The surviving spawn core is:

```go
command := req.Command
if command == "" {
	return uuid.Nil, fmt.Errorf("empty command")
}
cwd := req.Cwd
if cwd == "" {
	if home, err := os.UserHomeDir(); err == nil {
		cwd = home
	}
}
env := terminalEnvForXterm(os.Environ())
```

Keep the existing AI recovery block, including `computeResumeArgs`, `startResolveGeneration`, and its `sess.SetOnFirstPrompt` callback. Remove only the now-unused `resumeClaimed` assignment/branching.

- [ ] **Step 5: Remove generated Wails Profile bindings and verify regeneration stability**

Delete Profile exports from `App.js`/`App.d.ts` and the `main.SessionProfile` class from `models.ts`. Then run a Wails build to regenerate bindings from Go and ensure the generated tree stays Profile-free:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd desktop
wails build
cd ..
rg -n 'GetProfiles|SetProfiles|GetDefaultProfileID|SetDefaultProfileID|SessionProfile' desktop/frontend/wailsjs
```

Expected: Wails build PASS; final search returns no matches.

- [ ] **Step 6: Run desktop tests with explicit AI recovery coverage**

```bash
gofmt -w desktop/app.go desktop/config.go desktop/config_test.go desktop/relay_host.go
go test -tags webkit2_41 -timeout 60s ./desktop/ -run 'RetiredProfileFields|RelayHost.*Recovery|Resume|FirstPrompt' -count=1
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: PASS; legacy config loads and drops retired fields on the next save, ordinary session creation works, and AI resume injection still passes.

- [ ] **Step 7: Commit the model/runtime deletion**

```bash
git add desktop
git commit -m "refactor(session): remove session profile model"
```

### Task 5: Document the retired identifiers and current architecture

**Files:**
- Modify: `docs/spec/protocol.md`
- Modify: `docs/spec/architecture.md`
- Modify: `internal/e2eecrypto/aadtags.go`
- Modify: `internal/e2eecrypto/aadtags_test.go`
- Modify: `AGENTS.md`
- Modify: `docs/plans-history.md`
- Verify historical only: `docs/superpowers/specs/2026-08-17-session-profiles-design.md`
- Verify historical only: `docs/superpowers/specs/2026-08-21-mobile-profiles-hosts-design.md`

**Interfaces:**
- Consumes: final code state from Tasks 1-4 and the compatibility identifiers retained there.
- Produces: current documentation that calls Profiles removed, names `feature_removed`, and marks `0x3b`, `0x3c`, `0xF1`, `profiles_encrypted`, and Profile collections as retired/reserved.

- [ ] **Step 1: Add/adjust the AAD registry test expectation first**

Keep `AADTagProfiles byte = 0xF1`, but change its registry name to make retirement explicit:

```go
AADTagProfiles: "profiles_encrypted sync (retired)",
```

The existing uniqueness/protocol-table test must continue to require `0xF1`. Add an assertion that the registry label contains `retired`, then run:

```bash
go test ./internal/e2eecrypto/ -run AAD -count=1
```

Expected before the label change: FAIL on the new retirement-label assertion.

- [ ] **Step 2: Update protocol and architecture documentation**

In `docs/spec/protocol.md`:

- retain `0x3b/0x3c` in the frame table and mark both retired;
- retain Peer record kinds `0x10/0x11` and mark their create exchange retired;
- replace the active remote-create narrative with the released payload shapes plus the rule that current hosts return `feature_removed` and never fork;
- retain `0xF1` in the E2EE table as retired/reserved;
- state that `profiles_encrypted` may still be accepted/stored solely for old batch compatibility.

In `docs/spec/architecture.md`, remove Profile creation as a current desktop/mobile/Peer capability. Describe only the compatibility rejection where mixed-version routing is relevant. Do not add a replacement workspace or default-shell create feature.

- [ ] **Step 3: Update repository guidance and history**

In `AGENTS.md` redline #22, render the allocation as:

```text
profiles_encrypted sync（合成、已退休、禁止复用）=0xF1
```

The current `atterm-map` has no Session Profile route, so leave it unchanged. Add a concise top entry to `docs/plans-history.md` linking the removal spec and this plan; word the entry as the release-note sentence for the removal. Historical specs remain unchanged.

- [ ] **Step 4: Run documentation/registry checks and a current-doc search**

```bash
gofmt -w internal/e2eecrypto/aadtags.go internal/e2eecrypto/aadtags_test.go
go test ./internal/e2eecrypto/ ./internal/proto/
rg -n 'Session Profiles|Session profiles|会话配置档|profiles_encrypted|SESSION_CREATE|0xF1' README.md AGENTS.md docs/spec internal/e2eecrypto internal/proto
```

Expected: tests PASS. Matches in current docs/code describe retirement/compatibility only; historical `docs/superpowers/specs` and `docs/superpowers/plans` may still describe the feature in their dated context.

- [ ] **Step 5: Commit documentation and registry wording**

```bash
git add docs/spec docs/plans-history.md internal/e2eecrypto AGENTS.md
git commit -m "docs: retire session profile protocol"
```

### Task 6: Run repository-wide removal and release verification

**Files:**
- Verify all files changed in Tasks 1-5.
- Rebuild if drifted: `internal/relay/web-dist/**`
- Update only if verification exposes a real omission: source/test files named by the failing command.

**Interfaces:**
- Consumes: the complete removal branch.
- Produces: fresh evidence that all supported builds pass, embedded Web assets match source, and only explicit compatibility/historical references remain.

- [ ] **Step 1: Verify the source-reference allowlist**

Run:

```bash
rg -n 'SessionProfile|GetProfiles|SetProfiles|GetDefaultProfileID|SetDefaultProfileID|createSessionWithProfile|openProfilesBlob|selectedProfileId' \
  desktop internal web mobile cmd \
  --glob '!**/*history*' \
  --glob '!**/node_modules/**' \
  --glob '!internal/relay/web-dist/**'
```

Expected: no matches.

Verify the frozen `ProfileID` field separately:

```bash
rg -n 'ProfileID|profile_id' desktop internal \
  --glob '!**/node_modules/**'
```

Expected: matches are limited to `SessionCreatePayload`, strict validation/routing of that retired payload, compatibility tests, and retirement comments. There must be no `NewSessionReq.ProfileID`, Profile lookup, or PTY-spawn use.

Run the compatibility-reference search:

```bash
rg -n 'profiles_encrypted|CollectionProfiles|CollectionProfileConfig|CollectionProfileEnv|AADTagProfiles|TypeSessionCreate|TypeSessionCreated|RecordSessionCreate|RecordSessionCreated|feature_removed' \
  desktop internal docs/spec AGENTS.md \
  --glob '!**/node_modules/**'
```

Expected: matches are limited to protocol/schema/Relay whitelist tombstones, deterministic rejection code/tests, and current documentation that labels them retired.

- [ ] **Step 2: Run all Go verification required by the repository**

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
go test ./internal/prefssync/ ./internal/userstore/ ./internal/relay/ ./internal/configsync/ ./internal/e2eecrypto/ ./internal/proto/
```

Expected: every command exits `0` with no failing package.

- [ ] **Step 3: Run all frontend/mobile verification with Node 20**

```bash
source "$HOME/.nvm/nvm.sh"
nvm use 20
cd desktop/frontend
npm test
npm run build
npm run build:capacitor
cd ../../web
npm run build
npm test
npm run test:contract
cd ../mobile
npm test
cd ..
```

Expected: every command exits `0`; no Profile navigation or selector appears in built desktop, Web, or Capacitor output.

- [ ] **Step 4: Rebuild and verify embedded Web assets**

```bash
./scripts/build-web.sh
./scripts/check-web-dist.sh
git status --short internal/relay/web-dist
```

Expected: `check-web-dist.sh` prints `embedded web assets match a fresh build.` If the rebuild changes tracked output, stage it and amend the Task 1 frontend commit or create one explicit generated-assets commit:

```bash
git add internal/relay/web-dist
git commit -m "build(web): refresh embedded assets after profile removal"
```

- [ ] **Step 5: Inspect final diff and commit graph**

```bash
git diff --check
git status --short
git log --oneline --decorate -8
```

Expected: no whitespace errors, no uncommitted source/generated changes, and the five planned logical commits (plus an optional generated-assets commit only if Task 6 found drift).
