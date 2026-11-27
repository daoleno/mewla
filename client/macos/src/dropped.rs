//! Files dropped on Mewla or pasted into it from Finder, read for the app: `files.localCopy {path, maxBytes}` and
//! `files.localCopyDone {path}` (client/ui/more/files/incoming.ts).
//!
//! NSH 0.7.1 hands a dropped or pasted local file over as its `file:` URL alone, and its `fs/promises` cannot read
//! every such file: it opens each folder on the way for reading (O_RDONLY), and macOS keeps some folders unlistable.
//! The screenshot thumbnail dragged out after ⌘⇧4 is written to
//! `/var/folders/…/T/TemporaryItems/NSIRD_screencaptureui_*/`, whose `com.apple.macl` lets no app list it; the drag
//! grants the app that takes it the file alone. The kernel then refuses NSH (`System Policy: Mewla deny(1)
//! file-read-data …/NSIRD_screencaptureui_…`, owner 11-27). This host opens the file by its full path, which needs
//! only to search the folders, as any Mac app reads a drop, and clones it (APFS: no bytes copied) into a folder of
//! the app's own that `fs/promises` reads (`~/Library/Caches/<app id>/dropped`, within nsh.json's `${homeDir}`). The
//! app reads the copy and lets it go; what a crash leaves is cleared at the next launch.
//!
//! The answer: `{copy, size}`, `{folder: true}`, or `{failed, size?}` with `failed` one of `missing` (no file
//! there, or a file reference macOS no longer resolves), `denied` (macOS refused Mewla the file), `large` (past
//! `maxBytes`) or `unreadable`.

use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use serde_json::{Value, json};

/// The copies of dropped and pasted files, in the app's own folder.
pub struct Copies {
    dir: PathBuf,
    next: AtomicU64,
}

impl Copies {
    /// Copies kept in `dir`, cleared of what an earlier run left.
    pub fn new(dir: PathBuf) -> Self {
        if let Err(err) = std::fs::remove_dir_all(&dir)
            && err.kind() != ErrorKind::NotFound
        {
            log::warn!("dropped: cannot clear {}: {err}", dir.display());
        }
        Copies { dir, next: AtomicU64::new(0) }
    }

    /// `~/Library/Caches/<app id>/dropped`.
    pub fn in_caches(app_id: &str) -> Option<Self> {
        let home = std::env::var_os("HOME").filter(|home| !home.is_empty())?;
        Some(Self::new(PathBuf::from(home).join("Library/Caches").join(app_id).join("dropped")))
    }

    /// Copies the file at `path` (absolute) when it is at most `max` bytes: the answer the app reads.
    pub fn copy(&self, path: &Path, max: u64) -> Value {
        if !path.is_absolute() {
            return json!({ "failed": "missing" });
        }
        let failed = |err: std::io::Error| {
            log::warn!("dropped: cannot read {}: {err}", path.display());
            json!({ "failed": reason(&err) })
        };
        let info = match std::fs::metadata(path) {
            Ok(info) => info,
            Err(err) => return failed(err),
        };
        if info.is_dir() {
            return json!({ "folder": true });
        }
        if !info.is_file() {
            return json!({ "failed": "unreadable" });
        }
        if info.len() > max {
            return json!({ "failed": "large", "size": info.len() });
        }
        if let Err(err) = std::fs::create_dir_all(&self.dir) {
            log::warn!("dropped: cannot make {}: {err}", self.dir.display());
            return json!({ "failed": "unreadable" });
        }
        let copy = self.dir.join(format!("{}-{}", std::process::id(), self.next.fetch_add(1, Ordering::Relaxed)));
        match std::fs::copy(path, &copy) {
            // Its size may have changed since: the copy's is what the app reads.
            Ok(size) if size <= max => json!({ "copy": copy.to_string_lossy(), "size": size }),
            Ok(size) => {
                let _ = std::fs::remove_file(&copy);
                json!({ "failed": "large", "size": size })
            }
            Err(err) => {
                let _ = std::fs::remove_file(&copy);
                failed(err)
            }
        }
    }

    /// Removes the copy at `path`; anything not a copy of ours is left as it is.
    pub fn release(&self, path: &Path) {
        let ours = path.parent() == Some(self.dir.as_path())
            && path.file_name().and_then(|name| name.to_str()).is_some_and(|name| {
                name.split_once('-').is_some_and(|(pid, n)| {
                    !pid.is_empty() && !n.is_empty() && pid.bytes().chain(n.bytes()).all(|b| b.is_ascii_digit())
                })
            });
        if ours && let Err(err) = std::fs::remove_file(path) {
            log::warn!("dropped: cannot remove {}: {err}", path.display());
        }
    }
}

/// What the app says of a file the Mac would not let it read.
fn reason(err: &std::io::Error) -> &'static str {
    match err.kind() {
        // A file reference macOS no longer resolves stays `/.file/id=…`, under the Mac's own file `/.file`.
        ErrorKind::NotFound | ErrorKind::NotADirectory => "missing",
        ErrorKind::PermissionDenied => "denied",
        // EPERM: a privacy grant (TCC) or the file's own access list said no.
        _ if err.raw_os_error() == Some(libc::EPERM) => "denied",
        _ => "unreadable",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("mewla-dropped-test-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn a_dropped_file_is_copied_for_the_app_and_its_copy_let_go() {
        let base = scratch("copy");
        let dropped = base.join("截屏2026-11-27 12.25.59.png");
        std::fs::write(&dropped, b"picture").unwrap();
        let copies = Copies::new(base.join("dropped"));
        let answer = copies.copy(&dropped, 64);
        assert_eq!(answer["size"], 7);
        let copy = PathBuf::from(answer["copy"].as_str().unwrap());
        assert_eq!(std::fs::read(&copy).unwrap(), b"picture");
        copies.release(&copy);
        assert!(!copy.exists());
        assert!(dropped.exists(), "the person's file stays");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn what_cannot_be_copied_says_why() {
        let base = scratch("why");
        std::fs::write(base.join("big.bin"), vec![0u8; 65]).unwrap();
        let copies = Copies::new(base.join("dropped"));
        assert_eq!(copies.copy(&base.join("big.bin"), 64), json!({ "failed": "large", "size": 65 }));
        assert_eq!(copies.copy(&base.join("gone.png"), 64), json!({ "failed": "missing" }));
        assert_eq!(copies.copy(Path::new("/.file/id=6571367.8601"), 64), json!({ "failed": "missing" }));
        assert_eq!(copies.copy(&base, 64), json!({ "folder": true }));
        assert_eq!(copies.copy(Path::new("relative.png"), 64), json!({ "failed": "missing" }));
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn only_copies_are_removed_and_a_launch_clears_the_last_runs() {
        let base = scratch("release");
        let mine = base.join("mine.txt");
        std::fs::write(&mine, b"keep").unwrap();
        let dir = base.join("dropped");
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("1-0"), b"left by a crash").unwrap();
        let copies = Copies::new(dir.clone());
        assert!(!dir.join("1-0").exists());
        copies.release(&mine);
        assert!(mine.exists());
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("notes"), b"not a copy").unwrap();
        copies.release(&dir.join("notes"));
        assert!(dir.join("notes").exists());
        let _ = std::fs::remove_dir_all(&base);
    }
}
