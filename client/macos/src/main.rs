//! Mewla for Mac: the AppKit host around client/ui (as client/ios is the iOS host), which carries this computer's
//! daemon and `mewla` CLI in its bundle (`Contents/Helpers/mewla`).
//!
//! It runs the app the way NSH's release runner does (`nsh-macos-app`: the bundle in `Contents/Resources/nsh`, its
//! `app.json`, the logger), with a host channel that answers the calls the app makes of every host:
//! - `secure.get` / `secure.set` / `secure.remove`: the device key and other secrets;
//! - `app.pairing`: `mewla pair -json` of this computer's daemon (the phone QR, and the link the app pairs itself
//!   with, silently: it is this computer);
//! - `app.dock`: the Dock icon shown while the window is, hidden with only the menu bar cat;
//! - `app.stop`: Stop Mewla on this Mac… (the LaunchAgent unregistered);
//! - `link.start` / `link.stop` / `link.stopAll` / `link.events`: Mewla Relay's pinned tunnels (link.rs);
//! - `app.permissionGuide`: System Settings on a permission's list with the panel to drag Mewla in (permissions.rs).
//!
//! The daemon: the app (`com.daoleno.mewla`) registers it as a LaunchAgent with SMAppService, run through this
//! executable (`Mewla --daemon serve`, host.rs) so that macOS charges its permissions to the app, after taking over
//! the install `mewla boot install` made (agent.rs), so it runs with the app closed and after a restart. Mewla Dev
//! starts `mewla serve` as its child on its own state and stops it when the app quits.
//!
//! Mewla Dev runs beside an installed Mewla with its own state, address and data (its daemon leaves Chrome's native
//! host and the cloud CLIs to that Mewla: `MEWLA_BESIDE_INSTALLED`), named by the environment:
//! `MEWLA_APP_STATE_ROOT` (the daemon's state root), `MEWLA_APP_ADDR` (`127.0.0.1:<port>`), `MEWLA_APP_DATA_DIR`
//! (the app's storage and logs), `MEWLA_APP_PHONE_ADDRESS` (the address the phone QR names),
//! `MEWLA_APP_QUICK_CHAT_SHORTCUT`, `MEWLA_APP_BRAIN_HOST_EXECUTOR` (Brain held to one agent),
//! `MEWLA_APP_DEV_SCRIPT` (a JSON proof script the app runs once connected) and
//! `MEWLA_APP_BACKGROUND=1` (never the active app).

#[cfg(target_os = "macos")]
mod agent;
mod link;
#[cfg(target_os = "macos")]
mod daemon;
#[cfg(target_os = "macos")]
mod host;
#[cfg(target_os = "macos")]
mod permissions;
#[cfg(target_os = "macos")]
mod secure;

