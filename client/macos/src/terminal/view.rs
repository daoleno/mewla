//! The terminal view on the Mac (client/ui/terminal/contract.ts), the AppKit twin of client/ios's TerminalView.swift:
//! libghostty-vt's cells (through the phones' packer, vt.rs) drawn with CoreText in the bundled Maple Mono NF on an
//! exact grid of whole-pixel cells: each cell's glyph at its column, CJK and emoji centred in their two cells, box
//! drawing, blocks, braille and Powerline drawn from TerminalGlyphs so lines meet, every attribute (faint, italic,
//! five underline styles in their own color, strikethrough, overline, inverse) and the program's cursor shape.
//!
//! The keyboard: keys that are not text and Control combinations are encoded by libghostty-vt for the program's
//! modes; text comes through AppKit's text input (`NSTextInputClient`: an input method's composition drawn at the
//! cursor, dead keys, only committed text reported). ⌘ stays the menus': ⌘V pastes (framed for bracketed paste).
//!
//! Above the live screen it draws the history the page hands it (`history`, styled rows rendered by a second VT);
//! the trackpad and wheel scroll through it with AppKit's own precise deltas and momentum. With no history, or on the
//! alternate screen, a scroll is reported in lines (`scroll`), which the page makes wheel presses or cursor keys for
//! the program. A click reports the cell and the text under it (`tapCell`); text a program copies (OSC 52) is
//! reported (`copy`).

use std::cell::{Cell as StdCell, RefCell};
use std::collections::HashMap;
use std::rc::Rc;

use nsh_platform_macos::ComponentContext;
use objc2::rc::Retained;
use objc2::runtime::{AnyObject, Sel};
use objc2::{AnyThread, DefinedClass, MainThreadMarker, MainThreadOnly, define_class, msg_send, sel};
use objc2_app_kit::{
    NSCursor, NSEvent, NSEventModifierFlags, NSEventPhase, NSGraphicsContext, NSMenu, NSMenuItem, NSPasteboard,
    NSPasteboardTypeString, NSResponder, NSTextInputClient, NSTrackingArea, NSTrackingAreaOptions, NSView,
};
use objc2_core_foundation::{CFAttributedString, CFRetained, CFString, CGAffineTransform, CGFloat, CGPoint, CGRect, CGSize};
use objc2_core_graphics::{CGContext, CGLineCap};
use objc2_core_text::{
    CTFont, CTFontOrientation, CTLine, kCTFontAttributeName, kCTForegroundColorFromContextAttributeName,
    kCTLigatureAttributeName,
};
use objc2_foundation::{
    NSArray, NSAttributedString, NSAttributedStringKey, NSDictionary, NSNumber, NSObjectProtocol, NSPoint, NSRange,
    NSRangePointer, NSRect, NSSize, NSString,
};
use serde_json::json;

use super::fonts::Faces;
use super::keys::{Mods, ghostty_key, ghostty_mods, is_special, key_bytes};
use super::links::url_at;
use super::vt::{self, Frame, Run, Vt, css_color, spans, glyph_is_drawn, glyph_ops, glyph_thickness, layout, palette256};

/// History rows are rendered a page at a time through the scratch VT.
const PAGE: usize = 64;

/// What a run draws, laid out once.
enum Item {
    Text { line: CFRetained<CTLine>, x: CGFloat },
    /// Not one cell wide (CJK, emoji, a fallback face's symbol): centred in `boxw`, scaled down to fit.
    Fitted { line: CFRetained<CTLine>, x: CGFloat, width: CGFloat, boxw: CGFloat },
    Glyph { cp: u32, x: CGFloat },
}

/// What a run's items depend on: its text, face and place in the row (and a flag's second half). Rows that only moved
/// (scrolled output) find theirs made already.
#[derive(Hash, PartialEq, Eq)]
struct ItemKey {
    col: usize,
    cells: usize,
    flags: i32,
    units: Vec<u16>,
    next: Vec<u16>,
}

/// Runs' items kept across rows; emptied when full (a few screens' worth) and when the face or the cell changes.
const ITEM_CACHE_MAX: usize = 16_384;

/// A run and its items, made on first draw.
struct Cell {
    run: Run,
    items: RefCell<Option<Rc<Vec<Item>>>>,
}

#[derive(Clone)]
struct Line {
    cells: Rc<Vec<Cell>>,
    /// The row goes on in the next one (a selection's copy joins them).
    wrapped: bool,
}

impl Line {
    fn empty() -> Line {
        Line { cells: Rc::new(Vec::new()), wrapped: false }
    }
    fn of(runs: Vec<Run>, wrapped: bool) -> Line {
        Line { cells: Rc::new(runs.into_iter().map(|run| Cell { run, items: RefCell::new(None) }).collect()), wrapped }
    }
}

/// RGBA in 0…1.
#[derive(Clone, Copy)]
struct Rgba(CGFloat, CGFloat, CGFloat, CGFloat);

impl Rgba {
    fn of(rgb: u32, alpha: CGFloat) -> Rgba {
        let c = |shift: u32| ((rgb >> shift) & 0xff) as CGFloat / 255.0;
        Rgba(c(16), c(8), c(0), alpha)
    }
    fn fill(self, cg: &CGContext) {
        CGContext::set_rgb_fill_color(Some(cg), self.0, self.1, self.2, self.3);
    }
    fn stroke(self, cg: &CGContext) {
        CGContext::set_rgb_stroke_color(Some(cg), self.0, self.1, self.2, self.3);
    }
}

pub struct State {
    ctx: ComponentContext,
    vt: Option<Vt>,
    /// Output that arrived before the first layout made the VT.
    pending: Vec<String>,
    failed: bool,
    cols: usize,
    rows: usize,
    grid: Vec<Line>,
    cursor_col: usize,
    cursor_row: usize,
    cursor_visible: bool,
    cursor_style: i32,
    cursor_blinks: bool,
    blink_on: bool,
    /// A blink tick is on its way (one at a time, however often output restarts the blink).
    blink_pending: bool,
    /// The last output or key: the cursor stays lit for a blink's length after it.
    blink_held: std::time::Instant,
    needs_pull: bool,
    needs_full: bool,
    /// Live rows the VT changed since they were last marked for drawing, and whether all of them did.
    changed: Vec<usize>,
    changed_all: bool,
    /// A `refresh` is on its way.
    refresh_queued: bool,
    /// The content line the cursor was last drawn on (its old place is drawn again when it moves).
    cursor_drawn: Option<isize>,
    focused: bool,

    // The font: cells are whole pixels and the text size is set so one advance is exactly one cell.
    points: CGFloat,
    pixel_scale: CGFloat,
    faces: Faces,
    cell_w: CGFloat,
    cell_h: CGFloat,
    baseline: CGFloat,
    thickness: CGFloat,
    /// Whether bundled face `i` has each BMP character (then its advance is one cell).
    in_font: [HashMap<u16, bool>; 4],
    item_cache: HashMap<ItemKey, Rc<Vec<Item>>>,
    glyphs: HashMap<u32, Rc<Vec<vt::GlyphOp>>>,

    background: u32,
    foreground: u32,
    cursor_color: u32,
    selection: (u32, CGFloat),
    vt_palette: Option<Vec<u32>>,

    marked: String,

    // The history above the live screen: its styled rows as the page sent them and, as long, the ones drawn as
    // lines (None until first needed), oldest first. Content line i is history row i below H, live row i - H above.
    history_ansi: Vec<String>,
    history: Vec<Option<Line>>,
    history_cols: usize,
    alternate: bool,
    scratch: Option<Vt>,
    /// Points the content is scrolled down from the live screen (0 = the live screen at the bottom).
    scroll_y: CGFloat,
    /// A reported scroll's part of a line not sent yet.
    wheel_remainder: CGFloat,
    mark_row: isize,
    reported_top: isize,
    reported_bottom: bool,
    /// Where the left button went down (a click is a press and release on one cell).
    press: Option<(usize, isize)>,
    /// The button moved since it went down: a selection, not a click.
    dragged: bool,
    // The selection in content lines and columns, inclusive; a drag extends it from what it started on (the cell,
    // the word of a double click, the line of a triple click: `sel_mode` 1, 2, 3).
    sel_from: Option<(isize, usize)>,
    sel_to: Option<(isize, usize)>,
    sel_anchor: Option<((isize, usize), (isize, usize))>,
    sel_mode: u8,
    /// The pointer is over a link (its shape is the pointing hand).
    over_link: bool,
}

/// A cursor blink's half period (seconds).
const BLINK_S: f64 = 0.6;
/// Points above and below a changed row drawn with it (`line_rects`).
const OVERDRAW: CGFloat = 2.0;

