# Configuration export / import — design

Roadmap item 31 (P7). Write the user's configuration to a plain JSON file on
disk, and read one back. Never through the relay. Supersedes the Backlog's
"theme import/export", which this covers as one field among many.

## 1. What an export contains, and what it must never contain

The export is the synced preference set — `prefssync.SyncedKeys()` — in
plaintext. Two of those keys are E2EE-sealed blobs on the wire
(`ssh_hosts_encrypted`, `profiles_encrypted`), and a sealed blob is useless
in a file the user is meant to read and edit, so export writes their
contents instead.

**Not by unsealing them.** `sealSSHHosts`
(`desktop/prefssync_adapter.go:108`) bundles four things: the `SSHHost`
records, a `map[hostID]sshCredential` loaded from the OS keyring, the
`SSHKey` records, and a `map[keyID]sshKeySecret` — the private keys. That is
correct for sync between two desktops the same user controls, and it is
exactly what must never reach a plaintext file. Piping the unsealed blob
into an export would write every stored SSH password and private key to
disk in the clear.

So export reads the **host and key records only**, from config, and never
assembles the credential maps at all:

- `SSHHost` (`desktop/ssh_hosts_store.go:36`) carries host, port, user,
  tags, note, `IdentityFile` *path*, `ProxyJump`, `ProxyCommand`, and
  forwarding rules — no secret of any kind.
- `SSHKey` records are exported as identity metadata (id, label); the
  matching `sshKeySecret` is not.
- The keyring is not read by the export path. Not as an option, not behind
  a warning. An export file is a thing users mail to themselves and drop in
  cloud storage; it does not get to hold a private key.

An imported host therefore arrives complete except for its credential, and
the UI must say so rather than presenting a host that looks ready and fails
at dial time.

The distinction is worth stating plainly because the tempting
implementation — "unseal the two encrypted keys, dump the result" — is one
line and leaks everything.

### `SessionProfile.Env` is opt-in, again

`SessionProfile.Env` is governed by `SyncEnv`, documented as "default false
= Env never leaves this machine". An export file exists precisely to leave
the machine, so the same bit governs it — and it does so through the same
code: `stripUnsyncedEnv` (`desktop/profiles.go:47`) already exists for this,
and its comment says it is called unconditionally from inside `sealProfiles`
so the guarantee is "one no caller can accidentally skip, rather than a step
every future caller has to remember to perform first".

Export is exactly the future caller that comment anticipated. It calls
`stripUnsyncedEnv`, rather than reimplementing the filter.

There is a checkbox to include local env anyway, defaulting off, worded as
what it is — env vars frequently hold API tokens, and the person who set
`SyncEnv: false` already told us their intent once. When it is on, export
skips the strip; that is the one place the bypass exists, and it is
user-initiated per export.

## 2. File format

```jsonc
{
  "atterm_export": 1,          // format version; refuse anything else
  "exported_at": "2026-08-20T11:00:00Z",
  "app_version": "v0.4.x",
  "preferences": {             // key -> decoded value, one per SyncedKeys()
    "terminal_font_size": 14,
    "ssh_hosts": [ /* SSHHost records, credentials absent */ ],
    "profiles": [ /* SessionProfile records */ ]
  }
}
```

Two deliberate choices:

- The sealed keys are written under their **unsealed** names (`ssh_hosts`,
  not `ssh_hosts_encrypted`). The `_encrypted` suffix describes a transport
  detail that does not exist in a file, and a user editing this JSON should
  not have to know the wire format.
- `atterm_export` is a hard version gate, not a hint. An unknown value is
  refused outright. Guessing at a format from the future is how an import
  silently drops the fields it did not understand.

## 3. Import is a merge with a preview, not a restore

Import never wipes. It reports, per key, what it would do — `add`,
`replace`, or `unchanged` — and applies only after the user confirms. Lists
with stable IDs (hosts, profiles) merge by ID: same ID replaces, new ID
adds, and a local entry absent from the file is **kept**.

A "wipe and restore" mode is not offered. The failure it invites — importing
a partial file and losing every host that file did not mention — is
unrecoverable, and the merge covers the honest use case (moving to a new
machine, where local is empty and merge and restore are the same thing).

Malformed entries are skipped individually, with a count in the report. One
bad host must not abort the import — the same rule `Pull` already follows
per key, and for the same reason.

## 4. Import writes through the normal setters

Every imported key goes through the existing `App` setter for that key, so
it lands in the config store, marks dirty, and syncs like any other edit.
Import does not reach into `configStore` directly: doing so would leave the
sync meta untouched and the imported values would sit locally forever,
invisible to every other device, until something else happened to touch the
same key.

## 5. Surface

- `App.ExportConfig(includeLocalEnv bool) (string, error)` — returns the
  JSON. The frontend owns the save dialog.
- `App.PreviewConfigImport(jsonText string) (ImportPreview, error)` — parses
  and diffs, changing nothing.
- `App.ApplyConfigImport(jsonText string) (ImportReport, error)` — no
  `includeLocalEnv`: `Preview` doesn't take one either, and a lever Apply
  had that Preview didn't would let the two disagree about what a file
  produces. What a profile's `Env` ends up as is fully determined by
  `mergeProfiles` from what the file actually contains and the profile's
  own `SyncEnv`, not by a flag on the call.

`Preview` and `Apply` both take the raw text and both parse it. Handing the
frontend a parsed-and-cached handle would let the previewed bytes and the
applied bytes drift apart; parsing twice is cheap and keeps "what you were
shown" and "what was written" the same input.

## 6. Non-goals

- **No credentials, ever.** §1.
- **No encrypted export format.** The roadmap says plaintext, and a second
  encrypted format would need its own key management — which is what the
  relay's E2EE already is, for the people who want it.
- **No relay involvement.** Export reads local state and writes a local
  file. Import writes local state. Neither talks to the relay; the normal
  sync push that follows an import is the existing mechanism doing its
  existing job.
- **No partial-key selection UI.** Export is all-or-nothing over
  `SyncedKeys()`, plus the one env checkbox. Per-key pickers are a
  preferences UI for a preferences file.
