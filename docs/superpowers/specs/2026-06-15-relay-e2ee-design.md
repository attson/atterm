# Relay end-to-end encryption (E2EE) — design

Date: 2026-06-15
Status: Shipped through v0.2.102; M5 / M6 / M7-audit pending.
        See "Implementation status" below for the per-milestone breakdown.

## 0. Implementation status (as of v0.2.102)

The relay can no longer read terminal output bytes, task summaries,
live current_command strings, or title / cwd / spawn command for any
session whose agent runs with E2EE unlocked. Structural metadata
(session id, host, cols/rows, started_at, task_state, exit_code,
timestamps) remains visible — necessary for routing, push timing,
and session-list ordering.

| Milestone | Status | Tag | Notes |
| --- | --- | --- | --- |
| M1a Server-side OPAQUE | ✅ | v0.2.79 | bytemare/opaque, schema migration, register/login endpoints |
| M1b GET/PUT /api/me/key | ✅ | v0.2.80 | Server endpoints for wrap blob retrieval / rotation |
| M1c Desktop Go SDK | ✅ | v0.2.81 | internal/e2eeclient + Wails RegisterRemoteRelay binding |
| M1d-foundation TS | ✅ | v0.2.92 | Suite swap: ristretto255-SHA512 → P-256-SHA256-Scrypt for browser interop |
| M1d-interop | ✅ | v0.2.93 | TS ↔ Go cross-language OPAQUE round-trip (live test) |
| M1e Desktop UI register | ✅ | v0.2.82 | SettingsRelay segmented Log in / Register + claim_token field |
| M1f Keychain persistence | ✅ | v0.2.91 | zalando/go-keyring; account_key survives desktop relaunch |
| M1g Web auth.ts | ✅ | v0.2.94 | login + register + logout via OPAQUE; sessionStorage account_key |
| M1h Mobile auth (Capacitor) | ✅ | v0.2.95 | AttermSecureStorage account_key persistence |
| M1i Step-up endpoints | ✅ | v0.2.96 | POST /api/auth/stepup/{init,finalize}; 60s single-use token |
| M1i-enforce DELETE /api/me | ✅ | v0.2.97 | X-Step-Up-Token gate on hard-delete |
| M1j Web step-up driver | ✅ | v0.2.98 | requestStepUpToken; deleteMe() drives handshake |
| M2a Crypto primitives | ✅ | v0.2.83 | DeriveSessionKey + Seal/OpenOut + Seal/OpenUnsequenced |
| M2b Agent OUT seal | ✅ | v0.2.84 | sealOutFrame in desktop/uplink forwarder |
| M2c Mirror skip OSC | ✅ | v0.2.85 | session.MarkContentOpaque; relay stops parsing ciphertext |
| M2d Summary via ANNOUNCE | ✅ | v0.2.86 | mergeTaskInfo adopts agent-supplied Summary |
| M2e Agent IN/PASTE open | ✅ | v0.2.87 | openInboundFrame on inbound reader |
| M2f E2E ciphertext test | ✅ | v0.2.88 | TestUplink_E2E_OUTSealedThroughRelay |
| M3a Strip Summary | ✅ | v0.2.89 | stripContentFieldsFromSnapshot drops Summary |
| M3c Strip CurrentCommand | ✅ | v0.2.90 | stripMetaContentFields drops live command |
| M3b-agent Seal title/cwd/command | ✅ | v0.2.99 | sealSessionInfoContent writes SessionInfo.Sealed |
| M3b-web Decrypt SessionInfo | ✅ | v0.2.100 | openSessionFields in web bundle; listSessions overlay |
| M3b-mobile Decrypt SessionInfo | ✅ | v0.2.101 | Capacitor mirror of M3b-web |
| M3b-desktop Decrypt | N/A | — | Wails listRemoteSessions is a stub; nothing to decrypt |
| M3b-strip Drop plaintext | ✅ | v0.2.102 | Relay carries only Sealed for title/cwd/command/current_command |
| M4 OSC133/Summary processor | implicit | — | Agent already runs the parser via local Session; relay drops via M2c |
| M5 META protocol split | partial | — | MetaPayload sealed/clear split not yet formalised as a separate frame schema; agent's TypeMeta forwarder strips Summary + CurrentCommand (M3a/c). Title/Cwd/Command in TypeMeta are NOT yet sealed — only ANNOUNCE/SessionInfo is. |
| M6 Push + Webhook agent-side | pending | — | Web Push body still composed by relay using clear fields; with M3b-strip those are empty for sealed sessions, so labels degrade to "AT Term · session". Agent-composed bodies (with double envelope) not yet wired. |
| M7-status Spec status update | ✅ | this commit | |
| M7-audit External crypto review | pending | — | Self-review + the cross-language interop fixtures are the current proof; external audit not yet engaged. |

