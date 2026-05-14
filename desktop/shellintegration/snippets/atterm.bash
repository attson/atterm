# atterm shell integration — OSC 133 command boundary markers
# Loaded by atterm-spawned bash sessions via --rcfile.

if [[ -n "${ATTERM_SHELL_INTEGRATION_LOADED:-}" ]]; then
  return 0
fi
ATTERM_SHELL_INTEGRATION_LOADED=1

__atterm_prompt_start='\[\033]133;A\007\]'
__atterm_prompt_end='\[\033]133;B\007\]'

__atterm_preexec() {
  # Skip programmable-completion and PROMPT_COMMAND re-entries; only fire on
  # interactive command starts. BASH_COMMAND holds the about-to-run command.
  [[ -n "$COMP_LINE" ]] && return
  [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return
  printf '\033]133;C\007'
}

__atterm_precmd() {
  local exit=$?
  printf '\033]133;D;%s\007' "$exit"
}

# Chain into existing PROMPT_COMMAND rather than overwriting it.
PROMPT_COMMAND="__atterm_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"

# Bash has no native preexec; trap DEBUG approximates it.
trap '__atterm_preexec' DEBUG

PS1="${__atterm_prompt_start}${PS1}${__atterm_prompt_end}"