pub struct Ivars {
    state: RefCell<State>,
    /// What to tell the page once the state is let go (an event is never sent from inside a borrow).
    outbox: RefCell<Vec<(&'static str, serde_json::Value)>>,
    drawing: StdCell<bool>,
    /// Draw times (ms) of frames since the last report, and whether each drew the whole view (DESIGN.md §4).
    times: RefCell<Vec<(f64, bool, usize)>>,
    /// How far this view got, once each in the app's log (a run on a machine nobody watches reads it there):
    /// bit 0 the grid, 1 the first output, 2 the first paint.
    told: StdCell<u8>,
}

define_class!(
    // SAFETY: NSView may be subclassed; the overrides keep its contract (flipped, opaque, drawn in drawRect:).
    #[unsafe(super(NSView, NSResponder, objc2_foundation::NSObject))]
    #[thread_kind = MainThreadOnly]
    #[name = "MewlaTerminalView"]
    #[ivars = Ivars]
    pub struct TermView;

    impl TermView {
        #[unsafe(method(isFlipped))]
        fn is_flipped(&self) -> bool {
            true
        }

        #[unsafe(method(isOpaque))]
        fn is_opaque(&self) -> bool {
            true
        }

        #[unsafe(method(acceptsFirstResponder))]
        fn accepts_first_responder(&self) -> bool {
            true
        }

        #[unsafe(method(acceptsFirstMouse:))]
        fn accepts_first_mouse(&self, _event: Option<&NSEvent>) -> bool {
            true
        }

        #[unsafe(method(becomeFirstResponder))]
        fn become_first_responder(&self) -> bool {
            self.focus_changed(true);
            true
        }

        #[unsafe(method(resignFirstResponder))]
        fn resign_first_responder(&self) -> bool {
            self.focus_changed(false);
            true
        }

        #[unsafe(method(mouseDown:))]
        fn mouse_down(&self, event: &NSEvent) {
            self.focus();
            self.press(event);
            self.flush();
        }

        #[unsafe(method(mouseDragged:))]
        fn mouse_dragged(&self, event: &NSEvent) {
            self.drag(event);
            self.flush();
        }

        #[unsafe(method(mouseUp:))]
        fn mouse_up(&self, event: &NSEvent) {
            let (press, dragged) = {
                let mut s = self.ivars().state.borrow_mut();
                (s.press.take(), std::mem::replace(&mut s.dragged, false))
            };
            let at = self.cell_at(event);
            if !dragged && event.clickCount() == 1 && press.is_some() && press == at {
                if let Some((col, line)) = at {
                    self.tap(col, line);
                }
            }
            self.flush();
        }

        #[unsafe(method(mouseMoved:))]
        fn mouse_moved(&self, event: &NSEvent) {
            self.hover(event);
        }

        #[unsafe(method(cursorUpdate:))]
        fn cursor_update(&self, event: &NSEvent) {
            self.hover(event);
        }

        #[unsafe(method_id(menuForEvent:))]
        fn menu_for_event(&self, _event: &NSEvent) -> Option<Retained<NSMenu>> {
            Some(self.context_menu())
        }

        #[unsafe(method(validateMenuItem:))]
        fn validate_menu_item(&self, item: &NSMenuItem) -> bool {
            // Copy only with a selection.
            item.action() != Some(sel!(copy:)) || self.has_selection()
        }

        #[unsafe(method(selectAll:))]
        fn select_all_action(&self, _sender: Option<&AnyObject>) {
            self.select_all();
        }

        #[unsafe(method(scrollWheel:))]
        fn scroll_wheel(&self, event: &NSEvent) {
            self.wheel(event);
            self.flush();
        }

        #[unsafe(method(keyDown:))]
        fn key_down(&self, event: &NSEvent) {
            if self.key(event) {
                self.flush();
                return;
            }
            let events = NSArray::from_slice(&[event]);
            self.interpretKeyEvents(&events);
            self.flush();
        }

        #[unsafe(method(setFrameSize:))]
        fn set_frame_size(&self, size: NSSize) {
            let _: () = unsafe { msg_send![super(self), setFrameSize: size] };
            self.relayout();
            self.flush();
        }

        #[unsafe(method(viewDidMoveToWindow))]
        fn view_did_move_to_window(&self) {
            if self.trackingAreas().is_empty() {
                // The pointer's shape over the grid (I-beam, a hand over a link), wherever the view is.
                let options = NSTrackingAreaOptions::MouseMoved
                    | NSTrackingAreaOptions::CursorUpdate
                    | NSTrackingAreaOptions::ActiveInKeyWindow
                    | NSTrackingAreaOptions::InVisibleRect;
                let area = unsafe {
                    NSTrackingArea::initWithRect_options_owner_userInfo(NSTrackingArea::alloc(), self.bounds(), options, Some(self), None)
                };
                self.addTrackingArea(&area);
            }
            self.rescale();
            self.restart_blink();
            self.flush();
        }

        #[unsafe(method(viewDidChangeBackingProperties))]
        fn view_did_change_backing_properties(&self) {
            self.rescale();
            self.flush();
        }

        #[unsafe(method(drawRect:))]
        fn draw_rect(&self, dirty: NSRect) {
            let Some(graphics) = NSGraphicsContext::currentContext() else { return };
            let cg = graphics.CGContext();
            self.ivars().drawing.set(true);
            let started = std::time::Instant::now();
            let (drawn, visible) = self.paint(&cg, dirty);
            self.timed(started.elapsed().as_secs_f64() * 1000.0, drawn, visible);
            self.tell_painted();
            self.ivars().drawing.set(false);
            self.settle_changes(dirty);
            self.flush();
        }

        #[unsafe(method(copy:))]
        fn copy(&self, _sender: Option<&AnyObject>) {
            let text = self.selected_text();
            if text.is_empty() {
                return;
            }
            let pasteboard = NSPasteboard::generalPasteboard();
            pasteboard.clearContents();
            unsafe { pasteboard.setString_forType(&NSString::from_str(&text), NSPasteboardTypeString) };
        }

        #[unsafe(method(paste:))]
        fn paste_action(&self, _sender: Option<&AnyObject>) {
            let pasteboard = NSPasteboard::generalPasteboard();
            if let Some(text) = unsafe { pasteboard.stringForType(NSPasteboardTypeString) } {
                self.paste(&text.to_string());
            }
            self.flush();
        }

        #[unsafe(method(blinkTick:))]
        fn blink_tick(&self, _sender: Option<&AnyObject>) {
            let mut s = self.ivars().state.borrow_mut();
            s.blink_pending = false;
            if !s.cursor_blinks || self.window().is_none() {
                s.blink_on = true;
                return;
            }
            let held = s.blink_held.elapsed().as_secs_f64();
            if held < BLINK_S {
                // Output or a key since the last tick: lit until a blink's length after it.
                drop(s);
                self.schedule_blink(BLINK_S - held);
                return;
            }
            s.blink_on = !s.blink_on;
            drop(s);
            self.invalidate_cursor();
            self.schedule_blink(BLINK_S);
        }

        /// The changed rows marked for drawing, once per run loop pass however many writes came (`queue_refresh`).
        #[unsafe(method(refreshChanged:))]
        fn refresh_changed(&self, _sender: Option<&AnyObject>) {
            self.refresh();
        }
    }

    unsafe impl NSObjectProtocol for TermView {}

    unsafe impl NSTextInputClient for TermView {
        #[unsafe(method(insertText:replacementRange:))]
        unsafe fn insert_text(&self, string: &AnyObject, _replacement: NSRange) {
            let text = text_of(string);
            let had = !self.ivars().state.borrow().marked.is_empty();
            if had {
                self.ivars().state.borrow_mut().marked.clear();
                self.invalidate_cursor();
            }
            if !text.is_empty() {
                self.send(&text.replace('\n', "\r"));
            }
            self.flush();
        }

        #[unsafe(method(doCommandBySelector:))]
        unsafe fn do_command_by_selector(&self, _selector: Sel) {
            // The keys that make commands are taken in keyDown:; nothing beeps for the rest.
        }

        #[unsafe(method(setMarkedText:selectedRange:replacementRange:))]
        unsafe fn set_marked_text(&self, string: &AnyObject, _selected: NSRange, _replacement: NSRange) {
            self.ivars().state.borrow_mut().marked = text_of(string);
            self.invalidate_cursor();
        }

        #[unsafe(method(unmarkText))]
        fn unmark_text(&self) {
            self.ivars().state.borrow_mut().marked.clear();
            self.invalidate_cursor();
        }

        #[unsafe(method(selectedRange))]
        fn selected_range(&self) -> NSRange {
            NSRange::new(usize::MAX as _, 0)
        }

        #[unsafe(method(markedRange))]
        fn marked_range(&self) -> NSRange {
            let n = self.ivars().state.borrow().marked.encode_utf16().count();
            if n == 0 { NSRange::new(usize::MAX as _, 0) } else { NSRange::new(0, n) }
        }

        #[unsafe(method(hasMarkedText))]
        fn has_marked_text(&self) -> bool {
            !self.ivars().state.borrow().marked.is_empty()
        }

        #[unsafe(method_id(attributedSubstringForProposedRange:actualRange:))]
        unsafe fn attributed_substring(&self, _range: NSRange, _actual: NSRangePointer) -> Option<Retained<NSAttributedString>> {
            None
        }

        #[unsafe(method_id(validAttributesForMarkedText))]
        fn valid_attributes_for_marked_text(&self) -> Retained<NSArray<NSAttributedStringKey>> {
            NSArray::new()
        }

        #[unsafe(method(firstRectForCharacterRange:actualRange:))]
        unsafe fn first_rect_for_character_range(&self, _range: NSRange, _actual: NSRangePointer) -> NSRect {
            let rect = self.cursor_rect();
            let in_window = self.convertRect_toView(rect, None);
            match self.window() {
                Some(window) => window.convertRectToScreen(in_window),
                None => in_window,
            }
        }

        #[unsafe(method(characterIndexForPoint:))]
        fn character_index_for_point(&self, _point: NSPoint) -> usize {
            usize::MAX
        }
    }
);

/// The text an input method passes: an NSString or an NSAttributedString.
fn text_of(string: &AnyObject) -> String {
    if let Some(attributed) = string.downcast_ref::<NSAttributedString>() {
        return attributed.string().to_string();
    }
    if let Some(plain) = string.downcast_ref::<NSString>() {
        return plain.to_string();
    }
    String::new()
}

fn utf16(units: &[u16]) -> String {
    String::from_utf16_lossy(units)
}

/// A CoreFoundation object as the Objective-C object it is toll-free bridged to.
unsafe fn cf_object<T>(object: &T) -> &AnyObject {
    unsafe { &*(object as *const T as *const AnyObject) }
}

/// A CTLine of `text` in `font`, drawn in the context's fill color, no ligatures.
fn ct_line(text: &str, font: &CTFont) -> CFRetained<CTLine> {
    let string = NSString::from_str(text);
    // SAFETY: the CoreText attribute names are CFStrings, toll-free bridged to NSString; CTFont to its object.
    let keys: [&NSString; 3] = unsafe {
        [
            &*(kCTFontAttributeName as *const CFString as *const NSString),
            &*(kCTForegroundColorFromContextAttributeName as *const CFString as *const NSString),
            &*(kCTLigatureAttributeName as *const CFString as *const NSString),
        ]
    };
    let yes = NSNumber::new_bool(true);
    let none = NSNumber::new_i32(0);
    let values: [&AnyObject; 3] = [unsafe { cf_object(font) }, &yes, &none];
    let attributes: Retained<NSDictionary<NSString, AnyObject>> = NSDictionary::from_slices(&keys, &values);
    let attributed = unsafe { NSAttributedString::initWithString_attributes(NSAttributedString::alloc(), &string, Some(&attributes)) };
    // SAFETY: NSAttributedString and CFAttributedString are toll-free bridged.
    let cf: &CFAttributedString = unsafe { &*(&*attributed as *const NSAttributedString as *const CFAttributedString) };
    unsafe { CTLine::with_attributed_string(cf) }
}

fn line_width(line: &CTLine) -> CGFloat {
    unsafe { line.typographic_bounds(std::ptr::null_mut(), std::ptr::null_mut(), std::ptr::null_mut()) as CGFloat }
}

fn advance(unit: u16, font: &CTFont) -> CGFloat {
    let mut u = unit;
    let mut glyph: u16 = 0;
    let mut size = CGSize::new(0.0, 0.0);
    unsafe {
        font.glyphs_for_characters(std::ptr::NonNull::from(&mut u), std::ptr::NonNull::from(&mut glyph), 1);
        font.advances_for_glyphs(CTFontOrientation::Horizontal, std::ptr::NonNull::from(&mut glyph), &mut size, 1);
    }
    if size.width > 0.0 { size.width } else { (unsafe { font.size() }) * 0.6 }
}

impl TermView {
    pub fn new(mtm: MainThreadMarker, ctx: ComponentContext) -> Retained<Self> {
        let faces = Faces::at(13.0);
        let state = State {
            ctx,
            vt: None,
            pending: Vec::new(),
            failed: false,
            cols: 0,
            rows: 0,
            grid: Vec::new(),
            cursor_col: 0,
            cursor_row: 0,
            cursor_visible: true,
            cursor_style: 1,
            cursor_blinks: false,
            blink_on: true,
            blink_pending: false,
            blink_held: std::time::Instant::now(),
            needs_pull: false,
            needs_full: false,
            changed: Vec::new(),
            changed_all: false,
            refresh_queued: false,
            cursor_drawn: None,
            focused: false,
            points: 13.0,
            pixel_scale: 2.0,
            faces,
            cell_w: 8.0,
            cell_h: 16.0,
            baseline: 12.0,
            thickness: 1.0,
            in_font: Default::default(),
            item_cache: HashMap::new(),
            glyphs: HashMap::new(),
            background: 0x141210,
            foreground: 0xF4F0EA,
            cursor_color: 0xF4F0EA,
            selection: (0x3D7EFF, 0.33),
            vt_palette: None,
            marked: String::new(),
            history_ansi: Vec::new(),
            history: Vec::new(),
            history_cols: 0,
            alternate: false,
            scratch: None,
            scroll_y: 0.0,
            wheel_remainder: 0.0,
            mark_row: -1,
            reported_top: -1,
            reported_bottom: true,
            press: None,
            dragged: false,
            sel_from: None,
            sel_to: None,
            sel_anchor: None,
            sel_mode: 0,
            over_link: false,
        };
        let this = Self::alloc(mtm).set_ivars(Ivars { state: RefCell::new(state), outbox: RefCell::new(Vec::new()), drawing: StdCell::new(false), times: RefCell::new(Vec::new()), told: StdCell::new(0) });
        let frame = NSRect::new(NSPoint::new(0.0, 0.0), NSSize::new(100.0, 100.0));
        let view: Retained<Self> = unsafe { msg_send![super(this), initWithFrame: frame] };
        view.apply_font(13.0);
        view
    }

