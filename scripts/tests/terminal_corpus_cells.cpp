// The render corpus (scripts/terminal-corpus) through the app's cell packer (TerminalVt.cpp),
// cell by cell against libghostty-vt's own grid. For each screen it prints what the packer
// loses: cells whose glyph or background vanish, and attributes it never hands the renderer.
//
//   terminal_corpus_cells <corpus dir> [name...]         summary per screen
//   terminal_corpus_cells --grid <corpus dir> <name>      libghostty's text grid of one screen
#include "TerminalVt.h"

#include <ghostty/vt.h>

#include <cstdio>
#include <cstring>
#include <fstream>
#include <map>
#include <sstream>
#include <string>
#include <vector>

namespace {

int badRuns = 0;  // runs that break the plain-run contract (TerminalVt.h)

struct TruthCell {
    std::u32string text;  // empty: blank
    int width = 1;        // 2 for a wide cell (its tail is skipped)
    uint32_t fg = 0, bg = 0;  // effective (inverse applied), 0xRRGGBB
    bool bgSet = false;       // background differs from the default
    GhosttyStyleFlags flags = 0;
    int underline = 0;
    bool underlineColor = false;
};

struct PackedCell {
    std::u16string text;
    bool covered = false;
    uint32_t fg = 0, bg = 0;
    int flags = 0;
    uint32_t ul = 0;
};

uint32_t rgb(const GhosttyColorRgb& c) { return (uint32_t(c.r) << 16) | (uint32_t(c.g) << 8) | c.b; }

std::string readFile(const std::string& path) {
    std::ifstream in(path, std::ios::binary);
    std::stringstream ss;
    ss << in.rdbuf();
    return ss.str();
}

std::string utf8(const std::u32string& s) {
    std::string out;
    for (char32_t c : s) {
        if (c < 0x80) out += char(c);
        else if (c < 0x800) { out += char(0xC0 | (c >> 6)); out += char(0x80 | (c & 0x3F)); }
        else if (c < 0x10000) { out += char(0xE0 | (c >> 12)); out += char(0x80 | ((c >> 6) & 0x3F)); out += char(0x80 | (c & 0x3F)); }
        else { out += char(0xF0 | (c >> 18)); out += char(0x80 | ((c >> 12) & 0x3F)); out += char(0x80 | ((c >> 6) & 0x3F)); out += char(0x80 | (c & 0x3F)); }
    }
    return out;
}

std::u32string fromUtf16(const std::u16string& s) {
    std::u32string out;
    for (size_t i = 0; i < s.size(); i++) {
        char32_t c = s[i];
        if (c >= 0xD800 && c < 0xDC00 && i + 1 < s.size()) { c = 0x10000 + ((c - 0xD800) << 10) + (s[i + 1] - 0xDC00); i++; }
        out += c;
    }
    return out;
}

// libghostty's grid, straight from its render state.
std::vector<std::vector<TruthCell>> truth(const std::string& bytes, int cols, int rows, GhosttyRenderStateCursor* cursorOut) {
    GhosttyTerminal t = nullptr;
    ghostty_terminal_new(nullptr, &t, cols, rows);
    ghostty_terminal_vt_write(t, reinterpret_cast<const uint8_t*>(bytes.data()), bytes.size());
    GhosttyRenderState rs = nullptr;
    ghostty_render_state_new(nullptr, &rs);
    ghostty_render_state_update(rs, t);
    GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
    ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors);
    GhosttyRenderStateCursor cursor = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
    ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR, &cursor);
    if (cursorOut) *cursorOut = cursor;
    std::vector<std::vector<TruthCell>> grid;
    GhosttyRenderStateRowIterator it = nullptr;
    ghostty_render_state_row_iterator_new(nullptr, &it);
    ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &it);
    GhosttyRenderStateRowCells cells = nullptr;
    ghostty_render_state_row_cells_new(nullptr, &cells);
    while (ghostty_render_state_row_iterator_next(it)) {
        ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &cells);
        std::vector<TruthCell> row;
        while (ghostty_render_state_row_cells_next(cells)) {
            TruthCell c;
            GhosttyCell raw = 0;
            ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw);
            GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
            ghostty_cell_get(raw, GHOSTTY_CELL_DATA_WIDE, &wide);
            if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL || wide == GHOSTTY_CELL_WIDE_SPACER_HEAD) { c.width = 0; row.push_back(c); continue; }
            c.width = wide == GHOSTTY_CELL_WIDE_WIDE ? 2 : 1;
            GhosttyStyle style{};
            ghostty_style_default(&style);
            ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style);
            c.flags = style.flags;
            c.underline = GHOSTTY_STYLE_UNDERLINE(style);
            c.underlineColor = style.underline_color.tag != GHOSTTY_STYLE_COLOR_NONE;
            GhosttyColorRgb fg = colors.foreground, bg = colors.background;
            bool hasFg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &fg) == GHOSTTY_SUCCESS;
            bool hasBg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &bg) == GHOSTTY_SUCCESS;
            if (!hasFg) fg = colors.foreground;
            if (!hasBg) bg = colors.background;
            if (style.flags & GHOSTTY_STYLE_FLAG_INVERSE) { std::swap(fg, bg); hasBg = true; }
            c.fg = rgb(fg);
            c.bg = rgb(bg);
            c.bgSet = hasBg && c.bg != rgb(colors.background);
            uint32_t len = 0;
            if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, &len) == GHOSTTY_SUCCESS && len > 0) {
                std::vector<uint32_t> g(len);
                GhosttyCodepointBuffer b{};
                b.ptr = g.data();
                b.cap = g.size();
                if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, &b) == GHOSTTY_SUCCESS)
                    for (size_t i = 0; i < b.len; i++) c.text += char32_t(g[i]);
            }
            if (c.text == U" ") c.text.clear();
            row.push_back(c);
        }
        grid.push_back(row);
    }
    ghostty_render_state_row_cells_free(cells);
    ghostty_render_state_row_iterator_free(it);
    ghostty_render_state_free(rs);
    ghostty_terminal_free(t);
    return grid;
}

