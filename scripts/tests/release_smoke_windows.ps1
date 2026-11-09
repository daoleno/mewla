# Runs a built Windows release binary the way a user's computer would, on a
# Windows runner: its version, the session host with the terminal engine, the
# daemon answering /health, and install.ps1 installing it from a release-shaped
# zip. The release workflow runs it before staging the Windows archive.
#
#   ./scripts/tests/release_smoke_windows.ps1 -Binary bin/mewla-windows-amd64.exe -Version 0.3.0
param(
  [Parameter(Mandatory = $true)][string]$Binary,
  [Parameter(Mandatory = $true)][string]$Version
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$Binary = (Resolve-Path $Binary).Path
$root = Join-Path ([IO.Path]::GetTempPath()) ("mewla-smoke-" + [Guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path $root | Out-Null
$processes = @()
$failures = 0

function Check([string]$Name, [bool]$Ok, [string]$Detail) {
  if ($Ok) { Write-Host "ok   $Name" } else { Write-Host "FAIL $Name`n$Detail"; $script:failures++ }
}
function Run([string[]]$Arguments) {
  $previous = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $output = & $Binary @Arguments 2>&1 | Out-String
  $code = $LASTEXITCODE
  $ErrorActionPreference = $previous
  return @{ Code = $code; Output = $output }
}
function Start-Mewla([string]$Name, [string[]]$Arguments) {
  $log = Join-Path $root "$Name.log"
  $p = Start-Process -FilePath $Binary -ArgumentList $Arguments -PassThru -NoNewWindow `
    -RedirectStandardOutput $log -RedirectStandardError "$log.err"
  $script:processes += $p
  return $p
}

try {
  # 1. Version and platform.
  $r = Run @('version')
  Write-Host $r.Output.Trim()
  Check 'version' ($r.Code -eq 0 -and $r.Output.Contains("mewla $Version (windows/amd64)")) $r.Output

  # 2. The session host runs with the terminal engine.
  $hostState = Join-Path $root 'host-state'
  [void](Start-Mewla 'host' @('host', '--state-dir', $hostState))
  $status = $null
  for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    $status = Run @('host', 'status', '--state-dir', $hostState)
    if ($status.Code -eq 0) { break }
  }
  Write-Host $status.Output.Trim()
  Check 'session host with the terminal engine' ($status.Code -eq 0 -and $status.Output.Contains('terminal engine libghostty-vt')) $status.Output

  # 3. The daemon answers /health with this version.
  $env:MEWLA_BRAIN_HOST_EXECUTOR = 'agent'  # keeps the throwaway Brain from signing in
  $addr = '127.0.0.1:9879'
  [void](Start-Mewla 'daemon' @('-addr', $addr, '-state-dir', (Join-Path $root 'daemon-state')))
  $health = $null
  for ($i = 0; $i -lt 120; $i++) {
    Start-Sleep -Milliseconds 500
    try { $health = Invoke-RestMethod -Uri "http://$addr/health" -TimeoutSec 5; break } catch { $health = $null }
  }
  Check '/health' ($null -ne $health -and $health.status -eq 'ok' -and $health.version -eq $Version) ("health: " + ($health | ConvertTo-Json -Compress) + "`n" + (Get-Content -Raw -ErrorAction SilentlyContinue (Join-Path $root 'daemon.log.err')))

  # 4. install.ps1 installs it from a release-shaped zip and SHA256SUMS.
  Add-Type -AssemblyName System.IO.Compression
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $staged = Join-Path $root 'release'
  New-Item -ItemType Directory -Path $staged | Out-Null
  $zipPath = Join-Path $staged 'mewla-windows-amd64.zip'
  $zip = [IO.Compression.ZipFile]::Open($zipPath, 'Create')
  try {
    foreach ($name in @('LICENSE', 'NOTICE', 'TRADEMARKS.md')) {
      [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, (Join-Path $repo $name), $name)
    }
    [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $Binary, 'mewla.exe')
  } finally { $zip.Dispose() }
  $sum = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
  [IO.File]::WriteAllText((Join-Path $staged 'SHA256SUMS'), "$sum  mewla-windows-amd64.zip`n")
  $installDir = Join-Path $root 'installed'
  foreach ($kv in @{ MEWLA_INSTALL_FROM = $staged; MEWLA_INSTALL_DIR = $installDir; MEWLA_NO_START = '1'; MEWLA_NO_PATH_UPDATE = '1'; MEWLA_DRY_RUN = ''; MEWLA_VERSION = '' }.GetEnumerator()) {
    [Environment]::SetEnvironmentVariable($kv.Key, $kv.Value, 'Process')
  }
  $previous = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $installer = Join-Path $repo 'install.ps1'
  $out = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw '$installer' | Invoke-Expression" 2>&1 | Out-String
  $code = $LASTEXITCODE
  $ErrorActionPreference = $previous
  Write-Host $out.Trim()
  $installed = Join-Path $installDir 'mewla.exe'
  $ok = $code -eq 0 -and (Test-Path $installed)
  if ($ok) {
    $v = & $installed version 2>&1 | Out-String
    $ok = $v.Contains("mewla $Version (windows/amd64)")
  }
  Check 'install.ps1 from a release-shaped zip' $ok $out
} finally {
  foreach ($p in $processes) { if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue } }
  if ($failures -gt 0) {
    foreach ($log in Get-ChildItem -Path $root -Filter '*.log*' -ErrorAction SilentlyContinue) {
      Write-Host "--- $($log.Name)"; Get-Content -Raw $log.FullName | Write-Host
    }
  }
  Start-Sleep -Seconds 1
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $root
}
if ($failures -gt 0) { Write-Host "$failures Windows release check(s) failed"; exit 1 }
Write-Host 'Windows release checks passed'
