#if defined(__ANDROID__)
#include <jni.h>
#include <android/log.h>
#endif
#include <array>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>
#include <ghostty/vt.h>

#if defined(__ANDROID__)
#define TAG "TerminalVt"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, TAG, __VA_ARGS__)
#endif

// The core's scrollback is a byte budget, rounded up to whole pages: 10000
// bytes keeps one page, about 375-750 rows depending on width and styling.
// The app never reads it: the live grid renders only the viewport, and scroll
// history comes from the tmux capture (useTerminalHistory renders it in a
// separate 64-row core). Keeping 10000 *lines* instead
// (GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_LINES with an unlimited byte budget)
// measured about 9 MB per terminal at 100 columns and 17 MB at 200 for styled
// output, for rows nothing displays. tests/render_row_updates.cpp pins the
// current depth.
static constexpr size_t kScrollbackMaxBytes = 10000;
static constexpr size_t kMouseStackBufferSize = 128;

#if defined(__ANDROID__)
static jstring newStringFromUtf8Bytes(JNIEnv* env, const uint8_t* bytes, size_t len) {
    jclass stringClass = env->FindClass("java/lang/String");
    if (!stringClass) {
        return env->NewStringUTF("");
    }

    jmethodID ctor = env->GetMethodID(stringClass, "<init>", "([BLjava/lang/String;)V");
    if (!ctor) {
        return env->NewStringUTF("");
    }

    jbyteArray byteArray = env->NewByteArray((jsize)len);
    if (!byteArray) {
        return env->NewStringUTF("");
    }

    if (len > 0) {
        env->SetByteArrayRegion(
            byteArray,
            0,
            (jsize)len,
            reinterpret_cast<const jbyte*>(bytes)
        );
    }

    jstring charset = env->NewStringUTF("UTF-8");
    if (!charset) {
        env->DeleteLocalRef(byteArray);
        return env->NewStringUTF("");
    }

    auto result = static_cast<jstring>(env->NewObject(stringClass, ctor, byteArray, charset));
    env->DeleteLocalRef(byteArray);
    env->DeleteLocalRef(charset);

    if (env->ExceptionCheck() || !result) {
        env->ExceptionClear();
        return env->NewStringUTF("");
    }

    return result;
}
#endif

/**
 * Per-terminal state: owns the terminal, render state, and HTML formatter.
 */
struct TerminalHandle {
    GhosttyTerminal terminal = nullptr;
    GhosttyRenderState render_state = nullptr;
    GhosttyFormatter html_formatter = nullptr;
    GhosttyMouseEncoder mouse_encoder = nullptr;
    uint16_t cols = 0;
    uint16_t rows = 0;
    uint32_t cell_width_px = 1;
    uint32_t cell_height_px = 1;
    bool force_full_snapshot = true;
    std::vector<std::string> rendered_rows;
    uint16_t rendered_cols = 0;
    // The last text a program copied (OSC 52, OSC 1337 Copy, OSC 5522), until
    // nativeTakeClipboard hands it to the view.
    std::string clipboard;
    bool has_clipboard = false;
};

static GhosttyResult createHtmlFormatter(
    TerminalHandle* h,
    GhosttyFormatter* out)
{
    if (!h || !out) {
        return GHOSTTY_INVALID_VALUE;
    }

    GhosttyResult res = ghostty_formatter_terminal_new(nullptr, out, h->terminal);
    if (res != GHOSTTY_SUCCESS) {
        return res;
    }

    // A 1.0 formatter starts as trimmed plain text with the palette, style
    // and hyperlink extras. Pin every option this bridge relies on.
    const GhosttyFormatterFormat emit = GHOSTTY_FORMATTER_FORMAT_HTML;
    const bool trim = false;
    const GhosttyFormatterExtras extras = GHOSTTY_FORMATTER_EXTRAS_NONE;
    res = ghostty_formatter_set(*out, GHOSTTY_FORMATTER_OPT_EMIT, &emit);
    if (res == GHOSTTY_SUCCESS) {
        res = ghostty_formatter_set(*out, GHOSTTY_FORMATTER_OPT_TRIM, &trim);
    }
    if (res == GHOSTTY_SUCCESS) {
        res = ghostty_formatter_set(*out, GHOSTTY_FORMATTER_OPT_EXTRAS, &extras);
    }
    if (res != GHOSTTY_SUCCESS) {
        ghostty_formatter_free(*out);
        *out = nullptr;
    }
    return res;
}