    /// Logs a milestone the first time only (`told` bit).
    fn tell_once(&self, bit: u8, line: impl FnOnce() -> String) {
        let told = self.ivars().told.get();
        if told & (1 << bit) == 0 {
            self.ivars().told.set(told | (1 << bit));
            log::info!("{}", line());
        }
    }

    /// The first paint with text on it: how many cells carry it.
    fn tell_painted(&self) {
        if self.ivars().told.get() & 4 != 0 {
            return;
        }
        let cells: usize = {
            let s = self.ivars().state.borrow();
            s.grid
                .iter()
                .flat_map(|line| line.cells.iter())
                .filter(|cell| cell.run.units.iter().any(|&u| u != 0x20 && u != 0))
                .map(|cell| cell.run.cells)
                .sum()
        };
        if cells > 0 {
            self.tell_once(2, || format!("terminal: first paint ({cells} non-blank cells)"));
        }
    }

    /// Every 120 frames, their median and p95 draw time in the log, and how many drew the whole view.
    /// `drawn` of the `visible` rows were drawn: a frame is whole when every visible row was.
    fn timed(&self, ms: f64, drawn: usize, visible: usize) {
        let full = visible > 0 && drawn >= visible;
        let mut times = self.ivars().times.borrow_mut();
        times.push((ms, full, drawn));
        if times.len() < 120 {
            return;
        }
        let sorted = |whole_only: bool| {
            let mut ms: Vec<f64> = times.iter().filter(|t| !whole_only || t.1).map(|t| t.0).collect();
            ms.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
            ms
        };
        let at = |ms: &[f64], p: f64| if ms.is_empty() { 0.0 } else { ms[((ms.len() as f64 - 1.0) * p).round() as usize] };
        let (all, whole) = (sorted(false), sorted(true));
        let rows = times.iter().map(|t| t.2).sum::<usize>() as f64 / times.len() as f64;
        let s = self.ivars().state.borrow();
        log::info!(
            "terminal view: {} frames of {}x{} cells ({:.1} rows drawn each): median {:.2} ms, p95 {:.2} ms, max {:.2} ms; {} whole: median {:.2} ms, p95 {:.2} ms",
            all.len(), s.cols, s.rows, rows, at(&all, 0.5), at(&all, 0.95), at(&all, 1.0), whole.len(), at(&whole, 0.5), at(&whole, 0.95)
        );
        times.clear();
    }

    // MARK: Events to the page

    fn emit(&self, name: &'static str, data: serde_json::Value) {
        self.ivars().outbox.borrow_mut().push((name, data));
    }

    /// Sends what was queued, outside every borrow of the state.
    pub fn flush(&self) {
        let out: Vec<_> = self.ivars().outbox.borrow_mut().drain(..).collect();
        if out.is_empty() {
            return;
        }
        let ctx = self.ivars().state.borrow().ctx.clone();
        for (name, data) in out {
            ctx.emit(name, data);
        }
    }

    /// Bytes for the program.
    fn send(&self, data: &str) {
        if !data.is_empty() {
            self.emit("input", json!({ "data": data }));
        }
    }

    // MARK: Props and commands

    /// Text size in points: the grid follows from it.
    pub fn set_font_size(&self, points: CGFloat) {
        self.apply_font(points);
        {
            let mut s = self.ivars().state.borrow_mut();
            s.needs_full = true;
            s.needs_pull = true;
        }
        self.relayout();
        self.setNeedsDisplay(true);
    }

    fn rescale(&self) {
        let scale = self.window().map(|w| w.backingScaleFactor()).unwrap_or(2.0).max(1.0);
        let (changed, points) = {
            let s = self.ivars().state.borrow();
            (s.pixel_scale != scale, s.points)
        };
        if changed {
            self.ivars().state.borrow_mut().pixel_scale = scale;
            self.set_font_size(points);
        }
    }

    fn apply_font(&self, size: CGFloat) {
        let mut s = self.ivars().state.borrow_mut();
        s.points = size.max(4.0);
        let scale = s.pixel_scale;
        let probe = Faces::at(s.points);
        let adv = advance(0x4D, &probe.regular); // "M"
        let cell_px = (adv * scale).round().max(1.0);
        let snapped = s.points * (cell_px / scale) / adv.max(0.1);
        s.faces = Faces::at(snapped);
        s.cell_w = cell_px / scale;
        let (ascent, descent) = unsafe { (s.faces.regular.ascent(), s.faces.regular.descent()) };
        let cell_h_px = ((ascent + descent) * scale).round().max(1.0);
        s.cell_h = cell_h_px / scale;
        s.baseline = (((s.cell_h - ascent - descent) / 2.0 + ascent) * scale).round() / scale;
        s.thickness = glyph_thickness(cell_h_px as i32) as CGFloat / scale;
        s.in_font = Default::default();
        s.item_cache.clear();
        s.glyphs.clear();
        for line in &s.grid {
            line.cells.iter().for_each(|c| *c.items.borrow_mut() = None);
        }
        for line in s.history.iter().flatten() {
            line.cells.iter().for_each(|c| *c.items.borrow_mut() = None);
        }
    }

    /// Theme colors are CSS hex (`#rrggbb` or `#rrggbbaa`); the VT gets them opaque.
    pub fn set_theme(&self, background: &str, foreground: &str, cursor: &str, selection: &str, palette: &[String]) {
        let (Some(bg), Some(fg), Some(cur)) = (css_color(background), css_color(foreground), css_color(cursor)) else { return };
        {
            let mut s = self.ivars().state.borrow_mut();
            s.background = bg.0;
            s.foreground = fg.0;
            s.cursor_color = cur.0;
            if let Some(sel) = css_color(selection) {
                s.selection = sel;
            }
            let ansi: Vec<u32> = palette.iter().map(|c| css_color(c).map_or(0x808080, |c| c.0)).collect();
            s.vt_palette = Some(palette256(&ansi));
            // The history's colors are the VT's: its rows are drawn again when next needed.
            s.scratch = None;
            let n = s.history_ansi.len();
            s.history = vec![None; n];
        }
        self.apply_theme();
        self.setNeedsDisplay(true);
    }

    fn apply_theme(&self) {
        let mut s = self.ivars().state.borrow_mut();
        let (Some(vt), Some(palette)) = (s.vt.as_ref(), s.vt_palette.as_ref()) else { return };
        vt.set_theme(s.foreground, s.background, s.cursor_color, palette);
        s.needs_full = true;
        s.needs_pull = true;
    }

    pub fn write(&self, data: &str) {
        self.tell_once(1, || format!("terminal: first output ({} chars)", data.chars().count()));
        let copied = {
            let mut s = self.ivars().state.borrow_mut();
            let Some(vt) = s.vt.as_ref() else {
                s.pending.push(data.to_string());
                return;
            };
            vt.write(data);
            let copied = vt.take_clipboard();
            s.needs_pull = true;
            copied
        };
        if let Some(text) = copied {
            self.emit("copy", json!({ "text": text }));
        }
        self.queue_refresh();
        self.restart_blink();
    }