// What the app's packer hands its renderers, spread back over cells.
std::vector<std::vector<PackedCell>> packed(const std::string& bytes, int cols, int rows) {
    std::vector<std::vector<PackedCell>> grid(rows, std::vector<PackedCell>(cols));
    MewlaVt* vt = mewla_vt_create(cols, rows);
    mewla_vt_write(vt, reinterpret_cast<const uint8_t*>(bytes.data()), bytes.size());
    MewlaVtFrame f{};
    mewla_vt_frame(vt, true, &f);
    const int32_t* m = f.meta;
    size_t p = MEWLA_VT_HEADER;
    for (int r = 0; r < m[6]; r++) {
        int row = m[p], n = m[p + 1];
        p += MEWLA_VT_ROW;
        for (int i = 0; i < n; i++, p += MEWLA_VT_RUN) {
            int col = m[p], cellsN = m[p + 1];
            std::u16string text(reinterpret_cast<const char16_t*>(f.text) + m[p + 6], m[p + 7]);
            // Plain: one UTF-16 unit per cell. Otherwise a single cell (1 or 2 columns wide).
            if (int(text.size()) != cellsN && fromUtf16(text).size() != 1 && cellsN > 2) badRuns++;
            if (int(text.size()) != cellsN && cellsN == 2 && text.size() > 0 && fromUtf16(text).size() == 0) badRuns++;
            for (int k = 0; k < cellsN && col + k < cols && row < rows; k++) {
                PackedCell& c = grid[row][col + k];
                c.covered = true;
                c.fg = uint32_t(m[p + 2]) & 0xFFFFFF;
                c.bg = uint32_t(m[p + 3]);
                c.flags = m[p + 4];
                c.ul = uint32_t(m[p + 5]);
                if (k == 0) c.text = text;
            }
        }
    }
    mewla_vt_destroy(vt);
    return grid;
}

