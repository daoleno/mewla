//! libghostty-vt through the packer the phones share (client/native/terminal/TerminalVt.h, TerminalGlyphs.h, built
//! by build.rs), as client/ios's Vt.swift: one VT per terminal view (and one for its history), its frames read into
//! rows of runs. The frame layout's parsing is plain Rust (`Frame::parse`), tested anywhere.

// Off the Mac only the parsing is built (for its tests): the rest of the layout is the view's.
#![cfg_attr(not(target_os = "macos"), allow(dead_code))]

/// TerminalVt.h's frame layout and flags (kept in step with its enum).
pub mod layout {
    pub const HEADER: usize = 10;
    pub const ROW: usize = 3;
    pub const RUN: usize = 8;
    pub const BOLD: i32 = 1;
    pub const ITALIC: i32 = 2;
    pub const STRIKE: i32 = 16;
    pub const OVERLINE: i32 = 32;
    pub const CLUSTER: i32 = 128;
    pub const UNDERLINE_SHIFT: i32 = 8;
    pub const EMOJI_WIDE: i32 = 1 << 11;
    pub const FLAG_HEAD: i32 = 1 << 12;
    pub const FLAG_TAIL: i32 = 1 << 13;
}

/// One run of cells in one style: plain (one UTF-16 unit per cell) or a single cell (a wide character, a grapheme
/// of several code points, a code point outside the BMP).
#[derive(Clone, Debug, PartialEq)]
pub struct Run {
    pub col: usize,
    pub cells: usize,
    /// Final 0xAARRGGBB (inverse and faint applied); `bg` 0 is the theme's background.
    pub fg: u32,
    pub bg: u32,
    pub flags: i32,
    /// The underline's color, 0 = `fg`.
    pub underline_color: u32,
    pub units: Vec<u16>,
}

impl Run {
    pub fn plain(&self) -> bool {
        self.flags & layout::CLUSTER == 0
    }
    pub fn bold(&self) -> bool {
        self.flags & layout::BOLD != 0
    }
    pub fn italic(&self) -> bool {
        self.flags & layout::ITALIC != 0
    }
    pub fn strike(&self) -> bool {
        self.flags & layout::STRIKE != 0
    }
    pub fn overline(&self) -> bool {
        self.flags & layout::OVERLINE != 0
    }
    /// 0 none, 1 single, 2 double, 3 curly, 4 dotted, 5 dashed.
    pub fn underline(&self) -> i32 {
        (self.flags >> layout::UNDERLINE_SHIFT) & 7
    }
}

/// A changed row: its runs, and whether it goes on in the next one.
#[derive(Clone, Debug, PartialEq)]
pub struct DirtyRow {
    pub row: usize,
    pub wrapped: bool,
    pub runs: Vec<Run>,
}

/// What changed since the last frame.
#[derive(Clone, Debug, PartialEq)]
pub struct Frame {
    pub rows: usize,
    pub cols: usize,
    pub cursor_col: usize,
    pub cursor_row: usize,
    pub cursor_visible: bool,
    pub full: bool,
    /// 0 bar, 1 block, 2 underline, 3 hollow block.
    pub cursor_style: i32,
    pub cursor_blinking: bool,
    pub dirty: Vec<DirtyRow>,
}

impl Frame {
    /// `meta` and `text` as mewla_vt_frame hands them over; None when the header is short.
    pub fn parse(meta: &[i32], text: &[u16]) -> Option<Frame> {
        use layout::{HEADER, ROW, RUN};
        if meta.len() < HEADER {
            return None;
        }
        let count = meta[6].max(0) as usize;
        let mut dirty = Vec::with_capacity(count);
        let mut p = HEADER;
        for _ in 0..count {
            if p + ROW > meta.len() {
                break;
            }
            let row = meta[p].max(0) as usize;
            let runs_n = meta[p + 1].max(0) as usize;
            let wrapped = meta[p + 2] == 1;
            p += ROW;
            let mut runs = Vec::with_capacity(runs_n);
            for _ in 0..runs_n {
                if p + RUN > meta.len() {
                    break;
                }
                let (start, length) = (meta[p + 6], meta[p + 7]);
                let units = if start >= 0 && length >= 0 && (start + length) as usize <= text.len() {
                    text[start as usize..(start + length) as usize].to_vec()
                } else {
                    Vec::new()
                };
                runs.push(Run {
                    col: meta[p].max(0) as usize,
                    cells: meta[p + 1].max(0) as usize,
                    fg: meta[p + 2] as u32,
                    bg: meta[p + 3] as u32,
                    flags: meta[p + 4],
                    underline_color: meta[p + 5] as u32,
                    units,
                });
                p += RUN;
            }
            dirty.push(DirtyRow { row, wrapped, runs });
        }
        Some(Frame {
            rows: meta[0].max(0) as usize,
            cols: meta[1].max(0) as usize,
            cursor_col: meta[2].max(0) as usize,
            cursor_row: meta[3].max(0) as usize,
            cursor_visible: meta[4] == 1,
            full: meta[5] == 1,
            cursor_style: meta[7],
            cursor_blinking: meta[8] == 1,
            dirty,
        })
    }
}

