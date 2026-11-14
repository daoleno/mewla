// The terminal's own glyphs (client/native/terminal/TerminalGlyphs.cpp): every box-drawing
// line meets the cell edge where its neighbour's line does, at several cell sizes, so frames
// join; blocks tile exactly. With --pgm <file> it also writes a sheet of every glyph.
#include "TerminalGlyphs.h"

#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

namespace {

struct Bitmap {
    int w, h;
    std::vector<float> px;
    Bitmap(int w, int h) : w(w), h(h), px(size_t(w) * h, 0) {}
    float at(int x, int y) const { return px[size_t(y) * w + x]; }
};

// Rectangles exactly; everything else by sampling pixel centres (enough to see shapes).
Bitmap raster(uint32_t cp, int w, int h) {
    Bitmap b(w, h);
    MewlaGlyphOp ops[64];
    int n = mewla_glyph_ops(cp, w, h, ops, 64);
    for (int i = 0; i < n; i++) {
        const MewlaGlyphOp& op = ops[i];
        for (int y = 0; y < h; y++) {
            for (int x = 0; x < w; x++) {
                float cx = x + 0.5f, cy = y + 0.5f;
                bool in = false;
                if (op.kind == MEWLA_GLYPH_RECT || op.kind == MEWLA_GLYPH_ELLIPSE) {
                    in = cx >= op.p[0] && cx < op.p[2] && cy >= op.p[1] && cy < op.p[3];
                } else if (op.kind == MEWLA_GLYPH_POLYGON) {
                    // Even-odd point in polygon.
                    for (int a = 0, c = op.points - 1; a < op.points; c = a++) {
                        float xa = op.p[2 * a], ya = op.p[2 * a + 1], xc = op.p[2 * c], yc = op.p[2 * c + 1];
                        if ((ya > cy) != (yc > cy) && cx < (xc - xa) * (cy - ya) / (yc - ya) + xa) in = !in;
                    }
                } else if (op.kind == MEWLA_GLYPH_LINE) {
                    float dx = op.p[2] - op.p[0], dy = op.p[3] - op.p[1], len = std::sqrt(dx * dx + dy * dy);
                    float t = ((cx - op.p[0]) * dx + (cy - op.p[1]) * dy) / (len * len);
                    float d = std::fabs((cx - op.p[0]) * dy - (cy - op.p[1]) * dx) / len;
                    in = t >= 0 && t <= 1 && d <= op.stroke / 2;
                } else {
                    // Cubic: sample points along it.
                    for (int s = 0; s <= 64 && !in; s++) {
                        float u = s / 64.0f, v = 1 - u;
                        float bx = v * v * v * op.p[0] + 3 * v * v * u * op.p[2] + 3 * v * u * u * op.p[4] + u * u * u * op.p[6];
                        float by = v * v * v * op.p[1] + 3 * v * v * u * op.p[3] + 3 * v * u * u * op.p[5] + u * u * u * op.p[7];
                        in = std::fabs(bx - cx) <= std::max(op.stroke / 2, 0.5f) && std::fabs(by - cy) <= std::max(op.stroke / 2, 0.5f);
                    }
                }
                if (in) b.px[size_t(y) * w + x] = std::max(b.px[size_t(y) * w + x], op.alpha);
            }
        }
    }
    return b;
}

// Which pixels are lit along an edge: the column x (or row y).
std::string column(const Bitmap& b, int x) {
    std::string s;
    for (int y = 0; y < b.h; y++) s += b.at(x, y) > 0.5f ? '#' : '.';
    return s;
}
std::string row(const Bitmap& b, int y) {
    std::string s;
    for (int x = 0; x < b.w; x++) s += b.at(x, y) > 0.5f ? '#' : '.';
    return s;
}

int failures = 0;
void check(bool ok, const char* what, uint32_t cp, int w, int h, const std::string& got, const std::string& want) {
    if (ok) return;
    failures++;
    if (failures <= 30) std::printf("U+%04X %dx%d %s\n  got  %s\n  want %s\n", cp, w, h, what, got.c_str(), want.c_str());
}

}  // namespace

