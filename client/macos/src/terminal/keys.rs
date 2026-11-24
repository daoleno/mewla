//! What a key the terminal view takes itself sends to the program: the keys that are not text (Escape, Return, Tab,
//! Delete, the arrows and the navigation block) and Control with a letter. Everything else (letters, ⌥ compositions,
//! dead keys, an input method's text) goes through AppKit's text input (`interpretKeyEvents:`), which reports only
//! committed text. ⌘ is never the terminal's: it stays the menus'.
//!
//! Not tied to macOS (plain key codes and strings), so `cargo test` runs it anywhere.

/// The modifier keys held with a key.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Mods {
    pub shift: bool,
    pub control: bool,
    pub option: bool,
    pub command: bool,
}

/// AppKit's virtual key codes (Carbon `kVK_*`) the view takes itself.
pub mod code {
    pub const RETURN: u16 = 36;
    pub const TAB: u16 = 48;
    pub const DELETE: u16 = 51;
    pub const ESCAPE: u16 = 53;
    pub const KEYPAD_ENTER: u16 = 76;
    pub const HOME: u16 = 115;
    pub const PAGE_UP: u16 = 116;
    pub const FORWARD_DELETE: u16 = 117;
    pub const END: u16 = 119;
    pub const PAGE_DOWN: u16 = 121;
    pub const LEFT: u16 = 123;
    pub const RIGHT: u16 = 124;
    pub const DOWN: u16 = 125;
    pub const UP: u16 = 126;
}

/// The bytes for key `code` with `mods`, `plain` being what it types without modifiers
/// (`charactersIgnoringModifiers`); `None` leaves it to text input (or to the menus, for ⌘).
/// `app_cursor`: the program asked for application cursor keys (DECCKM).
pub fn key_bytes(code: u16, mods: Mods, plain: &str, app_cursor: bool) -> Option<String> {
    if mods.command {
        return None;
    }
    let arrow = |letter: char| -> String {
        let m = modifier_param(mods);
        if m > 1 {
            format!("\x1b[1;{m}{letter}")
        } else if app_cursor {
            format!("\x1bO{letter}")
        } else {
            format!("\x1b[{letter}")
        }
    };
    let tilde = |n: u8| -> String {
        let m = modifier_param(mods);
        if m > 1 { format!("\x1b[{n};{m}~") } else { format!("\x1b[{n}~") }
    };
    let out = match code {
        code::ESCAPE => "\x1b".to_string(),
        code::RETURN | code::KEYPAD_ENTER => if mods.option { "\x1b\r".to_string() } else { "\r".to_string() },
        code::TAB => if mods.shift { "\x1b[Z".to_string() } else { "\t".to_string() },
        code::DELETE => if mods.option { "\x1b\x7f".to_string() } else if mods.control { "\x08".to_string() } else { "\x7f".to_string() },
        code::FORWARD_DELETE => tilde(3),
        code::UP => arrow('A'),
        code::DOWN => arrow('B'),
        code::RIGHT => arrow('C'),
        code::LEFT => arrow('D'),
        code::HOME => arrow('H'),
        code::END => arrow('F'),
        code::PAGE_UP => tilde(5),
        code::PAGE_DOWN => tilde(6),
        _ if mods.control => return control(plain),
        _ => return None,
    };
    Some(out)
}

/// xterm's modifier parameter: 1 + shift 1 + option 2 + control 4.
fn modifier_param(mods: Mods) -> u8 {
    1 + u8::from(mods.shift) + 2 * u8::from(mods.option) + 4 * u8::from(mods.control)
}

/// Control with what the key types: a letter or `@[\]^_` and space become their control character
/// (`TerminalPane.tsx` `withControl` does the same for the phones' key bar).
fn control(plain: &str) -> Option<String> {
    let mut chars = plain.chars();
    let (Some(c), None) = (chars.next(), chars.next()) else { return None };
    let c = c.to_ascii_lowercase();
    let byte = match c {
        'a'..='z' => c as u8 - b'a' + 1,
        '@' | ' ' | '2' => 0,
        '[' | '3' => 0x1b,
        '\\' | '4' => 0x1c,
        ']' | '5' => 0x1d,
        '^' | '6' => 0x1e,
        '_' | '-' | '7' => 0x1f,
        '/' => 0x1f,
        '8' | '?' => 0x7f,
        _ => return None,
    };
    Some((byte as char).to_string())
}

/// Whether key `code` is one the view takes itself (not text): Escape, Return, Tab, Delete, the arrows, the
/// navigation block and the function keys.
pub fn is_special(code: u16) -> bool {
    matches!(
        code,
        code::ESCAPE | code::RETURN | code::KEYPAD_ENTER | code::TAB | code::DELETE | code::FORWARD_DELETE
            | code::UP | code::DOWN | code::LEFT | code::RIGHT | code::HOME | code::END | code::PAGE_UP | code::PAGE_DOWN
            | 114 | 122 | 120 | 99 | 118 | 96 | 97 | 98 | 100 | 101 | 109 | 103 | 111
    )
}

