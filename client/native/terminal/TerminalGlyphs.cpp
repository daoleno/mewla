// See TerminalGlyphs.h. The lines follow one rule for every box-drawing character: each arm
// (up, right, down, left) is light, heavy or double, sits at the same pixel offset in every
// cell of a size, and runs from the cell's edge to where it meets the others, so a frame of
// separate characters reads as one drawn line.
#include "TerminalGlyphs.h"

#include <algorithm>
#include <cmath>

namespace {

enum Weight { NONE = 0, LIGHT = 1, HEAVY = 2, DOUBLE = 3 };

// U+2500-257F as "URDL" weights; '-' is drawn elsewhere (dashes, arcs, diagonals).
const char* const kBox[128] = {
    "0101", "0202", "1010", "2020", "-", "-", "-", "-", "-", "-", "-", "-", "0110", "0210", "0120", "0220",
    "0011", "0012", "0021", "0022", "1100", "1200", "2100", "2200", "1001", "1002", "2001", "2002", "1110", "1210", "2110", "1120",
    "2120", "2210", "1220", "2220", "1011", "1012", "2011", "1021", "2021", "2012", "1022", "2022", "0111", "0112", "0211", "0212",
    "0121", "0122", "0221", "0222", "1101", "1102", "1201", "1202", "2101", "2102", "2201", "2202", "1111", "1112", "1211", "1212",
    "2111", "1121", "2121", "2112", "2211", "1122", "1221", "2212", "1222", "2122", "2221", "2222", "-", "-", "-", "-",
    "0303", "3030", "0310", "0130", "0330", "0013", "0031", "0033", "1300", "3100", "3300", "1003", "3001", "3003", "1310", "3130",
    "3330", "1013", "3031", "3033", "0313", "0131", "0333", "1303", "3101", "3303", "1313", "3131", "3333", "-", "-", "-",
    "-", "-", "-", "-", "0001", "1000", "0100", "0010", "0002", "2000", "0200", "0020", "0201", "1020", "0102", "2010",
};

struct Out {
    MewlaGlyphOp* ops;
    int32_t cap;
    int32_t n = 0;
    void push(const MewlaGlyphOp& op) {
        if (n < cap) ops[n] = op;
        n++;
    }
    void rect(float x0, float y0, float x1, float y1, float alpha = 1) {
        if (x1 <= x0 || y1 <= y0) return;
        MewlaGlyphOp op{MEWLA_GLYPH_RECT, 0, alpha, 0, {x0, y0, x1, y1, 0, 0, 0, 0}};
        push(op);
    }
    void polygon(std::initializer_list<float> pts) {
        MewlaGlyphOp op{MEWLA_GLYPH_POLYGON, int32_t(pts.size() / 2), 1, 0, {}};
        int i = 0;
        for (float v : pts) op.p[i++] = v;
        push(op);
    }
    void line(float x0, float y0, float x1, float y1, float stroke) {
        MewlaGlyphOp op{MEWLA_GLYPH_LINE, 0, 1, stroke, {x0, y0, x1, y1, 0, 0, 0, 0}};
        push(op);
    }
    void curve(int32_t kind, float x0, float y0, float cx1, float cy1, float cx2, float cy2, float x1, float y1, float stroke) {
        MewlaGlyphOp op{kind, 0, 1, stroke, {x0, y0, cx1, cy1, cx2, cy2, x1, y1}};
        push(op);
    }
    void ellipse(float x0, float y0, float x1, float y1) {
        MewlaGlyphOp op{MEWLA_GLYPH_ELLIPSE, 0, 1, 0, {x0, y0, x1, y1, 0, 0, 0, 0}};
        push(op);
    }
};

// Where a line of each weight sits across a span of `size` pixels: [start, start + thickness).
struct Lines {
    int t;           // light thickness
    int light, heavy, dbl1, dbl2, mid;
    Lines(int size, int thickness) : t(thickness) {
        light = (size - t) / 2;
        heavy = (size - 2 * t) / 2;
        dbl1 = (size - 3 * t) / 2;
        dbl2 = dbl1 + 2 * t;
        mid = size / 2;
    }
    int start(int w) const { return w == HEAVY ? heavy : w == DOUBLE ? dbl1 : light; }
    // The far edge of a weight's line (both lines for double).
    int end(int w) const { return w == HEAVY ? heavy + 2 * t : w == DOUBLE ? dbl2 + t : light + t; }
    int thick(int w) const { return w == HEAVY ? 2 * t : t; }
};

int heaviest(int a, int b) {
    // For where a perpendicular line stops: double spans widest, then heavy.
    if (a == DOUBLE || b == DOUBLE) return DOUBLE;
    return std::max(a, b);
}

void boxLines(Out& o, int w, int h, int t, int U, int R, int D, int L) {
    Lines X(w, t), Y(h, t);
    int vert = heaviest(U, D), horiz = heaviest(L, R);
    // Up arm: from the top edge down to `end` (exclusive), per line.
    auto upEnd = [&](bool leftLine) -> int {
        int side = leftLine ? L : R, other = leftLine ? R : L;
        if (U == DOUBLE) {
            if (side == DOUBLE) return Y.dbl1 + t;                // inner corner
            if (D) return Y.mid;                                  // runs on down
            if (other == DOUBLE) return Y.dbl2 + t;               // outer corner
        } else if (D) {
            return Y.mid;
        }
        if (horiz) return Y.end(horiz);
        return Y.end(U);
    };
    auto downStart = [&](bool leftLine) -> int {
        int side = leftLine ? L : R, other = leftLine ? R : L;
        if (D == DOUBLE) {
            if (side == DOUBLE) return Y.dbl2;
            if (U) return Y.mid;
            if (other == DOUBLE) return Y.dbl1;
        } else if (U) {
            return Y.mid;
        }
        if (horiz) return Y.start(horiz);
        return Y.start(D);
    };
    auto leftEnd = [&](bool topLine) -> int {
        int side = topLine ? U : D, other = topLine ? D : U;
        if (L == DOUBLE) {
            if (side == DOUBLE) return X.dbl1 + t;
            if (R) return X.mid;
            if (other == DOUBLE) return X.dbl2 + t;
        } else if (R) {
            return X.mid;
        }
        if (vert) return X.end(vert);
        return X.end(L);
    };
    auto rightStart = [&](bool topLine) -> int {
        int side = topLine ? U : D, other = topLine ? D : U;
        if (R == DOUBLE) {
            if (side == DOUBLE) return X.dbl2;
            if (L) return X.mid;
            if (other == DOUBLE) return X.dbl1;
        } else if (L) {
            return X.mid;
        }
        if (vert) return X.start(vert);
        return X.start(R);
    };
    if (U == DOUBLE) {
        o.rect(X.dbl1, 0, X.dbl1 + t, upEnd(true));
        o.rect(X.dbl2, 0, X.dbl2 + t, upEnd(false));
    } else if (U) {
        o.rect(X.start(U), 0, X.start(U) + X.thick(U), upEnd(true));
    }
    if (D == DOUBLE) {
        o.rect(X.dbl1, downStart(true), X.dbl1 + t, h);
        o.rect(X.dbl2, downStart(false), X.dbl2 + t, h);
    } else if (D) {
        o.rect(X.start(D), downStart(true), X.start(D) + X.thick(D), h);
    }
    if (L == DOUBLE) {
        o.rect(0, Y.dbl1, leftEnd(true), Y.dbl1 + t);
        o.rect(0, Y.dbl2, leftEnd(false), Y.dbl2 + t);
    } else if (L) {
        o.rect(0, Y.start(L), leftEnd(true), Y.start(L) + Y.thick(L));
    }
    if (R == DOUBLE) {
        o.rect(rightStart(true), Y.dbl1, w, Y.dbl1 + t);
        o.rect(rightStart(false), Y.dbl2, w, Y.dbl2 + t);
    } else if (R) {
        o.rect(rightStart(true), Y.start(R), w, Y.start(R) + Y.thick(R));
    }
}

// n dashes along a line, half a gap at each end so neighbouring cells keep the rhythm.
void dashes(Out& o, int w, int h, int t, bool heavy, bool vertical, int n) {
    int thick = heavy ? 2 * t : t;
    int span = vertical ? h : w;
    float period = float(span) / n;
    float gap = std::max(1.0f, std::round(period / 3));
    for (int i = 0; i < n; i++) {
        float a = std::round(i * period + gap / 2), b = std::round((i + 1) * period - gap / 2);
        if (vertical) {
            int x = (w - thick) / 2;
            o.rect(x, a, x + thick, b);
        } else {
            int y = (h - thick) / 2;
            o.rect(a, y, b, y + thick);
        }
    }
}

// ╭╮╯╰: a quarter circle between the two arms' centre lines, straight to the edges.
void arc(Out& o, int w, int h, int t, bool down, bool right) {
    Lines X(w, t), Y(h, t);
    float cx = X.light + t / 2.0f, cy = Y.light + t / 2.0f;
    float rx = right ? w - cx : cx, ry = down ? h - cy : cy;
    float r = std::min(rx, ry);
    const float k = 0.5523f * r;
    float sx = right ? 1 : -1, sy = down ? 1 : -1;
    // From the horizontal arm (r beyond the centre) round to the vertical arm (r beyond it).
    float ax = cx + sx * r, ay = cy, bx = cx, by = cy + sy * r;
    o.curve(MEWLA_GLYPH_CURVE, ax, ay, ax - sx * k, ay, bx, by - sy * k, bx, by, t);
    float edgeX = right ? w : 0, edgeY = down ? h : 0;
    if (std::fabs(edgeX - ax) > 0.01f) o.line(ax, cy, edgeX, cy, t);
    if (std::fabs(edgeY - by) > 0.01f) o.line(cx, by, cx, edgeY, t);
}

void block(Out& o, int w, int h, uint32_t cp) {
    auto fx = [&](int eighths) { return std::round(w * eighths / 8.0f); };
    auto fy = [&](int eighths) { return std::round(h * eighths / 8.0f); };
    float hw = std::round(w / 2.0f), hh = std::round(h / 2.0f);
    switch (cp) {
        case 0x2580: o.rect(0, 0, w, hh); return;
        case 0x2588: o.rect(0, 0, w, h); return;
        case 0x2590: o.rect(hw, 0, w, h); return;
        case 0x2591: o.rect(0, 0, w, h, 0.25f); return;
        case 0x2592: o.rect(0, 0, w, h, 0.5f); return;
        case 0x2593: o.rect(0, 0, w, h, 0.75f); return;
        case 0x2594: o.rect(0, 0, w, fy(1)); return;
        case 0x2595: o.rect(fx(7), 0, w, h); return;
        default: break;
    }
    if (cp >= 0x2581 && cp <= 0x2587) {  // lower eighths
        o.rect(0, fy(8 - int(cp - 0x2580)), w, h);
        return;
    }
    if (cp >= 0x2589 && cp <= 0x258F) {  // left eighths, 7/8 down to 1/8
        o.rect(0, 0, fx(8 - int(cp - 0x2588)), h);
        return;
    }
    // Quadrants U+2596-259F: bits upper-left, upper-right, lower-left, lower-right.
    static const uint8_t quads[] = {0b0010, 0b0001, 0b1000, 0b1011, 0b1001, 0b1110, 0b1101, 0b0100, 0b0110, 0b0111};
    if (cp >= 0x2596 && cp <= 0x259F) {
        uint8_t q = quads[cp - 0x2596];
        if (q & 0b1000) o.rect(0, 0, hw, hh);
        if (q & 0b0100) o.rect(hw, 0, w, hh);
        if (q & 0b0010) o.rect(0, hh, hw, h);
        if (q & 0b0001) o.rect(hw, hh, w, h);
    }
}

void braille(Out& o, int w, int h, uint32_t cp) {
    // Dots 1-8: (column, row) for bits 0..7.
    static const int col[8] = {0, 0, 0, 1, 1, 1, 0, 1};
    static const int row[8] = {0, 1, 2, 0, 1, 2, 3, 3};
    uint32_t bits = cp - 0x2800;
    float sw = w / 2.0f, sh = h / 4.0f;
    float d = std::max(1.0f, std::round(std::min(sw, sh) * 0.62f));
    for (int i = 0; i < 8; i++) {
        if (!(bits & (1u << i))) continue;
        float x = std::round(col[i] * sw + (sw - d) / 2), y = std::round(row[i] * sh + (sh - d) / 2);
        o.ellipse(x, y, x + d, y + d);
    }
}

void powerline(Out& o, int w, int h, int t, uint32_t cp) {
    float W = w, H = h, m = h / 2.0f;
    switch (cp) {
        case 0xE0B0: o.polygon({0, 0, W, m, 0, H}); return;
        case 0xE0B2: o.polygon({W, 0, 0, m, W, H}); return;
        case 0xE0B1: o.line(0, 0, W, m, t); o.line(W, m, 0, H, t); return;
        case 0xE0B3: o.line(W, 0, 0, m, t); o.line(0, m, W, H, t); return;
        case 0xE0B4: o.curve(MEWLA_GLYPH_CURVE_FILL, 0, 0, W * 4 / 3, 0, W * 4 / 3, H, 0, H, 0); return;
        case 0xE0B6: o.curve(MEWLA_GLYPH_CURVE_FILL, W, 0, -W / 3, 0, -W / 3, H, W, H, 0); return;
        case 0xE0B5: o.curve(MEWLA_GLYPH_CURVE, 0, 0, W * 4 / 3, 0, W * 4 / 3, H, 0, H, t); return;
        case 0xE0B7: o.curve(MEWLA_GLYPH_CURVE, W, 0, -W / 3, 0, -W / 3, H, W, H, t); return;
        case 0xE0B8: o.polygon({0, 0, W, H, 0, H}); return;
        case 0xE0BA: o.polygon({W, 0, W, H, 0, H}); return;
        case 0xE0BC: o.polygon({0, 0, W, 0, 0, H}); return;
        case 0xE0BE: o.polygon({0, 0, W, 0, W, H}); return;
        case 0xE0B9: case 0xE0BF: o.line(0, 0, W, H, t); return;
        case 0xE0BB: case 0xE0BD: o.line(W, 0, 0, H, t); return;
        default: return;
    }
}

// ⏴⏵⏶⏷⏸⏹⏺ (Claude Code's mode and status marks): no bundled or common system face draws them as
// text (Android's fallback is tofu or a colour emoji), so a solid shape the size of a lowercase letter.
void media(Out& o, int w, int h, uint32_t cp) {
    float s = std::max(3.0f, std::round(std::min(w * 0.8f, h * 0.42f)));
    float x0 = std::round((w - s) / 2), y0 = std::round((h - s) / 2), x1 = x0 + s, y1 = y0 + s;
    float xm = (x0 + x1) / 2, ym = (y0 + y1) / 2, bar = std::max(1.0f, std::round(s / 3));
    switch (cp) {
        case 0x23F4: o.polygon({x1, y0, x0, ym, x1, y1}); return;
        case 0x23F5: o.polygon({x0, y0, x1, ym, x0, y1}); return;
        case 0x23F6: o.polygon({x0, y1, xm, y0, x1, y1}); return;
        case 0x23F7: o.polygon({x0, y0, x1, y0, xm, y1}); return;
        case 0x23F8: o.rect(x0, y0, x0 + bar, y1); o.rect(x1 - bar, y0, x1, y1); return;
        case 0x23F9: o.rect(x0, y0, x1, y1); return;
        case 0x23FA: o.ellipse(x0, y0, x1, y1); return;
        default: return;
    }
}

}  // namespace

