// Run against the packaged Android libghostty-vt. This includes the exact shared
// Android/iOS formatter while excluding the JNI marshaling entry points.
#include <array>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>
#include <cstdio>
#include <cassert>
#include <chrono>
#undef __ANDROID__
#define LOGI(...) ((void)0)
#define LOGE(...) ((void)0)
#include "../android/src/main/cpp/jni_bridge.cpp"

int main() {
    TerminalHandle h;
    h.cols = 85;
    h.rows = 68;
    GhosttyTerminalOptions options = {};
    options.cols = h.cols;
    options.rows = h.rows;
    options.max_scrollback = 1000;
    assert(ghostty_terminal_new(nullptr, &h.terminal, options) == GHOSTTY_SUCCESS);
    assert(ghostty_render_state_new(nullptr, &h.render_state) == GHOSTTY_SUCCESS);
    std::vector<std::string> displayed;
    size_t assertions = 0;
    const auto write = [&](const std::string& data) {
        ghostty_terminal_vt_write(h.terminal, reinterpret_cast<const uint8_t*>(data.data()), data.size());
    };
    const auto consume = [&]() {
        assert(ghostty_render_state_update(h.render_state, h.terminal) == GHOSTTY_SUCCESS);
        GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
        ghostty_render_state_get(h.render_state, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
        RenderRowUpdates updates;
        assert(buildRenderRowUpdates(&h, h.rows, h.cols, dirty, &updates));
        if (updates.full) displayed.assign(h.rows, "");
        for (size_t i = 0; i < updates.indices.size(); i++) displayed.at(updates.indices[i]) = updates.html[i];
        std::string reconstructed;
        for (const auto& row : displayed) reconstructed += row;
        std::string full;
        assert(buildVisibleHtml(h.render_state, h.rows, &full));
        assert(reconstructed == full);
        assertions++;
        h.force_full_snapshot = false;
        clearRenderStateDirty(h.render_state);
        return updates;
    };
    assert(consume().full);
    write("a");
    auto typed = consume();
    assert(!typed.full && typed.indices.size() == 1 && typed.indices[0] == 0);
    write("\x1b[3;4H");
    assert(consume().indices.empty());
    for (int i = 0; i < 150; i++) {
        write("\x1b[38;5;" + std::to_string(i % 256) + "m\x1b[1m终端 漢字 é 👋 <&>\x1b[0m " +
            std::to_string(i) + std::string(i % 90, 'x') + "\r\n");
        consume();
    }
    for (const char* data : {"\x1b[?1049h", "\x1b[2J\x1b[Hvim", "\x1b[2;2H\x1b[K",
             "\x1b[?1049l", "\x1b[H\x1b[2J", "\x1b[7m逆色\x1b[0m", "\x1b[2J\x1b[H"}) {
        write(data);
        consume();
    }
    // Cells a fallback face may draw are pinned to their grid width; box
    // drawing, braille and Latin stay plain text from the bundled face.
    write("\x1b[2J\x1b[H中a⏺─⠿\xee\x82\xb0\xef\x80\x8c");
    {
        auto pinned = consume();
        assert(pinned.indices.size() == 1 && pinned.html[0].find(
            "<w>中</w>a<n>⏺</n>─⠿\xee\x82\xb0<n>\xef\x80\x8c</n></div>") != std::string::npos);
    }
    write("\x1b[2J\x1b[H");
    consume();
    // An application may clear/redraw everything while only a few rows differ.
    write("same"); consume();
    write("\x1b[2J\x1b[Hsame");
    assert(consume().indices.empty());
    for (int rows : {24, 42, 68}) {
        h.rows = rows;
        ghostty_terminal_resize(h.terminal, h.cols, h.rows, 8, 16);
        markFullSnapshot(&h);
        assert(consume().full);
    }
    GhosttyColorRgb fg = {120, 200, 80};
    assert(setTerminalOption(&h, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg, "foreground"));
    markFullSnapshot(&h);
    assert(consume().full);
    // Repeat sparse typing to cover row dirtiness across many consumes.
    for (int i = 0; i < 500; i++) {
        write("\x1b[2;1H" + std::to_string(i));
        auto update = consume();
        assert(!update.full && update.indices.size() == 1);
    }
    std::printf("%zu reconstructed frames match the full native formatter; sparse, cursor, ANSI/CJK, wrapping, alternate screen, resize and theme passed\n", assertions);
    ghostty_render_state_free(h.render_state);
    ghostty_terminal_free(h.terminal);
}
