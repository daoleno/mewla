# Install the Mewla daemon on Windows from official GitHub release assets:
#
#   irm https://mewla.ai/install.ps1 | iex
#
# Per user: no administrator, no WSL. It installs mewla.exe into
# %LOCALAPPDATA%\Programs\Mewla, adds that folder to your user PATH and starts
# Mewla now and whenever you log in (a scheduled task for your user only).
#
# Settings (environment variables, since `irm | iex` takes no arguments):
#   MEWLA_VERSION=vX.Y.Z[-beta.N]  install an exact release (default: newest stable)
#   MEWLA_INSTALL_DIR=<path>       install into this folder
#   MEWLA_NO_PATH_UPDATE=1         do not change your user PATH
#   MEWLA_NO_START=1               do not start Mewla or register it to start at login
#   MEWLA_DRY_RUN=1                print the plan without changing anything
#   MEWLA_INSTALL_FROM=<folder>    use mewla-windows-amd64.zip and SHA256SUMS from this
#                                  folder instead of downloading them (offline or staged)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

& {
  $Repository = 'daoleno/mewla'
  $GitHubWeb = "https://github.com/$Repository"
  $Archive = 'mewla-windows-amd64.zip'
  $Binary = 'mewla.exe'
  $ExpectedEntries = @('LICENSE', 'NOTICE', 'TRADEMARKS.md', $Binary)

  function Say([string]$Message) { Write-Host $Message }
  function Fail([string]$Message) { throw "mewla installer: error: $Message" }
  function Flag([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrEmpty($value) -or $value -eq '0') { return $false }
    if ($value -eq '1') { return $true }
    Fail "$Name must be 0 or 1"
  }

  # Windows PowerShell 5.1 turns a native program's stderr into errors under
  # 'Stop'; run native programs with 'Continue' and judge them by exit code.
  function Invoke-Native([string]$Path, [string[]]$Arguments, [switch]$Quiet) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
      if ($Quiet) { $null = & $Path @Arguments 2>&1 } else { & $Path @Arguments 2>&1 | ForEach-Object { Write-Host $_ } }
      return $LASTEXITCODE
    } finally { $ErrorActionPreference = $previous }
  }

  $DryRun = Flag 'MEWLA_DRY_RUN'
  $NoPathUpdate = Flag 'MEWLA_NO_PATH_UPDATE'
  $NoStart = Flag 'MEWLA_NO_START'
  $Version = "$env:MEWLA_VERSION".Trim()
  $InstallDir = "$env:MEWLA_INSTALL_DIR".Trim()
  $InstallFrom = "$env:MEWLA_INSTALL_FROM".Trim()

  if ($env:OS -ne 'Windows_NT') { Fail 'this installer is for Windows; on Linux and macOS use: curl -fsSL https://mewla.ai/install.sh | sh' }
  $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
  if ($arch -ne 'AMD64') { Fail "Windows on $arch is not supported yet; Mewla runs on 64-bit Intel and AMD Windows. See $GitHubWeb/blob/main/docs/install-daemon.md#supported-platforms" }
  if ([Environment]::OSVersion.Version.Build -lt 17763) { Fail 'Mewla needs Windows 10 version 1809 or later (it uses the Windows pseudo console)' }

  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = New-Object Security.Principal.WindowsPrincipal($identity)
  if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    # Everything still goes into this user's own folders and the login task
    # runs with the user's normal rights, so this is only a note.
    Write-Warning 'This PowerShell runs as Administrator. Mewla installs for your user only and does not need it; a normal window works the same.'
  }

  if ($Version -and $Version -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$') {
    Fail "invalid version '$Version'; expected vX.Y.Z or vX.Y.Z-beta.N with no leading zeroes"
  }
  if (-not $env:LOCALAPPDATA) { Fail 'LOCALAPPDATA is not set; run the installer in your own Windows session' }
  if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\Mewla' }
  if (-not [IO.Path]::IsPathRooted($InstallDir)) { Fail "install folder must be an absolute path: $InstallDir" }
  $InstallDir = [IO.Path]::GetFullPath($InstallDir)
  foreach ($protected in @($env:SystemRoot, $env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:ProgramData)) {
    if ($protected -and ($InstallDir.TrimEnd('\') -eq $protected.TrimEnd('\') -or $InstallDir.StartsWith($protected.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase))) {
      Fail "refusing to install into a system folder: $InstallDir"
    }
  }
  $Target = Join-Path $InstallDir $Binary

  if ($DryRun) {
    $what = if ($Version) { $Version } else { 'the newest stable release' }
    $source = if ($InstallFrom) { $InstallFrom } else { "$GitHubWeb/releases" }
    Say "Would install $what ($Archive) from $source into $Target"
    if (-not $NoPathUpdate) { Say "Would add $InstallDir to your user PATH" }
    if (-not $NoStart) { Say 'Would start Mewla now and at every login (mewla boot install)' }
    Say 'No files were changed.'
    return
  }

  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  $ProgressPreference = 'SilentlyContinue'

  # This install's mewla.exe processes (one renamed by an update is mewla.exe.old-N).
  function Get-MewlaProcesses {
    @(Get-Process -Name mewla -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path -like (Join-Path $InstallDir 'mewla.exe*') })
  }

  # The logon task runs Mewla only in this user's desktop session: installed with nobody at the desktop (over SSH),
  # `mewla boot install` starts it directly, and Mewla before 0.4.9 not the way the task does (under a console host
  # of its own), the one way `mewla update` and `mewla restart` recognise and restart. Such a Mewla, started by this
  # install and by no console host, is started again with the task's own command; one that ran before the install
  # is left alone. Over SSH a standard user has no CIM (Get-ScheduledTask, Win32_Process), and
  # what this session starts ends with it, so the process table and the start go through Win32 directly.
  function Restart-LikeTheTask([int[]]$Before) {
    $name = 'Mewla (' + ($env:USERNAME.Trim() -replace '[\x00-\x1f\\/:*?"<>|]', '_') + ')'
    $text = & schtasks.exe /query /tn $name /xml 2>$null | Out-String
    if ($LASTEXITCODE -ne 0 -or $text -notlike '*Managed by mewla boot install*') { return }
    $exec = ([xml]$text).Task.Actions.Exec
    if (-not ('MewlaInstall.Win32' -as [type])) { Add-Type -TypeDefinition $Win32Source }
    $table = [MewlaInstall.Win32]::Processes()
    $daemons = @(Get-MewlaProcesses | Where-Object { $Before -notcontains $_.Id } | Where-Object {
        $self = $null; $by = $null
        if ($table.TryGetValue([uint32]$_.Id, [ref]$self)) { [void]$table.TryGetValue($self.Parent, [ref]$by) }
        -not ($by -and @('mewla.exe', 'conhost.exe') -contains $by.Name.ToLowerInvariant())
      })
    if ($daemons.Count -ne 1) { return }
    Stop-Process -Id $daemons[0].Id -Force -ErrorAction SilentlyContinue
    Wait-Process -Id $daemons[0].Id -Timeout 15 -ErrorAction SilentlyContinue
    $dir = if ("$($exec.WorkingDirectory)") { "$($exec.WorkingDirectory)" } else { $env:USERPROFILE }
    [MewlaInstall.Win32]::StartDetached('"' + $exec.Command + '" ' + $exec.Arguments, $dir) | Out-Null
    $addr = if ("$($exec.Arguments)" -match '-addr\s+"?([^\s"]+)') { $Matches[1] } else { '127.0.0.1:9876' }
    $addr = $addr -replace '^0\.0\.0\.0:', '127.0.0.1:'
    for ($i = 0; $i -lt 60; $i++) {
      try { Invoke-RestMethod -Uri "http://$addr/health" -TimeoutSec 2 | Out-Null; return } catch { Start-Sleep -Milliseconds 500 }
    }
    Fail "Mewla did not answer on $addr after starting again; run '$Target'"
  }

  # The process table (id, parent, executable) and a start outside this session's job, as mewla.exe does it.
  $Win32Source = @'
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
namespace MewlaInstall {
  public class Proc { public uint Id; public uint Parent; public string Name; }
  public static class Win32 {
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    struct Entry { public uint Size, Usage, Id; public IntPtr Heap; public uint Module, Threads, Parent; public int Priority; public uint Flags;
      [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 260)] public string Exe; }
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    struct StartupInfo { public int Size; public string Reserved, Desktop, Title; public int X, Y, XSize, YSize, XChars, YChars, Fill, Flags;
      public short Show, Reserved2; public IntPtr Reserved3, In, Out, Err; }
    [StructLayout(LayoutKind.Sequential)]
    struct ProcessInfo { public IntPtr Process, Thread; public int Id, ThreadId; }
    [DllImport("kernel32.dll", SetLastError = true)] static extern IntPtr CreateToolhelp32Snapshot(uint flags, uint pid);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern bool Process32FirstW(IntPtr snapshot, ref Entry entry);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern bool Process32NextW(IntPtr snapshot, ref Entry entry);
    [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
    static extern bool CreateProcessW(string app, StringBuilder line, IntPtr pa, IntPtr ta, bool inherit, uint flags, IntPtr env, string dir, ref StartupInfo si, out ProcessInfo pi);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    public static Dictionary<uint, Proc> Processes() {
      var all = new Dictionary<uint, Proc>();
      IntPtr snapshot = CreateToolhelp32Snapshot(2, 0);
      if (snapshot == new IntPtr(-1)) return all;
      try {
        var entry = new Entry(); entry.Size = (uint)Marshal.SizeOf(typeof(Entry));
        for (bool ok = Process32FirstW(snapshot, ref entry); ok; ok = Process32NextW(snapshot, ref entry))
          all[entry.Id] = new Proc { Id = entry.Id, Parent = entry.Parent, Name = entry.Exe };
      } finally { CloseHandle(snapshot); }
      return all;
    }
    public static int StartDetached(string line, string dir) {
      const uint detached = 0x8, group = 0x200, breakaway = 0x1000000;
      var si = new StartupInfo(); si.Size = Marshal.SizeOf(typeof(StartupInfo));
      ProcessInfo pi;
      if (!CreateProcessW(null, new StringBuilder(line), IntPtr.Zero, IntPtr.Zero, false, detached | group | breakaway, IntPtr.Zero, dir, ref si, out pi)
          && !CreateProcessW(null, new StringBuilder(line), IntPtr.Zero, IntPtr.Zero, false, detached | group, IntPtr.Zero, dir, ref si, out pi))
        throw new Win32Exception(Marshal.GetLastWin32Error());
      CloseHandle(pi.Thread); CloseHandle(pi.Process);
      return pi.Id;
    }
  }
}
'@

  function Get-Official([string]$Url, [string]$OutFile) {
    if (-not $Url.StartsWith("$GitHubWeb/releases/download/")) { Fail "refusing non-official download URL: $Url" }
    try { Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $OutFile }
    catch { Fail "could not download ${Url}: $($_.Exception.Message); check the network and run the same command again" }
  }

  $work = Join-Path ([IO.Path]::GetTempPath()) ('mewla-install-' + [Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $work | Out-Null
  try {
    $zipPath = Join-Path $work $Archive
    $sumsPath = Join-Path $work 'SHA256SUMS'
    if ($InstallFrom) {
      foreach ($name in @($Archive, 'SHA256SUMS')) {
        $from = Join-Path $InstallFrom $name
        if (-not (Test-Path -LiteralPath $from -PathType Leaf)) { Fail "$from is missing" }
        Copy-Item -LiteralPath $from -Destination (Join-Path $work $name)
      }
      if (-not $Version) { $Version = 'from ' + $InstallFrom }
      Say "Installing Mewla from $InstallFrom..."
    } else {
      if (-not $Version) {
        # releases/latest redirects to the newest stable tag; unlike the API it
        # has no unauthenticated rate limit.
        try {
          $request = [Net.HttpWebRequest]::Create("$GitHubWeb/releases/latest")
          $request.Method = 'HEAD'
          $response = $request.GetResponse()
          $effective = $response.ResponseUri.AbsoluteUri
          $response.Close()
        } catch { Fail "could not reach $GitHubWeb/releases/latest to find the newest Mewla release: $($_.Exception.Message)" }
        if ($effective -notmatch '/releases/tag/(v[^/]+)$') { Fail "$GitHubWeb/releases/latest did not lead to a release tag (got $effective); set MEWLA_VERSION=vX.Y.Z to pin one" }
        $Version = $Matches[1]
      }
      $base = "$GitHubWeb/releases/download/$Version"
      Say "Downloading Mewla $Version for Windows x64 from $Repository..."
      Get-Official "$base/$Archive" $zipPath
      Get-Official "$base/SHA256SUMS" $sumsPath
    }

    # Exactly one well-formed lowercase SHA-256 line for the archive.
    $lines = @(Get-Content -LiteralPath $sumsPath | Where-Object { $_ -match "^\S+\s+\*?$([regex]::Escape($Archive))$" })
    if ($lines.Count -ne 1 -or $lines[0] -cnotmatch "^([0-9a-f]{64})\s+\*?$([regex]::Escape($Archive))$") {
      Fail "SHA256SUMS must contain exactly one well-formed lowercase SHA-256 entry for $Archive"
    }
    $expected = $Matches[1]
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { Fail "SHA-256 mismatch for $Archive" }

    # The archive holds exactly the four expected files, nothing else.
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
      $names = @($zip.Entries | ForEach-Object { $_.FullName })
      $unexpected = @($names | Where-Object { $ExpectedEntries -cnotcontains $_ })
      if ($names.Count -ne $ExpectedEntries.Count -or $unexpected.Count -gt 0 -or @($names | Sort-Object -Unique).Count -ne $names.Count) {
        Fail "$Archive has an unsafe or unexpected archive layout"
      }
      $entry = $zip.Entries | Where-Object { $_.FullName -ceq $Binary }
      New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
      $staged = Join-Path $InstallDir ('.mewla-install-' + [Guid]::NewGuid().ToString('N') + '.exe')
      [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $staged)
    } finally { $zip.Dispose() }

    try {
      if ((Invoke-Native $staged @('--help') -Quiet) -ne 0) { Fail 'the staged executable failed its safe --help check; the previous installation is unchanged' }
      if (Test-Path -LiteralPath $Target) {
        # A running mewla.exe cannot be overwritten, but it can be renamed:
        # move it aside, and the next Mewla start removes the old copy.
        try { Move-Item -LiteralPath $staged -Destination $Target -Force }
        catch {
          $aside = "$Target.old-" + [DateTime]::UtcNow.Ticks
          Move-Item -LiteralPath $Target -Destination $aside
          try { Move-Item -LiteralPath $staged -Destination $Target }
          catch { Move-Item -LiteralPath $aside -Destination $Target; throw }
        }
      } else {
        Move-Item -LiteralPath $staged -Destination $Target
      }
    } finally {
      if (Test-Path -LiteralPath $staged) { Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue }
    }
  } finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
  }
  Say "Installed Mewla $Version at $Target"

  if (-not $NoPathUpdate) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
      $raw = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
      $entries = @($raw -split ';' | Where-Object { $_ })
      if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })) {
        $key.SetValue('Path', (@($entries) + $InstallDir) -join ';', [Microsoft.Win32.RegistryValueKind]::ExpandString)
        # Setting a user variable through .NET tells open programs (Explorer,
        # new terminals) that the environment changed.
        [Environment]::SetEnvironmentVariable('MEWLA_INSTALLER_PATH_NOTICE', '1', 'User')
        [Environment]::SetEnvironmentVariable('MEWLA_INSTALLER_PATH_NOTICE', $null, 'User')
        Say "Added $InstallDir to your user PATH for new terminals."
      }
    } finally { $key.Close() }
    if (-not (@($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })) { $env:Path = "$env:Path;$InstallDir" }
  }

  if ($NoStart) {
    Say ''
    Say 'Next, start Mewla:'
    Say "  $Target"
    return
  }
  Say 'Starting Mewla now and whenever you log in...'
  $Before = @(Get-MewlaProcesses | ForEach-Object { $_.Id })
  if ((Invoke-Native $Target @('boot', 'install')) -ne 0) { Fail "Mewla is installed but did not start; run '$Target boot install' to see why" }
  Restart-LikeTheTask $Before
  Say ''
  Say 'Next, pair your phone or browser:'
  Say '  mewla pair'
}
