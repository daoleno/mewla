//! This computer's daemon: the `mewla` the app carries (`Contents/Helpers/mewla`), started when no daemon holds its
//! state, and asked for pairings with `mewla pair -json`.

use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use serde_json::Value;

pub struct Config {
    /// `Contents/Helpers/mewla`.
    pub cli: PathBuf,
    /// The daemon's state root (`MEWLA_STATE_ROOT`); `None`: its default, `~/.mewla`.
    pub state_root: Option<PathBuf>,
    /// Where it listens (`-addr`).
    pub addr: String,
    /// The address the phone QR names; `None`: the daemon's best (Mewla Relay, HTTPS, Tailscale, LAN).
    pub phone_address: Option<String>,
}

impl Config {
    /// The daemon's state root: [`Config::state_root`], or `~/.mewla`.
    pub fn root(&self) -> PathBuf {
        self.state_root.clone().unwrap_or_else(|| std::env::var_os("HOME").map(PathBuf::from).unwrap_or_default().join(".mewla"))
    }

    pub fn from_env(contents: &Path) -> Config {
        let var = |name: &str| std::env::var(name).ok().filter(|v| !v.trim().is_empty());
        Config {
            cli: contents.join("Helpers/mewla"),
            state_root: var("MEWLA_APP_STATE_ROOT").map(PathBuf::from),
            addr: var("MEWLA_APP_ADDR").unwrap_or_else(|| "127.0.0.1:9876".into()),
            phone_address: var("MEWLA_APP_PHONE_ADDRESS"),
        }
    }

    /// A command of the bundle's CLI with this daemon's environment: the variables of another Mewla (a Worker's
    /// `MEWLA_*`, a LaunchAgent's `XPC_SERVICE_NAME`) left out, so it never reaches that one.
    fn command(&self) -> Command {
        let mut command = Command::new(&self.cli);
        command.env_clear();
        for (key, value) in std::env::vars_os() {
            let name = key.to_string_lossy();
            if name.starts_with("MEWLA_") || name == "XPC_SERVICE_NAME" {
                continue;
            }
            command.env(&key, value);
        }
        if let Some(root) = &self.state_root {
            command.env("MEWLA_STATE_ROOT", root);
            // Its own daemon updates with the app, never by itself.
            command.env("MEWLA_NO_AUTOUPDATE", "1");
            // And it ends with the app however the app ends (a SIGTERM or a crash skips `stop`): it watches this pid.
            command.env("MEWLA_EXIT_WITH_PARENT", std::process::id().to_string());
            // It runs beside the Mewla installed here, which owns the computer-wide setup: Chrome's native host, the
            // cloud CLIs and the computer's cloud profiles are left to that one.
            command.env("MEWLA_BESIDE_INSTALLED", "1");
            // Mewla Dev on someone's Mac may hold Brain to one agent (`MEWLA_APP_BRAIN_HOST_EXECUTOR`, e.g. codex,
            // which is told its folder trust per launch and so writes nothing into the person's agent config).
            if let Ok(executor) = std::env::var("MEWLA_APP_BRAIN_HOST_EXECUTOR") {
                command.env("MEWLA_BRAIN_HOST_EXECUTOR", executor);
            }
        }
        command
    }

    fn log_path(&self) -> Option<PathBuf> {
        let root = self.state_root.clone().or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".mewla")))?;
        let logs = root.join("logs");
        std::fs::create_dir_all(&logs).ok()?;
        Some(logs.join("app-daemon.log"))
    }
}

/// Starts `mewla serve` (it exits at once when another daemon already holds the state: that one serves the app).
pub fn start(config: &Config) -> Option<Child> {
    if !config.cli.exists() {
        log::error!("no daemon in the app: {}", config.cli.display());
        return None;
    }
    let log = config.log_path().and_then(|path| std::fs::OpenOptions::new().create(true).append(true).open(path).ok());
    let mut command = config.command();
    command.args(["serve", "-addr", &config.addr]);
    if config.addr.starts_with("127.0.0.1:") && config.state_root.is_some() {
        command.arg("-loopback-only");
    }
    command.stdin(Stdio::null());
    match log {
        Some(file) => {
            let err = file.try_clone().ok();
            command.stdout(file);
            if let Some(err) = err {
                command.stderr(err);
            }
        }
        None => {
            command.stdout(Stdio::null()).stderr(Stdio::null());
        }
    }
    match command.spawn() {
        Ok(child) => {
            log::info!("daemon started (pid {}) on {}", child.id(), config.addr);
            Some(child)
        }
        Err(err) => {
            log::error!("daemon not started: {err}");
            None
        }
    }
}

/// Stops the daemon this app started (SIGTERM, then the end of a minute's grace is the system's).
pub fn stop(child: Option<Child>) {
    let Some(mut child) = child else { return };
    if let Ok(Some(_)) = child.try_wait() {
        return;
    }
    // SAFETY: a plain signal to our own child.
    unsafe {
        libc_kill(child.id() as i32, 15);
    }
    let deadline = Instant::now() + Duration::from_secs(20);
    while Instant::now() < deadline {
        if let Ok(Some(_)) = child.try_wait() {
            return;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let _ = child.kill();
}

unsafe extern "C" {
    #[link_name = "kill"]
    fn libc_kill(pid: i32, sig: i32) -> i32;
}

/// `mewla pair -json` of this daemon, retried while it starts (up to 100 s: a takeover waits for the old daemon).
pub fn pairing(config: &Config) -> Result<Value, String> {
    let deadline = Instant::now() + Duration::from_secs(100);
    let mut last = String::new();
    while Instant::now() < deadline {
        let mut command = config.command();
        command.args(["pair", "-json"]);
        if let Some(address) = &config.phone_address {
            command.arg(address);
        }
        match command.stdin(Stdio::null()).output() {
            Ok(output) if output.status.success() => {
                let pairing: Value = serde_json::from_slice(&output.stdout).map_err(|err| format!("mewla pair -json: {err}"))?;
                // A link of this computer's own address, once the daemon has recorded it.
                if pairing.get("local_link").and_then(Value::as_str).is_some_and(|s| !s.is_empty()) {
                    return Ok(pairing);
                }
                last = "This computer's Mewla has no address yet.".into();
            }
            Ok(output) => last = String::from_utf8_lossy(&output.stderr).trim().lines().last().unwrap_or("mewla pair failed").to_owned(),
            Err(err) => return Err(format!("{}: {err}", config.cli.display())),
        }
        std::thread::sleep(Duration::from_millis(500));
    }
    Err(last)
}

/// The name this Mac goes by (System Settings › General › Sharing).
pub fn computer_name() -> String {
    Command::new("/usr/sbin/scutil")
        .args(["--get", "ComputerName"])
        .output()
        .ok()
        .and_then(|output| String::from_utf8(output.stdout).ok())
        .map(|name| name.trim().to_owned())
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| "Mac".into())
}