static GhosttyResult createTerminalInstance(
    uint16_t cols,
    uint16_t rows,
    GhosttyTerminal* out)
{
    if (!out) {
        return GHOSTTY_INVALID_VALUE;
    }

    // The headers this bridge was compiled against and the library loaded at
    // run time must agree on the major version, or sized structs and enum
    // values below mean something different on each side of the call.
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

static std::string formatTerminalScreen(TerminalHandle* h)
{
    if (!h) return {};

    GhosttyFormatter formatter = h->html_formatter;
    if (!formatter) {
        return {};
    }

    uint8_t* outPtr = nullptr;
    size_t outLen = 0;
    GhosttyResult res = ghostty_formatter_format_alloc(formatter, nullptr, &outPtr, &outLen);

    if (res != GHOSTTY_SUCCESS || !outPtr) {
        return {};
    }

    std::string result(reinterpret_cast<const char*>(outPtr), outLen);
    ghostty_free(nullptr, outPtr, outLen);
    return result;
}

static TerminalHandle* getHandle(uintptr_t h) {
    return reinterpret_cast<TerminalHandle*>(h);
}

static void markFullSnapshot(TerminalHandle* h) {
    if (!h) {
        return;
    }
    h->force_full_snapshot = true;
}

static GhosttyResult populateRowIterator(
    GhosttyRenderState renderState,
    GhosttyRenderStateRowIterator rowIterator)
{
    if (!renderState || !rowIterator) {
        return GHOSTTY_INVALID_VALUE;
    }

    return ghostty_render_state_get(
        renderState,
        GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR,
        &rowIterator
    );
}

static GhosttyResult populateRowCells(
    GhosttyRenderStateRowIterator rowIterator,
    GhosttyRenderStateRowCells rowCells)
{
    if (!rowIterator || !rowCells) {
        return GHOSTTY_INVALID_VALUE;
    }

    return ghostty_render_state_row_get(
        rowIterator,
        GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
        &rowCells
    );
}

#if defined(__ANDROID__)
static jstring newStringFromStdString(JNIEnv* env, const std::string& value) {
    return newStringFromUtf8Bytes(
        env,
        reinterpret_cast<const uint8_t*>(value.data()),
        value.size()
    );
}
#endif

static bool colorsEqual(const GhosttyColorRgb& left, const GhosttyColorRgb& right) {
    return left.r == right.r && left.g == right.g && left.b == right.b;
}

static double colorPerceivedLuminance(const GhosttyColorRgb& color) {
    return (
        0.299 * static_cast<double>(color.r) +
        0.587 * static_cast<double>(color.g) +
        0.114 * static_cast<double>(color.b)
    ) / 255.0;
}

static uint8_t mixColorChannel(uint8_t from, uint8_t to, double weight) {
    const double mixed =
        static_cast<double>(from) +
        (static_cast<double>(to) - static_cast<double>(from)) * weight;
    if (mixed <= 0.0) {
        return 0;
    }
    if (mixed >= 255.0) {
        return 255;
    }
    return static_cast<uint8_t>(std::lround(mixed));
}

static GhosttyColorRgb mixColors(
    const GhosttyColorRgb& from,
    const GhosttyColorRgb& to,
    double weight)
{
    return GhosttyColorRgb{
        mixColorChannel(from.r, to.r, weight),
        mixColorChannel(from.g, to.g, weight),
        mixColorChannel(from.b, to.b, weight),
    };
}

static void appendCssHexColor(std::string* out, const GhosttyColorRgb& color) {
    static constexpr char hex[] = "0123456789abcdef";
    out->push_back('#');
    out->push_back(hex[(color.r >> 4) & 0xF]);
    out->push_back(hex[color.r & 0xF]);
    out->push_back(hex[(color.g >> 4) & 0xF]);
    out->push_back(hex[color.g & 0xF]);
    out->push_back(hex[(color.b >> 4) & 0xF]);
    out->push_back(hex[color.b & 0xF]);
}

static void appendUtf8(std::string* out, uint32_t codepoint) {
    if (codepoint > 0x10FFFF || (codepoint >= 0xD800 && codepoint <= 0xDFFF)) {
        codepoint = 0xFFFD;
    }

    if (codepoint <= 0x7F) {
        out->push_back(static_cast<char>(codepoint));
        return;
    }

    if (codepoint <= 0x7FF) {
        out->push_back(static_cast<char>(0xC0 | (codepoint >> 6)));
        out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
        return;
    }

    if (codepoint <= 0xFFFF) {
        out->push_back(static_cast<char>(0xE0 | (codepoint >> 12)));
        out->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3F)));
        out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
        return;
    }

    out->push_back(static_cast<char>(0xF0 | (codepoint >> 18)));
    out->push_back(static_cast<char>(0x80 | ((codepoint >> 12) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | ((codepoint >> 6) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | (codepoint & 0x3F)));
}

static void appendHtmlEscapedCodepoint(std::string* out, uint32_t codepoint) {
    switch (codepoint) {
        case '&':
            out->append("&amp;");
            return;
        case '<':
            out->append("&lt;");
            return;
        case '>':
            out->append("&gt;");
            return;
        default:
            appendUtf8(out, codepoint);
            return;
    }
}

// Rows are text runs, so a glyph advances by its font's width, not its cell's.
// The bundled mono face carries Latin, box drawing, blocks, braille and
// Powerline at exactly one cell, drawn as plain text; everything else may come from a system
// fallback face (CJK, symbols, emoji) with its own advance. Pin those cells to
// their grid width (<w> two cells, <n> one) so later columns never drift. The
// bare tags keep Chinese scrollback inside the history HTML budget.
static const char* pinnedCellClass(GhosttyCell cell, GhosttyCellWide wide) {
    if (wide == GHOSTTY_CELL_WIDE_WIDE) {
        return "w";
    }
    uint32_t codepoint = 0;
    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &codepoint);
    // Keep in sync with PLAIN_RANGES in scripts/subset-maple-mono.py.
    if (codepoint < 0x2000 ||
        (codepoint >= 0x2500 && codepoint <= 0x259F) ||
        (codepoint >= 0x2800 && codepoint <= 0x28FF) ||
        (codepoint >= 0xE0A0 && codepoint <= 0xE0A2) ||
        (codepoint >= 0xE0B0 && codepoint <= 0xE0B7)) {
        return nullptr;
    }
    return "n";
}

static bool appendCellText(
    GhosttyRenderStateRowCells rowCells,
    GhosttyCell cell,
    bool preserveBlankCell,
    std::string* htmlText)
{
    if (!htmlText) {
        return false;
    }

    bool hasText = false;
    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_HAS_TEXT, &hasText);
    if (!hasText) {
        if (preserveBlankCell) {
            htmlText->push_back(' ');
        }
        return false;
    }

    uint32_t graphemeLen = 0;
    if (ghostty_render_state_row_cells_get(
            rowCells,
            GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN,
            &graphemeLen) == GHOSTTY_SUCCESS &&
        graphemeLen > 0) {
        std::vector<uint32_t> graphemes(graphemeLen, 0);
        // 1.0 fills a GhosttyCodepointBuffer; handing it the bare array of 0.x
        // makes the library read the first codepoints as ptr and cap.
        GhosttyCodepointBuffer buffer{};
        buffer.ptr = graphemes.data();
        buffer.cap = graphemes.size();
        if (ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
                &buffer) == GHOSTTY_SUCCESS) {
            bool visible = false;
            for (size_t i = 0; i < buffer.len && i < graphemes.size(); i++) {
                const uint32_t codepoint = graphemes[i];
                appendHtmlEscapedCodepoint(htmlText, codepoint);
                if (codepoint != 0 && codepoint != ' ') {
                    visible = true;
                }
            }
            return visible;
        }
    }

    uint32_t codepoint = 0;
    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &codepoint);
    if (codepoint == 0) {
        if (preserveBlankCell) {
            htmlText->push_back(' ');
        }
        return false;
    }

    appendHtmlEscapedCodepoint(htmlText, codepoint);
    return codepoint != ' ';
}