/// A theme color: CSS hex `#rrggbb` or `#rrggbbaa` → (0xRRGGBB, alpha 0…1).
pub fn css_color(value: &str) -> Option<(u32, f64)> {
    let hex = value.trim().strip_prefix('#')?;
    let v = u32::from_str_radix(hex, 16).ok()?;
    match hex.len() {
        6 => Some((v, 1.0)),
        8 => Some((v >> 8, (v & 0xff) as f64 / 255.0)),
        _ => None,
    }
}

/// The 256-color palette from the theme's 16: xterm's 6×6×6 cube and gray ramp after them.
pub fn palette256(ansi: &[u32]) -> Vec<u32> {
    let mut out: Vec<u32> = (0..16).map(|i| ansi.get(i).copied().unwrap_or(0x808080)).collect();
    let steps = [0u32, 95, 135, 175, 215, 255];
    for r in steps {
        for g in steps {
            for b in steps {
                out.push(r << 16 | g << 8 | b);
            }
        }
    }
    for i in 0..24u32 {
        let v = 8 + i * 10;
        out.push(v << 16 | v << 8 | v);
    }
    out
}

/// GhosttyKeyAction.
pub const KEY_PRESS: i32 = 1;
pub const KEY_REPEAT: i32 = 2;

#[cfg(target_os = "macos")]
pub use native::*;

#[cfg(target_os = "macos")]
mod native {
    use std::ffi::c_void;

    use super::Frame;

    #[repr(C)]
    struct RawFrame {
        meta: *const i32,
        meta_count: usize,
        text: *const u16,
        text_count: usize,
    }

    /// One drawing step of a box-drawing, block, braille or Powerline glyph (TerminalGlyphs.h), in pixels.
    #[repr(C)]
    #[derive(Clone, Copy, Debug, Default)]
    pub struct GlyphOp {
        pub kind: i32,
        pub points: i32,
        pub alpha: f32,
        pub stroke: f32,
        pub p: [f32; 8],
    }

    unsafe extern "C" {
        fn mewla_vt_create(cols: u16, rows: u16) -> *mut c_void;
        fn mewla_vt_destroy(vt: *mut c_void);
        fn mewla_vt_resize(vt: *mut c_void, cols: u16, rows: u16, cell_width: f64, cell_height: f64);
        fn mewla_vt_set_theme(vt: *mut c_void, fg: u32, bg: u32, cursor: u32, palette: *const u32, count: usize) -> bool;
        fn mewla_vt_write(vt: *mut c_void, data: *const u8, length: usize);
        fn mewla_vt_frame(vt: *mut c_void, force_full: bool, out: *mut RawFrame) -> bool;
        fn mewla_vt_paste(vt: *mut c_void, data: *const u8, length: usize, out: *mut *const u8, out_length: *mut usize) -> bool;
        fn mewla_vt_take_clipboard(vt: *mut c_void, data: *mut *const u8, length: *mut usize) -> bool;
        fn mewla_vt_key(
            vt: *mut c_void,
            key: i32,
            mods: u16,
            action: i32,
            text: *const u8,
            text_length: usize,
            unshifted: u32,
            out: *mut *const u8,
            out_length: *mut usize,
        ) -> bool;
        #[allow(dead_code)]
        fn mewla_vt_link(vt: *mut c_void, col: u16, row: u16, out: *mut *const u8, out_length: *mut usize) -> bool;
        fn mewla_vt_focus(vt: *mut c_void, focused: bool, out: *mut *const u8, out_length: *mut usize) -> bool;
        fn mewla_glyph_is_drawn(codepoint: u32) -> bool;
        fn mewla_glyph_ops(codepoint: u32, width: i32, height: i32, out: *mut GlyphOp, capacity: i32) -> i32;
        fn mewla_glyph_thickness(height: i32) -> i32;
    }

