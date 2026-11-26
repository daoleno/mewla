//! The release's daemon under launchd, run through this app's own executable: `Mewla --daemon serve` spawns the
//! bundle's `Contents/Helpers/mewla serve` and stays its parent.
//!
//! Why: macOS charges a permission to the process responsible for the one asking. Run as its own launchd job, the bare
//! helper split Mewla in two (proven on a macOS 26 runner, client/macos/scripts/tcc-probe): Screen Recording went to
//! Mewla.app, Accessibility to the helper's path, listed as a lowercase "mewla" with no icon. Adding Mewla.app back
//! with + then granted only the first. With the app's executable as the job, both belong to Mewla.app (its bundle id
//! and signature), the one item System Settings shows, the panel drags (permissions.rs) and a person adds by hand.
//!
//! It passes launchd's signals to the daemon and ends with it (with its exit status), so launchd's KeepAlive starts
//! both again. Should it be killed outright, the daemon stops by itself (`MEWLA_STOP_WITH_HOST`), leaving the
//! session host and its Sessions running as on any daemon restart.

use std::ffi::OsString;
use std::path::PathBuf;
use std::process::{Command, ExitCode, Stdio};
use std::sync::atomic::{AtomicI32, Ordering};

/// The first argument that makes the executable the daemon's host rather than the app.
pub const FLAG: &str = "--daemon";

static CHILD: AtomicI32 = AtomicI32::new(0);

unsafe extern "C" {
    fn signal(signum: i32, handler: extern "C" fn(i32)) -> usize;
    fn kill(pid: i32, sig: i32) -> i32;
    fn dup2(from: i32, to: i32) -> i32;
}

const SIGHUP: i32 = 1;
const SIGINT: i32 = 2;
const SIGQUIT: i32 = 3;
const SIGTERM: i32 = 15;
const SIGUSR1: i32 = 30;
const SIGUSR2: i32 = 31;

extern "C" fn forward(sig: i32) {
    let child = CHILD.load(Ordering::SeqCst);
    if child > 0 {
        // SAFETY: kill is async-signal-safe.
        unsafe { kill(child, sig) };
    }
}

/// The daemon next to this executable: `Contents/MacOS/<app>` → `Contents/Helpers/mewla`.
pub fn helper() -> Option<PathBuf> {
    let exe = std::env::current_exe().ok()?.canonicalize().ok()?;
    Some(exe.parent()?.parent()?.join("Helpers/mewla"))
}

/// launchd's job names no file for output, so whatever the host writes (its own errors, a panic) went to /dev/null.
/// It goes to `~/.mewla/logs/host-stderr.log` instead (a file past 1 MB moves to `.1` first). Says whether it did; the
/// daemon is then given /dev/null and keeps its own (`daemon-stderr.log`, stderr_capture.go), so neither file holds
/// the other's lines.
fn capture_stderr() -> bool {
    use std::os::fd::{AsRawFd, FromRawFd};
    use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
    // SAFETY: fd 2 stays open; ManuallyDrop never closes it.
    let current = std::mem::ManuallyDrop::new(unsafe { std::fs::File::from_raw_fd(2) });
    let (Ok(now), Ok(null)) = (current.metadata(), std::fs::metadata("/dev/null")) else { return false };
    if now.dev() != null.dev() || now.ino() != null.ino() {
        return false;
    }
    let Some(home) = std::env::var_os("HOME") else { return false };
    let logs = PathBuf::from(home).join(".mewla/logs");
    if std::fs::create_dir_all(&logs).is_err() {
        return false;
    }
    let path = logs.join("host-stderr.log");
    if std::fs::metadata(&path).is_ok_and(|m| m.len() > 1 << 20) {
        let _ = std::fs::rename(&path, logs.join("host-stderr.log.1"));
    }
    let Ok(file) = std::fs::OpenOptions::new().create(true).append(true).mode(0o600).open(&path) else { return false };
    // SAFETY: both descriptors are open; dup2 leaves fd 2 a copy of the file.
    unsafe { dup2(file.as_raw_fd(), 2) == 2 }
}

/// Runs the daemon with `args` (after `--daemon`) and ends with it.
pub fn run(args: Vec<OsString>) -> ExitCode {
    let captured = capture_stderr();
    let Some(helper) = helper() else {
        eprintln!("mewla host: cannot find the app's daemon");
        return ExitCode::from(78);
    };
    for sig in [SIGHUP, SIGINT, SIGQUIT, SIGTERM, SIGUSR1, SIGUSR2] {
        // SAFETY: the handler only reads an atomic and calls kill.
        unsafe { signal(sig, forward) };
    }
    let mut command = Command::new(&helper);
    command.args(&args).env("MEWLA_STOP_WITH_HOST", std::process::id().to_string());
    if captured {
        command.stderr(Stdio::null());
    }
    let mut child = match command.spawn() {
        Ok(child) => child,
        Err(err) => {
            eprintln!("mewla host: {}: {err}", helper.display());
            return ExitCode::from(78);
        }
    };
    CHILD.store(child.id() as i32, Ordering::SeqCst);
    let status = loop {
        match child.wait() {
            Ok(status) => break status,
            Err(err) if err.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(err) => {
                eprintln!("mewla host: wait: {err}");
                return ExitCode::FAILURE;
            }
        }
    };
    CHILD.store(0, Ordering::SeqCst);
    use std::os::unix::process::ExitStatusExt;
    match (status.code(), status.signal()) {
        (Some(code), _) => ExitCode::from(code.clamp(0, 255) as u8),
        (None, Some(sig)) => ExitCode::from((128 + sig).clamp(0, 255) as u8),
        _ => ExitCode::FAILURE,
    }
}