static bool htmlHasVisibleText(const std::string& html) {
    bool inTag = false;
    for (size_t index = 0; index < html.size(); index += 1) {
        const char ch = html[index];
        if (inTag) {
            if (ch == '>') {
                inTag = false;
            }
            continue;
        }

        if (ch == '<') {
            inTag = true;
            continue;
        }

        if (ch == '&') {
            const size_t entityEnd = html.find(';', index + 1);
            if (entityEnd == std::string::npos) {
                return true;
            }

            const std::string entity = html.substr(index, entityEnd - index + 1);
            if (entity != "&nbsp;" && entity != "&#32;" && entity != "&#x20;") {
                return true;
            }
            index = entityEnd;
            continue;
        }

        if (ch != ' ') {
            return true;
        }
    }

    return false;
}

static bool resolveStyleColor(
    const GhosttyStyleColor& color,
    const GhosttyRenderStateColors& renderColors,
    GhosttyColorRgb* out)
{
    if (!out) {
        return false;
    }

    switch (color.tag) {
        case GHOSTTY_STYLE_COLOR_NONE:
            return false;
        case GHOSTTY_STYLE_COLOR_PALETTE:
            *out = renderColors.palette[color.value.palette];
            return true;
        case GHOSTTY_STYLE_COLOR_RGB:
            *out = color.value.rgb;
            return true;
        default:
            return false;
    }
}

static bool styleColorsEqual(const GhosttyStyleColor& left, const GhosttyStyleColor& right) {
    if (left.tag != right.tag) {
        return false;
    }

    switch (left.tag) {
        case GHOSTTY_STYLE_COLOR_NONE:
            return true;
        case GHOSTTY_STYLE_COLOR_PALETTE:
            return left.value.palette == right.value.palette;
        case GHOSTTY_STYLE_COLOR_RGB:
            return colorsEqual(left.value.rgb, right.value.rgb);
        default:
            return false;
    }
}

static bool stylesEquivalent(const GhosttyStyle& left, const GhosttyStyle& right) {
    return
        styleColorsEqual(left.fg_color, right.fg_color) &&
        styleColorsEqual(left.bg_color, right.bg_color) &&
        styleColorsEqual(left.underline_color, right.underline_color) &&
        left.flags == right.flags;
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
    if (!out) {
        return false;
    }

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

static std::string buildCellCss(
    const GhosttyStyle& style,
    const GhosttyRenderStateColors& renderColors,
    bool hasResolvedFg,
    const GhosttyColorRgb& resolvedFg,
    bool hasResolvedBg,
    const GhosttyColorRgb& resolvedBg)
{
    GhosttyColorRgb fg =
        resolveEffectiveForeground(style, renderColors, hasResolvedFg, resolvedFg);
    GhosttyColorRgb bg = renderColors.background;
    bool hasBg = resolveEffectiveBackground(style, renderColors, hasResolvedBg, resolvedBg, &bg);

    if ((style.flags & GHOSTTY_STYLE_FLAG_INVERSE)) {
        const bool hasExplicitFg =
            hasResolvedFg || style.fg_color.tag != GHOSTTY_STYLE_COLOR_NONE;
        const bool hasExplicitBg =
            hasBg || style.bg_color.tag != GHOSTTY_STYLE_COLOR_NONE;
        const bool isDefaultReverseOnLightTheme =
            !hasExplicitFg &&
            !hasExplicitBg &&
            colorPerceivedLuminance(renderColors.background) > 0.62;

        if (isDefaultReverseOnLightTheme) {
            const GhosttyColorRgb highlightSource =
                renderColors.cursor_has_value ? renderColors.cursor : renderColors.foreground;
            fg = renderColors.foreground;
            bg = mixColors(renderColors.background, highlightSource, 0.22);
        } else {
            const GhosttyColorRgb originalFg = fg;
            fg = bg;
            bg = originalFg;
        }
        hasBg = true;
    }

    std::string css;
    css.reserve(160);

    if ((style.flags & GHOSTTY_STYLE_FLAG_INVISIBLE)) {
        css.append("color:transparent;");
    } else if (!colorsEqual(fg, renderColors.foreground)) {
        css.append("color:");
        appendCssHexColor(&css, fg);
        css.push_back(';');
    }

    if (hasBg && !colorsEqual(bg, renderColors.background)) {
        css.append("background-color:");
        appendCssHexColor(&css, bg);
        css.push_back(';');
    }

    if ((style.flags & GHOSTTY_STYLE_FLAG_BOLD)) {
        css.append("font-weight:700;");
    }
    if ((style.flags & GHOSTTY_STYLE_FLAG_ITALIC)) {
        css.append("font-style:italic;");
    }
    if ((style.flags & GHOSTTY_STYLE_FLAG_FAINT)) {
        css.append("opacity:0.72;");
    }

    std::string decorationLine;
    if (GHOSTTY_STYLE_UNDERLINE(style) != GHOSTTY_SGR_UNDERLINE_NONE) {
        decorationLine.append(" underline");
    }
    if ((style.flags & GHOSTTY_STYLE_FLAG_STRIKETHROUGH)) {
        decorationLine.append(" line-through");
    }
    if ((style.flags & GHOSTTY_STYLE_FLAG_OVERLINE)) {
        decorationLine.append(" overline");
    }
    if (!decorationLine.empty()) {
        css.append("text-decoration-line:");
        css.append(decorationLine.c_str() + 1);
        css.push_back(';');
    }

    if (GHOSTTY_STYLE_UNDERLINE(style) != GHOSTTY_SGR_UNDERLINE_NONE) {
        css.append("text-decoration-style:");
        switch (GHOSTTY_STYLE_UNDERLINE(style)) {
            case GHOSTTY_SGR_UNDERLINE_DOUBLE:
                css.append("double;");
                break;
            case GHOSTTY_SGR_UNDERLINE_CURLY:
                css.append("wavy;");
                break;
            case GHOSTTY_SGR_UNDERLINE_DOTTED:
                css.append("dotted;");
                break;
            case GHOSTTY_SGR_UNDERLINE_DASHED:
                css.append("dashed;");
                break;
            case GHOSTTY_SGR_UNDERLINE_SINGLE:
            default:
                css.append("solid;");
                break;
        }

        GhosttyColorRgb underlineColor = {};
        if (resolveStyleColor(style.underline_color, renderColors, &underlineColor)) {
            css.append("text-decoration-color:");
            appendCssHexColor(&css, underlineColor);
            css.push_back(';');
        }
    }

    return css;
}

static void flushStyledSegment(
    std::string* rowHtml,
    const std::string& css,
    std::string* text)
{
    if (text->empty()) {
        return;
    }

    if (css.empty()) {
        rowHtml->append(*text);
        text->clear();
        return;
    }

    rowHtml->append("<span style=\"");
    rowHtml->append(css);
    rowHtml->append("\">");
    rowHtml->append(*text);
    rowHtml->append("</span>");
    text->clear();
}

static std::string buildRowHtml(
    GhosttyRenderStateRowIterator rowIterator,
    const GhosttyRenderStateColors& renderColors)
{
    GhosttyRenderStateRowCells rowCells = nullptr;
    if (ghostty_render_state_row_cells_new(nullptr, &rowCells) != GHOSTTY_SUCCESS || !rowCells) {
        return "";
    }

    const GhosttyResult rowCellsRes = populateRowCells(rowIterator, rowCells);
    if (rowCellsRes != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_cells_free(rowCells);
        return "";
    }

    std::string rowHtml;
    std::string segmentCss;
    std::string segmentText;
    rowHtml.reserve(256);
    segmentText.reserve(128);
    bool sawVisibleText = false;
    bool sawNonDefaultBackground = false;
    bool hasSegmentStyle = false;
    bool segmentHasFg = false;
    bool segmentHasBg = false;
    bool segmentHasNonDefaultBackground = false;
    GhosttyStyle segmentStyle{};
    GhosttyColorRgb segmentFg = renderColors.foreground;
    GhosttyColorRgb segmentBg = renderColors.background;

    while (ghostty_render_state_row_cells_next(rowCells)) {
        GhosttyCell cell = 0;
        if (ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW,
                &cell) != GHOSTTY_SUCCESS) {
            continue;
        }

        GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
        ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
        if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL || wide == GHOSTTY_CELL_WIDE_SPACER_HEAD) {
            continue;
        }

        GhosttyStyle style{};
        ghostty_style_default(&style);
        ghostty_render_state_row_cells_get(
            rowCells,
            GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE,
            &style
        );

        GhosttyColorRgb fg = renderColors.foreground;
        const bool hasFg =
            ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR,
                &fg) == GHOSTTY_SUCCESS;
        if (!hasFg) {
            fg = renderColors.foreground;
        }

        GhosttyColorRgb bg = renderColors.background;
        const bool hasBg =
            ghostty_render_state_row_cells_get(
                rowCells,
                GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR,
                &bg) == GHOSTTY_SUCCESS;

        const bool sameSegmentStyle =
            hasSegmentStyle &&
            segmentHasFg == hasFg &&
            segmentHasBg == hasBg &&
            colorsEqual(segmentFg, fg) &&
            colorsEqual(segmentBg, bg) &&
            stylesEquivalent(segmentStyle, style);

        if (!sameSegmentStyle && !segmentText.empty()) {
            flushStyledSegment(&rowHtml, segmentCss, &segmentText);
        }

        if (!sameSegmentStyle) {
            segmentCss = buildCellCss(style, renderColors, hasFg, fg, hasBg, bg);
            segmentStyle = style;
            segmentHasFg = hasFg;
            segmentHasBg = hasBg;
            segmentFg = fg;
            segmentBg = bg;
            segmentHasNonDefaultBackground =
                segmentCss.find("background-color:") != std::string::npos;
            hasSegmentStyle = true;
        }

        if (segmentHasNonDefaultBackground) {
            sawNonDefaultBackground = true;
        }

        const bool preserveBlankCell = true;
        const char* pinned = pinnedCellClass(cell, wide);
        if (pinned) {
            segmentText.push_back('<');
            segmentText.append(pinned);
            segmentText.push_back('>');
        }
        if (appendCellText(rowCells, cell, preserveBlankCell, &segmentText)) {
            sawVisibleText = true;
        }
        if (pinned) {
            segmentText.append("</");
            segmentText.append(pinned);
            segmentText.push_back('>');
        }
    }

    if (!segmentHasNonDefaultBackground) {
        while (!segmentText.empty() && segmentText.back() == ' ') {
            segmentText.pop_back();
        }
    }
    flushStyledSegment(&rowHtml, segmentCss, &segmentText);
    ghostty_render_state_row_cells_free(rowCells);
    if (!sawNonDefaultBackground && !sawVisibleText && !htmlHasVisibleText(rowHtml)) {
        return "";
    }
    return rowHtml;
}

