// Glyphs the terminal draws itself instead of taking them from a font: box drawing
// (U+2500-257F), block elements (U+2580-259F), braille (U+2800-28FF), the media marks
// (U+23F4-23FA) and the Powerline separators (U+E0B0-E0BF). They are built for an exact cell in whole device pixels, so
// lines and blocks meet their neighbours with no gap or overlap, whatever the font.
// Shared by Android (TerminalView.kt through jni_terminal.cpp) and iOS (TerminalView.swift
// through the bridging header). Pure C++, no libghostty: scripts/tests/terminal_glyphs.cpp.
#pragma once

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/// One drawing step, in pixels from the cell's top-left, in the cell's foreground color.
typedef enum {
  /// Fill the rectangle p[0],p[1] .. p[2],p[3] (whole pixels).
  MEWLA_GLYPH_RECT = 1,
  /// Fill the polygon of `points` points p[0..2*points).
  MEWLA_GLYPH_POLYGON = 2,
  /// Stroke the line p[0],p[1] -> p[2],p[3], `stroke` wide, butt ends.
  MEWLA_GLYPH_LINE = 3,
  /// Stroke the cubic p[0],p[1] (c1 p[2],p[3]) (c2 p[4],p[5]) -> p[6],p[7], `stroke` wide, butt ends.
  MEWLA_GLYPH_CURVE = 4,
  /// Fill the region the cubic p[0],p[1] .. p[6],p[7] closes with a straight line back.
  MEWLA_GLYPH_CURVE_FILL = 5,
  /// Fill the ellipse in the rectangle p[0],p[1] .. p[2],p[3].
  MEWLA_GLYPH_ELLIPSE = 6,
} MewlaGlyphKind;

typedef struct {
  int32_t kind;
  int32_t points;
  /// 1 opaque; shades (U+2591-2593) are partial.
  float alpha;
  float stroke;
  float p[8];
} MewlaGlyphOp;

/// Whether `codepoint` is drawn by mewla_glyph_ops (cheap; no allocation).
bool mewla_glyph_is_drawn(uint32_t codepoint);

/// The steps that draw `codepoint` in a `width` x `height` pixel cell: at most `capacity`
/// written to `out`; returns how many (0: not one of these glyphs, the font draws it).
int32_t mewla_glyph_ops(uint32_t codepoint, int32_t width, int32_t height, MewlaGlyphOp *out, int32_t capacity);

/// The light line thickness for a cell `height` pixels tall (heavy is twice it): also the
/// underline and strikethrough thickness, so every line in the terminal has one weight.
int32_t mewla_glyph_thickness(int32_t height);

#ifdef __cplusplus
}
#endif
