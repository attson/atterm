# Remote File Explorer Design

> Audience: engineers working on the AT Term desktop File Explorer, relay, and uplink protocol
> Last updated: 2026-07-16
> Status: draft
> Related: `docs/spec/protocol.md`, `docs/superpowers/specs/2026-05-16-plugin-system-design.md`, `docs/superpowers/specs/2026-06-07-file-explorer-preview-design.md`, `docs/superpowers/specs/2026-07-10-remote-file-channel-design.md`

## Goal

Make the desktop File Explorer work for remote panes with the same user-facing behavior as local panes:

- Directory tree follows the active pane cwd.
- Hidden-file filtering, lazy expand, preview tabs, code/markdown render toggle, image preview, audio/video preview, PDF preview, binary fallback, and open external all behave consistently.
- Directory watches refresh expanded nodes and current previews.
- File browsing is read-only. No file edit/write/delete operation is introduced.

Remote means: the active desktop pane is attached to a session whose PTY and filesystem live on another desktop host and are mirrored through the public relay.

## Non-Goals

- No file editing, upload, delete, rename, chmod, mkdir, or recursive search.
- No web/mobile File Explorer UI in this phase. The wire types are protocol-level and can be reused later, but this implementation targets `desktop/frontend`.
- No exposure of the existing `PluginFS` Wails binding through relay/uplink. `PluginFS` remains local-only.
- No attempt to browse sessions with `remote_permission=view` or `control`; remote filesystem access requires `full`.

## Current State

The existing File Explorer is a desktop plugin under `desktop/frontend/src/plugins/fileExplorer/`. It calls `platform.pluginHost.fs`, which is backed by the Wails-bound Go `desktop/PluginFS`.

`PluginFS` is intentionally local-only:

- It runs path resolution, symlink resolution, allow-root checks, denylist checks, binary detection, read caps, and fsnotify watches inside `desktop/plugin_fs.go`.
- `desktop/plugin_fs_server.go` serves same-origin `/pluginfs/<base64-path>` media/PDF URLs for already-authorized local paths.
- `.github/scripts/check-plugin-fs-isolation.sh` fails if `PluginFS` appears in `desktop/uplink*.go` or `internal/`.

Using this binding while a remote pane is active is wrong: the remote cwd path would be interpreted on the viewer machine, not on the host that owns the PTY.

## Approach

Add an explicit remote filesystem RPC channel over the existing WebSocket frame protocol.

The viewer desktop sends filesystem requests on the same `/client` `SessionConnection` used to attach the remote pane. The relay validates ownership, session permission, attached session, and driver where needed, then forwards the request through the existing mirror session inbound channel to the owning desktop uplink. The owning desktop performs the read-only filesystem operation against its local disk and returns a response through the uplink to the relay, which forwards it only to the requesting client.

This keeps `session_id` authoritative and follows the existing lazy uplink topology:

```text
FileExplorer
  -> RemoteSessionFSClient
  -> /client SessionConnection
  -> relay mirror session
  -> /uplink desktop host
  -> desktop RemoteFS handler
  -> /uplink response
  -> relay
  -> requester SessionConnection
```

## Protocol Additions

Use new additive frame types. Numeric assignments:

```go
TypeFSRequest  Type = 0x38 // client -> relay -> desktop uplink
TypeFSResponse Type = 0x39 // desktop uplink -> relay -> requester client
TypeFSEvent    Type = 0x3a // desktop uplink -> relay -> requester client
```

All three frames carry `SessionID` in the frame header. Payloads are JSON. Bytes inside JSON use Go's normal `[]byte` base64 encoding.

### `FS_REQUEST`

```json
{
  "request_id": "uuid-or-random-string",
  "op": "list_dir",
  "path": "/Users/alice/project",
  "max_bytes": 2097152,
  "offset": 0,
  "length": 262144,
  "watch_id": "optional-client-watch-id"
}
```

Operations:

- `list_dir`: returns directory entries.
- `file_meta`: returns size, mtime, and binary flag.
- `read_file`: returns up to `max_bytes`; hard-capped by host.
- `read_chunk`: returns bytes from `offset` with `length`; used for remote asset blob creation.
- `watch_dir`: starts a per-client, per-session directory watch.
- `unwatch_dir`: stops a previous watch.
- `open_external`: asks the host OS to open the file externally.

### `FS_RESPONSE`

```json
{
  "request_id": "same-as-request",
  "ok": true,
  "error": "",
  "entries": [
    { "name": "src", "isDir": true, "size": 0, "modTime": 1760000000000 }
  ],
  "meta": { "path": "/Users/alice/project/README.md", "size": 1234, "modTime": 1760000000000, "isBinary": false },
  "content": { "path": "/Users/alice/project/README.md", "data": "base64", "isBinary": false, "truncatedAt": 0 },
  "chunk": { "data": "base64", "offset": 0, "length": 262144, "eof": false, "contentType": "image/png" },
  "watch_id": "server-watch-id"
}
```

