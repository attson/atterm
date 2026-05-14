# atterm shell integration — OSC 133 command boundary markers
# Sourced by atterm-spawned PowerShell sessions via -NoExit -Command.

if ($env:ATTERM_SHELL_INTEGRATION_LOADED) { return }
$env:ATTERM_SHELL_INTEGRATION_LOADED = "1"

$global:__atterm_last_exit = 0

# Preserve the user's original prompt function (if any) so frameworks like
# oh-my-posh still render normally; we only wrap markers around it.
if (Get-Command __atterm_user_prompt -ErrorAction SilentlyContinue) { } else {
    if (Test-Path Function:\prompt) {
        Copy-Item Function:\prompt Function:\__atterm_user_prompt
    } else {
        function global:__atterm_user_prompt { "PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) " }
    }
}

function global:prompt {
    $exit = if ($?) { 0 } else { 1 }
    if ($LASTEXITCODE -ne $null) { $exit = $LASTEXITCODE }
    [Console]::Write("`e]133;D;$exit`a")
    [Console]::Write("`e]133;A`a")
    $p = __atterm_user_prompt
    [Console]::Write("`e]133;B`a")
    return $p
}

# Emit OSC 133;C just before a command starts running. PSReadLine fires
# OnExecute right after the user hits Enter, which is the closest hook to
# preexec; if PSReadLine is not loaded we fall back to a no-op (the user
# can still get D/A/B markers).
if (Get-Module -ListAvailable PSReadLine) {
    Import-Module PSReadLine
    Set-PSReadLineKeyHandler -Key Enter -ScriptBlock {
        [Console]::Write("`e]133;C`a")
        [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine()
    }
}
