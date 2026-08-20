# Snippets across many hosts — design

Roadmap item 29 (P6). Run one quick-action snippet on N selected SSH hosts
at once and read the results together.

## 1. What exists, and why it is the wrong shape for this

A quick-action template today is `{id, label, text, hotkey}`
(`desktop/quick_templates.go`). Running one means typing its `text` into the
focused terminal — literally: the frontend calls `sendInput(text)` and then,
16ms later, `sendInput("\r")`, because bundling the CR makes Codex read the
whole thing as a paste. That mechanism is right for "drive the agent in the
terminal I am looking at" and wrong for every part of this feature:

- It needs a terminal per host. Twenty hosts would mean twenty PTYs, twenty
  tabs, and twenty shells, to run one line each.
- It cannot report an exit code. Text typed at a prompt produces scrollback,
  not a status — the only way to know whether the command worked is to read
  the output and guess.
- It cannot tell output apart from prompt, echo, and shell noise, so
  "汇总各主机输出" would be aggregating transcripts, not results.

So multi-host execution is a different operation that happens to reuse the
snippet's text. It runs the snippet as a **command**, over a non-PTY exec
channel, and collects `(stdout+stderr, exit code, error)` per host.

## 2. Reuse, not a second SSH path

Item 26 made `sshclient.Conn` shell-less on purpose, and item 27 put every
jump-chain build behind `App.dialThroughJumps`, which returns a `jumpChain`
whose `Target()` is exactly such a `Conn`. Multi-host exec dials through
that same function.

This is deliberate and load-bearing: it means ProxyJump chains, per-hop host
key checking, and the `ProxyCommand` refusal all apply to batch runs on the
day they land, with no second implementation to keep in sync. Item 27's
constraint that `hostRunsProxyCommand` stays at exactly two call sites is
preserved — batch exec adds none, because it never dials a host itself.

`sshclient` gains one method:

```go
// Run executes cmd on the remote host over a fresh non-PTY session and
// returns its combined stdout+stderr. It is safe to call concurrently on
// one Conn: each call opens its own SSH channel.
func (c *Conn) Run(ctx context.Context, cmd string, limit int64) (output []byte, exitCode int, truncated bool, err error)
```

No PTY is requested, so there is no prompt, no echo, and no terminal control
sequences in the output — the bytes are the command's own. A non-zero exit
is not a Go error: `exitCode` carries it and `err` stays nil, because "the
command ran and failed" and "we could not run the command" are different
outcomes and the UI must show them differently.

## 3. Bounds, and why each one exists

Every bound below has a specific failure it prevents. None is decorative.

| Bound | Value | Prevents |
|---|---|---|
| Concurrent hosts | 8 | 200 selected hosts opening 200 simultaneous TCP dials, each with its own jump chain |
| Per-host wall clock | 60s | One unreachable host holding the whole batch open forever |
| Output per host | 256 KiB | `cat /var/log/syslog` on 20 hosts pulling gigabytes into the renderer |

Output over the cap is truncated at the head and flagged `truncated`, rather
than the run failing: a snippet whose output is too long still ran, and its
first 256 KiB is usually the part the user wanted.

## 4. Failure is per host

One host failing never cancels the others. Each host ends in exactly one of:

- `ok` — ran, exit code 0
- `failed` — ran, non-zero exit code (output still captured)
- `error` — never ran: unreachable, auth rejected, jump hop refused,
  `ProxyCommand` host, or timeout

Cancelling the run cancels the hosts still pending or running; hosts that
already finished keep their results.

## 5. Host keys are not negotiated mid-batch

A host whose key is not yet in `known_hosts` ends as `error` with "host key
not trusted yet", and the batch continues.

It would be easy to reuse item 27's TOFU dialog here, and it would be wrong.
Firing N modal trust prompts during a batch run trains exactly the reflex
that makes TOFU worthless — click-through — and it does it at the moment the
user is least able to check a fingerprint, because they are watching a
progress list. Trust is established by connecting to that host once, on
purpose, in the terminal. The batch consumes trust; it never asks for it.

## 6. Surface

`App.RunSnippetOnHosts(snippetID string, hostIDs []string) (runID string, err error)`
starts the run and returns immediately. Progress arrives as wails events
(`snippet:run:progress`), one per host state transition, carrying the
per-host result. `App.CancelSnippetRun(runID)` cancels.

Push, not poll: a batch takes as long as its slowest host, and a polling UI
would either lag or hammer the binding.

The UI is a panel listing selected hosts with live status, exit code, and
collapsible output, plus copy-all. Selection is by SSH host, multi-select.

## 7. Non-goals

- **No scheduling, no history.** Results live for the life of the run. A
  record of what was run where is an audit feature and belongs with item 16.
- **No per-host variable substitution.** The same text runs everywhere. Any
  templating language is a new surface with its own escaping rules.
- **No sudo prompt handling.** Non-PTY exec cannot answer a password prompt;
  a snippet needing one will hang until the 60s timeout. Documented, not
  worked around.
- **No mobile.** Batch exec is desktop-only, like the SSH host list.
