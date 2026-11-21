//! The release's daemon: a LaunchAgent the app registers with SMAppService (FACTS.md §2), from the plist the
//! bundle carries (`Contents/Library/LaunchAgents/ai.mewla.daemon.plist`, `BundleProgram`
//! `Contents/Helpers/mewla`). It runs with the app closed or quit and after a restart, and System Settings lists it
//! under the app in Login Items. Only the real app (`ai.mewla.app`) does this; Mewla Dev never does.
//!
//! Before it registers, it takes over the install `mewla boot install` made (docs/install-daemon.md), under this
//! label or under the one of before 11-21 (`com.daoleno.mewla.daemon`): that job is
//! booted out (one daemon, never two), its plist and its managed `~/Applications/Mewla.app` removed; the state in
//! `~/.mewla` (pairings, Brain, Work) is the same state the app's daemon opens. `~/.local/bin/mewla`, an installed copy
//! of the CLI, becomes a link to the app's.

use std::path::{Path, PathBuf};
use std::process::Command;

use objc2_foundation::NSString;
use objc2_service_management::{SMAppService, SMAppServiceStatus};

/// scripts/lib/release_identity.py's DAEMON_LABEL.
pub const LABEL: &str = "ai.mewla.daemon";
/// What `mewla boot install` called its job before 11-21 (LEGACY_DAEMON_LABEL).
const LEGACY_LABEL: &str = "com.daoleno.mewla.daemon";
const MANAGED_MARK: &str = "<!-- Managed by mewla boot install -->";

unsafe extern "C" {
    fn getuid() -> u32;
}

/// What happened, for the log.
pub fn adopt_and_register(home: &Path, cli: &Path) -> Vec<String> {
    let mut said = Vec::new();
    take_over_boot_install(home, &mut said);
    link_cli(home, cli, &mut said);
    register(&mut said);
    refresh_daemon(cli, &mut said);
    said
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
                said.push(format!("mewla restart -if-older (try {attempt}): {} {}", text(&out.stdout), text(&out.stderr)).trim_end().to_owned());
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

fn take_over_boot_install(home: &Path, said: &mut Vec<String>) {
    let mut taken = false;
    for label in [LABEL, LEGACY_LABEL] {
        taken |= take_over_job(home, label, said);
    }
    if !taken {
        return;
    }
    let old_app = home.join("Applications/Mewla.app");
    let managed = std::fs::read_to_string(old_app.join("Contents/Info.plist")).map(|info| info.contains("MewlaManagedBundle")).unwrap_or(false);
    if managed && std::fs::remove_dir_all(&old_app).is_ok() {
        said.push(format!("removed {}", old_app.display()));
    }
}

/// One label's boot install: booted out, its plist and metadata removed. Whether there was one.
fn take_over_job(home: &Path, label: &str, said: &mut Vec<String>) -> bool {
    let plist = home.join("Library/LaunchAgents").join(format!("{label}.plist"));
    let Ok(text) = std::fs::read_to_string(&plist) else { return false };
    if !text.contains(MANAGED_MARK) {
        said.push(format!("{} is not mewla boot install's: left as it is", plist.display()));
        return false;
    }
    // SAFETY: getuid has no preconditions.
    let target = format!("gui/{}/{label}", unsafe { getuid() });
    let out = Command::new("/bin/launchctl").args(["bootout", &target]).output();
    said.push(format!("launchctl bootout {target}: {:?}", out.map(|o| o.status.code())));
    // The job is gone once launchd no longer lists it (its daemon gets SIGTERM and up to a minute to stop).
    for _ in 0..120 {
        let listed = Command::new("/bin/launchctl").args(["print", &target]).output().map(|o| o.status.success()).unwrap_or(false);
        if !listed {
            break;
        }
        std::thread::sleep(std::time::Duration::from_millis(500));
    }
    for path in [plist.clone(), PathBuf::from(format!("{}.meta.json", plist.display()))] {
        if std::fs::remove_file(&path).is_ok() {
            said.push(format!("removed {}", path.display()));
        }
    }
    true
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
            let version = Command::new(&link).arg("version").output().map(|o| String::from_utf8_lossy(&o.stdout).into_owned()).unwrap_or_default();
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
fn launchd_program(target: &str) -> Option<String> {
    let out = Command::new("/bin/launchctl").args(["print", target]).output().ok().filter(|o| o.status.success())?;
    let text = String::from_utf8_lossy(&out.stdout).into_owned();
    text.lines().map(str::trim).find(|line| line.starts_with("program = ") || line.starts_with("program identifier = ")).map(str::to_owned)
}

/// Registers the bundle's LaunchAgent and waits for launchd to load it. `status()` alone is not enough: right after
/// a takeover, Background Task Management still holds the old install's item under the same label, and the service
/// reads as enabled while launchd has no job; registering again (after unregistering) loads the app's.
fn register(said: &mut Vec<String>) {
    // SAFETY: getuid has no preconditions.
    let target = format!("gui/{}/{LABEL}", unsafe { getuid() });
    // The app's job runs the bundle's program (`program identifier = Contents/Helpers/mewla`); boot install's ran
    // `~/Applications/Mewla.app/Contents/MacOS/mewla`.
    let ours = || launchd_program(&target).is_some_and(|program| program.contains("Contents/Helpers/mewla"));
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
            said.push(format!("enabled but not loaded: unregistered ({:?})", out.err().map(|e| e.localizedDescription().to_string())));
        }
        // Right after a takeover Background Task Management still holds the old install's item under this label and
        // refuses the registration (Operation not permitted) until it collects it, about a minute after its plist went
        // (proven on a clean macOS 26 runner: registered on the 8th try): try again until it lets go.
        let mut tries = 0;
        loop {
            tries += 1;
            match service.registerAndReturnError() {
                Ok(()) => {
                    said.push(format!("registered the daemon's LaunchAgent (try {tries}; was {}, now {})", status.0, service.status().0));
                    break;
                }
                Err(err) if tries < 10 => {
                    said.push(format!("register try {tries}: {} (code {}, status {})", err.localizedDescription(), err.code(), service.status().0));
                    std::thread::sleep(std::time::Duration::from_secs(10));
                }
                Err(err) => {
                    said.push(format!("the daemon's LaunchAgent: {} (code {}, status {})", err.localizedDescription(), err.code(), service.status().0));
                    return;
                }
            }
        }
    }
    for _ in 0..40 {
        if ours() {
            said.push(format!("launchd runs {target} from the app"));
            return;
        }
        std::thread::sleep(std::time::Duration::from_millis(500));
    }
    said.push(format!("launchd has not loaded {target} after 20 s: {:?}", launchd_program(&target).map(|p| p.chars().take(200).collect::<String>())));
}

/// Stop Mewla on this Mac…: the LaunchAgent is unregistered, so launchd stops the daemon and does not start it again
/// until the app registers it at its next launch.
pub fn unregister() -> Result<String, String> {
    // SAFETY: plain calls of SMAppService with a valid string.
    unsafe {
        let service = SMAppService::agentServiceWithPlistName(&NSString::from_str(&format!("{LABEL}.plist")));
        service.unregisterAndReturnError().map_err(|err| err.localizedDescription().to_string())?;
    }
    Ok("unregistered the daemon's LaunchAgent".into())
}