    /// Bytes the packer handed over (valid until its next call), as text for the program.
    fn taken(ok: bool, out: *const u8, len: usize) -> Option<String> {
        if !ok || out.is_null() || len == 0 {
            return None;
        }
        // SAFETY: the packer's buffer, `len` bytes, valid until its next call on the VT; copied at once.
        let bytes = unsafe { std::slice::from_raw_parts(out, len) };
        Some(String::from_utf8_lossy(bytes).into_owned())
    }

    /// One libghostty-vt terminal; freed with it.
    pub struct Vt(*mut c_void);

    impl Drop for Vt {
        fn drop(&mut self) {
            // SAFETY: made by mewla_vt_create, freed once.
            unsafe { mewla_vt_destroy(self.0) }
        }
    }

    fn dim(n: usize) -> Option<u16> {
        (1..=u16::MAX as usize).contains(&n).then_some(n as u16)
    }

    impl Vt {
        pub fn new(cols: usize, rows: usize) -> Option<Vt> {
            let (c, r) = (dim(cols)?, dim(rows)?);
            // SAFETY: plain sizes in; NULL when the library refuses.
            let handle = unsafe { mewla_vt_create(c, r) };
            (!handle.is_null()).then_some(Vt(handle))
        }

        /// Cell sizes in pixels.
        pub fn resize(&self, cols: usize, rows: usize, cell_w: f64, cell_h: f64) {
            let (Some(c), Some(r)) = (dim(cols), dim(rows)) else { return };
            unsafe { mewla_vt_resize(self.0, c, r, cell_w, cell_h) }
        }

        /// Opaque 0xRRGGBB colors; `palette` is palette256's 256.
        pub fn set_theme(&self, fg: u32, bg: u32, cursor: u32, palette: &[u32]) -> bool {
            unsafe { mewla_vt_set_theme(self.0, fg, bg, cursor, palette.as_ptr(), palette.len()) }
        }

        pub fn write(&self, data: &str) {
            unsafe { mewla_vt_write(self.0, data.as_ptr(), data.len()) }
        }

        pub fn frame(&self, full: bool) -> Option<Frame> {
            let mut raw = RawFrame { meta: std::ptr::null(), meta_count: 0, text: std::ptr::null(), text_count: 0 };
            // SAFETY: the out struct is ours; its buffers are the packer's until its next call, read at once.
            if !unsafe { mewla_vt_frame(self.0, full, &mut raw) } || raw.meta.is_null() {
                return None;
            }
            let meta = unsafe { std::slice::from_raw_parts(raw.meta, raw.meta_count) };
            let text = if raw.text.is_null() { &[][..] } else { unsafe { std::slice::from_raw_parts(raw.text, raw.text_count) } };
            Frame::parse(meta, text)
        }

        /// The text a program copied (OSC 52 and the like) during the writes since the last call, once.
        pub fn take_clipboard(&self) -> Option<String> {
            let (mut out, mut len) = (std::ptr::null(), 0usize);
            let ok = unsafe { mewla_vt_take_clipboard(self.0, &mut out, &mut len) };
            taken(ok, out, len)
        }

        /// Pasted text as the bytes for the program (framed for bracketed paste when it asked).
        pub fn paste(&self, text: &str) -> Option<String> {
            let (mut out, mut len) = (std::ptr::null(), 0usize);
            let ok = unsafe { mewla_vt_paste(self.0, text.as_ptr(), text.len(), &mut out, &mut len) };
            taken(ok, out, len)
        }

        /// A key as the program wants it now (cursor and keypad modes, Kitty flags, modifyOtherKeys).
        pub fn key(&self, key: i32, mods: u16, action: i32, text: &str, unshifted: u32) -> Option<String> {
            let (mut out, mut len) = (std::ptr::null(), 0usize);
            let ok = unsafe { mewla_vt_key(self.0, key, mods, action, text.as_ptr(), text.len(), unshifted, &mut out, &mut len) };
            taken(ok, out, len)
        }

