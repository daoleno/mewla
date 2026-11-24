//! The release's daemon: a LaunchAgent the app registers with SMAppService (FACTS.md §2), from the plist the
//! bundle carries (`Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist`, `BundleProgram`
//! `Contents/MacOS/Mewla --daemon serve`: this executable, which runs `Contents/Helpers/mewla serve` as its child so
//! macOS charges the daemon's permissions to the app; host.rs). It runs with the app closed or quit and after a restart, and System Settings lists it
//! under the app in Login Items. Only the real app (`com.daoleno.mewla`) does this; Mewla Dev never does.
//!
//! Before it registers, it takes over the install `mewla boot install` made (docs/install-daemon.md): that job is
//! booted out (one daemon, never two), its plist and its managed `~/Applications/Mewla.app` removed; the state in
//! `~/.mewla` (pairings, Brain, Work) is the same state the app's daemon opens. `~/.local/bin/mewla`, an installed copy
//! of the CLI, becomes a link to the app's.

use std::path::{Path, PathBuf};
use std::process::Command;

use objc2_foundation::NSString;
use objc2_service_management::{SMAppService, SMAppServiceStatus};

use crate::launchjob;
use serde_json::Value;
use std::{
    collections::BTreeMap,
    fs,
    time::{Duration, Instant},
};

pub const LABEL: &str = "com.daoleno.mewla.daemon";
const MANAGED_MARK: &str = "<!-- Managed by mewla boot install -->";

unsafe extern "C" {
    fn getuid() -> u32;
}

const INSTALLED_LABEL: &str = "com.daoleno.mewla.installed-daemon";
const INSTALLED_CLI: &str = "/Applications/Mewla.app/Contents/Helpers/mewla";
/// What both jobs run: the app's own executable, the daemon's parent (host.rs), so its permissions are the app's.
const INSTALLED_HOST: &str = "/Applications/Mewla.app/Contents/MacOS/Mewla";
/// The bundle-relative program of the app's job; before 0.4.26 it was the bare `Contents/Helpers/mewla`.
const APP_PROGRAM: &str = "Contents/MacOS/Mewla";

fn domain() -> String {
    format!("gui/{}", unsafe { getuid() })
}

pub fn adopt_and_register(home: &Path, cli: &Path) -> Vec<String> {
    let mut said = Vec::new();
    if let Err(error) = configure(home, cli, &mut said) {
        said.push(error);
    }
    said
}