### Known gaps after v0.2.102

1. **Web Push notification labels degrade for E2EE-sealed sessions**:
   webPushSessionLabel falls back to the literal "session" when title /
   current_command / command are all empty. Functional but uninformative.
   M6 fixes this by moving body composition to the agent.

2. **TypeMeta frames sent live during a session do not seal title/cwd**:
   The agent's uplink forwarder strips Summary + CurrentCommand from
   live TypeMeta (M3a/c) but does not yet add sealed bytes for Title or
   Cwd. The ANNOUNCE / SessionInfo path is fully sealed; live META updates
   are not. Low impact because the relay falls back to the ANNOUNCE
   snapshot, but worth closing in M5.

3. **No remote viewer sharing**: the design only supports a single user
   account viewing their own sessions across devices. Sharing a session
   with a different account would need a per-session viewer pairing
   flow that does not exist (spec §13.1).

4. **No password recovery**: forgotten password = admin reset = new
   account_key = old session ringbuf ciphertext is permanently
   unrecoverable (spec §13.2). Documented and accepted; sessions are
   ephemeral.

5. **No traffic-analysis defences**: frame counts, sizes, and inter-
   arrival times remain observable to the relay (spec §13.4). Padding
   / timing obfuscation deferred indefinitely.

6. **External cryptographic audit not engaged**: the test surface
   (Go-Go OPAQUE round-trip, TS-Go OPAQUE round-trip via live relay,
   the seal/open round-trip and AAD-binding properties) constitutes
   self-review only. An external review pass on the OPAQUE suite
   choice, the AAD scheme, and the wrap-key derivation is the missing
   M7 work.

## 1. Goal

Make the atterm relay **structurally unable** to read terminal content while
preserving every user-visible feature it offers today.

After this lands:

- A relay operator with full database + traffic access can see who is logged
  in, how many sessions they have, when sessions are active, command
  start/end timestamps, exit codes, task state transitions, and the byte
  rhythm of OUT frames — but **cannot read** terminal input/output bytes,
  command text, cwd, title, command summaries, or webhook bodies.
- Multi-device login keeps working: the same password unlocks the account
  on any device (desktop agent, mobile client, web client).
- Existing functionality stays intact: scrollback replay, session list with
  titles, OSC 133 task tracking, Web Push notifications, webhooks,
  driver/viewer overlay, viewer counts.

Out of scope (see §13): remote viewer collaboration, password recovery,
backward compatibility with the unencrypted protocol, traffic analysis
defenses (padding/timing).

## 2. Threat model

**In scope:**

- Honest-but-curious relay operator who can dump database, inspect traffic,
  and read live process memory at observation time.
- Malicious actor who steals the relay database after the fact.
- Network MITM (already mitigated by TLS; this layer adds defense in depth).

**Out of scope:**

- Active malicious operator who modifies the relay's running code to log
  passwords or extract keys from client requests at the application layer
  (would require remote attestation; not provided).
- Compromise of a logged-in client device (the device's OS keychain holds
  the account key; if an attacker has root on the device they have the key).
- Traffic analysis: frame counts, sizes, and timings remain observable.

## 3. Architecture

