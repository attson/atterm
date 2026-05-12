# Replay Progress And Throttled Attach Design

## Goal

When a remote client attaches to a session with a large scrollback, the UI must show that historical output is loading and avoid dumping the entire replay to the browser in one burst.

## Approach

The relay/session layer emits an explicit replay progress frame around attach scrollback replay. The browser and desktop clients use that frame to show a progress overlay while xterm receives historical output. The relay client writer paces replay `OUT` frames while a replay is active, so the browser event loop has chances to paint progress and accept user feedback instead of appearing stuck.

## Protocol

Add a backward-compatible frame type, `TypeReplayProgress = 0x13`, sent relay-to-client only. Payload is JSON:

```json
{"phase":"start|chunk|end","bytes":65536,"total_bytes":4194304,"seq":123}
```

Older clients ignore the unknown frame. New clients treat `start` as entering history-loading mode, `chunk` as progress updates, and `end` as the attach becoming fully live.

## Server Behavior

`session.Subscribe(sinceSeq)` sends replay progress frames for the initial scrollback replay. Replay bytes count terminal bytes only, not the soft truncation clear-screen marker. After replay, the subscriber is atomically attached to live fan-out without losing output produced during replay.

The relay `/client` writer uses progress start/end frames to detect replay mode and briefly pauses after configured replay byte intervals. This is intentionally server-side pacing: clients do not need to request special modes.

## Client Behavior

Desktop `TerminalView` displays a compact overlay with percent and bytes while replay is active. The web client shows the same progress in the terminal view. If a relay does not support progress frames, existing behavior continues.

## Testing

Tests cover progress frame emission, replay pacing state, desktop formatting, web formatting, and existing protocol compatibility.