fn configure(home: &Path, cli: &Path, said: &mut Vec<String>) -> Result<(), String> {
    let state = home.join(".mewla");
    fs::create_dir_all(&state).map_err(|e| e.to_string())?;
    let lock = fs::File::create(state.join("mac-app-takeover.lock")).map_err(|e| e.to_string())?;
    lock.try_lock().map_err(|_| "another instance is configuring the daemon".to_owned())?;
    let legacy = home.join("Library/LaunchAgents").join(format!("{LABEL}.plist"));
    let pending = state.join("mac-app-takeover.pending");
    let installed = state.join("mac-app-takeover.complete");
    let relative_job = format!("{}/{LABEL}", domain());
    let owner = Command::new(cli).arg("mac-app-owner").output().map_err(|e| e.to_string())?;
    let pid: u32 = std::str::from_utf8(&owner.stdout).ok()
        .filter(|_| owner.status.success())
        .and_then(|text| text.trim().parse().ok())
        .ok_or("could not identify the current daemon; kept its installation")?;
    if pid != 0 {
        // The job's process is the daemon itself (before 0.4.26, and boot install's) or its parent, the app's host.
        let parent = parent_pid(pid);
        let matches = |label: &str| {
            launchd_listing(&format!("{}/{label}", domain())).is_some_and(|s| {
                s.lines().any(|line| line.trim() == format!("pid = {pid}") || parent.is_some_and(|p| line.trim() == format!("pid = {p}")))
            })
        };
        if !matches(LABEL) && !matches(INSTALLED_LABEL) {
            return Err("externally managed daemon retained".into());
        }
    }
    if legacy.exists() {
        if !fs::read_to_string(&legacy).map_err(|e| e.to_string())?.contains(MANAGED_MARK) {
            return Err("externally managed daemon retained".into());
        }
        if cli != Path::new(INSTALLED_CLI) {
            return Err("move Mewla to /Applications before adopting the CLI installation".into());
        }
        if pending.exists() {
            restore(&legacy, &pending)?;
            return Ok(());
        }
        let before = health().ok_or("existing daemon did not answer; kept its installation")?;
        let legacy_cli = home.join(".local/bin/mewla");
        let owned = Command::new(&legacy_cli).args(["boot", "status"]).output().map_err(|e| e.to_string())?;
        if !owned.status.success() || !launchd_listing(&relative_job).is_some_and(|s| s.lines().any(|l| l == "\tstate = running")) {
            return Err("existing daemon ownership could not be confirmed; kept its installation".into());
        }
        // The previous plist, executable, CLI link and state remain in place for rollback.
        fs::write(&pending, &before.0).map_err(|e| e.to_string())?;
        let trial: Result<(), String> = (|| {
            launchctl(&["bootout", &relative_job])?;
            if !wait(60, || launchd_listing(&relative_job).is_none() && health().is_none()) {
                return Err("previous daemon did not stop cleanly".into());
            }
            let paired = devices(cli)?;
            register_installed(said)?;
            let version = Command::new(cli).args(["version", "--json"]).output().map_err(|e| e.to_string())?;
            let version: Value = serde_json::from_slice(&version.stdout).map_err(|e| e.to_string())?;
            let version = version["version"].as_str().ok_or("missing helper version")?;
            if !wait(60, || {
                installed_running() && health().is_some_and(|h| h.0 == before.0 && h.1 == before.1 && h.2 == version)
            }) {
                return Err("installed helper failed its identity and version health check".into());
            }
            if !paired.is_empty()
                && !wait(120, || {
                    devices(cli).is_ok_and(|now| {
                        paired.keys().all(|id| now.contains_key(id))
                            && paired.iter().any(|(id, seen)| now.get(id).is_some_and(|next| next != seen))
                    })
                })
            {
                return Err("no existing paired device reauthenticated during takeover".into());
            }
            Ok(())
        })();
        if let Err(e) = trial {
            restore(&legacy, &pending)?;
            return Err(format!("{e}; restored the previous daemon and login job"));
        }
        // Commit only after the new daemon answered with the old identity and a previous
        // device proved possession of its key. Until this point bootstrapping legacy is sufficient.
        fs::write(&installed, b"1").map_err(|e| e.to_string())?;
        fs::remove_file(&legacy).map_err(|e| e.to_string())?;
        let _ = fs::remove_file(legacy.with_extension("plist.meta.json"));
        let _ = fs::remove_file(&pending);
        let wrapper = home.join("Applications/Mewla.app");
        if fs::read_to_string(wrapper.join("Contents/Info.plist")).is_ok_and(|s| s.contains("MewlaManagedBundle")) {
            fs::remove_dir_all(wrapper).map_err(|e| e.to_string())?;
        }
        said.push("previous daemon identity and pairing verified; committed CLI takeover".into());
    } else if installed.exists() {
        register_installed(said)?;
    } else if cli == Path::new(INSTALLED_CLI)
        && launchd_listing(&relative_job).is_some_and(|s| s.contains("last exit code = 78") && s.contains("Contents/Helpers/mewla"))
    {
        // Recover an earlier app which already removed its managed CLI before launchd failed.
        unregister_label(LABEL)?;
        register_installed(said)?;
        if !wait(60, || installed_running() && health().is_some()) {
            return Err("installed daemon could not recover the previous app installation".into());
        }
        fs::write(installed, b"1").map_err(|e| e.to_string())?;
    } else {
        register(said);
    }
    link_cli(home, cli, said);
    refresh_daemon(cli, said);
    Ok(())
}