int cursorCode(const GhosttyRenderStateCursor& c) {
    int style = c.visual_style == GHOSTTY_CURSOR_STYLE_BAR ? 0 : c.visual_style == GHOSTTY_CURSOR_STYLE_UNDERLINE ? 2 : c.visual_style == GHOSTTY_CURSOR_STYLE_BLOCK_HOLLOW ? 3 : 1;
    return (c.viewport_has_value && c.visible ? 1 : 0) | style << 1 | (c.blinking ? 1 : 0) << 3;
}

// The packer's cursor, in cursorCode's form.
int packedCursor(const std::string& bytes, int cols, int rows) {
    MewlaVt* vt = mewla_vt_create(cols, rows);
    mewla_vt_write(vt, reinterpret_cast<const uint8_t*>(bytes.data()), bytes.size());
    MewlaVtFrame f{};
    mewla_vt_frame(vt, true, &f);
    int code = f.meta[4] | f.meta[7] << 1 | f.meta[8] << 3;
    mewla_vt_destroy(vt);
    return code;
}

struct Losses {
    int cells = 0, glyphLost = 0, bgLost = 0, faint = 0, strike = 0, overline = 0, underlineStyle = 0, underlineColor = 0, blink = 0, wide = 0;
    std::map<std::string, int> examples;
};

const char* cursorName(int s) {
    switch (s) {
        case GHOSTTY_CURSOR_STYLE_BAR: return "bar";
        case GHOSTTY_CURSOR_STYLE_BLOCK: return "block";
        case GHOSTTY_CURSOR_STYLE_UNDERLINE: return "underline";
        case GHOSTTY_CURSOR_STYLE_BLOCK_HOLLOW: return "hollow";
        default: return "?";
    }
}

}  // namespace

