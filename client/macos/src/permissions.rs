//! Giving Mewla a macOS permission (Screen Recording, Accessibility) the way the best Mac apps ask for it: System
//! Settings opens on the permission's own list and a small panel sits beside its window holding Mewla's icon, which the
//! person drags into the list. The same panel brings Mewla back after its row was removed with −, and when the row is
//! there but switched off, it says to turn it on.
//!
//! What is dragged is this app's own bundle (`NSBundle.mainBundle`): macOS charges both permissions to it, the daemon
//! included, which launchd runs as this executable's child (host.rs) and Mewla Dev as its own child (daemon.rs).
//!
//! The panel follows System Settings' window (the window server's list: no permission needed for a window's frame),
//! closes when that window closes, and once macOS reports the grant (TCC's preflight, which tells a row switched off
//! from no row) closes and brings Mewla to the front. Applying the grant in the daemon, by a restart where macOS needs
//! one, is the daemon's (`/agent-use` follow, grants.Manager).

use std::cell::RefCell;
use std::ffi::{CString, c_void};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

use objc2::rc::Retained;
use objc2::runtime::{AnyObject, ProtocolObject};
use objc2::{AnyThread, DefinedClass, MainThreadMarker, MainThreadOnly, define_class, msg_send};
use objc2_app_kit::{
    NSAccessibility, NSApplication, NSBackingStoreType, NSDragOperation, NSDraggingContext, NSDraggingItem, NSDraggingSession,
    NSDraggingSource, NSEvent, NSFont, NSImage, NSImageView, NSPanel, NSRunningApplication, NSScreen,
    NSTextAlignment, NSTextField, NSView, NSWindowStyleMask, NSWindowTitleVisibility, NSWorkspace,
};
use objc2_foundation::{NSArray, NSBundle, NSDictionary, NSNumber, NSObjectProtocol, NSPoint, NSRect, NSSize, NSString, NSURL};

/// A permission the panel asks for.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Permission {
    ScreenRecording,
    Accessibility,
}

impl Permission {
    pub fn from_id(id: &str) -> Option<Permission> {
        match id {
            "screen_recording" => Some(Permission::ScreenRecording),
            "accessibility" => Some(Permission::Accessibility),
            _ => None,
        }
    }

    /// System Settings › Privacy & Security, on this permission's list.
    pub fn settings_url(self) -> &'static str {
        match self {
            Permission::ScreenRecording => "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
            Permission::Accessibility => "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
        }
    }

    fn tcc_service(self) -> &'static str {
        match self {
            Permission::ScreenRecording => "kTCCServiceScreenCapture",
            Permission::Accessibility => "kTCCServiceAccessibility",
        }
    }

    /// The service's name for `tccutil`.
    fn tccutil_service(self) -> &'static str {
        match self {
            Permission::ScreenRecording => "ScreenCapture",
            Permission::Accessibility => "Accessibility",
        }
    }
}

/// Where Mewla stands in a permission's list, as TCC's preflight answers this app.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Listed {
    Granted,
    /// In the list, switched off.
    Off,
    /// Not in the list (never asked, removed with −, or a row of another signature of Mewla).
    Missing,
}

/// The panel's one line for the state the person starts from.
pub fn instruction(name: &str, listed: Listed) -> String {
    match listed {
        Listed::Off => format!("Turn on {name} in the list"),
        Listed::Missing | Listed::Granted => format!("Drag {name} into the list"),
    }
}

/// Where the panel goes beside System Settings' window, in Cocoa coordinates (origin at the bottom left of the main
/// screen): right of it, or left when the screen has no room there, its top a little below the window's title bar.
pub fn place_beside(settings: NSRect, panel: NSSize, screen: NSRect) -> NSPoint {
    const GAP: f64 = 12.0;
    const BELOW_TOP: f64 = 64.0;
    let right = settings.origin.x + settings.size.width + GAP;
    let x = if right + panel.width <= screen.origin.x + screen.size.width {
        right
    } else {
        (settings.origin.x - GAP - panel.width).max(screen.origin.x)
    };
    let top = settings.origin.y + settings.size.height - BELOW_TOP;
    let y = (top - panel.height).max(screen.origin.y);
    NSPoint::new(x, y)
}