fn wait(seconds: u64, mut ready: impl FnMut() -> bool) -> bool {
    let until = Instant::now() + Duration::from_secs(seconds);
    loop {
        if ready() {
            return true;
        }
        if Instant::now() >= until {
            return false;
        }
        std::thread::sleep(Duration::from_millis(500));
    }
}

fn health() -> Option<(String, String, String)> {
    let out = Command::new("/usr/bin/curl")
        .args(["--noproxy", "*", "-fsS", "--max-time", "1", "http://127.0.0.1:9876/health"])
        .output()
        .ok()?;
    let v: Value = serde_json::from_slice(&out.stdout).ok()?;
    if !out.status.success() || v["status"] != "ok" {
        return None;
    }
    Some((
        v["daemon_id"].as_str()?.to_owned(),
        v["daemon_public_key"].as_str()?.to_owned(),
        v["version"].as_str()?.to_owned(),
    ))
}

fn devices(cli: &Path) -> Result<BTreeMap<String, String>, String> {
    let out = Command::new(cli)
        .args(["devices", "list", "--json"])
        .output()
        .map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err("could not verify the paired device list".into());
    }
    let v: Value = serde_json::from_slice(&out.stdout).map_err(|e| e.to_string())?;
    if v["devices"].is_null() {
        return Ok(BTreeMap::new());
    }
    v["devices"]
        .as_array()
        .ok_or("invalid device list")?
        .iter()
        .map(|d| {
            Ok((
                d["id"].as_str().ok_or("missing device id")?.to_owned(),
                d["last_seen_at"].as_str().ok_or("missing last seen")?.to_owned(),
            ))
        })
        .collect()
}

fn launchctl(args: &[&str]) -> Result<(), String> {
    let status = Command::new("/bin/launchctl").args(args).status().map_err(|e| e.to_string())?;
    if status.success() {
        Ok(())
    } else {
        Err(format!("launchctl {}: {status}", args[0]))
    }
}

/// The parent of a process, from `ps`.
fn parent_pid(pid: u32) -> Option<u32> {
    let out = Command::new("/bin/ps").args(["-o", "ppid=", "-p", &pid.to_string()]).output().ok()?;
    std::str::from_utf8(&out.stdout).ok()?.trim().parse().ok().filter(|p| *p > 1)
}

fn installed_running() -> bool {
    launchd_listing(&format!("{}/{INSTALLED_LABEL}", domain()))
        .is_some_and(|s| s.lines().any(|l| l == "\tstate = running") && s.lines().any(|l| l.trim() == format!("program = {INSTALLED_HOST}")))
}

/// Why the daemon's job cannot start, in the app's words (This Mac says it: ui/mac/local.ts), once this launch found
/// it; empty otherwise.
static LAUNCH_PROBLEM: std::sync::Mutex<String> = std::sync::Mutex::new(String::new());

/// launchd refuses the job however often it is registered again.
pub const REFUSED: &str = "macOS is not letting Mewla start";
/// The person turned Mewla off in System Settings › General › Login Items.
pub const LOGIN_ITEM_OFF: &str = "Mewla is off in Login Items";

/// Why the daemon's job cannot start, when this launch found that it cannot.
pub fn launch_problem() -> Option<String> {
    LAUNCH_PROBLEM.lock().ok().map(|p| p.clone()).filter(|p| !p.is_empty())
}

fn installed_target() -> String {
    format!("{}/{INSTALLED_LABEL}", domain())
}

fn installed_job() -> Option<launchjob::Job> {
    launchd_listing(&installed_target()).map(|listing| launchjob::read(&listing))
}

