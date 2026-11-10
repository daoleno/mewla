// libghostty-vt behind TerminalVt.h. A trimmed copy of the Expo module's bridge
// (app/modules/terminal-vt/android/src/main/cpp/jni_bridge.cpp, which its iOS
// TerminalVtBridge.mm includes): the terminal, its render state and the compact
// cell runs Android's client/android/app/src/main/cpp/jni_bridge.cpp hands its
// Canvas renderer. No HTML formatter, no mouse encoder.
#include "TerminalVt.h"

#include <array>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>
#include <ghostty/vt.h>

#define LOGE(...) do { std::fprintf(stderr, "[TerminalVt] "); std::fprintf(stderr, __VA_ARGS__); std::fprintf(stderr, "\n"); } while (0)

// The core's scrollback is a byte budget, rounded up to whole pages: 10000 bytes
// keeps one page. Nothing reads it: the view renders the viewport, and history is
// tmux's copy mode on the computer (the same depth as the Android bridge).
static constexpr size_t kScrollbackMaxBytes = 10000;

struct MewlaVt {
    GhosttyTerminal terminal = nullptr;
    GhosttyRenderState render_state = nullptr;
    uint16_t cols = 0;
    uint16_t rows = 0;
    bool force_full = true;
    std::vector<int32_t> meta;
    std::u16string text;
};

static GhosttyResult createTerminalInstance(uint16_t cols, uint16_t rows, GhosttyTerminal* out)
{
    // The headers this bridge was compiled against and the library linked in must
    // agree on the major version, or sized structs and enum values disagree.
    const char* libraryVersion = ghostty_version();
    if (!libraryVersion || std::atoi(libraryVersion) != GHOSTTY_VT_VERSION_MAJOR) {
        LOGE("libghostty-vt %s does not match headers %s",
            libraryVersion ? libraryVersion : "(unknown)", GHOSTTY_VT_VERSION);
        return GHOSTTY_INVALID_VALUE;
    }

    GhosttyResult res = ghostty_terminal_new(nullptr, out, cols, rows);
    if (res != GHOSTTY_SUCCESS) {
        return res;
    }
    const size_t scrollbackMaxBytes = kScrollbackMaxBytes;
    res = ghostty_terminal_set(*out, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_BYTES, &scrollbackMaxBytes);
    if (res != GHOSTTY_SUCCESS) {
        ghostty_terminal_free(*out);
        *out = nullptr;
    }
    return res;
}

static GhosttyColorRgb resolveEffectiveForeground(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedFg,
    const GhosttyColorRgb& resolvedFg)
{
    if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_PALETTE) {
        uint8_t paletteIndex = style.fg_color.value.palette;
        if ((style.flags & GHOSTTY_STYLE_FLAG_BOLD) && paletteIndex <= GHOSTTY_COLOR_NAMED_WHITE) {
            paletteIndex = static_cast<uint8_t>(paletteIndex + 8);
        }
        return renderColors.palette[paletteIndex];
    }
    if (style.fg_color.tag == GHOSTTY_STYLE_COLOR_RGB) {
        return style.fg_color.value.rgb;
    }
    return hasResolvedFg ? resolvedFg : renderColors.foreground;
}

static bool resolveEffectiveBackground(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedBg,
    const GhosttyColorRgb& resolvedBg,
    GhosttyColorRgb* out)
{
    if (hasResolvedBg) {
        *out = resolvedBg;
        return true;
    }
    switch (style.bg_color.tag) {
        case GHOSTTY_STYLE_COLOR_PALETTE:
            *out = renderColors.palette[style.bg_color.value.palette];
            return true;
        case GHOSTTY_STYLE_COLOR_RGB:
            *out = style.bg_color.value.rgb;
            return true;
        case GHOSTTY_STYLE_COLOR_NONE:
        default:
            *out = renderColors.background;
            return false;
    }
}

static void clearRenderStateDirty(GhosttyRenderState renderState)
{
    GhosttyRenderStateDirty clean = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_set(renderState, GHOSTTY_RENDER_STATE_OPT_DIRTY, &clean);

    GhosttyRenderStateRowIterator rowIterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &rowIterator) != GHOSTTY_SUCCESS || !rowIterator) {
        return;
    }
    if (ghostty_render_state_get(renderState, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &rowIterator) == GHOSTTY_SUCCESS) {
        const bool cleanRow = false;
        while (ghostty_render_state_row_iterator_next(rowIterator)) {
            ghostty_render_state_row_set(rowIterator, GHOSTTY_RENDER_STATE_ROW_OPT_DIRTY, &cleanRow);
        }
    }
    ghostty_render_state_row_iterator_free(rowIterator);
}

static uint32_t roundPositivePixels(double value)
{
    if (!std::isfinite(value) || value <= 0) {
        return 1;
    }
    const long rounded = std::lround(value);
    return rounded > 0 ? static_cast<uint32_t>(rounded) : 1;
}

