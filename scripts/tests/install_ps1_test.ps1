# Offline tests for install.ps1 on Windows: a staged release folder with a
# real mewla.exe, a checked SHA-256, the zip layout guard, PATH and the swap
# over a running mewla.exe. Usage: install_ps1_test.ps1 -Binary <mewla.exe>
param([Parameter(Mandatory = $true)][string]$Binary)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$installer = Join-Path $repo 'install.ps1'
$root = Join-Path ([IO.Path]::GetTempPath()) ('mewla-ps1-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$failures = 0
$savedPath = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')

function Stage([string]$Name, [hashtable]$Entries, [string]$Sums) {
  $dir = Join-Path $root $Name
  New-Item -ItemType Directory -Path $dir | Out-Null
  $zipPath = Join-Path $dir 'mewla-windows-amd64.zip'
  $zip = [IO.Compression.ZipFile]::Open($zipPath, 'Create')
  foreach ($entry in $Entries.GetEnumerator()) {
    if ($entry.Value -is [string] -and (Test-Path -LiteralPath $entry.Value)) {
      [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $entry.Value, $entry.Key)
    } else {
      $writer = New-Object IO.StreamWriter($zip.CreateEntry($entry.Key).Open())
      $writer.Write([string]$entry.Value); $writer.Close()
    }
  }
  $zip.Dispose()
  if ([string]::IsNullOrEmpty($Sums)) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
    $Sums = "$hash  mewla-windows-amd64.zip`n0000000000000000000000000000000000000000000000000000000000000000  mewla-linux-amd64.tar.gz`n"
  }
  [IO.File]::WriteAllText((Join-Path $dir 'SHA256SUMS'), $Sums)
  return $dir
}

# Runs install.ps1 the way `irm | iex` does, in a fresh powershell.exe.
function Install([hashtable]$Env) {
  $vars = @{ MEWLA_INSTALL_FROM = ''; MEWLA_INSTALL_DIR = ''; MEWLA_NO_START = '1'; MEWLA_NO_PATH_UPDATE = '1'; MEWLA_DRY_RUN = ''; MEWLA_VERSION = '' }
  foreach ($k in $Env.Keys) { $vars[$k] = $Env[$k] }
  foreach ($k in $vars.Keys) { [Environment]::SetEnvironmentVariable($k, $vars[$k], 'Process') }
  $previous = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  $output = & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw '$installer' | Invoke-Expression" 2>&1 | Out-String
  $code = $LASTEXITCODE
  $ErrorActionPreference = $previous
  return @{ Code = $code; Output = $output }
}

function Check([string]$Name, [bool]$Ok, [string]$Detail) {
  if ($Ok) { Write-Host "ok   $Name" } else { Write-Host "FAIL $Name`n$Detail"; $script:failures++ }
}