/// A window-server frame (origin at the top left of the main screen, y down) in Cocoa's coordinates.
pub fn cocoa_rect(cg: NSRect, main_height: f64) -> NSRect {
    NSRect::new(NSPoint::new(cg.origin.x, main_height - cg.origin.y - cg.size.height), cg.size)
}

// --- TCC and the window server -------------------------------------------------------------------------------------

unsafe extern "C" {
    fn dlopen(path: *const i8, mode: i32) -> *mut c_void;
    fn dlsym(handle: *mut c_void, symbol: *const i8) -> *mut c_void;
}

#[link(name = "CoreGraphics", kind = "framework")]
unsafe extern "C" {
    fn CGWindowListCopyWindowInfo(option: u32, relative_to: u32) -> *mut AnyObject;
    fn CGPreflightScreenCaptureAccess() -> bool;
}

#[link(name = "ApplicationServices", kind = "framework")]
unsafe extern "C" {
    fn AXIsProcessTrusted() -> bool;
}

type Preflight = unsafe extern "C" fn(service: *const AnyObject, options: *const c_void) -> i32;

fn tcc_preflight() -> Option<Preflight> {
    static PREFLIGHT: std::sync::OnceLock<Option<usize>> = std::sync::OnceLock::new();
    let address = *PREFLIGHT.get_or_init(|| -> Option<usize> {
        let path = CString::new("/System/Library/PrivateFrameworks/TCC.framework/TCC").ok()?;
        let name = CString::new("TCCAccessPreflight").ok()?;
        // SAFETY: plain dlopen/dlsym of a system framework; RTLD_LAZY.
        let symbol = unsafe {
            let handle = dlopen(path.as_ptr(), 1);
            if handle.is_null() {
                return None;
            }
            dlsym(handle, name.as_ptr())
        };
        (!symbol.is_null()).then_some(symbol as usize)
    })
    .as_ref()?;
    // SAFETY: TCCAccessPreflight(CFStringRef service, CFDictionaryRef options) -> int.
    Some(unsafe { std::mem::transmute::<usize, Preflight>(address) })
}

/// TCC's own answer for this app: `None` where its preflight is not there.
fn exact(permission: Permission) -> Option<Listed> {
    let preflight = tcc_preflight()?;
    let service = NSString::from_str(permission.tcc_service());
    // SAFETY: an NSString is a CFString (toll-free bridged); no options.
    let answer = unsafe { preflight(Retained::as_ptr(&service).cast(), std::ptr::null()) };
    Some(match answer {
        0 => Listed::Granted,
        1 => Listed::Off,
        _ => Listed::Missing,
    })
}

/// Where this app stands in a permission's list (the daemon, this app's child, stands the same).
pub fn listed(permission: Permission) -> Listed {
    if let Some(listed) = exact(permission) {
        return listed;
    }
    // SAFETY: plain calls without arguments.
    let granted = unsafe {
        match permission {
            Permission::ScreenRecording => CGPreflightScreenCaptureAccess(),
            Permission::Accessibility => AXIsProcessTrusted(),
        }
    };
    if granted { Listed::Granted } else { Listed::Missing }
}

/// Whether Allow clears this app's rows first: only when TCC itself says Mewla.app has no row that counts (a row of
/// another signature of Mewla reads so for Screen Recording, while the list shows it on), and the daemon does not count
/// the grant already. This app's own answer decides, not the daemon's "off": an older Mewla's daemon row (its path,
/// switched off) makes the daemon read off while Mewla.app's row is the stale one. Mewla.app's row switched off reads
/// off here and is left alone, as is everything without an exact answer. (For Accessibility TCC answers off for a row
/// of another signature too, so such a row is left alone: macos-permissions.yml probe, `bundle-stale-req`.)
fn clears(exact: Option<Listed>, daemon: Option<&str>) -> bool {
    exact == Some(Listed::Missing) && !matches!(daemon, Some("granted" | "restart"))
}