    /// After `failed`: try to make the VT again.
    pub fn retry(&self) {
        let failed = std::mem::replace(&mut self.ivars().state.borrow_mut().failed, false);
        if failed {
            self.relayout();
            self.setNeedsDisplay(true);
        }
    }

    /// A new attachment: the screen starts empty. The history is the page's (`history`).
    pub fn reset(&self) {
        {
            let mut s = self.ivars().state.borrow_mut();
            s.vt = None;
            s.pending.clear();
            s.cols = 0;
            s.rows = 0;
            s.grid.clear();
        }
        self.relayout();
        self.setNeedsDisplay(true);
    }

    pub fn focus(&self) {
        if let Some(window) = self.window() {
            let me: &NSResponder = self;
            window.makeFirstResponder(Some(me));
        }
    }

    pub fn blur(&self) {
        let Some(window) = self.window() else { return };
        let mine = window.firstResponder().is_some_and(|r| std::ptr::addr_eq(&*r as *const NSResponder, self as *const Self));
        if mine {
            window.makeFirstResponder(None);
        }
    }

    fn focus_changed(&self, focused: bool) {
        let report = {
            let mut s = self.ivars().state.borrow_mut();
            s.focused = focused;
            if !focused {
                s.marked.clear();
            }
            s.vt.as_ref().and_then(|vt| vt.focus(focused))
        };
        if let Some(bytes) = report {
            self.send(&bytes);
        }
        self.restart_blink();
        self.setNeedsDisplay(true);
        self.flush();
    }

    /// Pasted text to the program, framed for bracketed paste when it asked for it.
    pub fn paste(&self, text: &str) {
        let bytes = self.ivars().state.borrow().vt.as_ref().and_then(|vt| vt.paste(text));
        if let Some(bytes) = bytes {
            self.send(&bytes);
            self.scroll_to_bottom();
        }
    }

    // MARK: The history

    /// The `history` command: void (`reset`), drop `removed` rows from the front, append `rows`. The rows on screen
    /// stay put.
    pub fn apply_history(&self, reset: bool, removed: usize, rows: Vec<String>, cols: usize, alternate: bool) {
        {
            let mut s = self.ivars().state.borrow_mut();
            if reset {
                s.history_ansi.clear();
                s.history.clear();
                s.mark_row = -1;
                s.sel_from = None;
                s.sel_to = None;
                s.scratch = None;
            } else if removed > 0 {
                let n = removed.min(s.history.len());
                let m = n.min(s.history_ansi.len());
                s.history_ansi.drain(..m);
                s.history.drain(..n);
                s.mark_row = if s.mark_row >= n as isize { s.mark_row - n as isize } else { -1 };
                Self::shift_selection(&mut s, n);
            }
            if cols > 0 && cols != s.history_cols {
                s.history_cols = cols;
                s.scratch = None;
                let n = s.history_ansi.len();
                s.history = vec![None; n];
            }
            s.alternate = alternate;
            let added = rows.len();
            s.history_ansi.extend(rows);
            s.history.extend(std::iter::repeat_n(None, added));
            // Scrolled up, the rows above the screen grew: the reader's rows stay where they are.
            if s.scroll_y > 0.0 && !reset {
                s.scroll_y += added as CGFloat * s.cell_h;
            }
            if alternate {
                s.scroll_y = 0.0;
            }
            let max = max_scroll(&s);
            s.scroll_y = s.scroll_y.clamp(0.0, max);
        }
        self.report_viewport();
        self.setNeedsDisplay(true);
    }

    /// Show history row `row` at the top.
    pub fn scroll_to(&self, row: usize) {
        let target = {
            let s = self.ivars().state.borrow();
            if s.alternate || s.history.is_empty() {
                return;
            }
            (s.history.len() - row.min(s.history.len())) as CGFloat * s.cell_h
        };
        self.set_scroll(target);
    }

    pub fn scroll_to_bottom(&self) {
        self.set_scroll(0.0);
    }

    /// Highlight one history row (the search's match); -1 none.
    pub fn mark(&self, row: isize) {
        {
            let mut s = self.ivars().state.borrow_mut();
            s.mark_row = if row >= 0 && (row as usize) < s.history.len() { row } else { -1 };
        }
        self.setNeedsDisplay(true);
    }

    fn set_scroll(&self, next: CGFloat) {
        {
            let mut s = self.ivars().state.borrow_mut();
            let clamped = next.clamp(0.0, max_scroll(&s));
            if clamped == s.scroll_y {
                return;
            }
            s.scroll_y = clamped;
        }
        self.report_viewport();
        self.setNeedsDisplay(true);
    }

    fn report_viewport(&self) {
        let (top, at_bottom) = {
            let mut s = self.ivars().state.borrow_mut();
            let up = if s.cell_h > 0.0 { (s.scroll_y / s.cell_h - 0.01).ceil() as isize } else { 0 };
            let top = s.history.len() as isize - up.max(0);
            let at_bottom = s.scroll_y < 0.5;
            if top == s.reported_top && at_bottom == s.reported_bottom {
                return;
            }
            s.reported_top = top;
            s.reported_bottom = at_bottom;
            (top, at_bottom)
        };
        self.emit("viewport", json!({ "top": top, "atBottom": at_bottom }));
    }

    /// A wheel or trackpad scroll: through the history, or reported in lines to the program.
    fn wheel(&self, event: &NSEvent) {
        let precise = event.hasPreciseScrollingDeltas();
        let phase = event.phase();
        let (scrolls, cell_h, scroll_y) = {
            let s = self.ivars().state.borrow();
            (!s.alternate && (!s.history.is_empty() || s.scroll_y > 0.0), s.cell_h.max(1.0), s.scroll_y)
        };
        // A finger coming down stops what a fling still owes the program.
        if phase == NSEventPhase::Began || phase == NSEventPhase::MayBegin {
            self.ivars().state.borrow_mut().wheel_remainder = 0.0;
            if !scrolls {
                self.emit("scroll", json!({ "lines": 0 }));
            }
        }
        // Points on a trackpad (with AppKit's momentum after the fingers lift); lines on a wheel. Positive is back in
        // time (content moves down).
        let dy = if precise { event.scrollingDeltaY() } else { event.scrollingDeltaY() * cell_h };
        if dy == 0.0 {
            return;
        }
        if scrolls {
            self.set_scroll(scroll_y + dy);
            return;
        }
        let lines = {
            let mut s = self.ivars().state.borrow_mut();
            s.wheel_remainder += dy / cell_h;
            let lines = s.wheel_remainder.trunc();
            s.wheel_remainder -= lines;
            lines as i64
        };
        if lines != 0 {
            self.emit("scroll", json!({ "lines": lines }));
        }
    }

    /// History row `i` as a line, drawn through the scratch VT with the rows of its page when it is first needed.
    fn history_line(s: &mut State, i: usize) -> Line {
        if let Some(Some(line)) = s.history.get(i) {
            return line.clone();
        }
        let start = i - i % PAGE;
        let end = (start + PAGE).min(s.history_ansi.len());
        if s.scratch.is_none() {
            if let Some(made) = Vt::new(s.history_cols.clamp(2, 1000), PAGE) {
                if let Some(palette) = s.vt_palette.as_ref() {
                    made.set_theme(s.foreground, s.background, s.cursor_color, palette);
                }
                s.scratch = Some(made);
            }
        }
        let mut rendered = vec![Line::empty(); PAGE];
        if let Some(scratch) = s.scratch.as_ref() {
            scratch.write(&format!("\x1b[0m\x1b[2J\x1b[H{}", s.history_ansi[start..end].join("\r\n")));
            if let Some(frame) = scratch.frame(true) {
                // A history row is a whole line (the computer joined wrapped rows).
                for row in frame.dirty.into_iter().filter(|r| r.row < PAGE) {
                    rendered[row.row] = Line::of(row.runs, false);
                }
            }
        }
        for r in start..end {
            s.history[r] = Some(rendered[r - start].clone());
        }
        rendered[i - start].clone()
    }

    // MARK: Layout and the grid

    fn relayout(&self) {
        let size = self.bounds().size;
        let made = {
            let mut s = self.ivars().state.borrow_mut();
            if size.width <= 0.0 || size.height <= 0.0 || s.cell_w <= 0.0 || s.cell_h <= 0.0 {
                return;
            }
            let c = (((size.width + 0.001) / s.cell_w) as usize).max(2);
            let r = (((size.height + 0.001) / s.cell_h) as usize).max(2);
            let (cw, ch) = (s.cell_w * s.pixel_scale, s.cell_h * s.pixel_scale);
            let mut made = false;
            if let Some(vt) = s.vt.as_ref() {
                if c == s.cols && r == s.rows {
                    return;
                }
                vt.resize(c, r, cw, ch);
            } else {
                if s.failed {
                    return;
                }
                let Some(vt) = Vt::new(c, r) else {
                    // Said once; `retry` tries again.
                    s.failed = true;
                    drop(s);
                    self.emit("failed", json!({ "message": format!("libghostty-vt could not make a {c}×{r} terminal.") }));
                    return;
                };
                vt.resize(c, r, cw, ch);
                s.vt = Some(vt);
                made = true;
            }
            s.cols = c;
            s.rows = r;
            s.grid = vec![Line::empty(); r];
            s.needs_full = true;
            s.needs_pull = true;
            made
        };
        if made {
            self.apply_theme();
            let (pending, copied) = {
                let mut s = self.ivars().state.borrow_mut();
                let pending = std::mem::take(&mut s.pending);
                let vt = s.vt.as_ref().expect("just made");
                for data in &pending {
                    vt.write(data);
                }
                (pending.len(), vt.take_clipboard())
            };
            let _ = pending;
            if let Some(text) = copied {
                self.emit("copy", json!({ "text": text }));
            }
        }
        let (c, r) = {
            let mut s = self.ivars().state.borrow_mut();
            Self::pull(&mut s);
            let max = max_scroll(&s);
            s.scroll_y = s.scroll_y.clamp(0.0, max);
            (s.cols, s.rows)
        };
        self.setNeedsDisplay(true);
        if c > 0 && r > 0 {
            self.tell_once(0, || format!("terminal: grid {c}x{r}"));
        }
        self.emit("size", json!({ "cols": c, "rows": r }));
        self.report_viewport();
    }

