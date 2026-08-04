# Shell Integration

AT Term auto-injects [OSC 133](https://gitlab.freedesktop.org/Per_Bothner/specifications/-/blob/master/proposals/semantic-prompts.md) command-boundary hooks into shells it spawns, so commands that take a while produce a system notification when they finish (only if the AT Term window is unfocused).

## What gets injected

Each time AT Term spawns a shell, we add a small wrapper that loads our snippet **after** your normal rc:

| Shell | Mechanism | User config touched |
|-------|-----------|---------------------|
| zsh | `$ZDOTDIR` set to a temp dir whose `.zshrc` sources your original rc, then ours | none |
| bash | `--rcfile <tmp>` passed at launch; tmp sources `~/.bashrc`, then ours | none |
| fish | `$XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish` (or `~/.config/...`) | shared file in `conf.d/` |
| PowerShell | `-NoExit -Command "& '<tmp>'"` | none (your `$PROFILE` is untouched) |

For zsh / bash / pwsh, the temp files are deleted when the session closes. The fish file persists across sessions (and across AT Term uninstalls) because fish auto-loads everything in `conf.d/`. Delete it manually if you no longer want it: `rm $XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish`.

## How to disable

Settings → General → "Enable shell integration". Disabling affects newly spawned sessions; existing sessions keep their current hooks until they exit.

## Configuration

| Setting | Default | Notes |
|---------|---------|-------|
| Enable shell integration | on | Master switch |
| Command-finished notification threshold (seconds) | 10 | Only commands lasting at least this long trigger a notification. Min 1, max 600. |

Notifications also require the AT Term window to be unfocused and the session to be local (a session you started, not one you cast-attached to from another machine).

## Manual install (unsupported shells)

cmd.exe, nu, xonsh, elvish and friends are not auto-injected. If you want OSC 133 markers in those shells, source the relevant snippet manually. The snippet contents are available in the AT Term source tree under `desktop/shellintegration/snippets/`.

## Frameworks (oh-my-zsh, powerlevel10k, starship, oh-my-posh)

Our snippets never touch your `PS1` directly; they use the shell's additive hook arrays (`precmd_functions`, `preexec_functions`, `PROMPT_COMMAND`, fish event hooks, PowerShell `prompt` function wrapping). Frameworks should keep working unchanged. If your prompt looks broken after enabling integration, please file a bug with the framework name and AT Term version.

## Privacy

OSC 133 markers carry only command-boundary metadata (start, end, exit code). The command text itself stays in your shell history and the PTY output stream — AT Term does not log it separately.