int main(int argc, char** argv) {
    if (argc >= 4 && std::strcmp(argv[1], "--grid") == 0) {
        std::string dir = argv[2], name = argv[3];
        int cols = argc > 4 ? std::atoi(argv[4]) : 52, rows = argc > 5 ? std::atoi(argv[5]) : 30;
        auto g = truth(readFile(dir + "/" + name + ".vt"), cols, rows, nullptr);
        for (auto& row : g) {
            std::string line;
            for (auto& c : row) if (c.width) line += c.text.empty() ? " " : utf8(c.text);
            std::printf("%s\n", line.c_str());
        }
        return 0;
    }
    if (argc < 2) { std::fprintf(stderr, "usage: %s <corpus dir> [name...]\n", argv[0]); return 2; }
    std::string dir = argv[1];
    std::vector<std::string> names;
    for (int i = 2; i < argc; i++) names.push_back(argv[i]);
    if (names.empty()) {
        std::string manifest = readFile(dir + "/corpus.json");
        for (size_t at = 0; (at = manifest.find("\n  \"", at)) != std::string::npos;) {
            at += 4;
            names.push_back(manifest.substr(at, manifest.find('"', at) - at));
        }
    }
    int total = 0;
    std::printf("%-17s %5s %6s %5s %5s %6s %5s %6s %6s %5s  cursor\n", "screen", "cells", "rows!=", "bg-", "dim-", "strike", "over-", "ulsty-", "ulcol-", "blink-");
    for (auto& name : names) {
        std::string bytes = readFile(dir + "/" + name + ".vt");
        // Geometry from corpus.json: "name": {"cols": C, "rows": R
        std::string manifest = readFile(dir + "/corpus.json");
        size_t k = manifest.find("\"" + name + "\": {");
        int cols = 52, rows = 30;
        if (k != std::string::npos) std::sscanf(manifest.c_str() + manifest.find("\"cols\":", k), "\"cols\": %d, \"rows\": %d", &cols, &rows);
        GhosttyRenderStateCursor cursor = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
        auto t = truth(bytes, cols, rows, &cursor);
        auto p = packed(bytes, cols, rows);
        Losses l;
        for (int r = 0; r < rows && r < int(t.size()); r++) {
            // The row's text both ways: each run's text at its first cell, blanks as spaces.
            std::u32string want, got;
            for (int c = 0; c < cols && c < int(t[r].size()); c++) {
                const TruthCell& tc = t[r][c];
                if (!tc.width) continue;
                want += (tc.text.empty() || (tc.flags & GHOSTTY_STYLE_FLAG_INVISIBLE)) ? std::u32string(U" ") : tc.text;
                const PackedCell& pc = p[r][c];
                if (!pc.covered) got += U' ';
                else if (!pc.text.empty()) got += fromUtf16(pc.text);
            }
            while (!want.empty() && want.back() == U' ') want.pop_back();
            while (!got.empty() && got.back() == U' ') got.pop_back();
            if (want != got) { l.glyphLost++; l.examples["row " + std::to_string(r) + ": " + utf8(want)]++; }
            for (int c = 0; c < cols && c < int(t[r].size()); c++) {
                const TruthCell& tc = t[r][c];
                if (!tc.width) continue;
                const PackedCell& pc = p[r][c];
                bool visible = !(tc.flags & GHOSTTY_STYLE_FLAG_INVISIBLE);
                if (!tc.text.empty() || tc.bgSet) l.cells++;
                if (tc.bgSet) {
                    // The renderer's background: packed bg, or fg when it swaps for inverse.
                    uint32_t drawn = pc.bg & 0xFFFFFF;  // inverse is applied by the packer
                    if (!pc.covered || drawn != tc.bg) {
                        l.bgLost++;
                        l.examples[(tc.flags & GHOSTTY_STYLE_FLAG_INVERSE) ? "bg of inverse blank" : "bg"]++;
                    }
                }
                bool hasInk = visible && (!tc.text.empty());
                // An attribute is lost when the cell has it and the packed run does not say so.
                if (hasInk && (tc.flags & GHOSTTY_STYLE_FLAG_FAINT) && !(pc.flags & MEWLA_VT_FAINT)) l.faint++;
                if ((tc.flags & GHOSTTY_STYLE_FLAG_STRIKETHROUGH) && visible && !(pc.flags & MEWLA_VT_STRIKE)) l.strike++;
                if ((tc.flags & GHOSTTY_STYLE_FLAG_OVERLINE) && visible && !(pc.flags & MEWLA_VT_OVERLINE)) l.overline++;
                if (tc.underline && visible && ((pc.flags >> MEWLA_VT_UNDERLINE_SHIFT) & 7) != tc.underline) l.underlineStyle++;
                if (tc.underlineColor && tc.underline && visible && pc.ul == 0) l.underlineColor++;
                if ((tc.flags & GHOSTTY_STYLE_FLAG_BLINK) && hasInk && !(pc.flags & MEWLA_VT_BLINK)) l.blink++;
            }
        }
        bool cursorLost = packedCursor(bytes, cols, rows) != cursorCode(cursor);
        total += l.glyphLost + l.bgLost + l.faint + l.strike + l.overline + l.underlineStyle + l.underlineColor + l.blink + (cursorLost ? 1 : 0) + badRuns;
        badRuns = 0;
        std::printf("%-17s %5d %6d %5d %5d %6d %5d %6d %6d %5d  %s%s%s\n", name.c_str(), l.cells, l.glyphLost, l.bgLost, l.faint, l.strike, l.overline,
            l.underlineStyle, l.underlineColor, l.blink, cursor.visible ? cursorName(cursor.visual_style) : "hidden",
            cursor.blinking ? " blinking" : "", packedCursor(bytes, cols, rows) != cursorCode(cursor) ? " (packer differs)" : "");
        for (auto& [what, n] : l.examples) std::printf("    %4d x %s\n", n, what.c_str());
    }
    std::printf("cells, attributes and cursors lost in all: %d\n", total);
    return total == 0 ? 0 : 1;
}
