//! The release's daemon: a LaunchAgent the app registers with SMAppService (FACTS.md §2), from the plist the
//! bundle carries (`Contents/Library/LaunchAgents/com.daoleno.mewla.daemon.plist`, `BundleProgram`
//! `Contents/Helpers/mewla`). It runs with the app closed or quit and after a restart, and System Settings lists it
//! under the app in Login Items. Only the real app (`com.daoleno.mewla`) does this; Mewla Dev never does.
//!
//! Before it registers, it takes over the install `mewla boot install` made (docs/install-daemon.md): that job is
//! booted out (one daemon, never two), its plist and its managed `~/Applications/Mewla.app` removed; the state in
//! `~/.mewla` (pairings, Brain, Work) is the same state the app's daemon opens. `~/.local/bin/mewla`, an installed copy
//! of the CLI, becomes a link to the app's.

use std::path::{Path, PathBuf};
use std::process::Command;

use core_foundation::{base::TCFType, bundle::CFBundle, url::CFURLRef};
use objc2_foundation::NSString;
use objc2_service_management::{SMAppService, SMAppServiceStatus};

pub const LABEL: &str = "com.daoleno.mewla.daemon";
const MANAGED_MARK: &str = "<!-- Managed by mewla boot install -->";

unsafe extern "C" {
    fn getuid() -> u32;
}

#[link(name = "CoreServices", kind = "framework")]
unsafe extern "C" {
    fn LSRegisterURL(url: CFURLRef, update: u8) -> i32;
}

/// What happened, for the log.
pub fn adopt_and_register(home: &Path, cli: &Path) -> Vec<String> {
    let mut said = Vec::new();
    if !take_over_boot_install(home, &mut said) {
        return said;
    }
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

fn take_over_boot_install(home: &Path, said: &mut Vec<String>) -> bool {
    let plist = home.join("Library/LaunchAgents").join(format!("{LABEL}.plist"));
    let text = match std::fs::read_to_string(&plist) {
        Ok(text) => text,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return true,
        Err(err) => {
            said.push(format!("inspect {}: {err}", plist.display()));
            return false;
        }
    };
    if !text.contains(MANAGED_MARK) {
        said.push(format!("{} is not mewla boot install's: left as it is", plist.display()));
        return false;
    }
    // SAFETY: getuid has no preconditions.
    let target = format!("gui/{}/{LABEL}", unsafe { getuid() });
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
    let old_app = home.join("Applications/Mewla.app");
    let managed = std::fs::read_to_string(old_app.join("Contents/Info.plist")).map(|info| info.contains("MewlaManagedBundle")).unwrap_or(false);
    if managed {
        // Removing a bundle from disk leaves its Launch Services record behind. Both bundles
        // claim com.daoleno.mewla, so unregister the old URL while it still resolves.
        let lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister";
        let status = Command::new(lsregister).arg("-u").arg(&old_app).status();
        said.push(format!("unregister boot install's bundle: {status:?}"));
        if let Err(err) = std::fs::remove_dir_all(&old_app) {
            said.push(format!("remove {}: {err}", old_app.display()));
            return false;
        }
        said.push(format!("removed {}", old_app.display()));
        // Both bundles have the same identifier. Refresh the surviving app's location before
        // SMAppService asks launchd to resolve its bundle-relative executable.
        if let Some(url) = CFBundle::main_bundle().bundle_url() {
            // SAFETY: url owns a valid CFURL for the duration of the call.
            let status = unsafe { LSRegisterURL(url.as_concrete_TypeRef(), 1) };
            said.push(format!("Launch Services registered this app after takeover: {status}"));
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
fn launchd_listing(target: &str) -> Option<String> {
    let out = Command::new("/bin/launchctl").args(["print", target]).output().ok().filter(|o| o.status.success())?;
    Some(String::from_utf8_lossy(&out.stdout).into_owned())
}

fn app_job_running(listing: &str) -> bool {
    listing.lines().any(|line| line == "\tstate = running")
        && listing.lines().any(|line| line == "\tprogram identifier = Contents/Helpers/mewla")
}

/// Registers the bundle's LaunchAgent and waits for launchd to load it. `status()` alone is not enough: right after
/// a takeover, Background Task Management still holds the old install's item under the same label, and the service
/// reads as enabled while launchd has no job; registering again (after unregistering) loads the app's.
fn register(said: &mut Vec<String>) {
    // SAFETY: getuid has no preconditions.
    let target = format!("gui/{}/{LABEL}", unsafe { getuid() });
    // The app's job runs the bundle's program (`program identifier = Contents/Helpers/mewla`); boot install's ran
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
            said.push(format!("enabled but not loaded: unregistered ({:?})", out.err().map(|e| e.localizedDescription().to_string())));
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
                    said.push(format!("registered the daemon's LaunchAgent (try {tries}; was {}, now {})", status.0, service.status().0));
                    for _ in 0..40 {
                        if let Some(listing) = launchd_listing(&target) {
                            if app_job_running(&listing) {
                                said.push(format!("launchd runs {target} from the app"));
                                return;
                            }
                            if !repaired && listing.lines().any(|line| matches!(line.trim(), "last exit code = 78: EX_CONFIG" | "last exit code = 78")) {
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