/// Before asking: when Mewla has no row that counts, removes this app's own rows for the permission (`tccutil reset
/// <service> <this bundle id>`: never a whole service, never another app's), so a row left by another signature of
/// Mewla does not stand in the list as on, and dragging Mewla in (or macOS's own ask) writes one that counts. Rows of
/// a path (an older Mewla's bare daemon, listed as "mewla") are not this bundle id's and stay as they are: nothing
/// points at them. Any thread; returns whether it cleared.
pub fn clear_stale(permission: Permission, daemon: Option<&str>) -> bool {
    if !clears(exact(permission), daemon) {
        return false;
    }
    let Some(id) = NSBundle::mainBundle().bundleIdentifier().map(|id| id.to_string()) else { return false };
    if id.is_empty() {
        return false;
    }
    let service = permission.tccutil_service();
    log::warn!("permission panel: clearing {id}'s rows for {service}");
    // At most 3 s: Allow must never wait on it (it answers in milliseconds).
    let mut child = match std::process::Command::new("/usr/bin/tccutil")
        .args(["reset", service, &id])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
    {
        Ok(child) => child,
        Err(err) => {
            log::warn!("permission panel: tccutil: {err}");
            return false;
        }
    };
    let deadline = Instant::now() + Duration::from_secs(3);
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break Some(status),
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(20)),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                break None;
            }
        }
    };
    let mut said = String::new();
    if let Some(mut out) = child.stdout.take() {
        use std::io::Read;
        let _ = out.read_to_string(&mut said);
    }
    match status {
        Some(status) => {
            log::warn!("permission panel: cleared {id}'s rows for {service} ({status}): {}", said.trim());
            status.success()
        }
        None => {
            log::warn!("permission panel: tccutil did not answer in 3 s; asking without clearing");
            false
        }
    }
}

/// System Settings' main window, in the window server's coordinates, when it is on screen.
fn settings_window() -> Option<NSRect> {
    let pids: Vec<i32> = NSRunningApplication::runningApplicationsWithBundleIdentifier(&NSString::from_str(
        "com.apple.systempreferences",
    ))
    .iter()
    .map(|app| app.processIdentifier())
    .collect();
    if pids.is_empty() {
        return None;
    }
    // On screen only, without desktop elements.
    const OPTIONS: u32 = (1 << 0) | (1 << 4);
    // SAFETY: a Copy function: the array is ours, toll-free bridged to NSArray of NSDictionary.
    let windows: Retained<NSArray<NSDictionary<NSString, AnyObject>>> = unsafe {
        let raw = CGWindowListCopyWindowInfo(OPTIONS, 0);
        Retained::from_raw(raw.cast())?
    };
    let number = |dict: &NSDictionary<NSString, AnyObject>, key: &str| -> Option<f64> {
        let value = dict.objectForKey(&NSString::from_str(key))?;
        value.downcast::<NSNumber>().ok().map(|n| n.doubleValue())
    };
    windows.iter().find_map(|window| {
        let pid = number(&window, "kCGWindowOwnerPID")? as i32;
        if !pids.contains(&pid) || number(&window, "kCGWindowLayer")? != 0.0 {
            return None;
        }
        let bounds = window.objectForKey(&NSString::from_str("kCGWindowBounds"))?;
        let bounds = bounds.downcast::<NSDictionary>().ok()?;
        // SAFETY: the bounds dictionary holds NSNumbers under X, Y, Width and Height.
        let bounds: &NSDictionary<NSString, AnyObject> = unsafe { &*(Retained::as_ptr(&bounds).cast()) };
        let rect = NSRect::new(
            NSPoint::new(number(bounds, "X")?, number(bounds, "Y")?),
            NSSize::new(number(bounds, "Width")?, number(bounds, "Height")?),
        );
        (rect.size.width > 200.0 && rect.size.height > 200.0).then_some(rect)
    })
}

// --- The panel -----------------------------------------------------------------------------------------------------

/// What the draggable icon carries: this app's bundle.
struct DragIvars {
    bundle: Retained<NSURL>,
}