    /// The rows that changed since the last pull; every row after a resize, a theme or a font.
    fn pull(s: &mut State) {
        let Some(frame): Option<Frame> = s.vt.as_ref().and_then(|vt| vt.frame(s.needs_full)) else { return };
        s.needs_pull = false;
        s.needs_full = false;
        if frame.full || frame.rows != s.grid.len() || frame.cols != s.cols {
            s.changed_all = true;
        }
        if frame.rows != s.grid.len() {
            s.grid.resize(frame.rows, Line::empty());
        }
        s.rows = frame.rows;
        s.cols = frame.cols;
        s.cursor_col = frame.cursor_col;
        s.cursor_row = frame.cursor_row;
        s.cursor_visible = frame.cursor_visible;
        s.cursor_style = frame.cursor_style;
        s.cursor_blinks = frame.cursor_blinking;
        for row in frame.dirty {
            if row.row < s.grid.len() {
                s.changed.push(row.row);
                s.grid[row.row] = Line::of(row.runs, row.wrapped);
            }
        }
    }

    // MARK: Content lines

    fn line(s: &mut State, i: isize) -> Line {
        if s.needs_pull {
            Self::pull(s);
        }
        let h = s.history.len() as isize;
        if i < h {
            return if i >= 0 { Self::history_line(s, i as usize) } else { Line::empty() };
        }
        let live = (i - h) as usize;
        s.grid.get(live).cloned().unwrap_or_else(Line::empty)
    }

    /// A line's text, blank cells as spaces, and the UTF-16 index of cell `col` in it: -1 past its end; a wide
    /// character or a grapheme maps to its start.
    fn row_text(line: &Line, col: usize) -> (String, isize) {
        let mut units: Vec<u16> = Vec::new();
        let mut at = 0usize;
        let mut index: isize = -1;
        for cell in line.cells.iter() {
            let run = &cell.run;
            if run.col > at {
                if col >= at && col < run.col {
                    index = (units.len() + (col - at)) as isize;
                }
                units.extend(std::iter::repeat_n(0x20, run.col - at));
            }
            if col >= run.col && col < run.col + run.cells {
                index = if run.plain() { (units.len() + (col - run.col)) as isize } else { units.len() as isize };
            }
            units.extend_from_slice(&run.units);
            at = run.col + run.cells;
        }
        (utf16(&units), index)
    }

    /// The content line and column under a mouse event; None outside the grid.
    fn cell_at(&self, event: &NSEvent) -> Option<(usize, isize)> {
        let p = self.convertPoint_fromView(event.locationInWindow(), None);
        let s = self.ivars().state.borrow();
        if s.rows == 0 || s.cell_h <= 0.0 || p.y < 0.0 || p.x < 0.0 {
            return None;
        }
        Some(Self::cell_under(&s, p))
    }

    /// The cell under `p` (in the view), clamped to the content.
    fn cell_under(s: &State, p: NSPoint) -> (usize, isize) {
        let col = ((p.x.max(0.0) / s.cell_w.max(1.0)) as usize).min(s.cols.saturating_sub(1));
        let line = line_under(s, p.y).clamp(0, ((s.history.len() + s.rows) as isize - 1).max(0));
        (col, line)
    }

    // MARK: Selection

    fn has_selection(&self) -> bool {
        let s = self.ivars().state.borrow();
        s.sel_from.is_some() && s.sel_to.is_some()
    }

    /// History rows removed from the front pull every content line up.
    fn shift_selection(s: &mut State, removed: usize) {
        let (Some(mut from), Some(mut to)) = (s.sel_from, s.sel_to) else { return };
        from.0 -= removed as isize;
        to.0 -= removed as isize;
        if to.0 < 0 {
            s.sel_from = None;
            s.sel_to = None;
            return;
        }
        if from.0 < 0 {
            from = (0, 0);
        }
        s.sel_from = Some(from);
        s.sel_to = Some(to);
    }

    /// The word under (line, col): the run of non-blank cells around it; None on a blank.
    fn word(s: &mut State, i: isize, col: usize) -> Option<(usize, usize)> {
        if col >= s.cols {
            return None;
        }
        let mut blank = vec![true; s.cols];
        let line = Self::line(s, i);
        for cell in line.cells.iter() {
            let run = &cell.run;
            for k in 0..run.cells {
                if run.col + k < s.cols {
                    blank[run.col + k] = run.plain() && run.units.get(k) == Some(&0x20);
                }
            }
        }
        if blank[col] {
            return None;
        }
        let (mut a, mut b) = (col, col);
        while a > 0 && !blank[a - 1] {
            a -= 1;
        }
        while b + 1 < s.cols && !blank[b + 1] {
            b += 1;
        }
        Some((a, b))
    }

    /// A press: one click starts a selection at its cell, two select the word, three the line.
    fn press(&self, event: &NSEvent) {
        let Some((col, line)) = self.cell_at(event) else { return };
        let clicks = event.clickCount();
        {
            let mut s = self.ivars().state.borrow_mut();
            s.press = Some((col, line));
            s.dragged = false;
            let last = s.cols.saturating_sub(1);
            let span = match clicks {
                1 => {
                    s.sel_from = None;
                    s.sel_to = None;
                    s.sel_mode = 1;
                    s.sel_anchor = Some(((line, col), (line, col)));
                    None
                }
                2 => Self::word(&mut s, line, col).map(|(a, b)| ((line, a), (line, b))),
                _ => Some(((line, 0), (line, last))),
            };
            if let Some((from, to)) = span {
                s.sel_mode = if clicks == 2 { 2 } else { 3 };
                s.sel_anchor = Some((from, to));
                s.sel_from = Some(from);
                s.sel_to = Some(to);
            }
        }
        self.setNeedsDisplay(true);
    }

    /// A drag: the selection from what the press started on to the cell under the pointer; past the top or bottom
    /// edge the content scrolls.
    fn drag(&self, event: &NSEvent) {
        let p = self.convertPoint_fromView(event.locationInWindow(), None);
        let height = self.bounds().size.height;
        let (scroll_y, cell_h) = {
            let s = self.ivars().state.borrow();
            (s.scroll_y, s.cell_h)
        };
        if p.y < 0.0 {
            self.set_scroll(scroll_y + cell_h / 2.0);
        } else if p.y > height {
            self.set_scroll(scroll_y - cell_h / 2.0);
        }
        {
            let mut s = self.ivars().state.borrow_mut();
            let Some((a0, a1)) = s.sel_anchor else { return };
            let (col, line) = Self::cell_under(&s, p);
            let at = (line, col);
            let last = s.cols.saturating_sub(1);
            let (from, to) = match s.sel_mode {
                2 => {
                    let (w0, w1) = Self::word(&mut s, line, col).map_or((at, at), |(a, b)| ((line, a), (line, b)));
                    (a0.min(w0), a1.max(w1))
                }
                3 => ((a0.0.min(line), 0), (a1.0.max(line), last)),
                _ => (a0.min(at), a0.max(at)),
            };
            if s.sel_mode == 1 && from == to && !s.dragged {
                // Not yet off the cell it started on.
                return;
            }
            s.dragged = true;
            s.sel_from = Some(from);
            s.sel_to = Some(to);
        }
        self.setNeedsDisplay(true);
    }

    /// Select All: every row, the history's included.
    fn select_all(&self) {
        {
            let mut s = self.ivars().state.borrow_mut();
            let lines = (s.history.len() + s.rows) as isize;
            s.sel_from = Some((0, 0));
            s.sel_to = Some(((lines - 1).max(0), s.cols.saturating_sub(1)));
        }
        self.setNeedsDisplay(true);
    }

    /// The text of cells c0..=c1 of a line (blanks as spaces; a wide character once, from its first cell).
    fn cells_text(line: &Line, c0: usize, c1: usize) -> String {
        let mut units: Vec<u16> = Vec::new();
        let mut at = c0;
        for cell in line.cells.iter() {
            let run = &cell.run;
            if run.col + run.cells <= c0 {
                continue;
            }
            if run.col > c1 {
                break;
            }
            while at < run.col && at <= c1 {
                units.push(0x20);
                at += 1;
            }
            if run.plain() {
                let a = c0.max(run.col) - run.col;
                let b = c1.min(run.col + run.cells - 1) - run.col;
                if b >= a && b < run.units.len() {
                    units.extend_from_slice(&run.units[a..=b]);
                }
            } else if run.col >= c0 {
                units.extend_from_slice(&run.units);
            }
            at = run.col + run.cells;
        }
        utf16(&units)
    }

    /// The selected text: rows joined by newlines, except where a row wraps onto the next.
    fn selected_text(&self) -> String {
        let mut s = self.ivars().state.borrow_mut();
        let (Some(from), Some(to)) = (s.sel_from, s.sel_to) else { return String::new() };
        let last = s.cols.saturating_sub(1);
        let mut out = String::new();
        for i in from.0..=to.0 {
            let content = Self::line(&mut s, i);
            let mut part = Self::cells_text(&content, if i == from.0 { from.1 } else { 0 }, if i == to.0 { to.1 } else { last });
            if content.wrapped && i != to.0 {
                out.push_str(&part);
            } else {
                while part.ends_with(' ') {
                    part.pop();
                }
                out.push_str(&part);
                if i != to.0 {
                    out.push('\n');
                }
            }
        }
        // Empty rows at the end (Select All over a screen not yet full) are not text.
        while out.ends_with('\n') {
            out.pop();
        }
        out
    }

    /// The pointer over the grid: a hand over a link, the I-beam elsewhere.
    fn hover(&self, event: &NSEvent) {
        let link = match self.cell_at(event) {
            Some((col, line)) => {
                let mut s = self.ivars().state.borrow_mut();
                let content = Self::line(&mut s, line);
                let (text, index) = Self::row_text(&content, col);
                url_at(&text, index)
            }
            None => false,
        };
        self.ivars().state.borrow_mut().over_link = link;
        if link { NSCursor::pointingHandCursor().set() } else { NSCursor::IBeamCursor().set() }
    }

