---
description: Log in or pair from Web, PWA, iPhone, iPad, or another desktop, and use AT Term viewer, driver, and remote permissions correctly.
---

# Cross-device clients

The desktop host runs the PTY. Web, PWA, iOS, or another desktop attaches to it. Leaving a remote client does not stop the task on the host; open the same session again to continue.

## Choose a client

| Client | Best for | Main limitation |
|---|---|---|
| Another desktop | Extended keyboard-and-mouse control | Requires the app and the same Relay account or Peer Space |
| Browser | Temporary viewing and takeover | Production requires HTTPS; local browser tab state can disappear when closed |
| PWA | A fixed app-like entry and Web Push | iOS notifications require Add to Home Screen; a plain Safari tab cannot receive them |
| iPhone / iPad | Mobile monitoring, short input, QR pairing | Currently built from source; it does not run a local PTY or SSH client |

## Log in or pair

With Relay, use the same account on the browser login page or in client settings. iOS can also scan a five-minute, one-time pairing code created on desktop. Successful credentials go to Keychain, not browser localStorage.

With Peer Space, create an invitation or connection bundle on an existing member and import it on the new device. Compare both paths in [Remote takeover and sessions](/en/guide/remote-takeover).

## Viewer, driver, and permission

A newly attached remote client starts as a **viewer**. It can watch output but cannot type. Use the takeover action to become the **driver**. The host and relay coordinate control, and a client returning to the foreground does not silently steal an existing driver.

The desktop owner also sets a remote permission per session:

| Permission | View output | Input / resize | Images, files, filesystem |
|---|---:|---:|---:|
| `view` | Yes | No | No |
| `control` | Yes | Yes | No |
| `full` | Yes | Yes | Yes |

Driver status does not bypass permission: a `view` session stays read-only. Both the relay and desktop host enforce the setting.

## Mobile input

Web and iOS terminals provide Enter, Esc, Tab, `Ctrl-C`, `Ctrl-D`, and arrow-key controls. AT Term handles iOS composition input separately, and viewer mode keeps the PTY dimensions chosen by the host so fitting a phone screen cannot relayout the running program.

After a connection recovers, output continues by sequence number; large history replays show progress. If output works but input does not, check driver status, session permission, and connection health, then see [FAQ and troubleshooting](/en/guide/faq).