define_class!(
    // SAFETY: NSImageView may be subclassed; the overrides keep its contract.
    #[unsafe(super(NSImageView, objc2_app_kit::NSControl, NSView, objc2_app_kit::NSResponder, objc2_foundation::NSObject))]
    #[thread_kind = MainThreadOnly]
    #[name = "MewlaPermissionDragView"]
    #[ivars = DragIvars]
    struct DragView;

    impl DragView {
        #[unsafe(method(mouseDown:))]
        fn mouse_down(&self, event: &NSEvent) {
            self.begin_drag(event);
        }

        #[unsafe(method(acceptsFirstMouse:))]
        fn accepts_first_mouse(&self, _event: Option<&NSEvent>) -> bool {
            true
        }

        #[unsafe(method(mouseDownCanMoveWindow))]
        fn mouse_down_can_move_window(&self) -> bool {
            false
        }
    }

    unsafe impl NSObjectProtocol for DragView {}

    unsafe impl NSDraggingSource for DragView {
        #[unsafe(method(draggingSession:sourceOperationMaskForDraggingContext:))]
        fn source_operation_mask(&self, _session: &NSDraggingSession, _context: NSDraggingContext) -> NSDragOperation {
            NSDragOperation::Copy | NSDragOperation::Generic | NSDragOperation::Link
        }
    }
);

impl DragView {
    fn new(mtm: MainThreadMarker, frame: NSRect, bundle: Retained<NSURL>, icon: &NSImage) -> Retained<DragView> {
        let this = DragView::alloc(mtm).set_ivars(DragIvars { bundle });
        // SAFETY: NSImageView's designated initializer.
        let this: Retained<DragView> = unsafe { msg_send![super(this), initWithFrame: frame] };
        this.setImage(Some(icon));
        this
    }

    /// The app's bundle as a file the list takes, its icon under the pointer.
    fn begin_drag(&self, event: &NSEvent) {
        let url = &self.ivars().bundle;
        let item = NSDraggingItem::initWithPasteboardWriter(NSDraggingItem::alloc(), ProtocolObject::from_ref(&**url));
        if let Some(image) = self.image() {
            // SAFETY: an NSImage is a valid dragging image.
            unsafe { item.setDraggingFrame_contents(self.bounds(), Some(&*image as &AnyObject)) };
        }
        let items = NSArray::from_retained_slice(&[item]);
        self.beginDraggingSessionWithItems_event_source(&items, event, ProtocolObject::from_ref(self));
    }
}

/// The panel open now, on the main thread.
struct Guide {
    permission: Permission,
    panel: Retained<NSPanel>,
    alive: Arc<AtomicBool>,
    started: Instant,
    /// When System Settings' window was last seen; None before it first is.
    seen: Option<Instant>,
    placed: Option<NSPoint>,
}

thread_local! {
    static GUIDE: RefCell<Option<Guide>> = const { RefCell::new(None) };
}

const PANEL: NSSize = NSSize { width: 248.0, height: 156.0 };
const ICON: f64 = 64.0;
/// How long the panel waits for a grant at most.
const LIMIT: Duration = Duration::from_secs(15 * 60);