```
┌────────────┐                                ┌──────────────┐
│   Agent    │  ──── encrypted frames ────>   │              │  ─── encrypted ──>  ┌─────────┐
│ (desktop)  │  <─── encrypted frames ────    │    Relay     │  <── encrypted ───  │ Client  │
│            │                                │  (blind)     │                     │ (mobile/│
│ PTY +      │  ──── plaintext control ──>    │              │  ─── plaintext ──>  │ desktop/│
│ OSC 133 +  │  <─── plaintext control ───    │              │  <── plaintext ───  │ web)    │
│ summary    │                                └──────────────┘                     └─────────┘
└────────────┘                                       │
     │                                               │
     │ account_key in OS keychain                    │ stores per user:
     │                                               │   opaque_record
     │                                               │   account_key_wrap (AEAD blob)
     │                                               │   account_key_salt
     │                                               │   account_key_kdf_params
     │                                               │
     │                                               │ ringbuf: opaque ciphertext keyed by seq
     │                                               │
     │                                               │ session metadata (plaintext):
     │                                               │   id, host_id, host, user,
     │                                               │   cols, rows, started_at,
     │                                               │   last_output_at, attention_at,
     │                                               │   task_state, exit_code,
     │                                               │   type, remote_permission,
     │                                               │   command_started_at/ended_at/duration_ms
     │
     │ session metadata (encrypted, opaque to relay):
     │   title, cwd, command, current_command, summary
```

Four components, responsibilities reshuffled:

1. **Agent (desktop)** — owns the PTY. Holds `account_key` after login.
   Now also responsible for: parsing OSC 133 sequences, generating
   command summaries (`RecentOutput`, `ErrorLines`), composing Web Push
   notification bodies, and (optionally) sending outbound webhooks directly.
2. **Relay** — pure ciphertext router for content; plaintext bookkeeper for
   structural metadata. No longer parses OUT bytes, no longer composes
   notification text from session content.
3. **Client (mobile / desktop frontend / web)** — same as today plus
   per-frame decryption. Holds `account_key` after login.
4. **OPAQUE auth service** — new code path inside relay binary; provides
   password-blind authentication.

## 4. Account-level key

### 4.1 Authentication: OPAQUE

OPAQUE is an asymmetric PAKE: the password never leaves the client, and
the relay stores a verifier from which password cannot be recovered (even
offline) without an expensive guess-and-check per candidate.

Library: `github.com/bytemare/opaque` (CFRG draft-12 aligned). One server
instance per relay, shared OPRF key generated at first boot and persisted
in the userstore.

Registration flow:

```
client                                              relay
─────────────────────────────────────────────────────────────────
                                                    GET /api/auth/opaque-config
                                                    ← {oprf_seed_id, cipher_suite}
opaque.ClientRegistration(password)
→ ke1
                          POST /api/auth/register/init {email, ke1}
                                                    server.RegistrationResponse(ke1)
                                                    ← ke2
opaque.FinalizeRegistration(ke2)
→ record, export_key
                          POST /api/auth/register/finalize
                            {email, record, account_key_wrap, salt, kdf_params, nonce}
                                                    store(user)
                                                    ← {user_id, session_token}
```

The `export_key` returned by OPAQUE is a stable per-user key derived
deterministically from password. We DO NOT use `export_key` directly as
`account_key`; we derive a separate `wrap_key`:

```
wrap_key = Argon2id(password=export_key, salt=fresh_random_16B,
                    m=64MB, t=3, p=1, len=32)
```

(`export_key` is already password-strong; the second Argon2 pass keeps the
wrapping rotatable without requiring another OPAQUE round.)

Then:

```
account_key = randbytes(32)
nonce       = randbytes(24)
wrapped     = XChaCha20Poly1305.Seal(
                key=wrap_key,
                nonce=nonce,
                aad="atterm-account-key-v1" || user_id,
                plaintext=account_key,
              )
```

Stored on relay: `(opaque_record, wrapped, nonce, salt, kdf_params)`.

Login flow mirrors registration (`ke1`/`ke2`/`ke3`); on success client
recomputes `wrap_key` from `export_key` + stored `salt`, decrypts
`account_key`, and persists it to the OS keychain.

### 4.2 Password change

Without rotating `account_key`:

```
new_wrap_key = Argon2id(new_export_key, new_salt, ...)
new_wrapped  = AEAD.Seal(new_wrap_key, new_nonce, account_key)
```

Client uploads `{new_opaque_record, new_wrapped, new_nonce, new_salt}`.
Old data stays decryptable. Active sessions unaffected.

### 4.3 Admin reset ("forgot password")

