# atterm auto-update install helper for Windows.
# Args: -ProcessId <pid> -Src <archive> -Dst <exe-path>
param(
  [Parameter(Mandatory=$true)][int]$ProcessId,
  [Parameter(Mandatory=$true)][string]$Src,
  [Parameter(Mandatory=$true)][string]$Dst
)

$log = Join-Path $env:LOCALAPPDATA "atterm\install-$ProcessId.log"
New-Item -ItemType Directory -Force -Path (Split-Path $log) | Out-Null
Start-Transcript -Path $log -Append | Out-Null

function Stop-InstallTranscript {
  try { Stop-Transcript | Out-Null } catch { }
}

function Test-IsAdministrator {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = New-Object Security.Principal.WindowsPrincipal($identity)
  return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if (-not (Test-IsAdministrator)) {
  # The NSIS installer defaults to Program Files, so replacement normally
  # needs UAC even though the helper is launched by the unelevated app.
  $arguments = @(
    "-NoProfile",
    "-ExecutionPolicy Bypass",
    "-File `"$PSCommandPath`"",
    "-ProcessId $ProcessId",
    "-Src `"$Src`"",
    "-Dst `"$Dst`""
  )
  Start-Process -FilePath "powershell.exe" -ArgumentList $arguments -Verb RunAs -ErrorAction Stop
  Stop-InstallTranscript
  exit 0
}

$attempts = 0
while ($attempts -lt 60) {
  if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
  $attempts++
}

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "atterm-update-$([guid]::NewGuid())")
Expand-Archive -Path $Src -DestinationPath $tmp.FullName -Force
$exe = Join-Path $tmp.FullName "AT Term.exe"
if (-not (Test-Path $exe)) { throw "AT Term.exe not in archive" }

# Move-Item can transiently fail if Windows still holds the .exe handle
# (close-after-exit can lag). Retry briefly before giving up.
$moved = $false
for ($i = 0; $i -lt 10; $i++) {
  try {
    Move-Item -Path $exe -Destination $Dst -Force -ErrorAction Stop
    $moved = $true
    break
  } catch {
    Start-Sleep -Milliseconds 500
  }
}
if (-not $moved) { throw "could not replace $Dst (file in use)" }

Start-Process -FilePath $Dst

Remove-Item $Src -Force -ErrorAction SilentlyContinue
Remove-Item $tmp.FullName -Recurse -Force

Stop-InstallTranscript