    /// Copy, Paste and Select All, as a right click or Control-click asks.
    fn context_menu(&self) -> Retained<NSMenu> {
        let mtm = MainThreadMarker::from(self);
        let menu = NSMenu::initWithTitle(NSMenu::alloc(mtm), &NSString::from_str(""));
        // No plug-ins: macOS would add its AutoFill items (Contact…, Passwords…) to a text input view's menu.
        menu.setAllowsContextMenuPlugIns(false);
        let item = |title: &str, action: Sel| {
            let item = unsafe {
                NSMenuItem::initWithTitle_action_keyEquivalent(NSMenuItem::alloc(mtm), &NSString::from_str(title), Some(action), &NSString::from_str(""))
            };
            unsafe { item.setTarget(Some(self)) };
            item
        };
        menu.addItem(&item("Copy", sel!(copy:)));
        menu.addItem(&item("Paste", sel!(paste:)));
        menu.addItem(&NSMenuItem::separatorItem(mtm));
        menu.addItem(&item("Select All", sel!(selectAll:)));
        menu
    }

    /// A click: the cell and its row's text go to the page (a link opens there, or the keyboard focus).
    fn tap(&self, col: usize, line: isize) {
        let (text, index, row) = {
            let mut s = self.ivars().state.borrow_mut();
            let content = Self::line(&mut s, line);
            let (text, index) = Self::row_text(&content, col);
            let h = s.history.len() as isize;
            (text, index, if line < h { -1 } else { line - h })
        };
        self.emit("tapCell", json!({ "col": col, "row": row, "text": text, "index": index }));
    }

    // MARK: The keyboard

    /// A key the view takes itself (true): one that is not text, or a Control combination, encoded for the program's
    /// modes; while an input method composes, every key is its.
    fn key(&self, event: &NSEvent) -> bool {
        if !self.ivars().state.borrow().marked.is_empty() {
            return false;
        }
        let flags = event.modifierFlags();
        let mods = Mods {
            shift: flags.contains(NSEventModifierFlags::Shift),
            control: flags.contains(NSEventModifierFlags::Control),
            option: flags.contains(NSEventModifierFlags::Option),
            command: flags.contains(NSEventModifierFlags::Command),
        };
        if mods.command {
            // ⌘ is the menus'; one no menu took ends here, without a beep.
            return true;
        }
        let code = event.keyCode();
        if !is_special(code) && !mods.control {
            return false;
        }
        let plain = event.charactersIgnoringModifiers().map(|s| s.to_string()).unwrap_or_default();
        let unshifted = plain.to_lowercase().chars().next().filter(|c| !('\u{f700}'..='\u{f8ff}').contains(c)).map_or(0, |c| c as u32);
        // The text a key types: none for the function keys (AppKit's private-use characters), the letter for Control.
        let text = if is_special(code) { String::new() } else { plain.clone() };
        let action = if event.isARepeat() { vt::KEY_REPEAT } else { vt::KEY_PRESS };
        let encoded = {
            let s = self.ivars().state.borrow();
            s.vt.as_ref().and_then(|vt| vt.key(ghostty_key(code), ghostty_mods(mods), action, &text, unshifted))
        };
        let Some(bytes) = encoded.or_else(|| key_bytes(code, mods, &plain, false)) else {
            // A Control combination nothing encodes (⌃é): the text system's.
            return !mods.control || is_special(code);
        };
        self.send(&bytes);
        self.scroll_to_bottom();
        true
    }

    /// Where the cursor is: under it the composition, and beside it the input method's candidates.
    fn cursor_rect(&self) -> NSRect {
        let s = self.ivars().state.borrow();
        let y = line_y(&s, (s.history.len() + s.cursor_row) as isize);
        NSRect::new(NSPoint::new(s.cursor_col as CGFloat * s.cell_w, y), NSSize::new(s.cell_w, s.cell_h))
    }

    /// The cursor lit again (output, a key, focus); one tick in flight keeps it blinking after.
    fn restart_blink(&self) {
        let schedule = {
            let mut s = self.ivars().state.borrow_mut();
            s.blink_held = std::time::Instant::now();
            s.blink_on = true;
            let schedule = s.cursor_blinks && !s.blink_pending && self.window().is_some();
            s.blink_pending |= schedule;
            schedule
        };
        self.invalidate_cursor();
        if schedule {
            self.schedule_blink(BLINK_S);
        }
    }

    fn schedule_blink(&self, after: f64) {
        self.ivars().state.borrow_mut().blink_pending = true;
        let _: () = unsafe { msg_send![self, performSelector: sel!(blinkTick:), withObject: None::<&AnyObject>, afterDelay: after] };
    }

    /// One `refresh` on the next run loop pass, in every mode (a live resize's too).
    fn queue_refresh(&self) {
        {
            let mut s = self.ivars().state.borrow_mut();
            if s.refresh_queued {
                return;
            }
            s.refresh_queued = true;
        }
        let modes = NSArray::from_slice(&[&*NSString::from_str("kCFRunLoopCommonModes")]);
        let _: () = unsafe {
            msg_send![self, performSelector: sel!(refreshChanged:), withObject: None::<&AnyObject>, afterDelay: 0.0f64, inModes: &*modes]
        };
    }

    /// Takes the VT's changed rows and marks only them, and the cursor's old and new rows, for drawing: a full-screen
    /// window under steady output redraws the rows that moved, not the whole view (DESIGN.md §4: frame time).
    fn refresh(&self) {
        let rects = {
            let mut s = self.ivars().state.borrow_mut();
            s.refresh_queued = false;
            if s.needs_pull {
                Self::pull(&mut s);
            }
            let all = std::mem::replace(&mut s.changed_all, false);
            let mut rows = std::mem::take(&mut s.changed);
            if all {
                None
            } else {
                let h = s.history.len() as isize;
                let mut lines: Vec<isize> = rows.drain(..).map(|r| h + r as isize).collect();
                lines.extend(s.cursor_drawn);
                if s.cursor_row < s.rows {
                    lines.push(h + s.cursor_row as isize);
                }
                Some(self.line_rects(&s, &lines))
            }
        };
        match rects {
            None => self.setNeedsDisplay(true),
            Some(rects) => {
                for rect in rects {
                    self.setNeedsDisplayInRect(rect);
                }
            }
        }
    }

    /// After a paint: a whole view drawn has drawn every change; rows the paint itself took from the VT (output
    /// that came between the last `refresh` and the paint) are marked for the next one.
    fn settle_changes(&self, dirty: NSRect) {
        let pending = {
            let mut s = self.ivars().state.borrow_mut();
            let bounds = self.bounds();
            if dirty.size.height >= bounds.size.height - 1.0 && dirty.size.width >= bounds.size.width - 1.0 {
                s.changed.clear();
                s.changed_all = false;
            }
            !s.changed.is_empty() || s.changed_all
        };
        if pending {
            self.queue_refresh();
        }
    }

    /// The cursor's row, old and new, drawn again (a blink, the composition).
    fn invalidate_cursor(&self) {
        let rects = {
            let s = self.ivars().state.borrow();
            let h = s.history.len() as isize;
            let mut lines: Vec<isize> = s.cursor_drawn.into_iter().collect();
            if s.cursor_row < s.rows {
                lines.push(h + s.cursor_row as isize);
            }
            self.line_rects(&s, &lines)
        };
        for rect in rects {
            self.setNeedsDisplayInRect(rect);
        }
    }

    /// The view's rects for content lines (merged where they touch), a little taller than their rows: a glyph that
    /// reaches past its cell (an icon, an underline's curl) is drawn again with its row.
    fn line_rects(&self, s: &State, lines: &[isize]) -> Vec<NSRect> {
        let width = self.bounds().size.width;
        let first = s.history.len() as isize - (snapped_scroll(s) / s.cell_h.max(1.0)).ceil() as isize - 1;
        let last = first + s.rows as isize + 2;
        let visible: Vec<isize> = lines.iter().copied().filter(|&i| i >= first && i <= last).collect();
        spans(&visible)
            .into_iter()
            .map(|(a, b)| {
                let y = line_y(s, a) - OVERDRAW;
                NSRect::new(NSPoint::new(0.0, y), NSSize::new(width, (b - a + 1) as CGFloat * s.cell_h + 2.0 * OVERDRAW))
            })
            .collect()
    }

    // MARK: Drawing