The relay admin clears the user's `opaque_record` and `wrapped`. On the
user's next login they go through registration again with a new password,
generating a **fresh `account_key`**. All previously stored ciphertexts
(ringbuf entries, sealed META blobs, sealed ANNOUNCE blobs) become
permanently undecryptable.

This is acceptable because sessions are ephemeral: ringbuf is bounded,
and titles/cwd/summary go stale quickly. Documented as expected behavior.

### 4.4 Future: recovery code (not built v1)

The schema supports adding additional wrap rows later (`method` enum:
`password`, `recovery_code`, …) without migrating the v1 row. v1 stores
only `method=password`. Reserved for a future spec.

### 4.5 Storage schema

Add to `internal/userstore/migrations/`:

```sql
CREATE TABLE user_opaque_records (
    user_id    TEXT PRIMARY KEY,
    record     BLOB NOT NULL,           -- OPAQUE envelope
    created_at INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE user_account_key_wraps (
    user_id    TEXT NOT NULL,
    method     TEXT NOT NULL,            -- "password" only in v1
    wrapped    BLOB NOT NULL,
    nonce      BLOB NOT NULL,
    salt       BLOB NOT NULL,
    kdf_params TEXT NOT NULL,            -- JSON: {"alg":"argon2id","m":67108864,"t":3,"p":1}
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, method),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

ALTER TABLE users ADD COLUMN auth_mode TEXT NOT NULL DEFAULT 'opaque';
-- "opaque" for E2EE-enabled accounts; legacy bcrypt rows simply do not exist
-- in fresh databases (no backward compat per memory feedback_no_backward_compat).
```

### 4.6 Multi-device

Every device logs in via OPAQUE independently. After login, each device has
the same `account_key` in its local OS keychain. There is no "primary
device" — agent is just another device.

## 5. Per-session key

```
session_key = HKDF-SHA256(
    ikm     = account_key,
    salt    = nil,
    info    = "atterm-session-v1" || session_uuid_bytes(16),
    length  = 32,
)
```

Derived lazily on agent + each client on first frame of a session.
Cached in memory keyed by session ID; never persisted.

Blast radius: leaking a single `session_key` reveals only that session's
ciphertexts, not the account or other sessions.

## 6. AEAD

Cipher: **XChaCha20-Poly1305** (`golang.org/x/crypto/chacha20poly1305`
on Go; `@noble/ciphers/chacha` on TS/JS).

Wire envelope of each encrypted payload section:

```
+--------+------------------+---------------+
| 0x01   | 24-byte nonce    | ciphertext    |
| 1B     | (random)         | (incl. 16B    |
| cipher_|                  |  Poly1305 tag)|
| id     |                  |               |
+--------+------------------+---------------+
```

`cipher_id=0x01` identifies XChaCha20-Poly1305. Reserved for future
cipher agility.

Additional authenticated data:

```
aad = session_uuid_bytes(16)
   || frame_type_byte(1)
   || aad_context(variable)
```

Where `aad_context` is:

- For `TypeOut`: 8B big-endian `seq`
- For `TypeIn`, `TypePasteImage`: empty (the 24B random AEAD nonce
  already provides per-frame uniqueness; session + type binding via the
  fixed prefix is sufficient)
- For `TypeMeta`, `TypeAnnounce` sealed sections: 8B monotonic
  `meta_epoch` chosen by agent (incremented per emit per session)
- For `TypeClose`, `TypeCommandEvent`: 8B big-endian `command_started_at`
  unix-nanos (binds the encrypted text to the specific command)

Nonce reuse is structurally impossible because nonces are random 24-byte
values (collision probability ~2^-96 per pair).

## 7. Frame layout changes

`internal/proto/frame.go` and `codec.go` change as follows. Wire header
(`version | type | payload_len | session_uuid`) stays identical so the
relay's framer keeps working.

### 7.1 Frame type table

