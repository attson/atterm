# atterm shell integration — OSC 133 command boundary markers
# Auto-loaded by fish from $XDG_CONFIG_HOME/fish/conf.d/.

if set -q __atterm_loaded
    return
end
set -g __atterm_loaded 1

function __atterm_preexec --on-event fish_preexec
    printf '\033]133;C\007'
end

function __atterm_postexec --on-event fish_postexec
    printf '\033]133;D;%s\007' $status
end

# Wrap fish_prompt without clobbering the user's definition. If they have one,
# rename it so we can call it from our wrapper; if not, our wrapper provides a
# minimal default. Either way the markers always bracket the real prompt.
if functions -q fish_prompt
    functions --copy fish_prompt __atterm_user_prompt
else
    function __atterm_user_prompt
        printf '%s@%s %s> ' $USER (prompt_hostname) (prompt_pwd)
    end
end

function fish_prompt
    printf '\033]133;A\007'
    __atterm_user_prompt
    printf '\033]133;B\007'
end