Only fields relevant to the operation are populated.

### `FS_EVENT`

```json
{
  "watch_id": "server-watch-id",
  "path": "/Users/alice/project/src",
  "event": "dir_changed"
}
```

`FS_EVENT` is only sent to the requester that created the watch. It is not broadcast to every subscriber.

## Permission Model

Remote filesystem requests require all of these checks:

- The client must be attached to the session on `/client`.
- The authenticated user must own the session, same as remote session attach today.
- The session's owner-published `remote_permission` must be `full`.
- The client must be the current driver for `open_external`.
- Read-only operations (`list_dir`, `file_meta`, `read_file`, `read_chunk`, `watch_dir`, `unwatch_dir`) require an attached client with `remote_permission=full`; they do not require driver status.
- The owning desktop uplink must repeat the `remote_permission == full` check before touching disk.

Relay drops unauthorized requests silently or replies with a structured `FS_RESPONSE { ok:false, error:"permission_denied" }`; the UI should surface a short toast/banner.

## Filesystem Security

Do not import or reference `PluginFS` from `desktop/uplink*.go` or `internal/`. Keep the existing isolation guard meaningful.

Instead, extract the common read-only filesystem implementation into a desktop-local helper that is not Wails-bound and is not imported by `internal/`, for example:

```text
desktop/fsaccess.go
desktop/fsaccess_test.go
```

`PluginFS` and the new remote host handler both delegate to this helper. The helper preserves the current policies:

- Path must be absolute.
- Symlinks are resolved before allow-root checks.
- Allowed roots are the host user's home plus active local session cwds.
- Deny exact/suffix/segment rules remain: `.ssh`, `.gnupg`, `.aws`, `.env`, `.env.*`.
- `read_file` rejects `max_bytes > 5 MiB`.
- `read_chunk` uses a 256 KiB chunk size, with an absolute per-request max below the WebSocket read limit.
- Binary detection samples the first 4 KiB.
- No write operation exists.

The dynamic "active local session cwd" enrichment should be implemented in the shared helper instead of duplicating allow-root logic in two places.

## Remote Media/PDF Behavior

Local media/PDF preview can keep using `/pluginfs/<path>`.

Remote media/PDF preview should use a frontend-managed `blob:` URL:

1. `FileEditor` calls `fileMeta` to decide the preview kind.
2. For remote image/audio/video/PDF preview, `RemoteSessionFSClient.assetUrlFor(path)` returns an object URL backed by bytes fetched via `read_chunk`.
3. The client may initially fetch the full file in chunks up to a 50 MiB cap, with cancellation when the preview unmounts.
4. If the file exceeds the remote asset cap or chunking fails, show the existing `BinaryBanner`.

This avoids inventing a relay HTTP byte-streaming endpoint in this phase while preserving the visual result for normal images, PDFs, and media files.

Future optimization: expose a proper HTTP Range-like endpoint once web/mobile need remote File Explorer. That is deliberately outside this design.

## Watch Behavior

Remote watches mirror local semantics:

- Watching is per expanded directory node, not recursive.
- Host-side hard cap remains 200 active watches per desktop process, shared with local watches or separately capped at the same number.
- Events are debounced at 100 ms per directory.
- The requester receives `FS_EVENT { event:"dir_changed" }` and refreshes the matching expanded node or active preview meta.
- On WebSocket detach, relay/uplink cleanup must release outstanding remote watches for that client.

If a host platform cannot create a watcher, the response is an error and the frontend falls back to manual refresh behavior just like local watcher failure.

## Frontend Architecture

Introduce a filesystem abstraction used by the File Explorer plugin:

```ts
interface FileSystemBridge {
  listDir(path: string): Promise<DirEntry[]>
  watchDir(path: string): Promise<number | string>
  unwatchDir(id: number | string): Promise<void>
  readFile(path: string, maxBytes?: number): Promise<FileContent>
  fileMeta(path: string): Promise<FileMetaInfo>
  openExternal(path: string): Promise<void>
  assetUrlFor(path: string): string
}
```

Add active-pane locality to `PluginContext`:

- `activeIsRemote: ComputedRef<boolean>`
- `activeSessionId` already exists.
- `activeEndpoint` already exists.

`FileExplorer.vue` selects the bridge from context:

- Local pane: `platform.pluginHost.fs`.
- Remote pane: `RemoteSessionFSClient` bound to the active `SessionConnection`.

`FileTree`, `FileEditor`, `CodeViewer`, `MarkdownPreview`, `ImagePreview`, `MediaPreview`, `PdfPreview`, and `BinaryBanner` should receive the selected bridge as a prop or via provide/inject. Avoid direct `usePlatform().pluginHost!.fs` inside leaf components after this change.

