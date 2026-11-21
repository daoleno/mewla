//! The app's secrets (`secure.*`: the device key the daemon knows this app by).
//!
//! The release (ai.mewla.app), once Developer ID signed, keeps them in the login Keychain as generic passwords under its
//! own service, as the iOS host does: the item trusts the app's designated requirement, so an update signed the same
//! way reads it with no prompt. Mewla Dev keeps them in `secure.json` (0600) in its scratch data directory: an ad hoc
//! signed build that changes with every build would get a Keychain prompt each time. A release that finds a secret
//! only in an older `secure.json` moves it into the Keychain on first read.

use std::collections::BTreeMap;
use std::io::Write;
use std::os::unix::fs::OpenOptionsExt;
use std::path::PathBuf;
use std::sync::Mutex;

/// errSecItemNotFound.
const NOT_FOUND: i32 = -25300;

pub struct Store {
    path: Option<PathBuf>,
    /// The Keychain service the release keeps its secrets under; `None`: the file.
    keychain: Option<String>,
    lock: Mutex<()>,
}

impl Store {
    pub fn open(dir: Option<PathBuf>, keychain: Option<String>) -> Store {
        let path = dir.map(|dir| {
            let _ = std::fs::create_dir_all(&dir);
            dir.join("secure.json")
        });
        Store { path, keychain, lock: Mutex::new(()) }
    }

    fn read(&self) -> Result<BTreeMap<String, String>, String> {
        let Some(path) = &self.path else { return Err("no data directory for secrets".into()) };
        match std::fs::read(path) {
            Ok(bytes) => serde_json::from_slice(&bytes).map_err(|err| format!("{}: {err}", path.display())),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(BTreeMap::new()),
            Err(err) => Err(format!("{}: {err}", path.display())),
        }
    }

    fn write(&self, items: &BTreeMap<String, String>) -> Result<(), String> {
        let Some(path) = &self.path else { return Err("no data directory for secrets".into()) };
        let temp = path.with_extension("json.new");
        let mut file = std::fs::OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(true)
            .mode(0o600)
            .open(&temp)
            .map_err(|err| format!("{}: {err}", temp.display()))?;
        file.write_all(&serde_json::to_vec(items).map_err(|err| err.to_string())?)
            .and_then(|()| file.sync_all())
            .map_err(|err| format!("{}: {err}", temp.display()))?;
        std::fs::rename(&temp, path).map_err(|err| format!("{}: {err}", path.display()))
    }

    pub fn get(&self, key: &str) -> Result<Option<String>, String> {
        let _guard = self.lock.lock().map_err(|_| "secure store poisoned")?;
        let Some(service) = &self.keychain else { return Ok(self.read()?.remove(key)) };
        if let Some(value) = keychain::get(service, key)? {
            return Ok(Some(value));
        }
        // An older build's file: the secret moves into the Keychain and leaves the file.
        let mut items = match self.read() {
            Ok(items) => items,
            Err(_) => return Ok(None),
        };
        let Some(value) = items.remove(key) else { return Ok(None) };
        keychain::set(service, key, &value)?;
        let _ = self.write(&items);
        Ok(Some(value))
    }

    pub fn set(&self, key: &str, value: &str) -> Result<(), String> {
        let _guard = self.lock.lock().map_err(|_| "secure store poisoned")?;
        if let Some(service) = &self.keychain {
            return keychain::set(service, key, value);
        }
        let mut items = self.read()?;
        items.insert(key.to_owned(), value.to_owned());
        self.write(&items)
    }

    pub fn remove(&self, key: &str) -> Result<(), String> {
        let _guard = self.lock.lock().map_err(|_| "secure store poisoned")?;
        if let Some(service) = &self.keychain {
            keychain::remove(service, key)?;
            // And any copy an older build left in the file.
            let Ok(mut items) = self.read() else { return Ok(()) };
            if items.remove(key).is_some() {
                let _ = self.write(&items);
            }
            return Ok(());
        }
        let mut items = self.read()?;
        if items.remove(key).is_some() {
            self.write(&items)?;
        }
        Ok(())
    }
}

/// Whether this app is signed with an Apple-issued identity (Developer ID): only then does a Keychain item keep
/// trusting it across updates. An ad hoc build's identity is its hash, new with every build, so it keeps the file.
pub fn keychain_usable() -> bool {
    use security_framework::os::macos::code_signing::{Flags, SecCode, SecRequirement};
    let Ok(requirement) = "anchor apple generic".parse::<SecRequirement>() else { return false };
    SecCode::for_self(Flags::NONE).and_then(|code| code.check_validity(Flags::NONE, &requirement)).is_ok()
}

/// Generic passwords in the login Keychain: service and account, the value as UTF-8.
mod keychain {
    use security_framework::passwords::{delete_generic_password, get_generic_password, set_generic_password};

    pub fn get(service: &str, key: &str) -> Result<Option<String>, String> {
        match get_generic_password(service, key) {
            Ok(bytes) => String::from_utf8(bytes).map(Some).map_err(|_| format!("Keychain item {key} is not text")),
            Err(err) if err.code() == super::NOT_FOUND => Ok(None),
            Err(err) => Err(format!("Keychain: {err}")),
        }
    }

    pub fn set(service: &str, key: &str, value: &str) -> Result<(), String> {
        set_generic_password(service, key, value.as_bytes()).map_err(|err| format!("Keychain: {err}"))
    }

    pub fn remove(service: &str, key: &str) -> Result<(), String> {
        match delete_generic_password(service, key) {
            Ok(()) => Ok(()),
            Err(err) if err.code() == super::NOT_FOUND => Ok(()),
            Err(err) => Err(format!("Keychain: {err}")),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::Store;

    /// The Keychain path on a Mac (the CI runner's login Keychain): set, read, move from a file, remove.
    /// Run with MEWLA_KEYCHAIN_TEST=1 (it writes a test item under its own service and removes it).
    #[test]
    fn keychain_round_trip_and_move_from_the_file() {
        if std::env::var_os("MEWLA_KEYCHAIN_TEST").is_none() {
            return;
        }
        let dir = std::env::temp_dir().join(format!("mewla-keychain-test-{}", std::process::id()));
        let service = format!("ai.mewla.app.test.{}", std::process::id());
        // An older build's file holds one secret.
        let file = Store::open(Some(dir.clone()), None);
        file.set("old", "from the file").unwrap();
        let store = Store::open(Some(dir.clone()), Some(service.clone()));
        assert_eq!(store.get("missing").unwrap(), None);
        store.set("device", "key-1").unwrap();
        store.set("device", "key-2").unwrap();
        assert_eq!(store.get("device").unwrap().as_deref(), Some("key-2"));
        assert_eq!(store.get("old").unwrap().as_deref(), Some("from the file"));
        assert_eq!(file.get("old").unwrap(), None, "moved out of the file");
        assert_eq!(store.get("old").unwrap().as_deref(), Some("from the file"), "read from the Keychain now");
        for key in ["device", "old"] {
            store.remove(key).unwrap();
            assert_eq!(store.get(key).unwrap(), None);
        }
        let _ = std::fs::remove_dir_all(&dir);
    }
}