/// Opens System Settings on the permission's list and the panel beside it (replacing any panel open). Main thread.
pub fn show(mtm: MainThreadMarker, permission: Permission) {
    close();
    let bundle = NSBundle::mainBundle();
    let bundle_url = bundle.bundleURL();
    let name = bundle
        .objectForInfoDictionaryKey(&NSString::from_str("CFBundleName"))
        .and_then(|value| value.downcast::<NSString>().ok())
        .map(|name| name.to_string())
        .unwrap_or_else(|| "Mewla".into());
    let start = listed(permission);
    log::warn!("permission panel: {permission:?} from {start:?} ({})", bundle_url.path().map(|p| p.to_string()).unwrap_or_default());

    if let Some(url) = NSURL::URLWithString(&NSString::from_str(permission.settings_url())) {
        NSWorkspace::sharedWorkspace().openURL(&url);
    }

    let style = NSWindowStyleMask::Titled
        | NSWindowStyleMask::Closable
        | NSWindowStyleMask::NonactivatingPanel
        | NSWindowStyleMask::FullSizeContentView
        | NSWindowStyleMask::UtilityWindow;
    // SAFETY: NSPanel's designated initializer; the panel is kept (released_when_closed off) until replaced.
    let panel = unsafe {
        let panel = NSPanel::initWithContentRect_styleMask_backing_defer(
            NSPanel::alloc(mtm),
            NSRect::new(NSPoint::new(0.0, 0.0), PANEL),
            style,
            NSBackingStoreType::Buffered,
            false,
        );
        panel.setReleasedWhenClosed(false);
        panel
    };
    panel.setFloatingPanel(true);
    panel.setHidesOnDeactivate(false);
    panel.setBecomesKeyOnlyIfNeeded(true);
    panel.setTitleVisibility(NSWindowTitleVisibility::Hidden);
    panel.setTitlebarAppearsTransparent(true);
    panel.setMovableByWindowBackground(true);
    panel.setTitle(&NSString::from_str(&name));

    let content = panel.contentView().expect("a panel has a content view");
    let icon = NSWorkspace::sharedWorkspace().iconForFile(&bundle_url.path().unwrap_or_else(|| NSString::from_str("")));
    let drag = DragView::new(
        mtm,
        NSRect::new(NSPoint::new((PANEL.width - ICON) / 2.0, 54.0), NSSize::new(ICON, ICON)),
        bundle_url.clone(),
        &icon,
    );
    drag.setAccessibilityLabel(Some(&NSString::from_str(&format!("{name}, drag into the list"))));
    content.addSubview(&drag);

    let label = |text: &str, y: f64, size: f64, bold: bool| {
        let field = NSTextField::labelWithString(&NSString::from_str(text), mtm);
        // SAFETY: the system font at a size and weight.
        let font = unsafe {
            if bold {
                NSFont::systemFontOfSize_weight(size, objc2_app_kit::NSFontWeightSemibold)
            } else {
                NSFont::systemFontOfSize(size)
            }
        };
        field.setFont(Some(&font));
        field.setAlignment(NSTextAlignment::Center);
        field.setFrame(NSRect::new(NSPoint::new(12.0, y), NSSize::new(PANEL.width - 24.0, size + 6.0)));
        content.addSubview(&field);
    };
    label(&name, 34.0, 11.0, false);
    label(&instruction(&name, start), 10.0, 13.0, true);

    let screen = NSScreen::mainScreen(mtm).map(|s| s.visibleFrame());
    if let Some(screen) = screen {
        // Before System Settings' window is known: the right edge of the screen.
        panel.setFrameOrigin(NSPoint::new(
            screen.origin.x + screen.size.width - PANEL.width - 24.0,
            screen.origin.y + screen.size.height - PANEL.height - 80.0,
        ));
    }
    panel.orderFrontRegardless();

    let alive = Arc::new(AtomicBool::new(true));
    GUIDE.with(|guide| {
        *guide.borrow_mut() = Some(Guide { permission, panel, alive: alive.clone(), started: Instant::now(), seen: None, placed: None })
    });
    // Four times a second, on the main thread, until the panel closes.
    std::thread::spawn(move || {
        while alive.load(Ordering::Relaxed) {
            std::thread::sleep(Duration::from_millis(250));
            dispatch2::DispatchQueue::main().exec_async(tick);
        }
    });
}

/// Closes the panel open now, if any. Main thread.
pub fn close() {
    GUIDE.with(|guide| {
        if let Some(guide) = guide.borrow_mut().take() {
            guide.alive.store(false, Ordering::Relaxed);
            guide.panel.orderOut(None);
        }
    });
}

/// The permission the panel open now asks for.
pub fn open_for() -> Option<Permission> {
    GUIDE.with(|guide| guide.borrow().as_ref().map(|g| g.permission))
}

/// The grant counts (the daemon's reading, from a fresh process): the panel asking for it closes and Mewla comes to the
/// front. This app's own reading (`tick`) can lag behind: TCC may answer a running process from what it cached.
pub fn given(mtm: MainThreadMarker, permission: Permission) {
    if open_for() != Some(permission) {
        return;
    }
    log::warn!("permission panel: {permission:?} given; back to Mewla");
    close();
    #[allow(deprecated)]
    NSApplication::sharedApplication(mtm).activateIgnoringOtherApps(true);
}