Pinned roots and open tabs must reset when switching between local and remote filesystem identities if the path string is the same but the backing host/session changes. The bridge identity should include `{ remote:boolean, sessionId?:string, hostId?:string }`.

## SessionConnection Changes

`desktop/frontend/src/lib/connection.ts` should support request/response RPC:

- `sendFSRequest(payload): Promise<FSResponse>`
- maintain a `Map<request_id, { resolve, reject, timeout }>`
- handle `FS_RESPONSE` by resolving only the matching pending request
- handle `FS_EVENT` through a callback/event emitter
- clear pending requests on detach/reconnect with a clear error

`TerminalView` or the pane/session connection owner exposes the active connection to plugin context without making plugins create a second `/client` attach. Reusing the existing attach avoids duplicate subscribers and duplicate stream requests.

## Relay Changes

In `internal/relay/client_conn.go`:

- Accept `FS_REQUEST` only after `ATTACH`.
- Enforce owner/session checks already done for attach.
- Enforce `remote_permission=full`.
- Record requester routing metadata, at minimum `{request_id -> subscriber/client}` for in-flight requests.
- Forward request to the mirror session inbound channel so `handleUplink` sends it to the owning desktop.

In `internal/relay/uplink_conn.go`:

- Forward `FS_RESPONSE` and `FS_EVENT` from the owning uplink back to the correct requester.
- Do not broadcast file responses to all subscribers.
- Drop responses for unknown request IDs or sessions not owned by this uplink.
- Clean up request routing and watches on client disconnect/uplink disconnect.

The relay must not inspect file contents beyond routing JSON request IDs. It does not need the account key and does not decrypt terminal content.

## Desktop Host Changes

In `desktop/uplink.go`:

- Read `FS_REQUEST` frames from the relay.
- Verify `remotePermission == full`.
- Dispatch to a desktop-local `RemoteFS` handler that uses the shared `fsaccess` helper.
- Reply with `FS_RESPONSE` or `FS_EVENT`.
- Track remote watch handles by `{session_id, request/client watch id}` and release them on unwatch or uplink shutdown.

`open_external` maps to the existing local behavior on the owning host. The UI should make clear through normal OS behavior that the file opens on the remote host, not the viewer machine. No file bytes are launched locally for this operation.

## E2EE Posture

This design does not add a sealed file-content envelope in the first implementation. It requires authenticated, owner-scoped relay access and `remote_permission=full`, but the relay will carry file bytes in `FS_RESPONSE`.

This is consistent with the current PASTE_FILE web posture noted in `docs/spec/protocol.md`, but it is not end-to-end encrypted. The spec must call this out clearly.

Future hardening can add `SealedFSResponse` with AAD byte `0x39` and strip plaintext `content/chunk` after all clients support it. If that is added, update `docs/spec/protocol.md` §E2EE envelope with a unique AAD row.

## Testing

Go:

- Shared fs helper keeps current `PluginFS` resolver/read/list/watch tests passing.
- Remote handler rejects relative paths, symlink escapes, denylisted paths, too-large reads, and too-large chunks.
- Relay drops or errors `FS_REQUEST` before attach.
- Relay enforces `remote_permission=full`.
- Relay routes `FS_RESPONSE` only to the requester with matching request ID.
- Uplink host repeats permission enforcement.
- Watch cleanup runs on client/uplink disconnect.

Frontend:

- File Explorer local pane still calls `platform.pluginHost.fs`.
- Remote pane uses `RemoteSessionFSClient`.
- Leaf components no longer import `platform.pluginHost.fs` directly.
- Remote code/markdown previews call `readFile`.
- Remote image/audio/video/PDF previews use `assetUrlFor` object URLs and revoke them on unmount/path change.
- Watch events refresh expanded tree nodes.
- Switching active pane between local and remote invalidates stale pinned/open path state when the filesystem identity changes.

Protocol:

- Update `internal/proto/frame.go`, `desktop/frontend/src/lib/proto.ts`, `web/src/shared/ws/protocol.ts`, and `docs/spec/protocol.md` in the same change.
- Add codec/payload round-trip tests where practical.

## Rollout

1. Extract and test shared desktop fs helper without changing behavior.
2. Add protocol constants and payload structs.
3. Implement frontend `FileSystemBridge` and keep local File Explorer behavior green.
4. Add `SessionConnection` FS request/response plumbing with frontend tests.
5. Add relay request routing and permission tests.
6. Add desktop uplink host handler and end-to-end Go tests.
7. Wire File Explorer to select local vs remote bridge.
8. Run targeted Go, Vitest, web protocol tests, and the PluginFS isolation guard.

## Decisions

- Read-only filesystem operations require attached `full` permission, not driver status.
- `open_external` requires attached `full` permission and current driver status.
- Remote asset previews load at most 50 MiB per file and fetch 256 KiB chunks.
- The File Explorer reuses the active terminal `SessionConnection`; it does not open a second `/client` attachment.
