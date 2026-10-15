# atterm shell integration — OSC 133 command boundary markers
# Loaded by atterm-spawned zsh sessions via a wrapper $ZDOTDIR/.zshrc.
# Safe to source manually outside atterm; the guard prevents double-load.

if [[ -n "${ATTERM_SHELL_INTEGRATION_LOADED:-}" ]]; then
  return 0
fi
ATTERM_SHELL_INTEGRATION_LOADED=1

# zsh passes preexec three forms of the command line:
#   $1 — exactly what the user typed
#   $2 — a single-line, history-expanded form that zsh SIZE-TRUNCATES (it can
#        drop the trailing token, e.g. "claude --permission-mode bypassPermissions"
#        arrives as "claude --permission-mode " — see the AI-resume flag-loss bug)
#   $3 — the full single-line command as it will run (alias-expanded, untruncated)
# Use $3 so the captured OSC 133;C command line is complete; fall back to $2/$1
# only if an older zsh doesn't provide it.
__atterm_preexec()      { printf '\033]133;C;%s\007' "${3:-${2:-$1}}"; }
__atterm_precmd()       { printf '\033]133;D;%s\007' "$?"; }

# Use additive hook arrays so frameworks (oh-my-zsh, powerlevel10k, starship)
# keep their own precmd/preexec entries intact.
typeset -ag precmd_functions
typeset -ag preexec_functions
precmd_functions+=(__atterm_precmd)
preexec_functions+=(__atterm_preexec)

# Wrap PS1 with prompt-start / prompt-end markers without disturbing the rest
# of the prompt. %{ ... %} prevents zsh from counting these bytes against the
# visible prompt width. Literal control bytes also work with PROMPT_SUBST off,
# without enabling command substitution in the user's existing prompt.
PS1=$'%{\033]133;A\007%}'"${PS1}"$'%{\033]133;B\007%}'