try {
  $good = @{ 'LICENSE' = 'l'; 'NOTICE' = 'n'; 'TRADEMARKS.md' = 't'; 'mewla.exe' = $Binary }
  $dir = Stage 'good' $good $null
  $installDir = Join-Path $root 'Programs\Mewla'

  $r = Install @{ MEWLA_INSTALL_FROM = $dir; MEWLA_INSTALL_DIR = $installDir; MEWLA_DRY_RUN = '1' }
  Check 'dry run changes nothing' ($r.Code -eq 0 -and $r.Output -match 'No files were changed' -and -not (Test-Path $installDir)) $r.Output

  $r = Install @{ MEWLA_INSTALL_FROM = $dir; MEWLA_INSTALL_DIR = $installDir; MEWLA_NO_PATH_UPDATE = '0' }
  $exe = Join-Path $installDir 'mewla.exe'
  Check 'installs from a staged release' ($r.Code -eq 0 -and (Test-Path $exe) -and $r.Output -match 'Installed Mewla') $r.Output
  $userPath = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')
  Check 'adds the folder to the user PATH once' (@(@($userPath -split ';') | Where-Object { $_ -ieq $installDir }).Count -eq 1) $userPath
  $version = & $exe version 2>&1 | Out-String
  Check 'the installed mewla.exe runs' ($LASTEXITCODE -eq 0) $version
  Check 'no staging files are left' (@(Get-ChildItem $installDir -Force | Where-Object { $_.Name -like '.mewla-install-*' }).Count -eq 0) ''

  # A second install over a running mewla.exe moves it aside instead of
  # failing: a long-running program stands in for the running daemon.
  $runDir = Join-Path $root 'running'
  New-Item -ItemType Directory -Path $runDir | Out-Null
  Copy-Item (Join-Path $env:SystemRoot 'System32\PING.EXE') (Join-Path $runDir 'mewla.exe')
  $hold = Start-Process -FilePath (Join-Path $runDir 'mewla.exe') -ArgumentList '-n', '60', '127.0.0.1' -PassThru -WindowStyle Hidden
  Start-Sleep -Milliseconds 500
  $r = Install @{ MEWLA_INSTALL_FROM = $dir; MEWLA_INSTALL_DIR = $runDir }
  $swapped = (Get-FileHash (Join-Path $runDir 'mewla.exe')).Hash -eq (Get-FileHash $Binary).Hash
  $aside = @(Get-ChildItem $runDir -Filter 'mewla.exe.old-*').Count
  Check 'installs over a running mewla.exe by moving it aside' ($r.Code -eq 0 -and $swapped -and $aside -eq 1 -and -not $hold.HasExited) "$($r.Output) swapped=$swapped aside=$aside"
  Stop-Process -Id $hold.Id -Force -ErrorAction SilentlyContinue

  $r = Install @{ MEWLA_INSTALL_FROM = $dir; MEWLA_INSTALL_DIR = $installDir; MEWLA_NO_PATH_UPDATE = '0' }
  Check 'reinstall succeeds' ($r.Code -eq 0) $r.Output
  $userPath = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')
  Check 'PATH entry is not duplicated' (@(@($userPath -split ';') | Where-Object { $_ -ieq $installDir }).Count -eq 1) $userPath

  $bad = Stage 'badsum' $good ("0" * 64 + "  mewla-windows-amd64.zip`n")
  $r = Install @{ MEWLA_INSTALL_FROM = $bad; MEWLA_INSTALL_DIR = (Join-Path $root 'bad1') }
  Check 'refuses a SHA-256 mismatch' ($r.Code -ne 0 -and $r.Output -match 'SHA-256 mismatch' -and -not (Test-Path (Join-Path $root 'bad1\mewla.exe'))) $r.Output

  $dup = Stage 'dupsum' $good $null
  $line = (Get-Content (Join-Path $dup 'SHA256SUMS'))[0]
  [IO.File]::WriteAllText((Join-Path $dup 'SHA256SUMS'), "$line`n$line`n")
  $r = Install @{ MEWLA_INSTALL_FROM = $dup; MEWLA_INSTALL_DIR = (Join-Path $root 'bad2') }
  Check 'refuses two SHA-256 entries for the archive' ($r.Code -ne 0 -and $r.Output -match 'exactly one') $r.Output

  $extra = $good.Clone(); $extra['evil.dll'] = 'x'
  $r = Install @{ MEWLA_INSTALL_FROM = (Stage 'extra' $extra $null); MEWLA_INSTALL_DIR = (Join-Path $root 'bad3') }
  Check 'refuses an extra archive entry' ($r.Code -ne 0 -and $r.Output -match 'unexpected archive layout' -and -not (Test-Path (Join-Path $root 'bad3\evil.dll'))) $r.Output

  $nested = @{ 'LICENSE' = 'l'; 'NOTICE' = 'n'; 'TRADEMARKS.md' = 't'; 'bin/mewla.exe' = $Binary }
  $r = Install @{ MEWLA_INSTALL_FROM = (Stage 'nested' $nested $null); MEWLA_INSTALL_DIR = (Join-Path $root 'bad4') }
  Check 'refuses a nested mewla.exe' ($r.Code -ne 0 -and $r.Output -match 'unexpected archive layout') $r.Output

  $broken = @{ 'LICENSE' = 'l'; 'NOTICE' = 'n'; 'TRADEMARKS.md' = 't'; 'mewla.exe' = 'not a program' }
  $keepDir = Join-Path $root 'keep'
  New-Item -ItemType Directory -Path $keepDir | Out-Null
  Copy-Item $Binary (Join-Path $keepDir 'mewla.exe')
  $r = Install @{ MEWLA_INSTALL_FROM = (Stage 'broken' $broken $null); MEWLA_INSTALL_DIR = $keepDir }
  $kept = (Get-FileHash (Join-Path $keepDir 'mewla.exe')).Hash -eq (Get-FileHash $Binary).Hash
  Check 'a binary that does not run leaves the old install' ($r.Code -ne 0 -and $kept) $r.Output

  $r = Install @{ MEWLA_VERSION = 'v1.02.3'; MEWLA_INSTALL_DIR = (Join-Path $root 'bad5') }
  Check 'refuses a malformed version' ($r.Code -ne 0 -and $r.Output -match 'invalid version') $r.Output

  $r = Install @{ MEWLA_INSTALL_DIR = $env:SystemRoot; MEWLA_DRY_RUN = '1' }
  Check 'refuses a system folder' ($r.Code -ne 0 -and $r.Output -match 'system folder') $r.Output
} finally {
  $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
  $key.SetValue('Path', $savedPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
  $key.Close()
  Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
if ($failures -gt 0) { Write-Host "$failures install.ps1 test(s) failed"; exit 1 }
Write-Host 'install.ps1 tests passed'
# The last refused install left a non-zero $LASTEXITCODE; this run passed.
exit 0
