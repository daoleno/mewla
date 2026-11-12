// The app's terminal cell packer (client/native/terminal/TerminalVt.cpp, Android's and iOS's) for
// selecting, copying and pasting (from the lead's parity #3 handover, on the shared layout):
// - a row that soft-wraps carries its wrapped flag (the views join it with the next row in a copy);
//   a row ended by a newline does not;
// - a paste is framed with ESC[200~ ... ESC[201~ only after the program turned on mode 2004, and
//   otherwise sends newlines as carriage returns; ESC inside the text cannot end the frame.
// Built and run by scripts/test-ghosttyvt.sh.
#include "TerminalVt.h"
#include <cstdio>
#include <cstring>
#include <string>

static std::string paste(MewlaVt *vt, const std::string &text) {
  const uint8_t *out = nullptr;
  size_t length = 0;
  if (!mewla_vt_paste(vt, reinterpret_cast<const uint8_t *>(text.data()), text.size(), &out, &length)) return "<failed>";
  return std::string(reinterpret_cast<const char *>(out), length);
}

static void write(MewlaVt *vt, const char *s) { mewla_vt_write(vt, reinterpret_cast<const uint8_t *>(s), std::strlen(s)); }

int main() {
  MewlaVt *vt = mewla_vt_create(10, 4);
  if (!vt) { std::puts("terminal_vt_select: create failed"); return 2; }
  int failures = 0;
  auto check = [&](bool ok, const char *what) { if (!ok) { failures++; std::printf("terminal_vt_select: FAIL %s\n", what); } };

  // Row 0 is twelve characters in ten columns (wraps into row 1); row 2 ends with a newline.
  write(vt, "0123456789AB\r\nshort\r\n");
  MewlaVtFrame frame{};
  check(mewla_vt_frame(vt, true, &frame), "frame");
  bool wraps[4] = {false, false, false, false};
  size_t at = MEWLA_VT_HEADER;
  for (int32_t r = 0; r < frame.meta[6]; r++) {
    const int32_t row = frame.meta[at], runs = frame.meta[at + 1], wrapped = frame.meta[at + 2];
    if (row >= 0 && row < 4) wraps[row] = wrapped == 1;
    at += MEWLA_VT_ROW + size_t(runs) * MEWLA_VT_RUN;
  }
  check(wraps[0], "row 0 soft-wraps");
  check(!wraps[1] && !wraps[2], "rows ended by a newline do not wrap");

  check(paste(vt, "a\nb") == "a\rb", "unbracketed: newline sent as carriage return");
  write(vt, "\x1b[?2004h");
  check(paste(vt, "a\nb") == "\x1b[200~a\nb\x1b[201~", "bracketed after mode 2004");
  const std::string escaped = paste(vt, "x\x1b[201~y");
  check(escaped.find("\x1b[201~y") == std::string::npos && escaped.rfind("\x1b[201~") == escaped.size() - 6,
        "ESC inside the paste cannot end the frame");
  write(vt, "\x1b[?2004l");
  check(paste(vt, "a\nb") == "a\rb", "unbracketed again after mode 2004 is off");

  mewla_vt_destroy(vt);
  std::printf("terminal_vt_select: %s\n", failures == 0 ? "ok" : "FAIL");
  return failures == 0 ? 0 : 1;
}