static void readRowWrapFlags(
    GhosttyRenderStateRowIterator rowIterator,
    bool* wrap,
    bool* wrapContinuation)
{
    if (wrap) {
        *wrap = false;
    }
    if (wrapContinuation) {
        *wrapContinuation = false;
    }
    if (!rowIterator) {
        return;
    }

    GhosttyRow row = 0;
    if (ghostty_render_state_row_get(
            rowIterator,
            GHOSTTY_RENDER_STATE_ROW_DATA_RAW,
            &row) != GHOSTTY_SUCCESS) {
        return;
    }

    if (wrap) {
        bool value = false;
        if (ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP, &value) == GHOSTTY_SUCCESS) {
            *wrap = value;
        }
    }
    if (wrapContinuation) {
        bool value = false;
        if (ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP_CONTINUATION, &value) == GHOSTTY_SUCCESS) {
            *wrapContinuation = value;
        }
    }
}

static void appendVisibleHtmlRow(
    std::string* out,
    uint16_t rowIndex,
    bool wrap,
    bool wrapContinuation,
    const std::string& rowHtml)
{
    out->append("<div class=\"terminal-row\" data-row=\"");
    out->append(std::to_string(rowIndex));
    out->append("\" data-wrap=\"");
    out->append(wrap ? "1" : "0");
    out->append("\" data-wrap-continuation=\"");
    out->append(wrapContinuation ? "1" : "0");
    out->append("\">");
    out->append(rowHtml);
    out->append("</div>");
}