    /// Draws what `dirty` asks for; how many rows it drew, of how many the view shows.
    fn paint(&self, cg: &CGContext, dirty: NSRect) -> (usize, usize) {
        let mut s = self.ivars().state.borrow_mut();
        if s.needs_pull {
            Self::pull(&mut s);
        }
        let bounds = self.bounds();
        Rgba::of(s.background, 1.0).fill(cg);
        CGContext::set_should_antialias(Some(cg), false);
        // Only the rects being drawn: AppKit may clip to their union, and the rows between them keep what they show.
        for rect in self.rects_being_drawn(dirty) {
            CGContext::fill_rect(Some(cg), rect);
        }
        if s.cell_h <= 0.0 {
            return (0, 0);
        }
        CGContext::set_text_matrix(Some(cg), CGAffineTransform { a: 1.0, b: 0.0, c: 0.0, d: -1.0, tx: 0.0, ty: 0.0 });
        let line_count = (s.history.len() + s.rows) as isize;
        let first = line_under(&s, dirty.origin.y.max(0.0)).max(0);
        let last = (line_count - 1).min(line_under(&s, (dirty.origin.y + dirty.size.height).min(bounds.size.height) - 0.001));
        let h = s.history.len() as isize;
        let top = line_under(&s, 0.0).max(0);
        let bottom = (line_count - 1).min(line_under(&s, bounds.size.height - 0.001));
        let visible = (bottom - top + 1).max(0) as usize;
        let mut drawn = 0usize;
        for i in first..=last {
            let y = line_y(&s, i);
            // The union of the marked rows spans the rows between them: only the marked ones are drawn.
            let row = NSRect::new(NSPoint::new(0.0, y), NSSize::new(bounds.size.width, s.cell_h));
            if !self.needsToDrawRect(row) {
                continue;
            }
            drawn += 1;
            let content = Self::line(&mut s, i);
            CGContext::set_should_antialias(Some(cg), false);
            for cell in content.cells.iter().filter(|c| c.run.bg != 0) {
                Rgba::of(cell.run.bg, 1.0).fill(cg);
                CGContext::fill_rect(
                    Some(cg),
                    CGRect::new(CGPoint::new(cell.run.col as CGFloat * s.cell_w, y), CGSize::new(cell.run.cells as CGFloat * s.cell_w, s.cell_h)),
                );
            }
            if i < h && i == s.mark_row {
                Rgba::of(s.selection.0, s.selection.1).fill(cg);
                CGContext::fill_rect(Some(cg), CGRect::new(CGPoint::new(0.0, y), CGSize::new(bounds.size.width, s.cell_h)));
            }
            if let (Some(from), Some(to)) = (s.sel_from, s.sel_to) {
                if i >= from.0 && i <= to.0 {
                    let c0 = if i == from.0 { from.1 } else { 0 };
                    let c1 = if i == to.0 { to.1 } else { s.cols.saturating_sub(1) };
                    if c1 >= c0 {
                        Rgba::of(s.selection.0, s.selection.1).fill(cg);
                        CGContext::fill_rect(
                            Some(cg),
                            CGRect::new(CGPoint::new(c0 as CGFloat * s.cell_w, y), CGSize::new((c1 - c0 + 1) as CGFloat * s.cell_w, s.cell_h)),
                        );
                    }
                }
            }
            for (k, cell) in content.cells.iter().enumerate() {
                let next = content.cells.get(k + 1).map(|c| &c.run);
                Self::draw_cell(&mut s, cell, y, cg, None, next);
            }
        }
        Self::draw_cursor(&mut s, cg, bounds.size.height);
        (drawn, visible)
    }

    /// The rects AppKit asks this draw for (`dirty` is their union).
    fn rects_being_drawn(&self, dirty: NSRect) -> Vec<NSRect> {
        let mut rects: *const NSRect = std::ptr::null();
        let mut count: objc2_foundation::NSInteger = 0;
        unsafe { self.getRectsBeingDrawn_count(&mut rects, &mut count) };
        if rects.is_null() || count <= 0 {
            return vec![dirty];
        }
        // SAFETY: AppKit owns `count` rects at `rects` for the rest of this drawRect:.
        unsafe { std::slice::from_raw_parts(rects, count as usize) }.to_vec()
    }

    fn face_index(run: &Run) -> usize {
        usize::from(run.bold()) + 2 * usize::from(run.italic())
    }

    /// What draws a character the run's face lacks: the upright face (slanted in an italic run) and its fallbacks.
    fn upright(s: &State, run: &Run) -> CFRetained<CTFont> {
        let f = &s.faces;
        match (run.italic(), run.bold()) {
            (true, true) => f.bold_slant.clone(),
            (true, false) => f.slant.clone(),
            (false, true) => f.bold.clone(),
            (false, false) => f.regular.clone(),
        }
    }

    /// Whether bundled face `index` draws this character itself (its advance is then exactly one cell).
    fn has_glyph(s: &mut State, unit: u16, index: usize) -> bool {
        if let Some(known) = s.in_font[index].get(&unit) {
            return *known;
        }
        let mut u = unit;
        let mut glyph: u16 = 0;
        let found = unsafe {
            s.faces.get(index).glyphs_for_characters(std::ptr::NonNull::from(&mut u), std::ptr::NonNull::from(&mut glyph), 1)
        } && glyph != 0;
        s.in_font[index].insert(unit, found);
        found
    }

    /// What a run draws: in-face text in one line per stretch, each other glyph on its own. `next` is the run after
    /// it in its row (a flag's second regional indicator).
    fn items(s: &mut State, run: &Run, next: Option<&Run>) -> Vec<Item> {
        let index = Self::face_index(run);
        let primary: CFRetained<CTFont> = CFRetained::from(s.faces.get(index));
        let upright = Self::upright(s, run);
        let cell_w = s.cell_w;
        let x0 = run.col as CGFloat * cell_w;
        let fitted = |units: &[u16], x: CGFloat, cells: usize| {
            let line = ct_line(&utf16(units), &upright);
            let width = line_width(&line);
            Item::Fitted { line, x, width, boxw: cells as CGFloat * cell_w }
        };
        let mut out = Vec::new();
        if run.flags & layout::FLAG_TAIL != 0 {
            // A flag's second regional indicator: the first draws the flag across both.
            return out;
        }
        if run.plain() {
            // Stretches drawn in one face: 0 the run's own, 1 the upright face (an italic run's other characters).
            let mut seg: Option<usize> = None;
            let mut seg_face = 0;
            for k in 0..=run.units.len() {
                let u = run.units.get(k).copied().unwrap_or(0x20);
                let drawn = k < run.units.len() && glyph_is_drawn(u as u32);
                let mut one: i32 = -1;
                if u != 0x20 && !drawn {
                    if Self::has_glyph(s, u, index) {
                        one = 0;
                    } else if run.italic() && Self::has_glyph(s, u, index - 2) {
                        one = 1;
                    }
                }
                if let Some(start) = seg {
                    if one != seg_face {
                        let font = if seg_face == 0 { &primary } else { &upright };
                        out.push(Item::Text { line: ct_line(&utf16(&run.units[start..k]), font), x: x0 + start as CGFloat * cell_w });
                        seg = None;
                    }
                }
                if k >= run.units.len() || u == 0x20 {
                    continue;
                }
                if drawn {
                    out.push(Item::Glyph { cp: u as u32, x: x0 + k as CGFloat * cell_w });
                } else if one < 0 {
                    out.push(fitted(&run.units[k..k + 1], x0 + k as CGFloat * cell_w, 1));
                } else if seg.is_none() {
                    seg = Some(k);
                    seg_face = one;
                }
            }
        } else if !run.units.is_empty() {
            let text = utf16(&run.units);
            let mut scalars = text.chars();
            let only = match (scalars.next(), scalars.next()) {
                (Some(c), None) => Some(c as u32),
                _ => None,
            };
            if let Some(cp) = only.filter(|cp| glyph_is_drawn(*cp)) {
                out.push(Item::Glyph { cp, x: x0 });
            } else if let (true, Some(next)) = (run.flags & layout::FLAG_HEAD != 0, next) {
                // Two regional indicators: the one flag, centred across both runs' cells.
                let both: Vec<u16> = run.units.iter().chain(next.units.iter()).copied().collect();
                out.push(fitted(&both, x0, run.cells + next.cells));
            } else if run.flags & layout::EMOJI_WIDE != 0 {
                // An emoji the VT gives one cell, with a blank cell after it: full size across both.
                out.push(fitted(&run.units, x0, 2));
            } else {
                out.push(fitted(&run.units, x0, run.cells));
            }
        }
        out
    }

    fn draw_cell(s: &mut State, cell: &Cell, y: CGFloat, cg: &CGContext, color: Option<u32>, next: Option<&Run>) {
        let run = &cell.run;
        // The borrow ends before a miss fills the cache.
        let cached = cell.items.borrow().clone();
        let items: Rc<Vec<Item>> = match cached {
            Some(items) => items,
            None => {
                let next_units = match next {
                    Some(n) if run.flags & layout::FLAG_HEAD != 0 => n.units.clone(),
                    _ => Vec::new(),
                };
                let key = ItemKey { col: run.col, cells: run.cells, flags: run.flags, units: run.units.clone(), next: next_units };
                let made = match s.item_cache.get(&key) {
                    Some(items) => items.clone(),
                    None => {
                        let made = Rc::new(Self::items(s, run, next));
                        if s.item_cache.len() >= ITEM_CACHE_MAX {
                            s.item_cache.clear();
                        }
                        s.item_cache.insert(key, made.clone());
                        made
                    }
                };
                *cell.items.borrow_mut() = Some(made.clone());
                made
            }
        };
        let ink = color.unwrap_or(run.fg);
        let rgba = Rgba::of(ink, 1.0);
        for item in items.iter() {
            match item {
                Item::Text { line, x } => {
                    CGContext::set_should_antialias(Some(cg), true);
                    rgba.fill(cg);
                    CGContext::set_text_position(Some(cg), *x, y + s.baseline);
                    unsafe { line.draw(cg) };
                }
                Item::Fitted { line, x, width, boxw } => {
                    CGContext::set_should_antialias(Some(cg), true);
                    rgba.fill(cg);
                    if *width <= boxw + 0.5 || *width <= 0.0 {
                        CGContext::set_text_position(Some(cg), x + (boxw - width) / 2.0, y + s.baseline);
                        unsafe { line.draw(cg) };
                    } else {
                        let k = boxw / width;
                        CGContext::save_g_state(Some(cg));
                        CGContext::translate_ctm(Some(cg), x + boxw / 2.0, y + s.cell_h / 2.0);
                        CGContext::scale_ctm(Some(cg), k, k);
                        CGContext::set_text_position(Some(cg), -width / 2.0, s.baseline - s.cell_h / 2.0);
                        unsafe { line.draw(cg) };
                        CGContext::restore_g_state(Some(cg));
                    }
                }
                Item::Glyph { cp, x } => Self::draw_glyph(s, *cp, *x, y, ink, cg),
            }
        }
        Self::decorate(s, run, y, ink, cg);
    }

