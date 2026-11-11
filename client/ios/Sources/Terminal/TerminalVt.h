// libghostty-vt for the terminal view (TerminalView.swift), as a small C API Swift calls
// through the bridging header. TerminalVt.cpp is the iOS twin of
// client/android/app/src/main/cpp/jni_bridge.cpp's nativeRowRuns path. Keep <ghostty/vt.h>
// out of this header: Swift and the tests see only these declarations.
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
/// [rows, cols, cursorCol, cursorRow, cursorVisible, full, dirtyRowCount,
///  (row, runCount, (col, cells, fg, bg, flags, textStart, textLength)*)*]:
/// fg/bg are 0xAARRGGBB (bg 0 = the theme's background), flags are
/// 1 bold, 2 italic, 4 underline, 8 inverse, and each run's text is
/// text[textStart ..< textStart + textLength] in UTF-16. Valid until the next call on `vt`.
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
bool mewla_vt_take_clipboard(MewlaVt *vt, const uint8_t *_Nullable *_Nonnull data, size_t *length);

#pragma clang assume_nonnull end

#ifdef __cplusplus
}
#endif