| Type | Const | Direction | Payload change |
|------|-------|-----------|----------------|
| 0x01 | `TypeOpen` | agent→relay | clear `OpenClearPayload` + sealed `OpenSealedPayload` |
| 0x02 | `TypeIn` | client→agent | sealed envelope only (random AAD ID prefix) |
| 0x03 | `TypeOut` | agent→client | seq(8B BE) ‖ sealed envelope |
| 0x04 | `TypeResize` | client→agent | unchanged (cols/rows only) |
| 0x05 | `TypeMeta` | agent→client | clear `MetaClearPayload` ‖ sealed `MetaSealedPayload` |
| 0x06 | `TypeClose` | agent→client | clear `{exit_code}` ‖ sealed `{reason}` |
| 0x10 | `TypeAttach` | client→relay | unchanged |
| 0x11 | `TypeList` | client→relay | unchanged |
| 0x12 | `TypeListResp` | relay→client | each session split clear/sealed (see §8) |
| 0x13 | `TypeReplayProgress` | relay→client | unchanged (byte counters only) |
| 0x20 | `TypePing` | both | unchanged |
| 0x21 | `TypePong` | both | unchanged |
| 0x30 | `TypeAnnounce` | uplink→relay | clear `AnnounceClear` ‖ sealed sessions[i].sealed |
| 0x31 | `TypeStreamRequest` | relay→uplink | unchanged |
| 0x32 | `TypeStreamStop` | relay→uplink | unchanged |
| 0x33 | `TypePasteImage` | client→agent | sealed envelope only |
| 0x34 | `TypeClaimDriver` | client→relay | clear `{client_id}` ‖ sealed `{client_name}` |
| 0x35 | `TypeCommandEvent` | uplink→relay | clear `{exit_code, elapsed_ms, started_at}` ‖ sealed `{label}` |
| 0x36 | `TypeViewers` | relay→uplink | unchanged |
| 0x40 | `TypeAuthInfo` | relay→uplink | unchanged |

### 7.2 Composite payload layout

Frames that carry both clear and sealed sections use a fixed prefix layout:

```
+----------+--------------+----------+--------------+
| u16 BE   | clear_json   | u32 BE   | sealed       |
| clear_   | (utf-8)      | sealed_  | envelope     |
| len      |              | len      | (§6)         |
+----------+--------------+----------+--------------+
```

Decoder: parse `clear_len`, lift `clear_json` bytes, parse `sealed_len`,
lift envelope, AEAD-Open with the session key and the appropriate AAD.

Empty sealed section: `sealed_len=0`, no envelope bytes (allowed for frames
that have nothing sensitive in this emit). Empty clear section is allowed
but unusual.

## 8. Field split: clear vs sealed

### 8.1 `SessionInfo` (used in `TypeListResp` and `TypeAnnounce.Sessions[]`)

**Clear** (relay reads):

```go
type SessionInfoClear struct {
    ID                 string  // uuid
    HostID             string  // uuid
    Host               string  // hostname (NOT user-set, derived from OS)
    User               string  // OS uid/username
    Cols, Rows         uint16
    StartedAt          int64
    LastOutputAt       int64
    AttentionAt        int64
    TaskState          string  // idle/running/waiting_input/completed/...
    CommandStartedAt   int64
    CommandEndedAt     int64
    CommandDurationMS  int
    CommandExitCode    *int
    Type               string  // shell/ai/test/build/deploy (classification)
    RemotePermission   string  // view/control/full
    Unread             bool    // computed by relay
    SealedBlobLen      int     // hint for client allocation
}
```

**Sealed** (relay sees ciphertext only):

```go
type SessionInfoSealed struct {
    Title          string
    Cwd            string
    Command        string  // raw spawn command
    CurrentCommand string  // OSC 133 'A' current cmd
    Summary        *SessionSummary
}
```

