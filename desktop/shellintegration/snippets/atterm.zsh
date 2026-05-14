# atterm shell integration — OSC 133 command boundary markers
# Loaded by atterm-spawned zsh sessions via a wrapper $ZDOTDIR/.zshrc.
# Safe to source manually outside atterm; the guard prevents double-load.

if [[ -n "${ATTERM_SHELL_INTEGRATION_LOADED:-}" ]]; then
  return 0
fi
ATTERM_SHELL_INTEGRATION_LOADED=1

__atterm_prompt_start() { printf '\033]133;A\007'; }
__atterm_prompt_end()   { printf '\033]133;B\007'; }
__atterm_preexec()      { printf '\033]133;C\007'; }
__atterm_precmd()       { printf '\033]133;D;%s\007' "$?"; }

# Use additive hook arrays so frameworks (oh-my-zsh, powerlevel10k, starship)
# keep their own precmd/preexec entries intact.
typeset -ag precmd_functions
typeset -ag preexec_functions
precmd_functions+=(__atterm_precmd)
preexec_functions+=(__atterm_preexec)

# Wrap PS1 with prompt-start / prompt-end markers without disturbing the rest
# of the prompt. %{ ... %} prevents zsh from counting these bytes against the
# visible prompt width.
PS1='%{$(__atterm_prompt_start)%}'"${PS1}"'%{$(__atterm_prompt_end)%}'