#[cfg(target_os = "macos")]
fn main() -> std::process::ExitCode {
    use nsh_platform_macos::{AppSource, AssetBase, Bundle, run_app};

    // launchd's job for the release's daemon (host.rs): this executable as the daemon's parent, never the app.
    let mut args = std::env::args_os().skip(1);
    if args.next().is_some_and(|first| first == host::FLAG) {
        return host::run(args.collect());
    }
    // Every part of `nsh/base` the app's code imports (the stock host serves only its core part to a Rust app).
    nsh_platform_macos::set_ui_sets(vec![nsh_base::registry()]);
    let (dir, mut options) = setup();
    let contents = dir.parent().and_then(|p| p.parent()).map(std::path::Path::to_path_buf).unwrap_or_default();
    // SAFETY: the bytecode is a resource of this app's own bundle, which the code signature covers.
    let bundle = match unsafe { Bundle::read_trusted(&dir.join("bundle.nshc")) } {
        Ok(bundle) => bundle,
        Err(err) => {
            log::error!("cannot read the app bundle in {}: {err}", dir.display());
            return std::process::ExitCode::FAILURE;
        }
    };
    let support_dir = nsh_engine::support_dir();
    let mut launch = nsh_engine::updates::launch(bundle, &dir, support_dir.as_deref());
    options.asset_base = Some(AssetBase::Directory(launch.assets.clone()));

    let config = daemon::Config::from_env(&contents);
    // The main window opens as large and where the person left it (the engine keeps its placement as it changes);
    // before they have one, at its first size. It never resizes itself while open. `MEWLA_APP_WINDOW_SIZE=<w>x<h>`
    // (Mewla Dev's proof runs) opens it at that size whatever was kept.
    let forced = std::env::var("MEWLA_APP_WINDOW_SIZE").ok().as_deref().and_then(parse_size);
    launch.bundle.main_window.get_or_insert_with(Default::default).restore_bounds = forced.is_none();
    // Its hidden 46 pt title bar and transparency from the first frame (ui/mac/mac.ts sets the same): set only later
    // by the app, the kept size came back 32 pt shorter on every launch (the fidelity captures of 11-22).
    let ours = serde_json::json!({
        "titleBar": { "style": "hidden", "height": 46, "controlsOffset": { "x": 16, "y": 17 } },
        "transparent": true,
    });
    match nsh_core::desktop::MainWindowOptions::from_json(&ours) {
        Ok(ours) => launch.bundle.main_window.get_or_insert_with(Default::default).attributes.merge(&ours.attributes),
        Err(err) => log::warn!("main window options: {err}"),
    }
    options.size = forced.unwrap_or_else(|| first_size(&config.root()));
    // The real app's daemon is its LaunchAgent (agent.rs); Mewla Dev's is its child, on its own state.
    let release = config.state_root.is_none() && options.app_id == "com.daoleno.mewla";
    let child = if release {
        let home = std::env::var_os("HOME").map(std::path::PathBuf::from).unwrap_or_default();
        let cli = config.cli.clone();
        std::thread::spawn(move || {
            // Warn: the release logger keeps info only for the app's own code (target `js`), and these few lines are
            // what explains a daemon that did not come up.
            for line in agent::adopt_and_register(&home, &cli) {
                log::warn!("takeover: {line}");
            }
        });
        None
    } else {
        daemon::start(&config)
    };
    // The release signed with Developer ID keeps its secrets in the Keychain under its own id; Mewla Dev and an ad hoc
    // signed release in their file (secure.rs).
    let keychain = release && secure::keychain_usable();
    log::warn!("secrets: {}", if keychain { "the Keychain" } else { "secure.json (not Developer ID signed, or Mewla Dev)" });
    let secrets = secure::Store::open(
        options.data_dir.clone().or_else(|| nsh_engine::logging::log_dir(&options.app_id).and_then(|d| d.parent().map(Into::into))),
        keychain.then(|| options.app_id.clone()),
    );
    install_host(&options.host, config, secrets, release);

    let code = run_app(AppSource::Bundle(launch.bundle), options);
    daemon::stop(child);
    code
}

