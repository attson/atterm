# atterm auto-update install helper for Windows.
# Args: -ProcessId <pid> -Src <archive> -Dst <exe-path>
param(
  [Parameter(Mandatory=$true)][int]$ProcessId,
  [Parameter(Mandatory=$true)][string]$Src,
  [Parameter(Mandatory=$true)][string]$Dst,
  [switch]$ElevatedWorker
)

$ErrorActionPreference = "Stop"
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

if (-not $ElevatedWorker) {
  # The NSIS installer defaults to Program Files, so replacement normally
  # needs UAC. Wait here so this original-user process, rather than the
  # elevated worker, launches the updated app with the original profile.
  $arguments = @(
    "-NoProfile",
    "-ExecutionPolicy Bypass",
    "-File `"$PSCommandPath`"",
    "-ProcessId $ProcessId",
    "-Src `"$Src`"",
    "-Dst `"$Dst`"",
    "-ElevatedWorker"
  )
  Stop-InstallTranscript

  try {
    $worker = Start-Process -FilePath "powershell.exe" -ArgumentList $arguments -Verb RunAs -Wait -PassThru -ErrorAction Stop
  } catch {
    Add-Content -Path $log -Value "Elevated update worker failed to start: $($_.Exception.Message)"
    exit 1
  }
  if ($worker.ExitCode -ne 0) {
    Add-Content -Path $log -Value "Elevated update worker exited with code $($worker.ExitCode)."
    exit $worker.ExitCode
  }

  Start-Transcript -Path $log -Append | Out-Null
  try {
    Start-Process -FilePath $Dst -ErrorAction Stop
  } catch {
    Write-Output "Update installed, but AT Term could not be restarted: $($_.Exception.Message)"
    Stop-InstallTranscript
    exit 1
  }
  Stop-InstallTranscript
  exit 0
}

if (-not (Test-IsAdministrator)) {
  Write-Output "Elevated update worker is not running as administrator."
  Stop-InstallTranscript
  exit 1
}

$tmp = $null
$workerExitCode = 0
try {
  $attempts = 0
  while ($attempts -lt 60) {
    if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 500
    $attempts++
  }
  if (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) {
    throw "AT Term did not exit before the update timeout."
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

  Remove-Item $Src -Force -ErrorAction SilentlyContinue
} catch {
  $workerExitCode = 1
  Write-Output "Update installation failed: $($_.Exception.Message)"
} finally {
  if ($null -ne $tmp) {
    Remove-Item $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
  }
  Stop-InstallTranscript
}
exit $workerExitCode