static bool buildVisibleHtml(
    GhosttyRenderState renderState,
    uint16_t expectedRows,
    std::string* out)
{
    if (!renderState || !out) {
        return false;
    }

    GhosttyRenderStateColors renderColors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
    if (ghostty_render_state_get(renderState, GHOSTTY_RENDER_STATE_DATA_COLORS, &renderColors) != GHOSTTY_SUCCESS) {
        return false;
    }

    GhosttyRenderStateRowIterator rowIterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &rowIterator) != GHOSTTY_SUCCESS || !rowIterator) {
        return false;
    }

    const GhosttyResult rowIteratorRes = populateRowIterator(renderState, rowIterator);
    if (rowIteratorRes != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(rowIterator);
        return false;
    }

    out->clear();
    out->reserve(static_cast<size_t>(expectedRows) * 96);

    uint16_t rowIndex = 0;
    while (ghostty_render_state_row_iterator_next(rowIterator)) {
        bool wrap = false;
        bool wrapContinuation = false;
        readRowWrapFlags(rowIterator, &wrap, &wrapContinuation);
        appendVisibleHtmlRow(
            out,
            rowIndex,
            wrap,
            wrapContinuation,
            buildRowHtml(rowIterator, renderColors)
        );
        rowIndex += 1;
    }

    ghostty_render_state_row_iterator_free(rowIterator);
    return rowIndex == expectedRows;
}

struct RenderRowUpdates {
    bool full = false;
    std::vector<uint16_t> indices;
    std::vector<std::string> html;
};

// Shared by Android and iOS. Ghostty's full-dirty flag can also mean a redraw
// of identical cells; only lifecycle/geometry/theme resets require a new base.
static bool buildRenderRowUpdates(
    TerminalHandle* h,
    uint16_t rows,
    uint16_t cols,
    GhosttyRenderStateDirty dirty,
    RenderRowUpdates* out)
{
    out->full = h->force_full_snapshot || h->rendered_rows.size() != rows ||
        h->rendered_cols != cols;
    out->indices.clear();
    out->html.clear();
    GhosttyRenderStateRowIterator iterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &iterator) != GHOSTTY_SUCCESS || !iterator) {
        return false;
    }
    if (populateRowIterator(h->render_state, iterator) != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(iterator);
        return false;
    }
    GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
    if (ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors) != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(iterator);
        return false;
    }
    h->rendered_rows.resize(rows);
    size_t index = 0;
    while (ghostty_render_state_row_iterator_next(iterator)) {
        if (index >= rows) break;
        bool rowDirty = true;
        ghostty_render_state_row_get(iterator, GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY, &rowDirty);
        if (out->full || dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL || rowDirty) {
            bool wrap = false, continuation = false;
            readRowWrapFlags(iterator, &wrap, &continuation);
            std::string html;
            appendVisibleHtmlRow(&html, static_cast<uint16_t>(index), wrap, continuation,
                buildRowHtml(iterator, colors));
            if (out->full || html != h->rendered_rows[index]) {
                out->indices.push_back(static_cast<uint16_t>(index));
                out->html.push_back(html);
                h->rendered_rows[index] = std::move(html);
            }
        }
        index++;
    }
    ghostty_render_state_row_iterator_free(iterator);
    if (index != rows) {
        h->force_full_snapshot = true;
        return false;
    }
    h->rendered_cols = cols;
    return true;
}

static void clearRenderStateDirty(GhosttyRenderState renderState) {
    if (!renderState) {
        return;
    }

    GhosttyRenderStateDirty clean = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_set(renderState, GHOSTTY_RENDER_STATE_OPT_DIRTY, &clean);

    GhosttyRenderStateRowIterator rowIterator = nullptr;
    if (ghostty_render_state_row_iterator_new(nullptr, &rowIterator) != GHOSTTY_SUCCESS || !rowIterator) {
        return;
    }

    if (populateRowIterator(renderState, rowIterator) != GHOSTTY_SUCCESS) {
        ghostty_render_state_row_iterator_free(rowIterator);
        return;
    }

    const bool cleanRow = false;
    while (ghostty_render_state_row_iterator_next(rowIterator)) {
        ghostty_render_state_row_set(
            rowIterator,
            GHOSTTY_RENDER_STATE_ROW_OPT_DIRTY,
            &cleanRow
        );
    }

    ghostty_render_state_row_iterator_free(rowIterator);
}

static uint32_t roundPositivePixels(float value) {
    if (!std::isfinite(value) || value <= 0) {
        return 1;
    }

    const long rounded = std::lround(value);
    return rounded > 0 ? static_cast<uint32_t>(rounded) : 1;
}

static uint32_t safeScreenPixels(uint16_t cells, uint32_t cellPixels) {
    const uint64_t screen = static_cast<uint64_t>(cells) * static_cast<uint64_t>(cellPixels);
    return screen > UINT32_MAX ? UINT32_MAX : static_cast<uint32_t>(screen);
}

static bool parseHexColor(const char* value, GhosttyColorRgb* out) {
    if (!value || !out) {
        return false;
    }

    const char* hex = value[0] == '#' ? value + 1 : value;
    if (std::strlen(hex) != 6) {
        return false;
    }

    char* end = nullptr;
    const unsigned long rgb = std::strtoul(hex, &end, 16);
    if (!end || end != hex + 6 || *end != '\0') {
        return false;
    }

    out->r = static_cast<uint8_t>((rgb >> 16) & 0xFF);
    out->g = static_cast<uint8_t>((rgb >> 8) & 0xFF);
    out->b = static_cast<uint8_t>(rgb & 0xFF);
    return true;
}

#if defined(__ANDROID__)
static bool parseHexColorString(JNIEnv* env, jstring value, GhosttyColorRgb* out) {
    if (!env || !value || !out) {
        return false;
    }

    const char* utf8 = env->GetStringUTFChars(value, nullptr);
    if (!utf8) {
        return false;
    }

    const bool ok = parseHexColor(utf8, out);
    env->ReleaseStringUTFChars(value, utf8);
    return ok;
}
#endif

static bool setTerminalOption(
    TerminalHandle* h,
    GhosttyTerminalOption option,
    const void* value,
    const char* label)
{
    if (!h || !h->terminal) {
        return false;
    }

    const GhosttyResult res = ghostty_terminal_set(h->terminal, option, value);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_set %s failed: %d", label, res);
        return false;
    }

    return true;
}

static bool decodeMouseAction(int action, GhosttyMouseAction* out) {
    if (!out) {
        return false;
    }

    switch (action) {
        case 0:
            *out = GHOSTTY_MOUSE_ACTION_PRESS;
            return true;
        case 1:
            *out = GHOSTTY_MOUSE_ACTION_RELEASE;
            return true;
        case 2:
            *out = GHOSTTY_MOUSE_ACTION_MOTION;
            return true;
        default:
            return false;
    }
}