/// Registers the installed job and waits until launchd runs it. A job loaded from an older app (the bare helper as
/// its program), or one whose launch constraint still names the helper's identity, or one launchd cannot set up
/// (EX_CONFIG) is unregistered and registered again: launchd keeps the constraint recorded at the first
/// registration, Background Task Management reuses its item for the same plist, and once asked to repair that item
/// it drops it; nothing but a new registration heals the job (the owner's Mac after 0.4.25 → 0.4.26). The session
/// host and its Sessions run on throughout (AbandonProcessGroup). Each step is said, for the app's log.
fn register_installed(said: &mut Vec<String>) -> Result<(), String> {
    let before = installed_job();
    if before.as_ref().is_some_and(|job| job.running && job.program.as_deref() == Some(INSTALLED_HOST)) {
        return Ok(());
    }
    said.push(format!("installed job: {}", before.as_ref().map(launchjob::describe).unwrap_or_else(|| "not loaded".into())));
    unsafe {
        let service = SMAppService::agentServiceWithPlistName(&NSString::from_str(&format!("{INSTALLED_LABEL}.plist")));
        for round in 1..=3 {
            let job = installed_job();
            let why = job.as_ref().and_then(|job| launchjob::stale(job, INSTALLED_HOST));
            if let Some(why) = why {
                said.push(format!("installed job round {round}: registering again: {why} (status {})", service.status().0));
                if let Err(err) = service.unregisterAndReturnError() {
                    said.push(format!("unregister: {}", err.localizedDescription()));
                }
                if !wait(45, || installed_job().is_none()) {
                    // A job Background Task Management no longer knows may outlive its unregistration: launchd's own way.
                    let out = launchctl(&["bootout", &installed_target()]);
                    said.push(format!("still loaded after unregister; bootout: {out:?}"));
                    if !wait(15, || installed_job().is_none()) {
                        return Err(fail(REFUSED, said, "the older daemon job did not unload"));
                    }
                }
            }
            match service.registerAndReturnError() {
                Ok(()) => said.push(format!("registered the installed job (status {})", service.status().0)),
                Err(err) => said.push(format!("register: {} (code {}, status {})", err.localizedDescription(), err.code(), service.status().0)),
            }
            if service.status() == SMAppServiceStatus::RequiresApproval {
                return Err(fail(LOGIN_ITEM_OFF, said, "the installed job needs approval in Login Items"));
            }
            // Its first spawn tells: running, or refused at once (EX_CONFIG) when an old item was reused.
            wait(20, || installed_job().is_some_and(|job| job.running || job.ex_config));
            let now = installed_job();
            said.push(format!("installed job after round {round}: {}", now.as_ref().map(launchjob::describe).unwrap_or_else(|| "not loaded".into())));
            if installed_running() {
                set_problem("");
                return Ok(());
            }
            if now.is_some() && now.as_ref().and_then(|job| launchjob::stale(job, INSTALLED_HOST)).is_none() {
                // Loaded, not refused, not running yet: launchd's throttle; the daemon follows.
                set_problem("");
                return Ok(());
            }
        }
    }
    Err(fail(REFUSED, said, "launchd still refuses the installed job"))
}

fn set_problem(words: &str) {
    if let Ok(mut problem) = LAUNCH_PROBLEM.lock() {
        *problem = words.to_owned();
    }
}

/// Keeps `words` for This Mac to say and returns the log's line.
fn fail(words: &str, said: &mut Vec<String>, line: &str) -> String {
    set_problem(words);
    said.push(format!("This Mac says: {words}"));
    line.to_owned()
}

fn unregister_label(label: &str) -> Result<(), String> {
    unsafe {
        let service = SMAppService::agentServiceWithPlistName(&NSString::from_str(&format!("{label}.plist")));
        if service.status() != SMAppServiceStatus::NotRegistered {
            service.unregisterAndReturnError().map_err(|e| e.localizedDescription().to_string())?;
        }
    }
    Ok(())
}

