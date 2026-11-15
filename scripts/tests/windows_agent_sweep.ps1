# Windows agent sweep: how Mewla starts the agents installed on this computer
# (official installer's .exe, npm's .cmd) in awkward folders with awkward
# arguments, and how a Worker started the way Brain starts one (mewla worker
# spawn) answers and closes there. Windows PowerShell 5.1 and PowerShell 7.
#
# Run it as the Windows user whose Mewla is running, against that daemon:
#   powershell -ExecutionPolicy Bypass -File windows_agent_sweep.ps1 [-StateDir C:\Users\me\.mewla] [-Workers]
# It prints one line per scenario (PASS / FAIL / SKIP and why) and writes the
# same as JSON to -Out. Everything it creates is under -Root, removed at the
# end; Workers it spawns are closed. Brain's own chat (web or phone) is not
# covered here: the daemon has no CLI for it.
param(
  [string]$Mewla = "",
  [string]$StateDir = "",
  [string]$Root = "",
  [string]$Out = "",
  [string[]]$Agents = @("claude", "codex"),
  [switch]$Workers,
  [int]$ReplySeconds = 150
)

$ErrorActionPreference = "Stop"
if (-not $Mewla) { $Mewla = (Get-Command mewla -ErrorAction Stop).Source }
if (-not $StateDir) { $StateDir = Join-Path $env:USERPROFILE ".mewla" }
# Not under %TEMP%: Mewla refuses temporary storage as a Worker's folder.
if (-not $Root) { $Root = Join-Path $env:USERPROFILE ("mewla-sweep-" + [guid]::NewGuid().ToString("N").Substring(0, 8)) }
if (-not $Out) { $Out = Join-Path $env:TEMP "mewla-sweep-results.json" }
$results = New-Object System.Collections.ArrayList
# Chinese text without non-ASCII in this file (Windows PowerShell 5.1 reads a
# script without a byte order mark as the ANSI code page).
$cjk = -join [char[]](0x9879, 0x76EE, 0x0020, 0x4E2D, 0x6587)

function Add-Result([string]$Scenario, [string]$Outcome, [string]$Detail) {
  $line = "{0,-4} {1}: {2}" -f $Outcome, $Scenario, ($Detail -replace "\s+", " ")
  Write-Output $line
  [void]$results.Add([pscustomobject]@{ scenario = $Scenario; outcome = $Outcome; detail = $Detail })
}