#[cfg(target_os = "macos")]
fn install_host(host: &nsh_engine::HostChannel, config: daemon::Config, secrets: secure::Store, release: bool) {
    use serde_json::{Map, Value, json};

    let mut params = Map::new();
    params.insert("deviceName".into(), json!("Mewla for Mac"));
    params.insert("computerName".into(), json!(daemon::computer_name()));
    // The release (com.daoleno.mewla): it starts at login by default; Mewla Dev never registers a login item.
    params.insert("release".into(), json!(release));
    if let Some(shortcut) = std::env::var("MEWLA_APP_QUICK_CHAT_SHORTCUT").ok().filter(|s| !s.is_empty()) {
        params.insert("quickChatShortcut".into(), json!(shortcut));
    }
    if let Some(script) = std::env::var("MEWLA_APP_DEV_SCRIPT").ok().and_then(|s| serde_json::from_str::<Value>(&s).ok()) {
        params.insert("devScript".into(), script);
    }
    host.set_params(params);

    let store = std::sync::Arc::new(secrets);
    for name in ["secure.get", "secure.set", "secure.remove"] {
        let store = store.clone();
        host.handle(name, move |call| {
            let key = call.data().get("key").and_then(Value::as_str).unwrap_or_default().to_owned();
            let result = match call.name() {
                "secure.get" => store.get(&key).map(|value| value.map_or(Value::Null, Value::String)),
                "secure.set" => {
                    let value = call.data().get("value").and_then(Value::as_str).unwrap_or_default().to_owned();
                    store.set(&key, &value).map(|()| Value::Null)
                }
                _ => store.remove(&key).map(|()| Value::Null),
            };
            call.reply(result);
        });
    }

    // The Expo app's storage the phone hosts hand over once: a Mac never had one.
    host.handle("app.legacyStorage", |call| call.reply(Ok(Value::Null)));
    host.handle("app.legacyStorageCopied", |call| call.reply(Ok(Value::Null)));

    // `app.dock {shown}`: the Dock icon while a window of the app is shown, none with only the menu bar cat
    // (DESIGN-MAC.md §2.4); the stock host sets the activation policy once, at launch.
    host.handle("app.dock", move |call| {
        let shown = call.data().get("shown").and_then(Value::as_bool).unwrap_or(true);
        dispatch2::DispatchQueue::main().exec_async(move || set_dock_icon(shown));
        call.reply(Ok::<Value, String>(Value::Null));
    });

    // `app.permissionGuide {permission, state?}`: System Settings on the permission's list and the panel beside it with
    // Mewla's icon to drag in (permissions.rs), after clearing Mewla's own rows that do not count (`state`: the daemon's); `{given}`: the daemon reads that grant as given, so the panel asking for it
    // closes and Mewla comes to the front; `{}` closes the panel.
    host.handle("app.permissionGuide", |call| {
        if let Some(given) = call.data().get("given").and_then(Value::as_str).and_then(permissions::Permission::from_id) {
            dispatch2::DispatchQueue::main().exec_async(move || {
                if let Some(mtm) = nsh_platform_macos::objc2::MainThreadMarker::new() {
                    permissions::given(mtm, given);
                }
            });
            call.reply(Ok::<Value, String>(Value::Null));
            return;
        }
        let id = call.data().get("permission").and_then(Value::as_str).map(str::to_owned);
        let permission = id.as_deref().map(permissions::Permission::from_id);
        if let Some(None) = permission {
            call.reply(Err::<Value, String>(format!("no such permission: {}", id.unwrap_or_default())));
            return;
        }
        // The grant's state as the daemon read it: Mewla's own rows that do not count are cleared before asking.
        if let Some(permission) = permission.flatten() {
            permissions::clear_stale(permission, call.data().get("state").and_then(Value::as_str));
        }
        dispatch2::DispatchQueue::main().exec_async(move || {
            let Some(mtm) = nsh_platform_macos::objc2::MainThreadMarker::new() else { return };
            match permission.flatten() {
                Some(permission) => permissions::show(mtm, permission),
                None => permissions::close(),
            }
        });
        call.reply(Ok::<Value, String>(Value::Null));
    });

    // `app.stop`: Stop Mewla on this Mac… (the app quits after it): the release's LaunchAgent is unregistered; Mewla
    // Dev's daemon, the app's child, stops as the app quits.
    host.handle("app.stop", move |call| {
        let Some(reply) = call.into_reply() else { return };
        std::thread::spawn(move || {
            let result = if release { agent::unregister() } else { Ok("the daemon stops with the app".into()) };
            if let Ok(line) = &result {
                log::warn!("stop: {line}");
            }
            reply.reply(result.map(Value::String));
        });
    });

    let config = std::sync::Arc::new(config);
    if release {
        let config = config.clone();
        host.handle("app.setLAN", move |call| {
            let enabled = call.data().get("enabled").and_then(Value::as_bool);
            let Some(reply) = call.into_reply() else { return };
            let config = config.clone();
            std::thread::spawn(move || {
                let result = enabled.ok_or("expected a LAN setting".to_string())
                    .and_then(|on| config.set_lan(on)).map(|()| Value::Null);
                reply.reply(result);
            });
        });
    }
    // Mewla Relay's pinned tunnels (link.rs), as the phone hosts carry them: a computer paired through Mewla Relay,
    // and the LAN or Tailscale address it announces, is reached only through one.
    let tunnels = std::sync::Arc::new(link::Tunnels::new(link::dial_by_name()));
    {
        let tunnels = tunnels.clone();
        host.handle("link.start", move |call| {
            let data = call.data();
            let text = |name: &str| data.get(name).and_then(Value::as_str).unwrap_or_default().to_owned();
            let (key, host_, pin, mode) = (text("key"), text("host"), text("pin"), text("mode"));
            let port = data.get("port").and_then(Value::as_u64).and_then(|p| u16::try_from(p).ok()).unwrap_or(0);
            let Some(reply) = call.into_reply() else { return };
            let tunnels = tunnels.clone();
            // It dials and handshakes: never on the engine's thread.
            std::thread::spawn(move || {
                reply.reply(tunnels.start(&key, &host_, port, &pin, &mode).map(|(port, rtt)| json!({ "port": port, "rttMs": rtt })));
            });
        });
    }
    {
        let tunnels = tunnels.clone();
        host.handle("link.stop", move |call| {
            let key = call.data().get("key").and_then(Value::as_str).unwrap_or_default().to_owned();
            tunnels.stop(&key);
            call.reply(Ok::<Value, String>(Value::Null));
        });
    }
    host.handle("link.stopAll", move |call| {
        tunnels.stop_all();
        call.reply(Ok::<Value, String>(Value::Null));
    });
    host.handle("link.events", |call| call.reply(Ok::<Value, String>(json!([]))));
    // `link.network {online}`: the Mac's network changed or it woke from sleep (link.rs `watch_network`).
    {
        let host = host.clone();
        link::watch_network(move |online| {
            let _ = host.send("link.network", json!({ "online": online }));
        });
    }

    host.handle("app.pairing", move |call| {
        let Some(reply) = call.into_reply() else { return };
        let config = config.clone();
        // `mewla pair` waits for the daemon (up to a minute while it starts): off the engine's thread.
        std::thread::spawn(move || reply.reply(daemon::pairing(&config)));
    });
}