fn tick() {
    let Some(mtm) = MainThreadMarker::new() else { return };
    enum Next {
        Stay,
        Close,
        Granted,
    }
    let next = GUIDE.with(|cell| {
        let mut cell = cell.borrow_mut();
        let Some(guide) = cell.as_mut() else { return Next::Stay };
        if !guide.alive.load(Ordering::Relaxed) {
            return Next::Stay;
        }
        // Closed with its close button.
        if !guide.panel.isVisible() {
            return Next::Close;
        }
        if listed(guide.permission) == Listed::Granted {
            return Next::Granted;
        }
        if guide.started.elapsed() > LIMIT {
            return Next::Close;
        }
        match settings_window() {
            Some(window) => {
                guide.seen = Some(Instant::now());
                let main_height = NSScreen::screens(mtm).firstObject().map(|s| s.frame().size.height).unwrap_or(0.0);
                let window = cocoa_rect(window, main_height);
                let screen = NSScreen::mainScreen(mtm).map(|s| s.visibleFrame()).unwrap_or(window);
                let at = place_beside(window, PANEL, screen);
                if guide.placed.is_none_or(|p| (p.x - at.x).abs() > 0.5 || (p.y - at.y).abs() > 0.5) {
                    guide.panel.setFrameOrigin(at);
                    guide.placed = Some(at);
                }
                Next::Stay
            }
            // System Settings closed (or gone to another Space) for two seconds after it was there.
            None if guide.seen.is_some_and(|seen| seen.elapsed() > Duration::from_secs(2)) => Next::Close,
            None => Next::Stay,
        }
    });
    match next {
        Next::Stay => {}
        Next::Close => close(),
        Next::Granted => {
            if let Some(permission) = open_for() {
                given(mtm, permission);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_panel_sits_right_of_system_settings_or_left_without_room() {
        let screen = NSRect::new(NSPoint::new(0.0, 0.0), NSSize::new(1512.0, 944.0));
        let settings = NSRect::new(NSPoint::new(300.0, 200.0), NSSize::new(715.0, 650.0));
        let at = place_beside(settings, PANEL, screen);
        assert_eq!(at.x, 300.0 + 715.0 + 12.0);
        assert_eq!(at.y, 200.0 + 650.0 - 64.0 - PANEL.height);
        let wide = NSRect::new(NSPoint::new(700.0, 200.0), NSSize::new(715.0, 650.0));
        assert_eq!(place_beside(wide, PANEL, screen).x, 700.0 - 12.0 - PANEL.width);
    }

    #[test]
    fn window_server_frames_turn_into_cocoa_frames() {
        let cg = NSRect::new(NSPoint::new(10.0, 40.0), NSSize::new(700.0, 600.0));
        let cocoa = cocoa_rect(cg, 944.0);
        assert_eq!(cocoa.origin.y, 944.0 - 40.0 - 600.0);
        assert_eq!(cocoa.origin.x, 10.0);
    }

    #[test]
    fn the_line_says_drag_when_mewla_is_not_in_the_list_and_turn_on_when_it_is() {
        assert_eq!(instruction("Mewla", Listed::Missing), "Drag Mewla into the list");
        assert_eq!(instruction("Mewla", Listed::Off), "Turn on Mewla in the list");
        assert_eq!(Permission::from_id("accessibility"), Some(Permission::Accessibility));
        assert_eq!(Permission::from_id("chrome"), None);
    }

    #[test]
    fn only_a_row_that_does_not_count_is_cleared() {
        assert!(clears(Some(Listed::Missing), Some("missing")));
        assert!(clears(Some(Listed::Missing), None));
        // The daemon reads off from an older Mewla's path row while Mewla.app's own row does not count: cleared.
        assert!(clears(Some(Listed::Missing), Some("off")));
        // Mewla.app's row switched off: left for the person to turn on.
        assert!(!clears(Some(Listed::Off), Some("off")));
        assert!(!clears(Some(Listed::Off), Some("missing")));
        // Counted already, or given and waiting for the restart: never.
        assert!(!clears(Some(Listed::Granted), None));
        assert!(!clears(Some(Listed::Missing), Some("granted")));
        assert!(!clears(Some(Listed::Missing), Some("restart")));
        // No exact answer (no TCC preflight): nothing is cleared.
        assert!(!clears(None, Some("missing")));
    }
}