/// The GhosttyKey (W3C `KeyboardEvent.code`, libghostty-vt's key/event.h) of AppKit's physical key `code` on an
/// ANSI keyboard; 0 (unidentified) for one it does not name.
pub fn ghostty_key(code: u16) -> i32 {
    // A..Z are 20..45 in GhosttyKey; AppKit's codes for them follow the ANSI layout.
    const LETTERS: [(u16, char); 26] = [
        (0, 'a'), (11, 'b'), (8, 'c'), (2, 'd'), (14, 'e'), (3, 'f'), (5, 'g'), (4, 'h'), (34, 'i'), (38, 'j'),
        (40, 'k'), (37, 'l'), (46, 'm'), (45, 'n'), (31, 'o'), (35, 'p'), (12, 'q'), (15, 'r'), (1, 's'), (17, 't'),
        (32, 'u'), (9, 'v'), (13, 'w'), (7, 'x'), (16, 'y'), (6, 'z'),
    ];
    if let Some((_, c)) = LETTERS.iter().find(|(k, _)| *k == code) {
        return 20 + (*c as i32 - 'a' as i32);
    }
    match code {
        29 => 6,  // 0
        18 => 7,  // 1
        19 => 8,  // 2
        20 => 9,  // 3
        21 => 10, // 4
        23 => 11, // 5
        22 => 12, // 6
        26 => 13, // 7
        28 => 14, // 8
        25 => 15, // 9
        50 => 1,  // `
        42 => 2,  // \
        33 => 3,  // [
        30 => 4,  // ]
        43 => 5,  // ,
        24 => 16, // =
        27 => 46, // -
        47 => 47, // .
        39 => 48, // '
        41 => 49, // ;
        44 => 50, // /
        code::DELETE => 53,
        code::RETURN => 58,
        49 => 63, // space
        code::TAB => 64,
        code::FORWARD_DELETE => 68,
        code::END => 69,
        code::HOME => 71,
        114 => 72, // Help/Insert
        code::PAGE_DOWN => 73,
        code::PAGE_UP => 74,
        code::DOWN => 75,
        code::LEFT => 76,
        code::RIGHT => 77,
        code::UP => 78,
        code::KEYPAD_ENTER => 97,
        code::ESCAPE => 120,
        122 => 121, // F1
        120 => 122,
        99 => 123,
        118 => 124,
        96 => 125,
        97 => 126,
        98 => 127,
        100 => 128,
        101 => 129,
        109 => 130,
        103 => 131,
        111 => 132, // F12
        _ => 0,
    }
}

/// GhosttyMods for `mods` (key/event.h: shift 1, ctrl 2, alt 4, super 8).
pub fn ghostty_mods(mods: Mods) -> u16 {
    u16::from(mods.shift) | u16::from(mods.control) << 1 | u16::from(mods.option) << 2 | u16::from(mods.command) << 3
}

/// The bytes as two hex digits each, for the log.
#[cfg(test)]
pub fn hex(data: &str) -> String {
    data.bytes().map(|b| format!("{b:02x}")).collect::<Vec<_>>().join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;

    const NONE: Mods = Mods { shift: false, control: false, option: false, command: false };

    #[test]
    fn keys_that_are_not_text() {
        assert_eq!(key_bytes(code::ESCAPE, NONE, "\u{1b}", false).as_deref(), Some("\x1b"));
        assert_eq!(key_bytes(code::RETURN, NONE, "\r", false).as_deref(), Some("\r"));
        assert_eq!(key_bytes(code::TAB, NONE, "\t", false).as_deref(), Some("\t"));
        assert_eq!(key_bytes(code::TAB, Mods { shift: true, ..NONE }, "\t", false).as_deref(), Some("\x1b[Z"));
        assert_eq!(key_bytes(code::DELETE, NONE, "\u{7f}", false).as_deref(), Some("\x7f"));
        assert_eq!(key_bytes(code::UP, NONE, "", false).as_deref(), Some("\x1b[A"));
        assert_eq!(key_bytes(code::UP, NONE, "", true).as_deref(), Some("\x1bOA"));
        assert_eq!(key_bytes(code::LEFT, Mods { shift: true, ..NONE }, "", true).as_deref(), Some("\x1b[1;2D"));
        assert_eq!(key_bytes(code::PAGE_DOWN, NONE, "", false).as_deref(), Some("\x1b[6~"));
        assert_eq!(key_bytes(code::FORWARD_DELETE, NONE, "", false).as_deref(), Some("\x1b[3~"));
    }

    #[test]
    fn control_letters() {
        let ctrl = Mods { control: true, ..NONE };
        assert_eq!(key_bytes(8, ctrl, "c", false).as_deref(), Some("\x03"));
        assert_eq!(key_bytes(8, Mods { shift: true, ..ctrl }, "C", false).as_deref(), Some("\x03"));
        assert_eq!(key_bytes(33, ctrl, "[", false).as_deref(), Some("\x1b"));
        assert_eq!(key_bytes(49, ctrl, " ", false).as_deref(), Some("\0"));
        assert_eq!(key_bytes(18, ctrl, "é", false), None);
    }

    #[test]
    fn text_and_command_are_not_the_views() {
        assert_eq!(key_bytes(0, NONE, "a", false), None);
        assert_eq!(key_bytes(14, Mods { option: true, ..NONE }, "e", false), None);
        assert_eq!(key_bytes(9, Mods { command: true, ..NONE }, "v", false), None);
        assert_eq!(key_bytes(code::UP, Mods { command: true, ..NONE }, "", false), None);
    }

    #[test]
    fn ghostty_keys() {
        assert_eq!(ghostty_key(0), 20); // a
        assert_eq!(ghostty_key(6), 45); // z
        assert_eq!(ghostty_key(8), 22); // c
        assert_eq!(ghostty_key(code::UP), 78);
        assert_eq!(ghostty_key(code::ESCAPE), 120);
        assert_eq!(ghostty_key(111), 132);
        assert_eq!(ghostty_key(200), 0);
        assert!(is_special(code::LEFT) && is_special(122) && !is_special(0));
        assert_eq!(ghostty_mods(Mods { shift: true, control: true, ..NONE }), 3);
    }

    #[test]
    fn hex_of_bytes() {
        assert_eq!(hex("\x1b[A"), "1b 5b 41");
        assert_eq!(hex("é"), "c3 a9");
    }
}