static bool decodeMouseButton(int button, GhosttyMouseButton* out, bool* hasButton) {
    if (!out || !hasButton) {
        return false;
    }

    switch (button) {
        case 0:
            *hasButton = false;
            *out = GHOSTTY_MOUSE_BUTTON_UNKNOWN;
            return true;
        case 1:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_LEFT;
            return true;
        case 2:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_RIGHT;
            return true;
        case 3:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_MIDDLE;
            return true;
        case 4:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_FOUR;
            return true;
        case 5:
            *hasButton = true;
            *out = GHOSTTY_MOUSE_BUTTON_FIVE;
            return true;
        default:
            return false;
    }
}

static std::string encodeMouseSequence(
    TerminalHandle* h,
    GhosttyMouseAction action,
    GhosttyMouseButton button,
    bool hasButton,
    float x,
    float y,
    GhosttyMods mods,
    bool anyButtonPressed)
{
    if (!h || !h->mouse_encoder) {
        return {};
    }

    GhosttyMouseEncoderSize size = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
    size.screen_width = safeScreenPixels(h->cols, h->cell_width_px);
    size.screen_height = safeScreenPixels(h->rows, h->cell_height_px);
    size.cell_width = h->cell_width_px;
    size.cell_height = h->cell_height_px;

    ghostty_mouse_encoder_set_from_terminal(h->mouse_encoder, h->terminal);
    ghostty_mouse_encoder_set(h->mouse_encoder, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
    ghostty_mouse_encoder_set(
        h->mouse_encoder,
        GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED,
        &anyButtonPressed
    );

    if (action != GHOSTTY_MOUSE_ACTION_MOTION) {
        ghostty_mouse_encoder_reset(h->mouse_encoder);
    }

    GhosttyMouseEvent event = nullptr;
    GhosttyResult res = ghostty_mouse_event_new(nullptr, &event);
    if (res != GHOSTTY_SUCCESS || !event) {
        LOGE("ghostty_mouse_event_new failed: %d", res);
        return {};
    }

    ghostty_mouse_event_set_action(event, action);
    if (hasButton) {
        ghostty_mouse_event_set_button(event, button);
    } else {
        ghostty_mouse_event_clear_button(event);
    }
    ghostty_mouse_event_set_mods(event, mods);
    ghostty_mouse_event_set_position(event, GhosttyMousePosition{ .x = x, .y = y });

    uint8_t stackBuffer[kMouseStackBufferSize] = {};
    size_t outLen = 0;
    res = ghostty_mouse_encoder_encode(
        h->mouse_encoder,
        event,
        stackBuffer,
        sizeof(stackBuffer),
        &outLen
    );

    if (res == GHOSTTY_OUT_OF_SPACE && outLen > sizeof(stackBuffer)) {
        std::string dynamicBuffer(outLen, '\0');
        res = ghostty_mouse_encoder_encode(
            h->mouse_encoder,
            event,
            reinterpret_cast<uint8_t*>(dynamicBuffer.data()),
            dynamicBuffer.size(),
            &outLen
        );
        ghostty_mouse_event_free(event);
        if (res != GHOSTTY_SUCCESS || outLen == 0) {
            return {};
        }
        dynamicBuffer.resize(outLen);
        return dynamicBuffer;
    }

    ghostty_mouse_event_free(event);
    if (res != GHOSTTY_SUCCESS || outLen == 0) {
        return {};
    }

    return std::string(reinterpret_cast<const char*>(stackBuffer), outLen);
}

// A program's clipboard write, during ghostty_terminal_vt_write: keep its
// first text representation for the view to take after the write, and answer.
static void onClipboardWrite(GhosttyTerminal, void* userdata, const GhosttyClipboardWrite* write)
{
    if (!write) {
        return;
    }
    auto* h = static_cast<TerminalHandle*>(userdata);
    GhosttyClipboardWriteReply reply = GHOSTTY_INIT_SIZED(GhosttyClipboardWriteReply);
    reply.result = GHOSTTY_CLIPBOARD_WRITE_RESULT_UNSUPPORTED;
    for (size_t i = 0; h && write->contents && i < write->contents_len; i += 1) {
        const GhosttyClipboardContent& content = write->contents[i];
        const std::string mime = content.mime.ptr
            ? std::string(reinterpret_cast<const char*>(content.mime.ptr), content.mime.len)
            : std::string();
        if (mime.rfind("text/", 0) != 0) {
            continue;
        }
        h->clipboard = content.data.ptr
            ? std::string(reinterpret_cast<const char*>(content.data.ptr), content.data.len)
            : std::string();
        h->has_clipboard = true;
        reply.result = GHOSTTY_CLIPBOARD_WRITE_RESULT_SUCCESS;
        break;
    }
    if (write->reply) {
        write->reply(write, &reply);
    }
}

#if defined(__ANDROID__)
extern "C" {

JNIEXPORT jlong JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeCreateTerminal(
    JNIEnv* env, jobject, jint cols, jint rows)
{
    auto* h = new TerminalHandle();
    h->cols = (uint16_t)cols;
    h->rows = (uint16_t)rows;

    GhosttyResult res = createTerminalInstance(h->cols, h->rows, &h->terminal);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_terminal_new failed: %d", res);
        delete h;
        return 0;
    }

    res = ghostty_render_state_new(nullptr, &h->render_state);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("ghostty_render_state_new failed: %d", res);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = createHtmlFormatter(h, &h->html_formatter);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("html formatter init failed: %d", res);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    res = ghostty_mouse_encoder_new(nullptr, &h->mouse_encoder);
    if (res != GHOSTTY_SUCCESS) {
        LOGE("mouse encoder init failed: %d", res);
        ghostty_formatter_free(h->html_formatter);
        ghostty_render_state_free(h->render_state);
        ghostty_terminal_free(h->terminal);
        delete h;
        return 0;
    }

    const bool trackLastCell = true;
    ghostty_mouse_encoder_set(
        h->mouse_encoder,
        GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL,
        &trackLastCell
    );

    // OSC 52 and the other clipboard writes reach the view (nativeTakeClipboard).
    // Without these the terminal ignores them; a failure only loses copying.
    setTerminalOption(h, GHOSTTY_TERMINAL_OPT_USERDATA, h, "userdata");
    setTerminalOption(
        h,
        GHOSTTY_TERMINAL_OPT_CLIPBOARD_WRITE,
        reinterpret_cast<const void*>(&onClipboardWrite),
        "clipboard write"
    );

    LOGI("Terminal created: %dx%d", cols, rows);
    return reinterpret_cast<jlong>(h);
}

JNIEXPORT void JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeDestroyTerminal(
    JNIEnv*, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h) return;

    ghostty_mouse_encoder_free(h->mouse_encoder);
    ghostty_formatter_free(h->html_formatter);
    ghostty_render_state_free(h->render_state);
    ghostty_terminal_free(h->terminal);
    delete h;
    LOGI("Terminal destroyed");
}