static uint32_t argbOf(const GhosttyColorRgb& c)
{
    return 0xff000000u | (uint32_t(c.r) << 16) | (uint32_t(c.g) << 8) | uint32_t(c.b);
}

static GhosttyColorRgb rgbOf(uint32_t value)
{
    GhosttyColorRgb c = {};
    c.r = static_cast<uint8_t>((value >> 16) & 0xff);
    c.g = static_cast<uint8_t>((value >> 8) & 0xff);
    c.b = static_cast<uint8_t>(value & 0xff);
    return c;
}

static void appendUtf16(std::u16string* out, uint32_t cp)
{
    if (cp < 0x10000) {
        out->push_back(char16_t(cp));
    } else {
        cp -= 0x10000;
        out->push_back(char16_t(0xD800 + (cp >> 10)));
        out->push_back(char16_t(0xDC00 + (cp & 0x3FF)));
    }
}

extern "C" MewlaVt* mewla_vt_create(uint16_t cols, uint16_t rows)
{
    if (cols == 0 || rows == 0) {
        return nullptr;
    }
    auto* vt = new MewlaVt();
    vt->cols = cols;
    vt->rows = rows;
    GhosttyResult res = createTerminalInstance(cols, rows, &vt->terminal);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_new failed: %d", res);
        delete vt;
        return nullptr;
    }
    res = ghostty_render_state_new(nullptr, &vt->render_state);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_new failed: %d", res);
        ghostty_terminal_free(vt->terminal);
        delete vt;
        return nullptr;
    }
    return vt;
}

extern "C" void mewla_vt_destroy(MewlaVt* vt)
{
    if (!vt) {
        return;
    }
    ghostty_render_state_free(vt->render_state);
    ghostty_terminal_free(vt->terminal);
    delete vt;
}

extern "C" void mewla_vt_resize(MewlaVt* vt, uint16_t cols, uint16_t rows, double cellWidth, double cellHeight)
{
    if (cols == 0 || rows == 0) {
        return;
    }
    vt->cols = cols;
    vt->rows = rows;
    ghostty_terminal_resize(vt->terminal, cols, rows, roundPositivePixels(cellWidth), roundPositivePixels(cellHeight));
    vt->force_full = true;
}

extern "C" bool mewla_vt_set_theme(
    MewlaVt* vt, uint32_t foreground, uint32_t background, uint32_t cursor,
    const uint32_t* palette, size_t paletteCount)
{
    if (paletteCount < 256) {
        LOGE("setTheme palette too small: %zu", paletteCount);
        return false;
    }
    const GhosttyColorRgb fg = rgbOf(foreground);
    const GhosttyColorRgb bg = rgbOf(background);
    const GhosttyColorRgb cursorColor = rgbOf(cursor);
    std::array<GhosttyColorRgb, 256> colors = {};
    for (size_t i = 0; i < 256; i++) {
        colors[i] = rgbOf(palette[i]);
    }
    const struct { GhosttyTerminalOption option; const void* value; const char* label; } options[] = {
        { GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg, "foreground" },
        { GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &bg, "background" },
        { GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursorColor, "cursor" },
        { GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, colors.data(), "palette" },
    };
    for (const auto& o : options) {
        const GhosttyResult res = ghostty_terminal_set(vt->terminal, o.option, o.value);
        if (res != GHOSTTY_SUCCESS) {
            LOGE("ghostty_terminal_set %s failed: %d", o.label, res);
            return false;
        }
    }
    vt->force_full = true;
    return true;
}

extern "C" void mewla_vt_write(MewlaVt* vt, const uint8_t* data, size_t length)
{
    if (!data || length == 0) {
        return;
    }
    ghostty_terminal_vt_write(vt->terminal, data, length);
}

