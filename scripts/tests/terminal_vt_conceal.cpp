// The app's terminal cell packer (client/native/terminal/TerminalVt.cpp, Android's and iOS's)
// against the desktop libghostty-vt: concealed
// text (SGR 8, as the daemon prints its "mewla-origin" marker) is not in the frame, and a
// concealed run on a coloured background keeps its background. Built and run by
// scripts/test-ghosttyvt.sh.
#include "TerminalVt.h"
#include <cstdio>
#include <cstring>
#include <string>

int main() {
  MewlaVt *vt = mewla_vt_create(40, 4);
  if (!vt) { std::puts("terminal_vt_conceal: create failed"); return 2; }
  const char *out = "A\x1b[8mHIDDEN\x1b[0mB\r\n\x1b[8;44mXY\x1b[0m";
  mewla_vt_write(vt, reinterpret_cast<const uint8_t *>(out), std::strlen(out));
  MewlaVtFrame frame{};
  if (!mewla_vt_frame(vt, true, &frame)) { std::puts("terminal_vt_conceal: frame failed"); return 2; }
  std::string text;
  for (size_t i = 0; i < frame.text_count; i++) text.push_back(frame.text[i] < 128 ? char(frame.text[i]) : '?');
  bool background = false;
  size_t at = MEWLA_VT_HEADER;
  for (int32_t r = 0; r < frame.meta[6]; r++) {
    const int32_t row = frame.meta[at], runs = frame.meta[at + 1];
    at += MEWLA_VT_ROW;
    for (int32_t k = 0; k < runs; k++, at += MEWLA_VT_RUN) {
      if (row == 1 && frame.meta[at + 3] != 0) background = true;
    }
  }
  mewla_vt_destroy(vt);
  const bool ok = text.find("HIDDEN") == std::string::npos && text.find("XY") == std::string::npos &&
      text.find('A') != std::string::npos && text.find('B') != std::string::npos && background;
  std::printf("terminal_vt_conceal: %s (text [%s], background kept %d)\n", ok ? "ok" : "FAIL", text.c_str(), background);
  return ok ? 0 : 1;
}
