// libghostty-vt for the terminal views, as a small C API: Android's TerminalView.kt through
// client/android/app/src/main/cpp/jni_terminal.cpp, iOS's TerminalView.swift through the bridging
// header. One packer for both, tested on the host (scripts/tests/terminal_corpus_cells.cpp,
// terminal_vt_conceal.cpp). Keep <ghostty/vt.h> out of this header: Swift and the tests see only
// these declarations.
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#pragma clang assume_nonnull begin

typedef struct MewlaVt MewlaVt;

/// What changed since the last frame. `meta` is
/// [rows, cols, cursorCol, cursorRow, cursorVisible, full, dirtyRowCount, cursorStyle, cursorBlinking, cursorWideTail,
///  (row, runCount, wrapped, (col, cells, fg, bg, flags, underlineColor, textStart, textLength)*)*]
/// (MEWLA_VT_HEADER, MEWLA_VT_ROW, MEWLA_VT_RUN ints).
/// - cursorStyle: 0 bar, 1 block, 2 underline, 3 hollow block. `wrapped`: the row goes on in the next one.
/// - fg/bg are final 0xAARRGGBB: inverse and faint are applied (bg 0 = the theme's background).
///   underlineColor 0 = the run's fg.
/// - flags: MEWLA_VT_BOLD ... ; bits 8-10 the underline style (MEWLA_VT_UNDERLINE_SHIFT): 0 none,
///   1 single, 2 double, 3 curly, 4 dotted, 5 dashed.
/// - A run's text is text[textStart ..< textStart + textLength] in UTF-16. A run is either plain
///   (one UTF-16 unit per cell, every cell narrow) or, flagged MEWLA_VT_CLUSTER, a single cell: a wide
///   character, a grapheme of several code points, or a code point outside the BMP (lengths alone
///   cannot tell: a wide emoji is two units in two cells). Blank cells with no background and no
///   decoration are left out.
/// Valid until the next call on `vt`.
enum {
  MEWLA_VT_HEADER = 10,
  MEWLA_VT_ROW = 3,
  MEWLA_VT_RUN = 8,
  MEWLA_VT_BOLD = 1,
  MEWLA_VT_ITALIC = 2,
  MEWLA_VT_FAINT = 4,
  MEWLA_VT_INVERSE = 8,
  MEWLA_VT_STRIKE = 16,
  MEWLA_VT_OVERLINE = 32,
  MEWLA_VT_BLINK = 64,
  MEWLA_VT_CLUSTER = 128,
  MEWLA_VT_UNDERLINE_SHIFT = 8,
  /// Drawing hints (the grid and text are the VT's): a one-cell emoji (VS16 or keycap) whose next cell
  /// is blank, drawn at full size across both; two adjacent regional indicators, the head drawing the
  /// flag across both runs' cells and the tail no glyph.
  MEWLA_VT_EMOJI_WIDE = 1 << 11,
  MEWLA_VT_FLAG_HEAD = 1 << 12,
  MEWLA_VT_FLAG_TAIL = 1 << 13,
};
typedef struct {
  const int32_t *_Nullable meta;
  size_t meta_count;
  const uint16_t *_Nullable text;
  size_t text_count;
} MewlaVtFrame;

/// NULL when the library refuses (sizes out of range, a version mismatch).
MewlaVt *_Nullable mewla_vt_create(uint16_t cols, uint16_t rows);
void mewla_vt_destroy(MewlaVt *_Nullable vt);
/// Cell sizes in pixels (what pixel-reporting programs are told).
void mewla_vt_resize(MewlaVt *vt, uint16_t cols, uint16_t rows, double cell_width, double cell_height);
/// Colors are 0xRRGGBB; `palette` holds `palette_count` (256) of them.
bool mewla_vt_set_theme(MewlaVt *vt, uint32_t foreground, uint32_t background, uint32_t cursor, const uint32_t *palette, size_t palette_count);
/// The computer's output, UTF-8 bytes.
void mewla_vt_write(MewlaVt *vt, const uint8_t *_Nullable data, size_t length);
/// The rows that changed (every row with `force_full`, or after a resize or a theme).
bool mewla_vt_frame(MewlaVt *vt, bool force_full, MewlaVtFrame *out);
/// The text a program copied (OSC 52, OSC 1337 Copy, OSC 5522) during the writes since the
/// last call, once: UTF-8 in `*data` (valid until the next call on `vt`). False when none.
/// Pasted text (UTF-8) as the bytes to send the program: framed when it asked for bracketed paste
/// (mode 2004), control bytes stripped, newlines as returns otherwise. `*out` is valid until the next
/// call on `vt`. False when there is nothing to send.
bool mewla_vt_paste(MewlaVt *vt, const uint8_t *_Nullable data, size_t length, const uint8_t *_Nullable *_Nonnull out, size_t *out_length);
bool mewla_vt_take_clipboard(MewlaVt *vt, const uint8_t *_Nullable *_Nonnull data, size_t *length);
/// A key as the bytes to send the program, encoded by libghostty-vt for the program's modes now (cursor
/// and keypad keys, Kitty keyboard flags, modifyOtherKeys). `key` is a GhosttyKey (the W3C
/// KeyboardEvent.code set), `mods` GhosttyMods, `action` a GhosttyKeyAction; `text` the UTF-8 the key
/// types (may be empty), `unshifted` the code point it types without Shift (0 when unknown). In `*out`
/// until the next call on `vt`; false when the key sends nothing.
bool mewla_vt_key(MewlaVt *vt, int32_t key, uint16_t mods, int32_t action, const uint8_t *_Nullable text, size_t text_length,
                  uint32_t unshifted, const uint8_t *_Nullable *_Nonnull out, size_t *out_length);
/// Whether the program asked for mouse reports (then a plain drag is the program's, not a selection).
bool mewla_vt_mouse_tracking(MewlaVt *vt);
/// A mouse press, release or move (a GhosttyMouseAction; `button` a GhosttyMouseButton, 0 for none) at
/// x, y pixels from the grid's top-left, as the bytes to send; false when the program does not report it.
bool mewla_vt_mouse(MewlaVt *vt, int32_t action, int32_t button, uint16_t mods, double x, double y,
                    const uint8_t *_Nullable *_Nonnull out, size_t *out_length);
/// Focus gained or lost, as the bytes to send when the program asked for focus reports (mode 1004).
bool mewla_vt_focus(MewlaVt *vt, bool focused, const uint8_t *_Nullable *_Nonnull out, size_t *out_length);

#pragma clang assume_nonnull end

#ifdef __cplusplus
}
#endif
