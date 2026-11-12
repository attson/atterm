# Remove Session Profiles

Date: 2026-11-11
Status: Drafted - awaiting user review
Replaces: [2026-08-17-session-profiles-design.md](./2026-08-17-session-profiles-design.md)

## 0. Decision

Remove Session Profiles as a product feature. The feature's user value does not justify its
surface area: a single-session launch preset currently requires dedicated desktop and mobile UI,
session-spawn branching, E2EE preference sync, config import/export, Peer config replication, and
remote session-create routing.

New builds will not let users define, select, sync, inspect, or launch a Session Profile. Mobile
and Peer remote session creation by profile is removed with it. New local tabs and splits use the
existing non-profile path: the configured default shell and the caller's normal working-directory
choice.

The feature shipped in `v0.6.27`, so its external identifiers cannot be erased or reused. They
remain only as compatibility tombstones: reserved identifiers, an old preference-key acceptance
boundary, and deterministic rejection of requests from older clients. A tombstone does not retain
a hidden launch path.

## 1. Goals

- Remove all desktop and mobile Session Profile UI.
- Remove Profile-dependent local and remote session creation.
- Remove the Profile model, Wails bindings, active preference sync, import/export, and Peer config
  projection.
- Make mixed-version behavior deterministic and bounded.
- Permanently reserve the identifiers already visible to released clients.
- Reduce tests and implementation to the minimum compatibility boundary.

## 2. Non-goals

- Do not replace profiles with workspace templates, tab groups, or another launcher.
- Do not add remote "open default shell" behavior.
- Do not bulk-delete existing `profiles_encrypted` rows from Relay databases.
- Do not rewrite historical design documents or git history. Historical specs remain historical;
  current architecture and protocol docs describe the retired state.
- Do not reuse retired frame types, AAD tags, preference keys, or config collection names.

## 3. Resulting user behavior

### 3.1 Desktop

- Settings no longer contains **Session profiles**.
- The tab bar no longer contains a profile selector.
- New tab uses the configured default shell and the existing default working directory.
- New split retains today's non-profile behavior, including the caller-selected/current working
  directory where applicable.
- No profile can override shell, cwd, startup command, or environment.
- Desktop Settings no longer offers "Open on a Peer device" for a profile.

### 3.2 Mobile

- Settings no longer contains **Synced profiles**.
- Mobile cannot ask a desktop to create a shell. It can still discover and attach to sessions that
  already exist.
- Synced SSH-host display and all unrelated mobile settings remain unchanged.

### 3.3 Existing data

- New clients stop reading and writing `profiles_encrypted`.
- Existing local JSON fields `profiles` and `default_profile_id` are treated as retired input. Go's
  normal unknown-field behavior allows old config files to load; the next successful config save
  rewrites the typed config without those fields. This removal is intentional, and downgrading
  afterward does not recover the old local copy.
- Existing Relay ciphertext is left in place. It is unread by the Relay and ignored by new clients.
  No destructive server-side sweep is introduced.
- Config imports containing a legacy `profiles` entry do not fail the entire import. The entry is
  reported as retired/skipped, while unrelated preferences continue through the normal preview and
  apply flow.

## 4. Compatibility tombstones

### 4.1 Protocol frame types

`SESSION_CREATE (0x3b)` and `SESSION_CREATED (0x3c)` remain declared in `internal/proto` and listed
in `docs/spec/protocol.md` as retired. Their byte values are permanently unavailable for another
meaning.

New clients never send `SESSION_CREATE`. A new desktop receiving a structurally valid request from
an older Relay or Peer client returns one `SESSION_CREATED` response with:

```json
{
  "request_id": "the-original-request-id",
  "ok": false,
  "error": "feature_removed"
}
```

The response has a nil session id. Invalid payloads retain the existing `invalid_request` behavior
when enough information exists to address a response. Requests are never forwarded to PTY creation
and never wait for the old 30-second client timeout.

Relay routing for these two retired frame types remains compatible with old endpoints. This lets a
released old client receive the host's deterministic rejection and prevents a Relay upgrade alone
from corrupting the wire contract. The active new-client initiation APIs and the old host fork
handler are removed.

Authenticated Peer host routes keep only the same minimal rejection responder. They do not keep
profile lookup, permission-to-command mapping, or session creation.

### 4.2 Preference key

`profiles_encrypted` is removed from `internal/prefssync.SyncedKeys`, desktop adapters, dirty-state
tracking, and UI status labels. Therefore new clients neither pull nor push it.

The Relay's `allowedPreferenceKeys` retains `profiles_encrypted` with its historical JSON-string
shape. This is an input-compatibility tombstone: an old client may include the key in a batch PUT
without causing the entire batch, including unrelated current preferences, to fail. The Relay
continues to store the opaque value but never interprets it.

The whitelist entry must carry a retirement comment and a test proving two properties:

1. an old-style PUT containing the key is accepted; and
2. the current desktop sync-key list does not contain it.

### 4.3 AAD and config-sync identifiers

AAD `0xF1` remains in `internal/e2eecrypto.AADTags`, the protocol table, and `AGENTS.md`, renamed as
the retired `profiles_encrypted` namespace. It is never reassigned.