fn restore(legacy: &Path, pending: &Path) -> Result<(), String> {
    let expected = fs::read_to_string(pending).map_err(|e| e.to_string())?;
    unregister_label(INSTALLED_LABEL)?;
    if !wait(45, || launchd_listing(&format!("{}/{INSTALLED_LABEL}", domain())).is_none()) {
        return Err("could not unload the trial job; previous installation retained".into());
    }
    let old = format!("{}/{LABEL}", domain());
    if launchd_listing(&old).is_none() {
        launchctl(&["bootstrap", &domain(), legacy.to_str().ok_or("invalid login job path")?])?;
    }
    if !wait(60, || health().is_some_and(|h| h.0 == expected)) {
        return Err("restored the original login job but its daemon did not answer".into());
    }
    fs::remove_file(pending).map_err(|e| e.to_string())?;
    Ok(())
}

/// An update replaces the app while its daemon runs on: the LaunchAgent is already registered and loaded, so nothing
/// above starts the new one. The CLI of this app restarts a daemon older than itself onto it (`mewla restart
/// -if-older`: in place, after Workers' model replies finish; Sessions and Workers keep running) and leaves a current
/// one alone.
fn refresh_daemon(cli: &Path, said: &mut Vec<String>) {
    // The daemon may still be starting (the first launch, a login): ask again for a minute until it answers.
    for attempt in 1..=12 {
        match Command::new(cli).args(["restart", "-if-older"]).output() {
            Ok(out) => {
                let text = |bytes: &[u8]| String::from_utf8_lossy(bytes).trim().to_owned();
                said.push(
                    format!("mewla restart -if-older (try {attempt}): {} {}", text(&out.stdout), text(&out.stderr))
                        .trim_end()
                        .to_owned(),
                );
                if out.status.success() {
                    return;
                }
            }
            Err(err) => {
                said.push(format!("mewla restart -if-older: {err}"));
                return;
            }
        }
        std::thread::sleep(std::time::Duration::from_secs(5));
    }
}

/// `~/.local/bin/mewla`: an installed copy of the CLI (install.sh's) becomes a link to the app's, so a terminal runs
/// the CLI of the daemon that runs. Anything else there is left alone.
fn link_cli(home: &Path, cli: &Path, said: &mut Vec<String>) {
    let link = home.join(".local/bin/mewla");
    match std::fs::symlink_metadata(&link) {
        Ok(meta) if meta.file_type().is_symlink() => {
            if std::fs::read_link(&link).ok().as_deref() == Some(cli) {
                return;
            }
            let target = std::fs::canonicalize(&link).ok();
            if !target.is_some_and(|t| t.to_string_lossy().contains(".app/Contents/")) {
                said.push(format!("{} links elsewhere: left as it is", link.display()));
                return;
            }
        }
        Ok(meta) if meta.is_file() => {
            let version = Command::new(&link)
                .arg("version")
                .output()
                .map(|o| String::from_utf8_lossy(&o.stdout).into_owned())
                .unwrap_or_default();
            if !version.starts_with("mewla ") {
                said.push(format!("{} is not Mewla's CLI: left as it is", link.display()));
                return;
            }
        }
        Ok(_) => return,
        Err(_) => {
            if !link.parent().is_some_and(Path::is_dir) {
                return;
            }
        }
    }
    let temp = link.with_extension("app-link");
    let _ = std::fs::remove_file(&temp);
    match std::os::unix::fs::symlink(cli, &temp).and_then(|()| std::fs::rename(&temp, &link)) {
        Ok(()) => said.push(format!("{} -> {}", link.display(), cli.display())),
        Err(err) => said.push(format!("{}: {err}", link.display())),
    }
}

/// What launchd runs for the job (its `program` or `program identifier` line), when it has it loaded.
fn launchd_listing(target: &str) -> Option<String> {
    let out = Command::new("/bin/launchctl")
        .args(["print", target])
        .output()
        .ok()
        .filter(|o| o.status.success())?;
    Some(String::from_utf8_lossy(&out.stdout).into_owned())
}

fn app_job_running(listing: &str) -> bool {
    listing.lines().any(|line| line == "\tstate = running")
        && listing
            .lines()
            .any(|line| line.trim().split(" (mode:").next() == Some(&*format!("program identifier = {APP_PROGRAM}")))
}