/// Shows or hides the app's Dock icon (Regular or Accessory); an app that never activates (Mewla Dev in the background,
/// Prohibited) is left as it is.
#[cfg(target_os = "macos")]
fn set_dock_icon(shown: bool) {
    use nsh_platform_macos::objc2::MainThreadMarker;
    use nsh_platform_macos::objc2_app_kit::{NSApplication, NSApplicationActivationPolicy};

    let Some(mtm) = MainThreadMarker::new() else { return };
    let app = NSApplication::sharedApplication(mtm);
    if app.activationPolicy() == NSApplicationActivationPolicy::Prohibited {
        return;
    }
    let policy = if shown { NSApplicationActivationPolicy::Regular } else { NSApplicationActivationPolicy::Accessory };
    let before = app.activationPolicy();
    if before != policy {
        app.setActivationPolicy(policy);
    }
    // What macOS holds now, not what was asked: the Mac run reads it (a Dock icon is Regular).
    log::warn!("dock: shown {shown}: activation policy {:?} -> {:?}", before.0, app.activationPolicy().0);
}

/// The app's `nsh` resources and its window's options, read as NSH's release runner reads them
/// (nsh-macos-app `setup`, NSH 0.6.1, Apache-2.0), with the app's data directory from `MEWLA_APP_DATA_DIR`.
#[cfg(target_os = "macos")]
fn setup() -> (std::path::PathBuf, nsh_platform_macos::AppOptions) {
    use nsh_platform_macos::{ActivationPolicy, AppOptions, Appearance, AssetBase, Limits};

    let dir = std::env::current_exe()
        .and_then(|exe| exe.canonicalize())
        .ok()
        .and_then(|exe| Some(exe.parent()?.parent()?.join("Resources/nsh")))
        .unwrap_or_default();
    let mut options = AppOptions {
        asset_base: Some(AssetBase::Directory(dir.join("assets"))),
        limits: Limits::standalone(),
        login_item: true,
        ..AppOptions::default()
    };
    let info = std::fs::read_to_string(dir.join("app.json"))
        .ok()
        .and_then(|text| serde_json::from_str::<serde_json::Value>(&text).ok());
    let field = |key: &str| info.as_ref().and_then(|info| info.get(key)?.as_str().map(str::to_owned));
    if let Some(id) = field("id") {
        options.app_id = id;
    }
    options.data_dir = std::env::var_os("MEWLA_APP_DATA_DIR")
        .map(std::path::PathBuf::from)
        .or_else(|| field("dataDir").as_deref().and_then(nsh_engine::resolve_data_dir));
    if let Some(name) = field("name") {
        options.title = name;
    }
    match field("appearance").as_deref() {
        Some("light") => options.appearance = Appearance::Light,
        Some("dark") => options.appearance = Appearance::Dark,
        _ => {}
    }
    if field("activationPolicy").as_deref() == Some("accessory") {
        options.activation_policy = ActivationPolicy::Accessory;
    }
    // The design's Brain frame (DESIGN-MAC.md `w-home`); `main` picks the first size (`first_size`).
    options.size = MAIN_SIZE;
    // ⌘W hides the window; the app (the menu bar cat, the perch) keeps running until Quit.
    options.keep_alive_on_close = true;
    // Mewla Dev on a Mac someone is using: never the active app, so its windows come up without taking the keyboard
    // (NSApplicationActivationPolicyProhibited, as `nsh test --target macos` runs apps).
    options.background = std::env::var("MEWLA_APP_BACKGROUND").as_deref() == Ok("1");
    install_logger(&options.app_id, options.data_dir.as_deref());
    (dir, options)
}