JNIEXPORT void JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeResize(
    JNIEnv*, jobject, jlong handle,
    jint cols, jint rows, jfloat cellWidth, jfloat cellHeight)
{
    auto* h = getHandle(handle);
    if (!h) return;

    h->cols = (uint16_t)cols;
    h->rows = (uint16_t)rows;
    h->cell_width_px = roundPositivePixels(cellWidth);
    h->cell_height_px = roundPositivePixels(cellHeight);

    ghostty_terminal_resize(
        h->terminal,
        h->cols,
        h->rows,
        h->cell_width_px,
        h->cell_height_px
    );
    markFullSnapshot(h);
}
JNIEXPORT void JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeSetTheme(
    JNIEnv* env, jobject, jlong handle,
    jstring foreground, jstring background, jstring cursor, jobjectArray palette)
{
    auto* h = getHandle(handle);
    if (!h || !palette) return;

    GhosttyColorRgb fg = {};
    GhosttyColorRgb bg = {};
    GhosttyColorRgb cursorColor = {};
    if (!parseHexColorString(env, foreground, &fg) ||
        !parseHexColorString(env, background, &bg) ||
        !parseHexColorString(env, cursor, &cursorColor)) {
        LOGE("nativeSetTheme received an invalid theme color");
        return;
    }

    const jsize paletteLen = env->GetArrayLength(palette);
    if (paletteLen < 256) {
        LOGE("nativeSetTheme palette too small: %d", static_cast<int>(paletteLen));
        return;
    }

    std::array<GhosttyColorRgb, 256> paletteColors = {};
    for (jsize i = 0; i < 256; i += 1) {
        auto* entry = static_cast<jstring>(env->GetObjectArrayElement(palette, i));
        const bool ok = entry && parseHexColorString(env, entry, &paletteColors[static_cast<size_t>(i)]);
        if (entry) {
            env->DeleteLocalRef(entry);
        }
        if (!ok) {
            LOGE("nativeSetTheme invalid palette color at index %d", static_cast<int>(i));
            return;
        }
    }

    if (!setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg, "foreground") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &bg, "background") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursorColor, "cursor") ||
        !setTerminalOption(h, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, paletteColors.data(), "palette")) {
        return;
    }

    markFullSnapshot(h);
}

JNIEXPORT jstring JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeEncodeMouseEvent(
    JNIEnv* env, jobject, jlong handle, jint action, jint button,
    jfloat x, jfloat y, jint mods, jboolean anyButtonPressed)
{
    auto* h = getHandle(handle);
    if (!h) {
        return env->NewStringUTF("");
    }

    GhosttyMouseAction decodedAction = GHOSTTY_MOUSE_ACTION_PRESS;
    if (!decodeMouseAction(action, &decodedAction)) {
        return env->NewStringUTF("");
    }

    GhosttyMouseButton decodedButton = GHOSTTY_MOUSE_BUTTON_UNKNOWN;
    bool hasButton = false;
    if (!decodeMouseButton(button, &decodedButton, &hasButton)) {
        return env->NewStringUTF("");
    }

    const std::string encoded = encodeMouseSequence(
        h,
        decodedAction,
        decodedButton,
        hasButton,
        x,
        y,
        static_cast<GhosttyMods>(mods),
        anyButtonPressed == JNI_TRUE
    );
    return newStringFromStdString(env, encoded);
}





} // extern "C"
#endif // defined(__ANDROID__)

// ---- spike: compact cell runs for a native renderer ----
static uint32_t argbOf(const GhosttyColorRgb& c) { return 0xff000000u | (uint32_t(c.r) << 16) | (uint32_t(c.g) << 8) | uint32_t(c.b); }

static void appendUtf16(std::u16string* out, uint32_t cp) {
    if (cp < 0x10000) out->push_back(char16_t(cp));
    else { cp -= 0x10000; out->push_back(char16_t(0xD800 + (cp >> 10))); out->push_back(char16_t(0xDC00 + (cp & 0x3FF))); }
}