/// Registers the bundle's LaunchAgent and waits for launchd to load it. `status()` alone is not enough: right after
/// a takeover, Background Task Management still holds the old install's item under the same label, and the service
/// reads as enabled while launchd has no job; registering again (after unregistering) loads the app's.
fn register(said: &mut Vec<String>) {
    // SAFETY: getuid has no preconditions.
    let target = format!("gui/{}/{LABEL}", unsafe { getuid() });
    // The app's job runs the bundle's program (`program identifier = Contents/MacOS/Mewla`; an older app's loaded job,
    // `Contents/Helpers/mewla`, is not ours and is registered again below); boot install's ran
    // `~/Applications/Mewla.app/Contents/MacOS/mewla`.
    let ours = || launchd_listing(&target).is_some_and(|listing| app_job_running(&listing));
    // SAFETY: plain calls of SMAppService with a valid string.
    unsafe {
        let service = SMAppService::agentServiceWithPlistName(&NSString::from_str(&format!("{LABEL}.plist")));
        let status = service.status();
        if status == SMAppServiceStatus::Enabled && ours() {
            said.push("the daemon's LaunchAgent is registered and loaded".into());
            return;
        }
        if status == SMAppServiceStatus::Enabled {
            let out = service.unregisterAndReturnError();
            said.push(format!(
                "enabled but not loaded: unregistered ({:?})",
                out.err().map(|e| e.localizedDescription().to_string())
            ));
        }
        // Right after a takeover Background Task Management still holds the old install's item under this label and
        // refuses the registration (Operation not permitted) until it collects it, about a minute after its plist went
        // (proven on a clean macOS 26 runner: registered on the 8th try): try again until it lets go.
        let mut tries = 0;
        let mut repaired = false;
        'registration: loop {
            tries += 1;
            match service.registerAndReturnError() {
                Ok(()) => {
                    said.push(format!(
                        "registered the daemon's LaunchAgent (try {tries}; was {}, now {})",
                        status.0,
                        service.status().0
                    ));
                    for _ in 0..40 {
                        if let Some(listing) = launchd_listing(&target) {
                            if app_job_running(&listing) {
                                said.push(format!("launchd runs {target} from the app"));
                                return;
                            }
                            if !repaired
                                && listing
                                    .lines()
                                    .any(|line| matches!(line.trim(), "last exit code = 78: EX_CONFIG" | "last exit code = 78"))
                            {
                                match service.unregisterAndReturnError() {
                                    Ok(()) => {
                                        repaired = true;
                                        said.push("unregistered the unresolved bundle job after EX_CONFIG".into());
                                        continue 'registration;
                                    }
                                    Err(err) => {
                                        said.push(format!("unregister unresolved job: {}", err.localizedDescription()));
                                        return;
                                    }
                                }
                            }
                        }
                        std::thread::sleep(std::time::Duration::from_millis(500));
                    }
                    said.push(format!("launchd did not start {target}"));
                    return;
                }
                Err(err) if tries < 10 => {
                    said.push(format!(
                        "register try {tries}: {} (code {}, status {})",
                        err.localizedDescription(),
                        err.code(),
                        service.status().0
                    ));
                    std::thread::sleep(std::time::Duration::from_secs(10));
                }
                Err(err) => {
                    said.push(format!(
                        "the daemon's LaunchAgent: {} (code {}, status {})",
                        err.localizedDescription(),
                        err.code(),
                        service.status().0
                    ));
                    return;
                }
            }
        }
    }
}

/// Stop Mewla on this Mac…: the LaunchAgent is unregistered, so launchd stops the daemon and does not start it again
/// until the app registers it at its next launch.
pub fn unregister() -> Result<String, String> {
    let migrated = std::env::var_os("HOME").is_some_and(|home| PathBuf::from(home).join(".mewla/mac-app-takeover.complete").exists());
    unregister_label(if migrated { INSTALLED_LABEL } else { LABEL })?;
    Ok("unregistered the daemon's LaunchAgent".into())
}
