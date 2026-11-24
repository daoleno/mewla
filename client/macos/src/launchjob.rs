//! What launchd holds for one of the daemon's jobs, read from `launchctl print gui/<uid>/<label>` (agent.rs). Plain
//! text in, so `cargo test` reads it anywhere.

/// The app's own signing identifier: what launchd's launch constraint for the daemon's job must name, since the job
/// runs the app's executable (0.4.26 on; before, the bare helper, signed `com.daoleno.mewla.daemon`).
pub const APP_SIGNER: &str = "com.daoleno.mewla";

#[derive(Debug, Default, PartialEq, Eq)]
pub struct Job {
    pub running: bool,
    /// `program = …`, an absolute path (the installed job's).
    pub program: Option<String>,
    /// `program identifier = …` without its mode (the bundle job's, relative to the app).
    pub program_identifier: Option<String>,
    /// The launch constraint's `signing-identifier` (`LWCR` › `reqs`), the identity launchd lets run the program.
    pub signer: Option<String>,
    /// `last exit code = 78`: launchd could not set the job up (EX_CONFIG), its program never ran.
    pub ex_config: bool,
}

pub fn read(listing: &str) -> Job {
    let mut job = Job::default();
    for line in listing.lines() {
        let trimmed = line.trim();
        if line == "\tstate = running" {
            job.running = true;
        } else if line.starts_with('\t') && !line.starts_with("\t\t") {
            if let Some(program) = trimmed.strip_prefix("program = ") {
                job.program = Some(program.to_owned());
            } else if let Some(id) = trimmed.strip_prefix("program identifier = ") {
                job.program_identifier = Some(id.split(" (mode:").next().unwrap_or(id).to_owned());
            } else if trimmed == "last exit code = 78" || trimmed.starts_with("last exit code = 78:") {
                job.ex_config = true;
            }
        }
        if trimmed.starts_with("\"signing-identifier\" =>") {
            job.signer = trimmed.rsplit('"').nth(1).map(str::to_owned);
        }
    }
    job
}

/// Why a job that is not running must be registered again, or None. launchd keeps the launch constraint recorded
/// when the job was first registered: after the program became the app's executable, an old constraint (the
/// helper's identity) makes every spawn fail with EX_CONFIG, and Background Task Management, asked to repair it,
/// drops its item, so the job is never repaired by itself (the owner's Mac, 11-23: 6,745 refused spawns in 9 h).
pub fn stale(job: &Job, program: &str) -> Option<String> {
    if job.running {
        return None;
    }
    if let Some(signer) = job.signer.as_deref().filter(|signer| *signer != APP_SIGNER) {
        return Some(format!("its launch constraint names {signer}"));
    }
    if job.program.as_deref().is_some_and(|p| p != program) {
        return Some(format!("it runs {}", job.program.as_deref().unwrap_or_default()));
    }
    job.ex_config.then(|| "launchd could not set it up (EX_CONFIG)".to_owned())
}

/// One line for the app's log: what launchd holds.
pub fn describe(job: &Job) -> String {
    format!(
        "{}, program {}, signer {}{}",
        if job.running { "running" } else { "not running" },
        job.program.as_deref().or(job.program_identifier.as_deref()).unwrap_or("?"),
        job.signer.as_deref().unwrap_or("?"),
        if job.ex_config { ", last exit 78 (EX_CONFIG)" } else { "" },
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    const HOST: &str = "/Applications/Mewla.app/Contents/MacOS/Mewla";

    // The owner's Mac, 11-24 08:44 (`launchctl print gui/501/com.daoleno.mewla.installed-daemon`, trimmed).
    const OWNER: &str = "gui/501/com.daoleno.mewla.installed-daemon = {\n\tactive count = 0\n\tpath = (submitted by smd.48601)\n\ttype = Submitted\n\tmanaged_by = com.apple.xpc.ServiceManagement\n\tstate = spawn scheduled\n\n\tprogram = /Applications/Mewla.app/Contents/MacOS/Mewla\n\tBTM uuid = 7D6BCB69-43FC-4133-B665-8A0625408403\n\targuments = {\n\t\t/Applications/Mewla.app/Contents/MacOS/Mewla\n\t\t--daemon\n\t\tserve\n\t}\n\n\tLWCR = {\n\t\t\"reqs\" => {\n\t\t\t\"signing-identifier\" => \"com.daoleno.mewla.daemon\"\n\t\t\t\"validation-category\" => 6\n\t\t\t\"team-identifier\" => \"HD84J3DJ2B\"\n\t\t}\n\t\t\"vers\" => 1\n\t}\n\n\tdomain = gui/501 [100021]\n\tminimum runtime = 5\n\truns = 6731\n\tlast exit code = 78: EX_CONFIG\n\n\tresource coalition = {\n\t\tstate = active\n\t}\n\tjob state = spawn failed\n}\n";

    #[test]
    fn the_owners_job_is_stale_by_its_old_constraint() {
        let job = read(OWNER);
        assert!(!job.running);
        assert_eq!(job.program.as_deref(), Some(HOST));
        assert_eq!(job.signer.as_deref(), Some("com.daoleno.mewla.daemon"));
        assert!(job.ex_config);
        assert_eq!(stale(&job, HOST).as_deref(), Some("its launch constraint names com.daoleno.mewla.daemon"));
    }

    #[test]
    fn a_running_job_is_left_alone_whatever_it_says() {
        let listing = OWNER.replace("\tstate = spawn scheduled", "\tstate = running");
        assert_eq!(stale(&read(&listing), HOST), None);
    }

    #[test]
    fn the_apps_own_constraint_with_ex_config_is_stale_and_a_clean_one_is_not() {
        let own = OWNER.replace("com.daoleno.mewla.daemon\"", "com.daoleno.mewla\"");
        assert_eq!(stale(&read(&own), HOST).as_deref(), Some("launchd could not set it up (EX_CONFIG)"));
        let clean = own.replace("\tlast exit code = 78: EX_CONFIG\n", "\tlast exit code = (never exited)\n");
        assert_eq!(stale(&read(&clean), HOST), None);
    }

    #[test]
    fn an_older_apps_program_is_stale() {
        let old = OWNER.replace("\tprogram = /Applications/Mewla.app/Contents/MacOS/Mewla", "\tprogram = /Applications/Mewla.app/Contents/Helpers/mewla");
        let old = old.replace("\tlast exit code = 78: EX_CONFIG\n", "").replace("com.daoleno.mewla.daemon\"", "com.daoleno.mewla\"");
        assert_eq!(stale(&read(&old), HOST).as_deref(), Some("it runs /Applications/Mewla.app/Contents/Helpers/mewla"));
    }

    #[test]
    fn the_bundle_jobs_program_identifier_drops_its_mode() {
        let job = read("x = {\n\tstate = running\n\tprogram identifier = Contents/MacOS/Mewla (mode: 2)\n}\n");
        assert!(job.running);
        assert_eq!(job.program_identifier.as_deref(), Some("Contents/MacOS/Mewla"));
        assert_eq!(job.signer, None);
    }
}