        /// The OSC 8 hyperlink on screen cell col, row (links: the next slice).
        #[allow(dead_code)]
        pub fn link(&self, col: usize, row: usize) -> Option<String> {
            let (Some(c), Some(r)) = (u16::try_from(col).ok(), u16::try_from(row).ok()) else { return None };
            let (mut out, mut len) = (std::ptr::null(), 0usize);
            let ok = unsafe { mewla_vt_link(self.0, c, r, &mut out, &mut len) };
            taken(ok, out, len)
        }

        /// Focus gained or lost, as the bytes to send when the program asked for focus reports.
        pub fn focus(&self, focused: bool) -> Option<String> {
            let (mut out, mut len) = (std::ptr::null(), 0usize);
            let ok = unsafe { mewla_vt_focus(self.0, focused, &mut out, &mut len) };
            taken(ok, out, len)
        }
    }

    pub fn glyph_is_drawn(cp: u32) -> bool {
        unsafe { mewla_glyph_is_drawn(cp) }
    }

    /// A drawn glyph's steps for a cell `width` × `height` pixels.
    pub fn glyph_ops(cp: u32, width: i32, height: i32) -> Vec<GlyphOp> {
        let mut buffer = [GlyphOp::default(); 64];
        let n = unsafe { mewla_glyph_ops(cp, width, height, buffer.as_mut_ptr(), buffer.len() as i32) };
        buffer[..n.clamp(0, 64) as usize].to_vec()
    }

    pub fn glyph_thickness(height: i32) -> i32 {
        unsafe { mewla_glyph_thickness(height) }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_rows_and_runs() {
        // 2 rows × 5 cols, cursor at 1,0 visible, full, one dirty row with two runs, block cursor, not blinking.
        let meta = [
            2, 5, 1, 0, 1, 1, 1, 1, 0, 0, // header
            0, 2, 1, // row 0, 2 runs, wrapped
            0, 2, 0xff112233u32 as i32, 0, layout::BOLD, 0, 0, 2, // "ab"
            2, 2, 0xff445566u32 as i32, 0xff000000u32 as i32, layout::CLUSTER, 0, 2, 1, // "界" in two cells
        ];
        let text = [b'a' as u16, b'b' as u16, 0x754c];
        let frame = Frame::parse(&meta, &text).expect("a frame");
        assert_eq!((frame.rows, frame.cols, frame.cursor_col, frame.cursor_row), (2, 5, 1, 0));
        assert!(frame.cursor_visible && frame.full && !frame.cursor_blinking);
        assert_eq!(frame.cursor_style, 1);
        assert_eq!(frame.dirty.len(), 1);
        let row = &frame.dirty[0];
        assert!(row.wrapped);
        assert_eq!(row.runs[0].units, vec![b'a' as u16, b'b' as u16]);
        assert!(row.runs[0].bold() && row.runs[0].plain());
        assert_eq!(row.runs[1].units, vec![0x754c]);
        assert!(!row.runs[1].plain());
        assert_eq!(row.runs[1].fg, 0xff445566);
    }

    #[test]
    fn short_or_out_of_range_frames() {
        assert_eq!(Frame::parse(&[1, 2, 3], &[]), None);
        // A run whose text is out of range keeps its cells and no text.
        let meta = [1, 3, 0, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 3, 0, 0, 0, 0, 9, 3];
        let frame = Frame::parse(&meta, &[]).expect("a frame");
        assert_eq!(frame.dirty[0].runs[0].cells, 3);
        assert!(frame.dirty[0].runs[0].units.is_empty());
    }

    #[test]
    fn colors() {
        assert_eq!(css_color("#1d1b19"), Some((0x1d1b19, 1.0)));
        assert_eq!(css_color("#F2F3F533").map(|(c, _)| c), Some(0xF2F3F5));
        assert_eq!(css_color("red"), None);
        let p = palette256(&[0x000000, 0xff0000]);
        assert_eq!(p.len(), 256);
        assert_eq!(p[1], 0xff0000);
        assert_eq!(p[2], 0x808080);
        assert_eq!(p[16], 0x000000);
        assert_eq!(p[231], 0xffffff);
        assert_eq!(p[255], 0xeeeeee);
    }
}