// meta: [rows, cols, cursorCol, cursorRow, cursorVisible, full, dirtyRowCount, (row, runCount, (col, cells, fg, bg, flags)*)*]
extern "C" JNIEXPORT jobjectArray JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeRowRuns(JNIEnv* env, jobject, jlong handle, jboolean forceFull)
{
    auto* h = getHandle(handle);
    if (!h) return nullptr;
    if (ghostty_render_state_update(h->render_state, h->terminal) != GHOSTTY_SUCCESS) return nullptr;
    uint16_t rows = h->rows, cols = h->cols;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLS, &cols);
    GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
    bool full = forceFull || dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL;
    std::vector<jint> meta;
    std::vector<std::u16string> texts;
    bool cursorIn = false; uint16_t cx = 0, cy = 0; bool cursorVisible = false;
    ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE, &cursorIn);
    if (cursorIn) {
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X, &cx);
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y, &cy);
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE, &cursorVisible);
    }
    meta.insert(meta.end(), {rows, cols, cx, cy, cursorVisible ? 1 : 0, full ? 1 : 0, 0});
    jint dirtyCount = 0;
    if (dirty != GHOSTTY_RENDER_STATE_DIRTY_FALSE || full) {
        GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
        ghostty_render_state_get(h->render_state, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors);
        GhosttyRenderStateRowIterator it = nullptr;
        ghostty_render_state_row_iterator_new(nullptr, &it);
        populateRowIterator(h->render_state, it);
        GhosttyRenderStateRowCells cells = nullptr;
        ghostty_render_state_row_cells_new(nullptr, &cells);
        jint index = 0;
        while (ghostty_render_state_row_iterator_next(it)) {
            bool rowDirty = true;
            ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY, &rowDirty);
            if (full || rowDirty) {
                dirtyCount++;
                meta.push_back(index);
                size_t runCountAt = meta.size(); meta.push_back(0);
                populateRowCells(it, cells);
                jint col = 0; jint runs = 0;
                jint runCol = -1, runCells = 0; uint32_t runFg = 0, runBg = 0; jint runFlags = 0;
                std::u16string runText;
                auto flush = [&]() {
                    if (runCol < 0) return;
                    bool blank = runBg == 0; for (char16_t c : runText) if (c != u' ') { blank = false; break; }
                    if (!blank) { meta.insert(meta.end(), {runCol, runCells, (jint)runFg, (jint)runBg, runFlags}); texts.push_back(runText); runs++; }
                    runCol = -1; runCells = 0; runText.clear();
                };
                while (ghostty_render_state_row_cells_next(cells)) {
                    GhosttyCell cell = 0;
                    if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &cell) != GHOSTTY_SUCCESS) { col++; continue; }
                    GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
                    ghostty_cell_get(cell, GHOSTTY_CELL_DATA_WIDE, &wide);
                    if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL || wide == GHOSTTY_CELL_WIDE_SPACER_HEAD) { col++; continue; }
                    GhosttyStyle style{}; ghostty_style_default(&style);
                    ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &style);
                    GhosttyColorRgb fgc = colors.foreground;
                    bool hasFg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &fgc) == GHOSTTY_SUCCESS;
                    GhosttyColorRgb bgc = colors.background;
                    bool hasBg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &bgc) == GHOSTTY_SUCCESS;
                    uint32_t fg = argbOf(resolveEffectiveForeground(style, colors, hasFg, fgc));
                    GhosttyColorRgb eb; bool nonDefaultBg = resolveEffectiveBackground(style, colors, hasBg, bgc, &eb);
                    uint32_t bg = nonDefaultBg ? argbOf(eb) : 0;
                    // Concealed text (SGR 8, e.g. the daemon's origin marker) is packed as blanks: its
                    // background stays, its glyphs and underline do not (TerminalVt.cpp does the same).
                    bool concealed = (style.flags & GHOSTTY_STYLE_FLAG_INVISIBLE) != 0;
                    jint flags = (style.flags & GHOSTTY_STYLE_FLAG_BOLD ? 1 : 0) | (style.flags & GHOSTTY_STYLE_FLAG_ITALIC ? 2 : 0) | ((style.flags & GHOSTTY_STYLE_UNDERLINE_MASK) && !concealed ? 4 : 0) | (style.flags & GHOSTTY_STYLE_FLAG_INVERSE ? 8 : 0);
                    jint width = wide == GHOSTTY_CELL_WIDE_WIDE ? 2 : 1;
                    if (runCol < 0 || fg != runFg || bg != runBg || flags != runFlags || width == 2) { flush(); runCol = col; runFg = fg; runBg = bg; runFlags = flags; }
                    bool hasText = false; ghostty_cell_get(cell, GHOSTTY_CELL_DATA_HAS_TEXT, &hasText);
                    uint32_t cp = 0; ghostty_cell_get(cell, GHOSTTY_CELL_DATA_CODEPOINT, &cp);
                    if (concealed || !hasText || cp == 0) runText.push_back(u' ');
                    else {
                        uint32_t glen = 0;
                        if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN, &glen) == GHOSTTY_SUCCESS && glen > 1) {
                            std::vector<uint32_t> g(glen, 0); GhosttyCodepointBuffer b{}; b.ptr = g.data(); b.cap = g.size();
                            if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF, &b) == GHOSTTY_SUCCESS) for (size_t i = 0; i < b.len; i++) appendUtf16(&runText, g[i]);
                            else appendUtf16(&runText, cp);
                        } else appendUtf16(&runText, cp);
                    }
                    // A wide cell covers two columns, but its spacer tail is the second cell (counted above).
                    runCells += width; col++;
                    if (width == 2) flush();
                }
                flush();
                meta[runCountAt] = runs;
            }
            index++;
        }
        ghostty_render_state_row_cells_free(cells);
        ghostty_render_state_row_iterator_free(it);
        clearRenderStateDirty(h->render_state);
    }
    meta[6] = dirtyCount;
    jclass objClass = env->FindClass("java/lang/Object");
    jobjectArray result = env->NewObjectArray(2, objClass, nullptr);
    jintArray m = env->NewIntArray((jsize)meta.size());
    env->SetIntArrayRegion(m, 0, (jsize)meta.size(), meta.data());
    jclass strClass = env->FindClass("java/lang/String");
    jobjectArray t = env->NewObjectArray((jsize)texts.size(), strClass, nullptr);
    for (size_t i = 0; i < texts.size(); i++) {
        jstring s = env->NewString(reinterpret_cast<const jchar*>(texts[i].data()), (jsize)texts[i].size());
        env->SetObjectArrayElement(t, (jsize)i, s); env->DeleteLocalRef(s);
    }
    env->SetObjectArrayElement(result, 0, m);
    env->SetObjectArrayElement(result, 1, t);
    return result;
}

extern "C" JNIEXPORT void JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeWriteBytes(JNIEnv* env, jobject, jlong handle, jbyteArray data)
{
    auto* h = getHandle(handle);
    if (!h) return;
    jsize len = env->GetArrayLength(data);
    jbyte* bytes = env->GetByteArrayElements(data, nullptr);
    ghostty_terminal_vt_write(h->terminal, reinterpret_cast<const uint8_t*>(bytes), (size_t)len);
    env->ReleaseByteArrayElements(data, bytes, JNI_ABORT);
}

// The text a program copied during the last writes, once; null when none.
extern "C" JNIEXPORT jstring JNICALL
Java_com_daoleno_mewla_terminal_Vt_nativeTakeClipboard(JNIEnv* env, jobject, jlong handle)
{
    auto* h = getHandle(handle);
    if (!h || !h->has_clipboard) return nullptr;
    h->has_clipboard = false;
    std::string text;
    text.swap(h->clipboard);
    return newStringFromStdString(env, text);
}
