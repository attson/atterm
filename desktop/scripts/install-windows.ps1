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

$attempts = 0
while ($attempts -lt 60) {
  if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
  $attempts++
}

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "atterm-update-$([guid]::NewGuid())")
Expand-Archive -Path $Src -DestinationPath $tmp.FullName -Force
$exe = Join-Path $tmp.FullName "atterm-desktop.exe"
if (-not (Test-Path $exe)) { throw "atterm-desktop.exe not in archive" }

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

Stop-Transcript | Out-Null