extern "C" bool mewla_glyph_is_drawn(uint32_t cp) {
    return (cp >= 0x2500 && cp <= 0x259F) || (cp >= 0x2800 && cp <= 0x28FF) || (cp >= 0x23F4 && cp <= 0x23FA) ||
           (cp >= 0xE0B0 && cp <= 0xE0BF);
}

extern "C" int32_t mewla_glyph_thickness(int32_t height) {
    return std::max(1, int(std::lround(height / 14.0)));
}

extern "C" int32_t mewla_glyph_ops(uint32_t cp, int32_t w, int32_t h, MewlaGlyphOp* out, int32_t capacity) {
    if (!mewla_glyph_is_drawn(cp) || w <= 0 || h <= 0) return 0;
    Out o{out, capacity};
    int t = mewla_glyph_thickness(h);
    if (cp >= 0x2500 && cp <= 0x257F) {
        const char* spec = kBox[cp - 0x2500];
        if (spec[0] != '-') {
            boxLines(o, w, h, t, spec[0] - '0', spec[1] - '0', spec[2] - '0', spec[3] - '0');
        } else {
            switch (cp) {
                case 0x2504: dashes(o, w, h, t, false, false, 3); break;
                case 0x2505: dashes(o, w, h, t, true, false, 3); break;
                case 0x2506: dashes(o, w, h, t, false, true, 3); break;
                case 0x2507: dashes(o, w, h, t, true, true, 3); break;
                case 0x2508: dashes(o, w, h, t, false, false, 4); break;
                case 0x2509: dashes(o, w, h, t, true, false, 4); break;
                case 0x250A: dashes(o, w, h, t, false, true, 4); break;
                case 0x250B: dashes(o, w, h, t, true, true, 4); break;
                case 0x254C: dashes(o, w, h, t, false, false, 2); break;
                case 0x254D: dashes(o, w, h, t, true, false, 2); break;
                case 0x254E: dashes(o, w, h, t, false, true, 2); break;
                case 0x254F: dashes(o, w, h, t, true, true, 2); break;
                case 0x256D: arc(o, w, h, t, true, true); break;
                case 0x256E: arc(o, w, h, t, true, false); break;
                case 0x256F: arc(o, w, h, t, false, false); break;
                case 0x2570: arc(o, w, h, t, false, true); break;
                case 0x2571: o.line(w, 0, 0, h, t); break;
                case 0x2572: o.line(0, 0, w, h, t); break;
                case 0x2573: o.line(w, 0, 0, h, t); o.line(0, 0, w, h, t); break;
                default: break;
            }
        }
    } else if (cp >= 0x23F4 && cp <= 0x23FA) {
        media(o, w, h, cp);
    } else if (cp >= 0x2500 && cp <= 0x259F) {
        block(o, w, h, cp);
    } else if (cp >= 0x2800 && cp <= 0x28FF) {
        braille(o, w, h, cp);
    } else {
        powerline(o, w, h, t, cp);
    }
    return std::min(o.n, capacity);
}
