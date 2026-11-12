// The packer's input encoders (TerminalVt.cpp mewla_vt_key/mouse/focus): libghostty-vt's own key, mouse
// and focus encoding, following the modes the program sets. Built and run by scripts/test-ghosttyvt.sh.
#include "TerminalVt.h"

#include <cstdio>
#include <cstring>
#include <string>

#include <ghostty/vt.h>

static int failures = 0;

static void expect(const char* what, bool ok, const std::string& got, const std::string& want)
{
    if (got == want && ok == !want.empty()) return;
    failures++;
    std::printf("FAIL %s: got [", what);
    for (unsigned char c : got) std::printf(c < 0x20 || c > 0x7e ? "\\x%02x" : "%c", c);
    std::printf("] want [");
    for (unsigned char c : want) std::printf(c < 0x20 || c > 0x7e ? "\\x%02x" : "%c", c);
    std::printf("]\n");
}

static void write(MewlaVt* vt, const char* s) { mewla_vt_write(vt, reinterpret_cast<const uint8_t*>(s), std::strlen(s)); }

static void key(MewlaVt* vt, const char* what, GhosttyKey k, uint16_t mods, const char* text, uint32_t unshifted, const std::string& want)
{
    const uint8_t* out = nullptr;
    size_t n = 0;
    bool ok = mewla_vt_key(vt, k, mods, GHOSTTY_KEY_ACTION_PRESS, reinterpret_cast<const uint8_t*>(text), std::strlen(text), unshifted, &out, &n);
    expect(what, ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", want);
}

int main()
{
    MewlaVt* vt = mewla_vt_create(80, 24);
    mewla_vt_resize(vt, 80, 24, 10, 20);

    key(vt, "a types a", GHOSTTY_KEY_A, 0, "a", 'a', "a");
    key(vt, "Shift+a types A", GHOSTTY_KEY_A, GHOSTTY_MODS_SHIFT, "A", 'a', "A");
    key(vt, "Enter", GHOSTTY_KEY_ENTER, 0, "", 0, "\r");
    key(vt, "Backspace", GHOSTTY_KEY_BACKSPACE, 0, "", 0, "\x7f");
    key(vt, "Ctrl+C", GHOSTTY_KEY_C, GHOSTTY_MODS_CTRL, "", 'c', "\x03");
    key(vt, "Alt+b", GHOSTTY_KEY_B, GHOSTTY_MODS_ALT, "b", 'b', "\x1b" "b");
    key(vt, "Up, normal cursor keys", GHOSTTY_KEY_ARROW_UP, 0, "", 0, "\x1b[A");
    key(vt, "Shift+Tab", GHOSTTY_KEY_TAB, GHOSTTY_MODS_SHIFT, "", 0, "\x1b[Z");
    key(vt, "F5", GHOSTTY_KEY_F5, 0, "", 0, "\x1b[15~");
    key(vt, "Ctrl+Right", GHOSTTY_KEY_ARROW_RIGHT, GHOSTTY_MODS_CTRL, "", 0, "\x1b[1;5C");
    key(vt, "typed CJK passes through", GHOSTTY_KEY_UNIDENTIFIED, 0, "你", 0, "你");
    write(vt, "\x1b[?1h");
    key(vt, "Up, application cursor keys (vim, less)", GHOSTTY_KEY_ARROW_UP, 0, "", 0, "\x1bOA");
    write(vt, "\x1b[?1l\x1b[>1u");
    key(vt, "Kitty keyboard: Ctrl+C disambiguated", GHOSTTY_KEY_C, GHOSTTY_MODS_CTRL, "", 'c', "\x1b[99;5u");
    write(vt, "\x1b[<u");

    const uint8_t* out = nullptr;
    size_t n = 0;
    bool ok = mewla_vt_mouse(vt, GHOSTTY_MOUSE_ACTION_PRESS, GHOSTTY_MOUSE_BUTTON_LEFT, 0, 25, 65, &out, &n);
    expect("no mouse report without tracking", ok, "", "");
    expect("tracking off", mewla_vt_mouse_tracking(vt), "", "");
    write(vt, "\x1b[?1000h\x1b[?1006h");
    expect("tracking on", mewla_vt_mouse_tracking(vt), "x", "x");
    ok = mewla_vt_mouse(vt, GHOSTTY_MOUSE_ACTION_PRESS, GHOSTTY_MOUSE_BUTTON_LEFT, 0, 25, 65, &out, &n);
    expect("SGR press at cell 3,4", ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", "\x1b[<0;3;4M");
    ok = mewla_vt_mouse(vt, GHOSTTY_MOUSE_ACTION_RELEASE, GHOSTTY_MOUSE_BUTTON_LEFT, 0, 25, 65, &out, &n);
    expect("SGR release", ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", "\x1b[<0;3;4m");
    ok = mewla_vt_mouse(vt, GHOSTTY_MOUSE_ACTION_PRESS, GHOSTTY_MOUSE_BUTTON_FOUR, 0, 5, 5, &out, &n);
    expect("wheel up", ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", "\x1b[<64;1;1M");

    ok = mewla_vt_focus(vt, true, &out, &n);
    expect("no focus report unless asked", ok, "", "");
    write(vt, "\x1b[?1004h");
    ok = mewla_vt_focus(vt, true, &out, &n);
    expect("focus in", ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", "\x1b[I");
    ok = mewla_vt_focus(vt, false, &out, &n);
    expect("focus out", ok, ok ? std::string(reinterpret_cast<const char*>(out), n) : "", "\x1b[O");

    mewla_vt_destroy(vt);
    if (failures) {
        std::printf("terminal_vt_input: %d failures\n", failures);
        return 1;
    }
    std::printf("terminal_vt_input: keys, mouse and focus encode for the program's modes\n");
    return 0;
}