Rationale for keeping `task_state`/`exit_code`/timestamps clear: relay
uses these to trigger Web Push notifications ("command done", "waiting
input") and to compute the unread badge. Encrypting them eliminates push
functionality without a content-layer benefit (they are short
enumerations, not user content).

`Host`/`User` stay clear because they reflect machine identity, not
user-controlled labels. (Hostnames can be sensitive; documented as a
deliberate trade-off — operator already learns this from the websocket
TLS SNI and source IP in practice.)

### 8.2 `MetaPayload`

**Clear**:

```go
type MetaClearPayload struct {
    DriverClientID   string  // routing identifier
    Cols, Rows       uint16
    TaskState        string
    CommandStartedAt int64
    CommandEndedAt   int64
    CommandDurationMS int
    CommandExitCode  *int
    LastOutputAt     int64
    AttentionAt      int64
    Type             string
    MetaEpoch        uint64  // monotonic per (session, agent), bound to sealed AAD
}
```

**Sealed**:

```go
type MetaSealedPayload struct {
    Cwd              string
    Title            string
    DriverClientName string
    CurrentCommand   string
    Summary          *SessionSummary
}
```

### 8.3 `OpenPayload`

**Clear**: `Cols, Rows, HostID, Host, User, StartedAt`.
**Sealed**: `Command, Cwd, Title`.

### 8.4 `ClaimDriverPayload`

**Clear**: `ClientID`. **Sealed**: `ClientName`.

### 8.5 `CommandEventPayload`

**Clear**: `ExitCode, ElapsedMS`. **Sealed**: `Label`.

(Plus see §10.3 for the Webhook trade-off on `Label`.)

### 8.6 `ClosePayload`

**Clear**: `ExitCode`. **Sealed**: `Reason`.

## 9. OSC 133 + Summary migration

### 9.1 New package `internal/terminalproc`

Move from `internal/session/session.go`:

- `updateTerminalState(data []byte) bool`
- `parseOSC133Exit(payload string) int`
- `extractCommand`, `extractTaskState`, all OSC 133 parsing helpers

Move from `internal/session/summary.go`:

- `makeSummary` and helpers
- `summaryOutputBytes` constant

New package signature:

```go
package terminalproc

type Processor struct { /* ANSI parser state, OSC 133 state */ }

func New(sessionID uuid.UUID) *Processor

// Feed delivers a chunk of raw PTY output. Returns events the processor
// derived from this chunk; caller emits them as plaintext-clear META
// updates and (for the sealed fields) sealed META updates.
func (p *Processor) Feed(seq uint64, data []byte) []Event

type EventKind int
const (
    EventTaskStateChange EventKind = iota
    EventCommandStarted
    EventCommandFinished
    EventCurrentCommandChange
    EventSummary
)

type Event struct {
    Kind             EventKind
    TaskState        string  // for EventTaskStateChange
    Command          string  // for EventCommandStarted / CurrentCommandChange
    StartedAt        int64
    EndedAt          int64
    ExitCode         *int
    DurationMS       int
    Summary          *proto.SessionSummary
}
```

### 9.2 Agent wiring

`desktop/relay_host.go` already owns each active PTY's chunk pipe. Plug
the processor between PTY read and the AEAD-seal-and-send step. The
processor produces:

- **Clear META fields** → fold into next `MetaClearPayload` emit
- **Sealed META fields** → fold into next `MetaSealedPayload` emit
  (only emit when something sealed changed, to keep traffic down)
- **CommandEvent** triggers → emit `TypeCommandEvent` for Web Push pipeline

### 9.3 Relay dropouts

After M4 ships, delete from `internal/session/session.go`:

- `updateTerminalState`
- All OSC 133 parsing code
- `makeSummary` and Summary tracking state
- `session.PushOut` no longer returns a "metaChanged" flag; it only writes
  ringbuf and fans out

This is the largest single deletion in the migration. Tests covering OSC
133 in `internal/session/` move to `internal/terminalproc/`.

## 10. Web Push and Webhook migration

### 10.1 Web Push body encryption (double envelope)

Today the relay composes push payload from `SessionInfo`:

```go
"title": fmt.Sprintf("AT Term · %s", label),
"body":  "...",
```

After M6 the agent composes the payload, AEAD-seals it with `account_key`,
and sends it as part of a new frame:

```go
type TypeUserPushEnvelope = 0x37  // uplink→relay

type UserPushEnvelopePayload struct {
    UserID       string   // routing (relay knows this anyway)
    SealedBody   []byte   // AEAD(account_key, push_body_json)
    Kind         string   // "command-finished" | "waiting-input" | "idle"
    // For idle suppression decisions only; not user content:
    SessionID    string
}
```

Relay calls `webpush.Send` with `SealedBody` as the literal payload. The
Web Push standard already encrypts that payload to each browser's
subscription keys; the SW decrypts that envelope first, then unwraps the
inner AEAD with `account_key` (which it loads from a `caches`-backed
secure store), and renders the notification.

VAPID keys and browser subscription keys stay relay-side: they identify
the sender to the push service and the recipient's user-agent, not user
content.

### 10.2 Webhook outbound — two delivery modes

Each webhook row gains a new `delivery_mode` column with two values:

**`relay` (degraded under E2EE).** Relay POSTs the webhook body itself.
Because `Label` is now sealed (§8.5), the relay cannot include it; the
emitted body carries clear fields only (`session_id`, `exit_code`,
`elapsed_ms`, `started_at`, `host_id`, `host`, `user`, `task_state`) and
omits any sealed content. Users who enable this mode accept that their
webhook receives no command label or summary.

**`agent` (full UX, new in M6).** Webhook config (URL, HMAC secret,
event mask) is still stored relay-side, but **only the agent reads it**
via an authenticated config API. The agent POSTs directly to the
user-configured URL with a complete plaintext body including label and
summary. Relay never sees or relays the payload.

Default for new webhooks: `agent`. Existing relay-side webhooks default
to `relay` on migration; users can opt in to `agent` mode in settings.

### 10.3 `CommandEventPayload.Label` placement

Trade-off documented in §8.5: `Label` is sealed in transit. Relay-side
webhook mode (`delivery_mode=relay`) effectively loses Label visibility.
Push notifications also use Label, but the agent-composed push payload
(§10.1) carries Label inside the sealed body, so push UX is unaffected.

## 11. Key handling on devices

### 11.1 Desktop / mobile agent + client

`account_key` lives in OS keychain via the existing
`internal/secstore` plugin (per memory: PR #101 fixed iOS Keychain
registration). Key entries:

```
Service:  com.atterm.account-key
Account:  <relay_origin>:<user_id>
Value:    base64(account_key)
```

Loaded into memory on app start if user is logged in (token present).
Wiped on logout.

### 11.2 Web client (browser)

Stored in IndexedDB inside a non-extractable `CryptoKey`-style wrap is
not possible because we need raw key bytes for HKDF derivations done in
JS. Instead: store base64 in IndexedDB under a non-shared origin and
rely on origin isolation. Document that web client should run on a
dedicated origin (existing relay UI does this).

### 11.3 Web Push service worker

The SW receives encrypted push payloads (§10.1). It needs the
`account_key` to unwrap them. Strategy: SW reads from the same
IndexedDB store the main app uses. If the SW receives a push before any
client has logged in, the SW shows a generic notification ("AT Term
update") and the user opens the app to see details.

## 12. Migration milestones

Each milestone is independently shippable as a tagged release. No
backward compat (per `feedback_no_backward_compat` memory): existing
sessions and existing relay databases require a one-time reset when M1
lands.

### M1 — OPAQUE auth + account_key wrapping

- Replace bcrypt/argon2 password verification with OPAQUE protocol
- New userstore tables `user_opaque_records`, `user_account_key_wraps`
- Client SDK: OPAQUE client implementation, account_key generation
- Desktop + mobile + web UI: registration, login, logout flows
- Persist `account_key` in OS keychain (desktop, mobile via
  `internal/secstore`); IndexedDB (web)
- No frame-level changes; frames still plaintext after this milestone
- Release: this can ship without user-visible behavior change beyond
  forcing re-registration

### M2 — AEAD envelope + content frames

- Implement AEAD envelope codec (§6)
- Encrypt `TypeOut`, `TypeIn`, `TypePasteImage`
- Relay ringbuf stores opaque ciphertext keyed by seq
- Clients decrypt on receive
- Resize stays plaintext (cols/rows only)
- Release: encryption first goes live; replay still works

### M3 — META + Open + Close + ClaimDriver field split

- Implement composite payload layout (§7.2)
- Split `MetaPayload`, `OpenPayload`, `ClosePayload`, `ClaimDriverPayload`
- Clients render sealed META fields after decrypt
- Relay updates session list from clear META fields only
- Release: session list still shows title/cwd because clients decrypt

### M4 — OSC 133 + Summary processor migration

- Create `internal/terminalproc` package
- Move parsing code from `internal/session/`
- Wire agent's per-PTY chunk pipe through `terminalproc.Processor`
- Delete OSC 133 and Summary code from `internal/session/`
- Tests migrate alongside
- Release: behavior unchanged externally; relay drops dead code

### M5 — ANNOUNCE field split

- Split `AnnouncePayload.Sessions[]` into clear + sealed per session
- Update uplink, session list HTTP endpoint, frontend rendering
- Release: session list ANNOUNCE flow fully E2EE

### M6 — Web Push + Webhook migration

- New `TypeUserPushEnvelope` frame
- Agent composes push body, seals with `account_key`
- Relay drops in-process push body composition
- Service worker decryption flow
- New webhook `delivery_mode` field + agent-side outbound webhook
- Relay-side webhooks degrade to omit `Label`
- Release: push remains functional, webhooks have new UX choice

### M7 — Audit, cleanup, docs

- Remove any remaining plaintext fallback flags
- Cryptographic review (external or peer)
- User-facing documentation: threat model, what relay can see, what to
  do if password lost
- Operator-facing documentation: server-side observable metadata
- Release: 1.0 of E2EE

## 13. Non-goals (explicit)

1. **Remote viewer collaboration.** A non-account-holder cannot decrypt a
   session. If sharing is added later, it requires a per-session viewer
   pairing flow (out of scope here).
2. **Password recovery.** Forgotten password = admin reset = new
   `account_key` = old data unrecoverable. Documented as deliberate.
3. **Backward compatibility.** No protocol fallback. Existing databases
   require migration (one-shot script) that drops session history and
   asks all users to re-register at M1 release.
4. **Traffic analysis defenses.** Frame counts, sizes, and inter-arrival
   times remain observable. Padding/timing obfuscation deferred.
5. **Forward secrecy for stored ciphertext.** A future device compromise
   that yields `account_key` decrypts all past ringbuf entries. Mitigated
   in practice by the bounded ringbuf size and the operator's data
   retention policy; not by cryptography.
6. **Hostname / username confidentiality.** `Host` and `User` stay clear
   for session-list ergonomics; documented as a known leak.

## 14. Testing strategy

### 14.1 Per-milestone unit tests

- **M1**: OPAQUE registration/login round-trip; wrap/unwrap correctness;
  Argon2 parameter sanity; admin reset flow regenerates key.
- **M2**: AEAD envelope codec golden tests; nonce collision probability
  (statistical); ringbuf decrypt-after-replay; PasteImage round-trip.
- **M3**: composite payload encode/decode; AAD binding (tampered
  `meta_epoch` rejected); empty sealed section handling.
- **M4**: terminalproc OSC 133 fixture corpus (reused from existing
  `internal/session` tests); Summary character-set safety.
- **M5**: ANNOUNCE split correctness; session list ordering preserved.
- **M6**: push envelope double-encryption; SW decrypt fixture; webhook
  delivery mode selector.

### 14.2 Integration tests

- End-to-end "relay sees no plaintext" test: spin up relay + agent +
  client in-process; instrument the relay to scan every payload buffer
  for known plaintext strings; assert all marker strings absent. Tests
  added per milestone as features ship.
- Multi-device test: log in same user from agent + two clients; both
  clients decrypt the same OUT stream; one client logs out, ringbuf
  re-attach still works for the other.
- Password change test: change password mid-session; ongoing OUT frames
  still decrypt; new login on a third device picks up new wrap; old
  device session continues unaffected until logout.

### 14.3 Cryptographic property tests

- HKDF determinism: same `account_key` + `session_uuid` → same
  `session_key`.
- AAD binding: any single-bit flip in AAD → Open fails.
- Nonce uniqueness within a session over realistic message volume.

## 15. Open questions (do not block design)

These are flagged for the writing-plans phase, not unresolved design
decisions:

1. **OPAQUE library version pinning.** `bytemare/opaque` is pre-1.0;
   pick a tag and document.
2. **Argon2 parameters on mobile.** 64MB memory is tight on older
   iPhones; profile and adjust per-platform if needed.
3. **Key compromise rotation.** Admin reset is the only rotation today.
   Self-service "rotate my account_key" UX is a future enhancement.
4. **Push catch-up after offline period.** If multiple push events
   queue while the app is closed, the SW shows generic copy until next
   foreground decrypt. Acceptable for v1; revisit if user feedback bad.