#[cfg(target_os = "macos")]
fn install_logger(app_id: &str, data_dir: Option<&std::path::Path>) {
    let mut unified = nsh_platform_apple::unified_log::Options::default();
    unified.filtered = true;
    let unified = nsh_platform_apple::unified_log::logger(unified);
    let mut logging = nsh_engine::logging::LogOptions::default();
    logging.directory = match data_dir {
        Some(dir) => Some(dir.join("logs")),
        None => nsh_engine::logging::log_dir(app_id),
    };
    logging.output = Some(Box::new(move |r: &log::Record<'_>| unified.log(r)));
    nsh_engine::logging::install(logging);
}

/// The main window's default: the design's Brain frame, two panes (DESIGN-MAC.md `w-home`).
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const MAIN_SIZE: (i32, i32) = (1280, 800);
/// The design's first-run window: one column, the waking cat, then ready (the gallery's m-waking, m-ready, m-noagent,
/// m-perm frames).
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const FIRST_RUN_SIZE: (i32, i32) = (720, 560);

/// The main window's size before the person has one of their own: the first-run window on a Mac where Mewla's daemon
/// has never run (so there is no Session or Work for the left pane to hold), the main default where it has (its
/// state root holds its identity or Brain's database). Read from disk before the daemon is up, so an approximation: a
/// daemon that ran once and holds nothing still opens two panes.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn first_size(state_root: &std::path::Path) -> (i32, i32) {
    let ran = ["identity.json", "brain/brain.db"].iter().any(|marker| state_root.join(marker).exists());
    if ran { MAIN_SIZE } else { FIRST_RUN_SIZE }
}

/// `<w>x<h>` in points.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn parse_size(text: &str) -> Option<(i32, i32)> {
    let (w, h) = text.trim().split_once('x')?;
    let (w, h) = (w.trim().parse::<i32>().ok()?, h.trim().parse::<i32>().ok()?);
    (w >= 200 && h >= 200).then_some((w, h))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_new_mac_opens_the_first_run_window_and_one_that_ran_mewla_the_main_one() {
        let root = std::env::temp_dir().join(format!("mewla-first-size-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        assert_eq!(first_size(&root), FIRST_RUN_SIZE, "no state root at all");
        std::fs::create_dir_all(root.join("logs")).unwrap();
        assert_eq!(first_size(&root), FIRST_RUN_SIZE, "only the app's daemon log");
        std::fs::create_dir_all(root.join("brain")).unwrap();
        std::fs::write(root.join("brain/brain.db"), b"").unwrap();
        assert_eq!(first_size(&root), MAIN_SIZE, "Brain's database");
        std::fs::remove_dir_all(&root).unwrap();
    }

    #[test]
    fn a_forced_size_reads_w_by_h() {
        assert_eq!(parse_size("1280x800"), Some((1280, 800)));
        assert_eq!(parse_size(" 720 x 560 "), Some((720, 560)));
        assert_eq!(parse_size("1280"), None);
        assert_eq!(parse_size("0x0"), None);
    }
}

#[cfg(not(target_os = "macos"))]
fn main() {
    eprintln!("Mewla for Mac builds on macOS");
    std::process::exit(1);
}