The legacy config-sync collection identifiers `profiles`, `profile_config`, and `profile_env` remain
recognized tombstones where schema validation requires knowledge of historical records. New desktop
code does not bootstrap, project, materialize into app config, or emit those collections. Incoming
legacy records may remain in the generic encrypted replica, but are excluded from app-config
projection and ignored without rejecting unrelated records in the same bundle.

`internal/configsync.RelayKeys` currently mirrors active `prefssync.SyncedKeys`. That contract is
split into active and retired sets: active keys must still match the desktop sync engine, while
`profiles_encrypted` is asserted only in the retired compatibility set. Generic validation may
recognize the retired key; no active adapter may decode or materialize it.

## 5. Implementation boundaries

### 5.1 Delete active frontend surfaces

- Delete `SettingsProfiles.vue` and `SettingsProfilesMobile.vue` and their tests.
- Remove both Settings tabs, icons, events, and i18n strings.
- Remove the TabBar profile props, selector, and selection event.
- Remove App-level profile loading, selected-profile state, refresh events, and Profile IDs from
  local spawn requests.
- Remove `createSessionWithProfile` and profile views/codecs from the current platform interfaces,
  Capacitor bridge, Wails bridge, and their tests.
- Regenerate or reconcile Wails bindings after the Go APIs disappear.

### 5.2 Delete active desktop behavior

- Remove `SessionProfile`, `Profiles`, `DefaultProfileID`, and the Get/Set bindings.
- Remove `ProfileID` from new-session requests and remove profile resolution, environment merging,
  and startup-command injection from `relayHost.NewSession`.
- Preserve AI recovery's first-prompt injection independently; deleting profile startup commands
  must not change recovery resume behavior.
- Remove Profile handling from config clone, import/export, preference adapters, relay codecs, Peer
  config bootstrap/projection, and local dirty-state logic.
- Replace the forking `sessionCreateHandler` with the minimal retired-request responder described in
  section 4.1.
- Remove current-client Peer session-create controls while keeping host-side rejection for released
  peers.

### 5.3 Keep only compatibility metadata and responders

The following are intentionally not deleted:

- `TypeSessionCreate`, `TypeSessionCreated`, and the minimal payload structs needed to reject an old
  request with its original `request_id`;
- Relay routing required to deliver that rejection across old/new version boundaries;
- the Relay preference whitelist entry for `profiles_encrypted`;
- AAD `0xF1` and legacy config-sync identifier registrations; and
- focused compatibility tests for those tombstones.

These retained elements must be labeled `retired` or `compatibility tombstone` in code comments so
they are not mistaken for an incomplete feature.

## 6. Mixed-version matrix

| Requester | Host | Result |
|---|---|---|
| new | new or old | No create UI/API exists; no request is sent. |
| old | new | Immediate `feature_removed`; no PTY is forked. |
| old | old through new Relay | Relay keeps routing the released contract; old behavior remains between old endpoints. |
| new | old | No create UI/API exists; no request is sent. |

For preference sync, old clients may continue to store the retired opaque key. New clients ignore it
and continue syncing every current key. This asymmetry is intentional and avoids coordinating a
flag day across desktop, mobile, and Relay deployments.

## 7. Documentation

- Mark the Remote session create section of `docs/spec/protocol.md` retired, document
  `feature_removed`, and retain the allocated bytes.
- Mark the `0xF1` AAD row retired.
- Remove current Session Profile and remote-create descriptions from `docs/spec/architecture.md`.
- Update `AGENTS.md` AAD allocation text so `0xF1` remains reserved but no longer appears active.
- Remove current-feature navigation from repository maps where applicable; do not edit historical
  specs except to add a superseded link if useful.

## 8. Verification

Automated verification must cover:

- frontend build and unit tests after both settings tabs and the TabBar selector disappear;
- desktop Go tests for ordinary new-tab/split spawning without Profile fields;
- AI recovery resume injection still firing through its existing first-prompt path;
- Relay accepting an old `profiles_encrypted` preference PUT while current `SyncedKeys` excludes it;
- Relay and authenticated Peer paths returning `feature_removed` with the matching request id and
  never invoking PTY creation;
- legacy config JSON loading without a startup failure;
- legacy config import reporting Profile data as retired/skipped without blocking unrelated data;
- protocol/AAD registry tests continuing to reserve `0x3b`, `0x3c`, and `0xF1`; and
- repository search showing no user-facing Profile UI/API references outside historical docs and
  explicit compatibility tombstones.

Run the relevant focused suites followed by:

```bash
go test -tags webkit2_41 -timeout 60s ./desktop/ ./internal/prefssync/ ./internal/userstore/ ./internal/relay/ ./internal/configsync/
cd desktop/frontend && npm test && npm run build
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

Because the shared desktop frontend is also the Web and Capacitor UI, all three frontend builds are
part of the removal gate even though Session Profiles were not exposed in the plain Web build.

## 9. Rollout and failure policy

- Release notes state that Session Profiles and mobile/Peer profile-based session creation were
  removed.
- No automatic retry or fallback creates a default shell for an old request. A fallback would turn
  an explicit removed Profile request into a different command context and violate the rule that a
  remote requester cannot choose execution fields.
- A malformed or old request must fail closed and must never reach `NewSession`.
- Tombstone removal is not scheduled. External numeric identifiers remain reserved permanently;
  the small acceptance/rejection boundary is cheaper than an accidental cross-version reinterpretation.