int main(int argc, char** argv) {
    // The edge each weight leaves, from a reference: the straight lines ─ ━ ═ │ ┃ ║.
    const int sizes[][2] = {{13, 29}, {12, 27}, {20, 44}, {9, 20}, {24, 52}, {7, 15}};
    // Weights of U+2500-257F as URDL, the same table the code uses, re-read from the glyphs:
    // for each spec'd character every arm's edge must equal the straight line of that weight.
    const char* spec[128] = {
        "0101", "0202", "1010", "2020", "-", "-", "-", "-", "-", "-", "-", "-", "0110", "0210", "0120", "0220",
        "0011", "0012", "0021", "0022", "1100", "1200", "2100", "2200", "1001", "1002", "2001", "2002", "1110", "1210", "2110", "1120",
        "2120", "2210", "1220", "2220", "1011", "1012", "2011", "1021", "2021", "2012", "1022", "2022", "0111", "0112", "0211", "0212",
        "0121", "0122", "0221", "0222", "1101", "1102", "1201", "1202", "2101", "2102", "2201", "2202", "1111", "1112", "1211", "1212",
        "2111", "1121", "2121", "2112", "2211", "1122", "1221", "2212", "1222", "2122", "2221", "2222", "-", "-", "-", "-",
        "0303", "3030", "0310", "0130", "0330", "0013", "0031", "0033", "1300", "3100", "3300", "1003", "3001", "3003", "1310", "3130",
        "3330", "1013", "3031", "3033", "0313", "0131", "0333", "1303", "3101", "3303", "1313", "3131", "3333", "-", "-", "-",
        "-", "-", "-", "-", "0001", "1000", "0100", "0010", "0002", "2000", "0200", "0020", "0201", "1020", "0102", "2010",
    };
    for (auto& s : sizes) {
        int w = s[0], h = s[1];
        Bitmap hl = raster(0x2500, w, h), hh = raster(0x2501, w, h), hd = raster(0x2550, w, h);
        Bitmap vl = raster(0x2502, w, h), vh = raster(0x2503, w, h), vd = raster(0x2551, w, h);
        std::string hEdge[4] = {std::string(h, '.'), column(hl, 0), column(hh, 0), column(hd, 0)};
        std::string vEdge[4] = {std::string(w, '.'), row(vl, 0), row(vh, 0), row(vd, 0)};
        for (int i = 0; i < 128; i++) {
            if (spec[i][0] == '-') continue;
            uint32_t cp = 0x2500 + i;
            Bitmap b = raster(cp, w, h);
            int U = spec[i][0] - '0', R = spec[i][1] - '0', D = spec[i][2] - '0', L = spec[i][3] - '0';
            check(row(b, 0) == vEdge[U], "top edge", cp, w, h, row(b, 0), vEdge[U]);
            check(row(b, h - 1) == vEdge[D], "bottom edge", cp, w, h, row(b, h - 1), vEdge[D]);
            check(column(b, 0) == hEdge[L], "left edge", cp, w, h, column(b, 0), hEdge[L]);
            check(column(b, w - 1) == hEdge[R], "right edge", cp, w, h, column(b, w - 1), hEdge[R]);
            // Joined: every lit pixel connects to an edge (no floating fragments), and the
            // centre is lit (the arms meet).
            bool centre = false;
            for (int y = h / 2 - 3; y <= h / 2 + 3; y++)
                for (int x = w / 2 - 3; x <= w / 2 + 3; x++)
                    if (x >= 0 && y >= 0 && x < w && y < h && b.at(x, y) > 0.5f) centre = true;
            check(centre, "arms meet at the centre", cp, w, h, "", "");
        }
        // Arcs leave the light line's edges on their two arms.
        const struct { uint32_t cp; int U, R, D, L; } arcs[] = {{0x256D, 0, 1, 1, 0}, {0x256E, 0, 0, 1, 1}, {0x256F, 1, 0, 0, 1}, {0x2570, 1, 1, 0, 0}};
        for (auto& a : arcs) {
            Bitmap b = raster(a.cp, w, h);
            if (a.R) check(column(b, w - 1) == hEdge[1], "arc right edge", a.cp, w, h, column(b, w - 1), hEdge[1]);
            if (a.L) check(column(b, 0) == hEdge[1], "arc left edge", a.cp, w, h, column(b, 0), hEdge[1]);
            if (a.D) check(row(b, h - 1) == vEdge[1], "arc bottom edge", a.cp, w, h, row(b, h - 1), vEdge[1]);
            if (a.U) check(row(b, 0) == vEdge[1], "arc top edge", a.cp, w, h, row(b, 0), vEdge[1]);
        }
        // Blocks: the full block covers every pixel; ▀ + ▄ tile it with no overlap; ▌ + ▐ likewise.
        Bitmap full = raster(0x2588, w, h), up = raster(0x2580, w, h), lo = raster(0x2584, w, h), le = raster(0x258C, w, h), ri = raster(0x2590, w, h);
        for (int y = 0; y < h; y++)
            for (int x = 0; x < w; x++) {
                check(full.at(x, y) == 1, "█ covers the cell", 0x2588, w, h, "", "");
                check(up.at(x, y) + lo.at(x, y) == 1, "▀ + ▄ tile the cell", 0x2580, w, h, "", "");
                check(le.at(x, y) + ri.at(x, y) == 1, "▌ + ▐ tile the cell", 0x258C, w, h, "", "");
            }
        // Powerline: the solid arrows touch the top and bottom edges at the cell's left (right) side.
        Bitmap pl = raster(0xE0B0, w, h), pr = raster(0xE0B2, w, h);
        check(pl.at(0, 1) > 0.5f && pl.at(0, h - 2) > 0.5f && pl.at(w - 1, h / 2) > 0.5f, "", 0xE0B0, w, h, "fills its corners", "");
        check(pr.at(w - 1, 1) > 0.5f && pr.at(w - 1, h - 2) > 0.5f && pr.at(0, h / 2) > 0.5f, "", 0xE0B2, w, h, "fills its corners", "");
        // The media marks: solid, centred, clear of the cell's edges.
        for (uint32_t c = 0x23F4; c <= 0x23FA; c++) {
            Bitmap m = raster(c, w, h);
            // Pause is two bars with the gap at the centre: lit on both sides of it instead.
            bool left = false, right = false;
            for (int x = 0; x < w / 2; x++) left = left || m.at(x, h / 2) > 0.5f;
            for (int x = (w + 1) / 2; x < w; x++) right = right || m.at(x, h / 2) > 0.5f;
            check(c == 0x23F8 ? left && right : m.at(w / 2, h / 2) > 0.5f, "", c, w, h, "is solid at the centre", "");
            check(m.at(0, 0) < 0.1f && m.at(w - 1, h - 1) < 0.1f && m.at(w / 2, 0) < 0.1f && m.at(w / 2, h - 1) < 0.1f, "", c, w, h,
                  "stays clear of the edges", "");
        }
    }
    if (argc > 2 && std::strcmp(argv[1], "--pgm") == 0) {
        // A sheet: U+2500-259F, U+2800-283F, U+23F4-23FA, U+E0B0-E0BF at 13x29, 16 per row, 2 px apart.
        std::vector<uint32_t> cps;
        for (uint32_t c = 0x2500; c <= 0x259F; c++) cps.push_back(c);
        for (uint32_t c = 0x2800; c <= 0x283F; c++) cps.push_back(c);
        for (uint32_t c = 0x23F4; c <= 0x23FA; c++) cps.push_back(c);
        for (uint32_t c = 0xE0B0; c <= 0xE0BF; c++) cps.push_back(c);
        int w = 13, h = 29, per = 16, rows = int((cps.size() + per - 1) / per);
        int W = per * (w + 2), H = rows * (h + 2);
        std::vector<unsigned char> img(size_t(W) * H, 40);
        for (size_t i = 0; i < cps.size(); i++) {
            Bitmap b = raster(cps[i], w, h);
            int ox = int(i % per) * (w + 2), oy = int(i / per) * (h + 2);
            for (int y = 0; y < h; y++)
                for (int x = 0; x < w; x++) img[size_t(oy + y) * W + ox + x] = (unsigned char)(20 + 235 * b.at(x, y));
        }
        FILE* f = std::fopen(argv[2], "wb");
        std::fprintf(f, "P5\n%d %d\n255\n", W, H);
        std::fwrite(img.data(), 1, img.size(), f);
        std::fclose(f);
    }
    std::printf(failures ? "%d failures\n" : "ok: every box-drawing line meets its neighbour\n", failures);
    return failures ? 1 : 0;
}