# Quote one argument for CreateProcess (CommandLineToArgvW rules), so the
# daemon CLI gets exactly the text here in both PowerShell 5.1 and 7.
function Quote-Arg([string]$Arg) {
  if ($Arg -ne "" -and $Arg -notmatch '[\s"]') { return $Arg }
  $sb = New-Object System.Text.StringBuilder
  [void]$sb.Append('"')
  $slashes = 0
  foreach ($c in $Arg.ToCharArray()) {
    if ($c -eq '\') { $slashes++; continue }
    if ($c -eq '"') { [void]$sb.Append('\' * (2 * $slashes + 1)); [void]$sb.Append('"'); $slashes = 0; continue }
    [void]$sb.Append('\' * $slashes); $slashes = 0; [void]$sb.Append($c)
  }
  [void]$sb.Append('\' * (2 * $slashes))
  [void]$sb.Append('"')
  return $sb.ToString()
}

# Run a program with exact arguments; returns exit code and output.
function Invoke-Exact([string]$File, [string[]]$Arguments, [string]$Dir = "", [int]$TimeoutSeconds = 60) {
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = $File
  $psi.Arguments = (($Arguments | ForEach-Object { Quote-Arg $_ }) -join " ")
  $psi.UseShellExecute = $false
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.StandardOutputEncoding = [System.Text.Encoding]::UTF8
  $psi.StandardErrorEncoding = [System.Text.Encoding]::UTF8
  if ($Dir) { $psi.WorkingDirectory = $Dir }
  try { $p = [System.Diagnostics.Process]::Start($psi) } catch { return @{ code = -1; out = "start: $($_.Exception.Message)" } }
  $stdout = $p.StandardOutput.ReadToEndAsync()
  $stderr = $p.StandardError.ReadToEndAsync()
  if (-not $p.WaitForExit($TimeoutSeconds * 1000)) { try { $p.Kill() } catch {}; return @{ code = -2; out = "timed out after $TimeoutSeconds s" } }
  return @{ code = $p.ExitCode; out = ($stdout.Result + $stderr.Result).Trim() }
}

function Shell-Word([string]$Text) { return "'" + $Text.Replace("'", "'\''") + "'" }

# --- folders ---------------------------------------------------------------
New-Item -ItemType Directory -Force $Root | Out-Null
$folders = [ordered]@{}
$folders["plain"] = Join-Path $Root "plain"
$folders["spaces"] = Join-Path $Root "with spaces & ^caret (1)"
$folders["percent"] = Join-Path $Root "100% done"
$folders["cjk"] = Join-Path $Root ($cjk + [char]0x00E9)
# Near MAX_PATH (a program can still start in it), and past it (Windows
# starts no program in a folder that long: Mewla must say so).
$long = $Root
while ($long.Length -lt 200) { $long = Join-Path $long "a-rather-long-folder-name" }
$folders["long-200"] = $long
$longer = $long
while ($longer.Length -lt 270) { $longer = Join-Path $longer "a-rather-long-folder-name" }
$folders["long-270"] = $longer
$folders["readonly"] = Join-Path $Root "readonly"
$folders["programfiles"] = Join-Path $env:ProgramFiles ("MewlaSweep " + (Split-Path $Root -Leaf))
foreach ($name in @($folders.Keys)) {
  try { New-Item -ItemType Directory -Force $folders[$name] | Out-Null } catch { Add-Result "folder:$name" "SKIP" "cannot create: $($_.Exception.Message)"; $folders.Remove($name) }
}
if ($folders.Contains("readonly")) {
  & icacls $folders["readonly"] /deny "${env:USERNAME}:(W,AD,WD)" | Out-Null
}
$unc = "\\localhost\" + $folders["plain"].Substring(0, 1) + "$" + $folders["plain"].Substring(2)
if (Test-Path $unc) { $folders["unc"] = $unc } else { Add-Result "folder:unc" "SKIP" "no admin share at $unc" }
$otherDrive = Get-PSDrive -PSProvider FileSystem | Where-Object { $_.Root -ne ($env:SystemDrive + "\") -and $_.Free -gt 0 } | Select-Object -First 1
if ($otherDrive) {
  $d = Join-Path $otherDrive.Root ("mewla-sweep-" + (Split-Path $Root -Leaf))
  try { New-Item -ItemType Directory -Force $d | Out-Null; $folders["other-drive"] = $d } catch { Add-Result "folder:other-drive" "SKIP" $_.Exception.Message }
} else { Add-Result "folder:other-drive" "SKIP" "only $env:SystemDrive here" }
if ($env:OneDrive -and (Test-Path $env:OneDrive)) {
  $d = Join-Path $env:OneDrive ("mewla-sweep-" + (Split-Path $Root -Leaf)); New-Item -ItemType Directory -Force $d | Out-Null; $folders["onedrive"] = $d
} else { Add-Result "folder:onedrive" "SKIP" "OneDrive is not set up for $env:USERNAME" }
$missing = Join-Path $Root "does-not-exist"

# --- install styles --------------------------------------------------------
$installs = @()
foreach ($agent in $Agents) {
  $found = @(Get-Command $agent -All -ErrorAction SilentlyContinue | Where-Object { $_.CommandType -eq "Application" -and $_.Source -match '\.(exe|cmd|bat)$' })
  if ($found.Count -eq 0) { Add-Result "install:$agent" "SKIP" "not on PATH"; continue }
  foreach ($f in $found) { $installs += [pscustomobject]@{ agent = $agent; path = $f.Source; kind = [IO.Path]::GetExtension($f.Source).TrimStart(".") } }
  $first = $found[0]
  $v = Invoke-Exact $first.Source @("--version") "" 60
  Add-Result "install:$agent" "INFO" ("first on PATH: {0} ({1}); version: {2}" -f $first.Source, ([IO.Path]::GetExtension($first.Source)), ($v.out -split "`n")[0])
}

# --- launches: Brain's and Workers' own argument shapes through mewla sh ----
$json = '{"permissions":{"defaultMode":"bypassPermissions"},"note":"100% & ^caret | <x> \"quoted\" ' + $cjk + '"}'
foreach ($install in $installs) {
  $program = $install.path -replace '\\', '/'
  $agentArgs = if ($install.agent -eq "claude") { "--settings " + (Shell-Word $json) + " --add-dir " + (Shell-Word $folders["spaces"]) + " --version" } else { "-c " + (Shell-Word 'model="x y 100%"') + " --version" }
  foreach ($name in $folders.Keys) {
    if ($name -eq "long-270") { continue } # this harness cannot start a program there either
    $r = Invoke-Exact $Mewla @("sh", "-c", ((Shell-Word $program) + " " + $agentArgs)) $folders[$name] 90
    $label = "launch:$($install.agent).$($install.kind):$name"
    if ($r.code -eq 0 -and $r.out -match '\d+\.\d+') { Add-Result $label "PASS" ($r.out -split "`n")[-1] } else { Add-Result $label "FAIL" ("exit {0}: {1}" -f $r.code, $r.out) }
  }
}

# --- Workers, started as Brain starts them ---------------------------------
function Worker-Id($text) {
  try { $o = $text | ConvertFrom-Json } catch { return "" }
  if ($o.worker -and $o.worker.id) { return $o.worker.id }
  if ($o.id) { return $o.id }
  return ""
}
if ($Workers) {
  $cases = @("spaces", "cjk", "percent", "readonly", "programfiles", "long-200", "long-270", "unc", "other-drive", "onedrive") | Where-Object { $folders.Contains($_) }
  foreach ($agent in $Agents) {
    if (-not ($installs | Where-Object { $_.agent -eq $agent })) { continue }
    foreach ($name in @($cases) + @("missing")) {
      $dir = if ($name -eq "missing") { $missing } else { $folders[$name] }
      $token = "pong-" + [guid]::NewGuid().ToString("N").Substring(0, 6)
      $promptFile = Join-Path $env:TEMP ("sweep-prompt-" + $token + ".md")
      [IO.File]::WriteAllText($promptFile, "Reply with exactly this line and nothing else, then stop: $token 100% & ^caret ""quoted"" $cjk", (New-Object System.Text.UTF8Encoding $false))
      $label = "worker:${agent}:$name"
      $spawn = Invoke-Exact $Mewla @("worker", "spawn", "-state-dir", $StateDir, "-name", "Sweep $agent $name", "-executor", $agent, "-cwd", $dir, "-profile", "quick", "-prompt-file", $promptFile) "" 120
      Remove-Item $promptFile -ErrorAction SilentlyContinue
      $id = Worker-Id $spawn.out
      if (-not $id) {
        $outcome = if ($name -eq "missing" -and $spawn.out -match "(?i)not exist|cannot find|no such") { "PASS" } elseif ($name -eq "long-270" -and $spawn.out -match "(?i)too long|path") { "PASS" } else { "FAIL" }
        Add-Result $label $outcome ("spawn exit {0}: {1}" -f $spawn.code, $spawn.out)
        continue
      }
      $deadline = (Get-Date).AddSeconds($ReplySeconds)
      $seen = $false; $last = ""
      while ((Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 5
        $cap = Invoke-Exact $Mewla @("worker", "capture", "-state-dir", $StateDir, "-id", $id) "" 30
        $last = $cap.out
        if (([regex]::Matches($last, [regex]::Escape($token))).Count -ge 2) { $seen = $true; break }
      }
      $status = Invoke-Exact $Mewla @("worker", "status", "-state-dir", $StateDir, "-id", $id) "" 30
      $close = Invoke-Exact $Mewla @("worker", "close", "-state-dir", $StateDir, "-id", $id, "--force") "" 60
      if ($seen) { Add-Result $label "PASS" "replied; closed exit $($close.code)" }
      else {
        $tail = (($last -replace '\\n', "`n") -split "`n" | Where-Object { $_.Trim() } | Select-Object -Last 6) -join " / "
        Add-Result $label "FAIL" ("no reply in {0}s; status: {1}; screen: {2}" -f $ReplySeconds, ($status.out -replace "\s+", " ").Substring(0, [Math]::Min(300, ($status.out -replace "\s+", " ").Length)), $tail)
      }
    }
  }
  # An agent that is not installed fails at once, saying so.
  $absent = Invoke-Exact $Mewla @("worker", "spawn", "-state-dir", $StateDir, "-name", "Sweep absent", "-executor", "grok", "-cwd", $folders["plain"], "-prompt", "hi") "" 60
  if ((Worker-Id $absent.out) -eq "" -and $absent.out -match "(?i)not installed|not found|PATH") { Add-Result "worker:absent-agent" "PASS" $absent.out } else { Add-Result "worker:absent-agent" "FAIL" $absent.out }
}

# --- clean up --------------------------------------------------------------
if ($folders.Contains("readonly")) { & icacls $folders["readonly"] /remove:d $env:USERNAME | Out-Null }
foreach ($name in @("programfiles", "other-drive", "onedrive")) { if ($folders.Contains($name)) { Remove-Item -Recurse -Force -LiteralPath $folders[$name] -ErrorAction SilentlyContinue } }
Remove-Item -Recurse -Force -LiteralPath ("\\?\" + $Root) -ErrorAction SilentlyContinue
$results | ConvertTo-Json -Depth 3 | Set-Content -Encoding UTF8 $Out
$fails = @($results | Where-Object { $_.outcome -eq "FAIL" }).Count
Write-Output ("{0} scenarios, {1} failed; results in {2}" -f $results.Count, $fails, $Out)
if ($fails -gt 0) { exit 1 }