    /// A box-drawing, block, braille or Powerline glyph from TerminalGlyphs, exactly in its cell (in pixels).
    fn draw_glyph(s: &mut State, cp: u32, x: CGFloat, y: CGFloat, color: u32, cg: &CGContext) {
        let scale = s.pixel_scale;
        let (wpx, hpx) = ((s.cell_w * scale).round() as i32, (s.cell_h * scale).round() as i32);
        let ops = s.glyphs.entry(cp).or_insert_with(|| Rc::new(glyph_ops(cp, wpx, hpx))).clone();
        CGContext::save_g_state(Some(cg));
        CGContext::translate_ctm(Some(cg), x, y);
        CGContext::scale_ctm(Some(cg), 1.0 / scale, 1.0 / scale);
        for op in ops.iter() {
            let p: Vec<CGFloat> = op.p.iter().map(|v| *v as CGFloat).collect();
            let ink = Rgba::of(color, op.alpha as CGFloat);
            ink.fill(cg);
            ink.stroke(cg);
            CGContext::set_line_cap(Some(cg), CGLineCap::Butt);
            CGContext::set_line_width(Some(cg), op.stroke as CGFloat);
            match op.kind {
                1 => {
                    CGContext::set_should_antialias(Some(cg), false);
                    CGContext::fill_rect(Some(cg), CGRect::new(CGPoint::new(p[0], p[1]), CGSize::new(p[2] - p[0], p[3] - p[1])));
                }
                2 => {
                    CGContext::set_should_antialias(Some(cg), true);
                    CGContext::begin_path(Some(cg));
                    CGContext::move_to_point(Some(cg), p[0], p[1]);
                    for k in 1..(op.points.max(1) as usize).min(4) {
                        CGContext::add_line_to_point(Some(cg), p[2 * k], p[2 * k + 1]);
                    }
                    CGContext::close_path(Some(cg));
                    CGContext::fill_path(Some(cg));
                }
                3 => {
                    CGContext::set_should_antialias(Some(cg), true);
                    CGContext::begin_path(Some(cg));
                    CGContext::move_to_point(Some(cg), p[0], p[1]);
                    CGContext::add_line_to_point(Some(cg), p[2], p[3]);
                    CGContext::stroke_path(Some(cg));
                }
                4 | 5 => {
                    CGContext::set_should_antialias(Some(cg), true);
                    CGContext::begin_path(Some(cg));
                    CGContext::move_to_point(Some(cg), p[0], p[1]);
                    CGContext::add_curve_to_point(Some(cg), p[2], p[3], p[4], p[5], p[6], p[7]);
                    if op.kind == 5 {
                        CGContext::close_path(Some(cg));
                        CGContext::fill_path(Some(cg));
                    } else {
                        CGContext::stroke_path(Some(cg));
                    }
                }
                6 => {
                    CGContext::set_should_antialias(Some(cg), true);
                    CGContext::fill_ellipse_in_rect(Some(cg), CGRect::new(CGPoint::new(p[0], p[1]), CGSize::new(p[2] - p[0], p[3] - p[1])));
                }
                _ => {}
            }
        }
        CGContext::restore_g_state(Some(cg));
    }

    /// Underline (its style and color), strikethrough and overline across the run's cells, one line weight.
    fn decorate(s: &State, run: &Run, y: CGFloat, color: u32, cg: &CGContext) {
        let x0 = run.col as CGFloat * s.cell_w;
        let x1 = x0 + run.cells as CGFloat * s.cell_w;
        let t = s.thickness;
        let scale = s.pixel_scale;
        let snap = |v: CGFloat| (v * scale).round() / scale;
        let rect = |x: CGFloat, y: CGFloat, w: CGFloat, h: CGFloat| CGRect::new(CGPoint::new(x, y), CGSize::new(w, h));
        if run.underline() != 0 {
            let ink = Rgba::of(if run.underline_color != 0 { run.underline_color } else { color }, 1.0);
            ink.fill(cg);
            ink.stroke(cg);
            let under = snap((y + s.baseline + t.max(s.points * 0.12)).min(y + s.cell_h - 3.0 * t));
            match run.underline() {
                2 => {
                    CGContext::set_should_antialias(Some(cg), false);
                    CGContext::fill_rect(Some(cg), rect(x0, under, x1 - x0, t));
                    CGContext::fill_rect(Some(cg), rect(x0, under + 2.0 * t, x1 - x0, t));
                }
                3 => {
                    CGContext::set_should_antialias(Some(cg), true);
                    CGContext::set_line_width(Some(cg), t);
                    CGContext::begin_path(Some(cg));
                    CGContext::move_to_point(Some(cg), x0, under + t);
                    let half = s.cell_w / 2.0;
                    let (mut x, mut up) = (x0, true);
                    while x < x1 {
                        let cy = if up { under - t } else { under + 3.0 * t };
                        CGContext::add_quad_curve_to_point(Some(cg), x + half / 2.0, cy, (x + half).min(x1), under + t);
                        x += half;
                        up = !up;
                    }
                    CGContext::stroke_path(Some(cg));
                }
                4 => {
                    CGContext::set_should_antialias(Some(cg), false);
                    let mut x = x0;
                    while x < x1 {
                        CGContext::fill_rect(Some(cg), rect(x, under, t.min(x1 - x), t));
                        x += 2.0 * t;
                    }
                }
                5 => {
                    CGContext::set_should_antialias(Some(cg), false);
                    let dash = (2.0 * t).max(snap(s.cell_w * 0.4));
                    let mut x = x0;
                    while x < x1 {
                        CGContext::fill_rect(Some(cg), rect(x, under, dash.min(x1 - x), t));
                        x += 2.0 * dash;
                    }
                }
                _ => {
                    CGContext::set_should_antialias(Some(cg), false);
                    CGContext::fill_rect(Some(cg), rect(x0, under, x1 - x0, t));
                }
            }
        }
        CGContext::set_should_antialias(Some(cg), false);
        Rgba::of(color, 1.0).fill(cg);
        if run.strike() {
            CGContext::fill_rect(Some(cg), rect(x0, snap(y + s.baseline - s.points * 0.3), x1 - x0, t));
        }
        if run.overline() {
            CGContext::fill_rect(Some(cg), rect(x0, y, x1 - x0, t));
        }
    }

    fn draw_cursor(s: &mut State, cg: &CGContext, height: CGFloat) {
        s.cursor_drawn = None;
        if s.vt.is_none() || s.cursor_row >= s.rows {
            return;
        }
        let line = (s.history.len() + s.cursor_row) as isize;
        let y = line_y(s, line);
        if y >= height || y + s.cell_h <= 0.0 {
            return;
        }
        s.cursor_drawn = Some(line);
        let x = s.cursor_col as CGFloat * s.cell_w;
        let rect = |x: CGFloat, y: CGFloat, w: CGFloat, h: CGFloat| CGRect::new(CGPoint::new(x, y), CGSize::new(w, h));
        if !s.marked.is_empty() {
            // The composition at the cursor, underlined; only the committed text is sent. Drawn while the program
            // hides the cursor too (Claude Code draws its own and hides the terminal's).
            let line = ct_line(&s.marked, &s.faces.regular);
            let w = line_width(&line);
            CGContext::set_should_antialias(Some(cg), false);
            Rgba::of(s.background, 1.0).fill(cg);
            CGContext::fill_rect(Some(cg), rect(x, y, w, s.cell_h));
            CGContext::set_should_antialias(Some(cg), true);
            Rgba::of(s.foreground, 1.0).fill(cg);
            CGContext::set_text_position(Some(cg), x, y + s.baseline);
            unsafe { line.draw(cg) };
            CGContext::set_should_antialias(Some(cg), false);
            CGContext::fill_rect(Some(cg), rect(x, y + s.cell_h - s.thickness, w, s.thickness));
            return;
        }
        if !s.cursor_visible || (s.cursor_blinks && s.focused && !s.blink_on) {
            return;
        }
        // The cell under the cursor (a wide character takes its two cells).
        let row = s.grid.get(s.cursor_row).cloned().unwrap_or_else(Line::empty);
        let under = row.cells.iter().position(|c| s.cursor_col >= c.run.col && s.cursor_col < c.run.col + c.run.cells);
        let wide = under.is_some_and(|k| !row.cells[k].run.plain() && row.cells[k].run.cells == 2);
        let cx = if wide { row.cells[under.unwrap_or(0)].run.col as CGFloat * s.cell_w } else { x };
        let w = if wide { 2.0 * s.cell_w } else { s.cell_w };
        let t = s.thickness;
        CGContext::set_should_antialias(Some(cg), false);
        Rgba::of(s.cursor_color, 1.0).fill(cg);
        if !s.focused || s.cursor_style == 3 {
            // Unfocused: a hollow block, the text stays readable.
            CGContext::fill_rect(Some(cg), rect(cx, y, w, t));
            CGContext::fill_rect(Some(cg), rect(cx, y + s.cell_h - t, w, t));
            CGContext::fill_rect(Some(cg), rect(cx, y, t, s.cell_h));
            CGContext::fill_rect(Some(cg), rect(cx + w - t, y, t, s.cell_h));
        } else if s.cursor_style == 0 {
            let bar = t.max((s.cell_w / 8.0 * s.pixel_scale).round() / s.pixel_scale);
            CGContext::fill_rect(Some(cg), rect(cx, y, bar, s.cell_h));
        } else if s.cursor_style == 2 {
            CGContext::fill_rect(Some(cg), rect(cx, y + s.cell_h - 2.0 * t, w, 2.0 * t));
        } else {
            CGContext::fill_rect(Some(cg), rect(cx, y, w, s.cell_h));
            // The character under a block cursor, in the background color.
            if let Some(k) = under {
                CGContext::save_g_state(Some(cg));
                CGContext::clip_to_rect(Some(cg), rect(cx, y, w, s.cell_h));
                let next = row.cells.get(k + 1).map(|c| c.run.clone());
                let cell = Cell { run: row.cells[k].run.clone(), items: RefCell::new(None) };
                let bg = s.background | 0xff00_0000;
                Self::draw_cell(s, &cell, y, cg, Some(bg), next.as_ref());
                CGContext::restore_g_state(Some(cg));
            }
        }
    }
}

fn max_scroll(s: &State) -> CGFloat {
    if s.alternate { 0.0 } else { s.history.len() as CGFloat * s.cell_h }
}

/// The scroll offset on the pixel grid.
fn snapped_scroll(s: &State) -> CGFloat {
    (s.scroll_y * s.pixel_scale).round() / s.pixel_scale
}

/// The y of content line `i`.
fn line_y(s: &State, i: isize) -> CGFloat {
    (i - s.history.len() as isize) as CGFloat * s.cell_h + snapped_scroll(s)
}

/// The content line under y (may be past either end).
fn line_under(s: &State, y: CGFloat) -> isize {
    s.history.len() as isize + ((y - snapped_scroll(s)) / s.cell_h.max(1.0)).floor() as isize
}