extern "C" bool mewla_vt_frame(MewlaVt* vt, bool forceFull, MewlaVtFrame* out)
{
    *out = MewlaVtFrame{};
    if (ghostty_render_state_update(vt->render_state, vt->terminal) != GHOSTTY_SUCCESS) {
        return false;
    }
    uint16_t rows = vt->rows, cols = vt->cols;
    ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
    ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_COLS, &cols);
    GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
    const bool full = forceFull || vt->force_full || dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL;

    bool cursorIn = false, cursorVisible = false;
    uint16_t cx = 0, cy = 0;
    ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, &cursorIn);
    if (cursorIn) {
        ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cx);
        ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cy);
        ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursorVisible);
    }

    std::vector<int32_t>& meta = vt->meta;
    std::u16string& text = vt->text;
    meta.clear();
    text.clear();
    meta.insert(meta.end(), {rows, cols, cx, cy, cursorVisible ? 1 : 0, full ? 1 : 0, 0});
    int32_t dirtyCount = 0;

    if (full || dirty != GHOSTTY_RENDER_STATE_DIRTY_FALSE) {
        GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
        ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors);
        GhosttyRenderStateRowIterator it = nullptr;
        GhosttyRenderStateRowCells cells = nullptr;
        if (ghostty_render_state_row_iterator_new(nullptr, &it) != GHOSTTY_SUCCESS || !it ||
            ghostty_render_state_row_cells_new(nullptr, &cells) != GHOSTTY_SUCCESS || !cells ||
            ghostty_render_state_get(vt->render_state, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &it) != GHOSTTY_SUCCESS) {
            ghostty_render_state_row_cells_free(cells);
            ghostty_render_state_row_iterator_free(it);
            return false;
        }
        std::vector<uint32_t> grapheme;
        int32_t index = 0;
        while (ghostty_render_state_row_iterator_next(it)) {
            bool rowDirty = true;
            ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY, &rowDirty);
            if (!(full || rowDirty) || ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &cells) != GHOSTTY_SUCCESS) {
                index++;
                continue;
            }
            dirtyCount++;
            meta.push_back(index);
            const size_t runCountAt = meta.size();
            meta.push_back(0);
            int32_t col = 0, runs = 0;
            int32_t runCol = -1, runCells = 0, runFlags = 0;
            uint32_t runFg = 0, runBg = 0;
            size_t runStart = 0;
            auto flush = [&]() {
                if (runCol < 0) return;
                bool blank = runBg == 0;
                for (size_t i = runStart; blank && i < text.size(); i++) blank = text[i] == u' ';
                if (blank) {
                    text.resize(runStart);
                } else {
                    meta.insert(meta.end(), {runCol, runCells, int32_t(runFg), int32_t(runBg), runFlags,
                        int32_t(runStart), int32_t(text.size() - runStart)});
                    runs++;
                }
                runCol = -1;
                runCells = 0;
            };
            while (ghostty_render_state_row_cells_next(cells)) {
                GhosttyCell cell = 0;
                if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &cell) != GHOSTTY_SUCCESS) {
                    col++;
                    continue;
                }
                GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
                ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
                if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL || wide == GHOSTTY_CELL_WIDE_SPACER_HEAD) {
                    col++;
                    continue;
                }
                GhosttyStyle style{};
                ghostty_style_default(&style);
                ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style);
                GhosttyColorRgb fgc = colors.foreground;
                const bool hasFg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &fgc) == GHOSTTY_SUCCESS;
                GhosttyColorRgb bgc = colors.background;
                const bool hasBg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &bgc) == GHOSTTY_SUCCESS;
                const uint32_t fg = argbOf(resolveEffectiveForeground(style, colors, hasFg, fgc));
                GhosttyColorRgb eb;
                const uint32_t bg = resolveEffectiveBackground(style, colors, hasBg, bgc, &eb) ? argbOf(eb) : 0;
                const int32_t flags = (style.flags & GHOSTTY_STYLE_FLAG_BOLD ? 1 : 0) |
                    (style.flags & GHOSTTY_STYLE_FLAG_ITALIC ? 2 : 0) |
                    ((style.flags & GHOSTTY_STYLE_UNDERLINE_MASK) ? 4 : 0) |
                    (style.flags & GHOSTTY_STYLE_FLAG_INVERSE ? 8 : 0);
                const int32_t width = wide == GHOSTTY_CELL_WIDE_WIDE ? 2 : 1;
                if (runCol < 0 || fg != runFg || bg != runBg || flags != runFlags || width == 2) {
                    flush();
                    runCol = col;
                    runFg = fg;
                    runBg = bg;
                    runFlags = flags;
                    runStart = text.size();
                }
                bool hasText = false;
                ghostty_cell_get(cell, GHOSTTY_CELL_DATA_HAS_TEXT, &hasText);
                uint32_t cp = 0;
                ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &cp);
                if (!hasText || cp == 0) {
                    text.push_back(u' ');
                } else {
                    uint32_t glen = 0;
                    if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, &glen) == GHOSTTY_SUCCESS && glen > 1) {
                        grapheme.assign(glen, 0);
                        GhosttyCodepointBuffer b{};
                        b.ptr = grapheme.data();
                        b.cap = grapheme.size();
                        if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, &b) == GHOSTTY_SUCCESS) {
                            for (size_t i = 0; i < b.len; i++) appendUtf16(&text, grapheme[i]);
                        } else {
                            appendUtf16(&text, cp);
                        }
                    } else {
                        appendUtf16(&text, cp);
                    }
                }
                // The wide cell's spacer tail comes next and counts its own column.
                runCells += width;
                col++;
                if (width == 2) flush();
            }
            flush();
            meta[runCountAt] = runs;
            index++;
        }
        ghostty_render_state_row_cells_free(cells);
        ghostty_render_state_row_iterator_free(it);
        clearRenderStateDirty(vt->render_state);
        vt->force_full = false;
    }
    meta[6] = dirtyCount;
    out->meta = meta.data();
    out->meta_count = meta.size();
    out->text = reinterpret_cast<const uint16_t*>(text.data());
    out->text_count = text.size();
    return true;
}
