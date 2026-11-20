//! The app's secrets (`secure.*`: the device key the daemon knows this app by).
//!
//! The skeleton keeps them in `secure.json` (0600) in the app's data directory, which Mewla Dev puts in its own
//! scratch: an ad hoc signed build that changes with every build would get a Keychain prompt each time. The signed
//! release keeps them in the Keychain (as the iOS host does), under the app's designated requirement.

use std::collections::BTreeMap;
use std::io::Write;
use std::os::unix::fs::OpenOptionsExt;
use std::path::PathBuf;
use std::sync::Mutex;

pub struct Store {
    path: Option<PathBuf>,
    lock: Mutex<()>,
}

impl Store {
    pub fn open(dir: Option<PathBuf>) -> Store {
        let path = dir.map(|dir| {
            let _ = std::fs::create_dir_all(&dir);
            dir.join("secure.json")
        });
        Store { path, lock: Mutex::new(()) }
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
        Ok(self.read()?.remove(key))
    }

    pub fn set(&self, key: &str, value: &str) -> Result<(), String> {
        let _guard = self.lock.lock().map_err(|_| "secure store poisoned")?;
        let mut items = self.read()?;
        items.insert(key.to_owned(), value.to_owned());
        self.write(&items)
    }

    pub fn remove(&self, key: &str) -> Result<(), String> {
        let _guard = self.lock.lock().map_err(|_| "secure store poisoned")?;
        let mut items = self.read()?;
        if items.remove(key).is_some() {
            self.write(&items)?;
        }
        Ok(())
    }
}
