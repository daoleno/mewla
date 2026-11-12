#!/usr/bin/env python3
"""Synthetic terminal screens for the render corpus (scripts/terminal-corpus/README.md).

Each screen is raw bytes a program would write, replayable into any terminal
of the screen's size (cols x rows in corpus.json). Recorded sessions of real
programs sit beside them (record.sh). Run: python3 -I gen.py <out dir>.
"""
import json
import os
import sys

COLS, ROWS = 52, 30
E = "\x1b"
CSI = E + "["
RESET = CSI + "0m"


def sgr(*codes):
    return CSI + ";".join(str(c) for c in codes) + "m"


def at(row, col):
    return f"{CSI}{row};{col}H"


def screen(lines):
    """A fresh screen with `lines` from the top, cursor left after the last."""
    return CSI + "H" + CSI + "2J" + "\r\n".join(lines)


def sgr_screen():
    out = []
    # 16 colors as foreground and background.
    out.append("16 " + "".join(sgr(30 + i) + f"{i:X}" for i in range(8)) + "".join(sgr(90 + i) + f"{i + 8:X}" for i in range(8)) + RESET)
    out.append("bg " + "".join(sgr(40 + i) + "  " for i in range(8)) + "".join(sgr(100 + i) + "  " for i in range(8)) + RESET)
    # 256: the cube's diagonal and the gray ramp.
    out.append("256 " + "".join(sgr(48, 5, 16 + i * 6) + " " for i in range(36)) + RESET)
    out.append("gray " + "".join(sgr(48, 5, 232 + i) + " " for i in range(24)) + RESET)
    # Truecolor gradient, background and foreground.
    out.append("rgb " + "".join(sgr(48, 2, int(255 * i / 47), 80, 255 - int(255 * i / 47)) + " " for i in range(48)) + RESET)
    out.append("rgbfg " + "".join(sgr(38, 2, 255, int(255 * i / 45), 60) + "█" for i in range(46)) + RESET)
    out.append(sgr(1) + "bold" + RESET + " " + sgr(2) + "dim" + RESET + " " + sgr(3) + "italic" + RESET + " " + sgr(1, 3) + "bold-italic" + RESET + " " + sgr(1, 2) + "bold+dim" + RESET)
    out.append(sgr(31) + "red " + sgr(1) + "boldred" + RESET + " " + sgr(2, 32) + "dimgreen" + RESET + " " + sgr(1, 34) + "boldblue" + RESET)
    out.append(sgr(4) + "single" + RESET + " " + CSI + "4:2m" + "double" + RESET + " " + CSI + "4:3m" + "curly" + RESET + " " + CSI + "4:4m" + "dotted" + RESET + " " + CSI + "4:5m" + "dashed" + RESET)
    out.append(CSI + "4:3m" + CSI + "58:2::255:60:60m" + "spell error" + RESET + " " + sgr(4) + CSI + "58;5;45m" + "cyan underline" + RESET)
    out.append(sgr(9) + "strikethrough" + RESET + " " + sgr(53) + "overline" + RESET + " " + sgr(4, 9) + "under+strike" + RESET)
    out.append(sgr(7) + "inverse" + RESET + " " + sgr(7, 31) + "inv red" + RESET + " " + sgr(7) + "   " + RESET + "<-inverse blanks")
    out.append("[" + sgr(8) + "concealed" + RESET + "] [" + sgr(8, 41) + "hidden on red" + RESET + "]")
    out.append(sgr(5) + "blink" + RESET + " " + sgr(6) + "rapid" + RESET + " " + sgr(1, 4, 33, 44) + "bold under yellow/blue" + RESET)
    out.append(sgr(3) + "italic overhang: fffjjj/// " + RESET + "|next")
    out.append(sgr(38, 5, 208) + "256 orange" + RESET + " " + sgr(38, 2, 130, 170, 255) + "rgb blue" + RESET + " " + sgr(48, 5, 236) + " dark bg row padded to the edge" + CSI + "K" + RESET)
    out.append("Ligatures? -> => != === <= >= :: www |> ->> ")
    out.append("Underscores: snake_case_name __init__ ____")
    out.append("Descenders: gjpqy Accents: ÀÉÎÕÜ àéîõü ß ñ")
    out.append("Digits 0O 1lI| {}[]() <> ;:,. ~`'\" @#$%^&*")
    return screen(out)


def box_screen():
    out = []
    out.append("┌──────┬──────┐ ┏━━━━━━┳━━━━━━┓ ╔══════╦═════╗")
    out.append("│ light│ cell │ ┃ heavy┃ cell ┃ ║double║ cell║")
    out.append("├──────┼──────┤ ┣━━━━━━╋━━━━━━┫ ╠══════╬═════╣")
    out.append("└──────┴──────┘ ┗━━━━━━┻━━━━━━┛ ╚══════╩═════╝")
    out.append("╭─────────────╮ ┌╌╌╌╌╌┐ ┍━━┑ ╓──╖ ╒══╕ ╱╲╳")
    out.append("│ rounded box │ ╎ dash╎ │mx│ ║  ║ │  │ ╲╱ ")
    out.append("╰─────────────╯ └╌╌╌╌╌┘ ┕━━┙ ╙──╜ ╘══╛ ")
    out.append("blocks ▀▄█▌▐░▒▓ ▁▂▃▄▅▆▇█ ▏▎▍▌▋▊▉█")
    out.append("quads ▖▗▘▙▚▛▜▝▞▟ shades ░░▒▒▓▓██")
    out.append("bars " + "".join(sgr(32) + c for c in "▁▂▃▅▇█▇▅▃▂▁") + RESET + " " + sgr(42) + "█▀▀▀█" + RESET + " " + sgr(31) + "▐█▌" + RESET)
    out.append("braille ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ ⣿⣷⣯⣟⡿⢿⣻⣽ ⠁⠂⠄⡀⢀⠠⠐⠈")
    out.append("solid wall (rows must touch):")
    for _ in range(3):
        out.append(sgr(36) + "████████" + RESET + " " + sgr(33) + "││││││││" + RESET + " " + sgr(35) + "┃┃┃┃┃┃┃┃" + RESET + " " + sgr(34) + "▓▓▓▓▓▓▓▓" + RESET)
    out.append("powerline " + sgr(30, 42) + " main " + sgr(32, 44) + "\ue0b0" + sgr(30, 44) + " ~/src " + sgr(34, 49) + "\ue0b0" + RESET + " \ue0b1 \ue0b2\ue0b3 \ue0b4\ue0b5\ue0b6\ue0b7 \ue0a0\ue0a1\ue0a2")
    out.append("nerd  \uf00c \uf002 \uf01d \ue009 \uf07b \uf015 \ue5ff \uf113 \ue725 \uf489 \U000f0219")
    out.append("arrows ← ↑ → ↓ ↔ ⇐ ⇒ ↳ ↵ ⏎ ➜ ❯ › ‹ » «")
    out.append("marks ● ○ ◉ ◆ ■ □ ▸ ▶ ✓ ✗ ✔ ✘ ⚠ ★ • … —")
    out.append("claude ⏺ ⎿ ✻ ✳ ✢ ∗ ⏸ ⏵⏵ · ✽ ✶")
    out.append("math ≡ ≠ ≤ ≥ ∞ ∑ √ ≪ ⊚ ℝ ∀ ∃ ∈ λ π Ω")
    out.append(sgr(2) + "──────────────────────────────────────────────────" + RESET)
    out.append(sgr(48, 5, 236) + "▌" + RESET + " tree: ├── src/  │   └── main.go  └── README")
    return screen(out)


def cjk_screen():
    out = []
    out.append("中文混排：Claude 帮你写代码，Codex 也行。")
    out.append("全角：ＡＢＣ１２３！？（）【】「」『』、。")
    out.append("かな カナ ﾊﾝｶｸ 한국어 텍스트 日本語")
    out.append("|中|文|对|齐|测|试|abcd|ab|")
    out.append("|1234567890123456789012345678|")
    out.append("emoji: 👋 😿 🎉 ✅ ❤️ ☺️ ⚠️ 1️⃣ 🇨🇳 🇺🇸")
    out.append("zwj: 👨‍👩‍👧‍👦 🏳️‍🌈 🧑‍💻 skin: 👍🏽 |end")
    out.append("combining: e\u0301 a\u0308 n\u0303 Tiếng Việt ǹ\u0323 Z\u0336|")
    out.append("thai: ภาษาไทย arabic: مرحبا hebrew: שלום")
    out.append(sgr(1) + "粗体中文" + RESET + " " + sgr(4) + "下划线中文" + RESET + " " + sgr(7) + "反显中文" + RESET + " " + sgr(42, 30) + "背景中文" + RESET)
    out.append(sgr(9) + "删除线中文" + RESET + " " + sgr(3) + "斜体中文" + RESET + " " + sgr(2) + "暗淡中文" + RESET)
    out.append("┌──────┬──────────┐")
    out.append("│ 名称 │ 状态     │")
    out.append("├──────┼──────────┤")
    out.append("│ 构建 │ ✓ 通过   │")
    out.append("│ 测试 │ ✗ 失败 3 │")
    out.append("└──────┴──────────┘")
    # A wide character that does not fit at the last column wraps whole.
    out.append("x" * 51 + "宽")
    out.append("末尾折行：" + "汉" * 30)
    out.append("❯ 输入中文命令 ls 目录/ ")
    return screen(out)


def prompt_screen():
    b, d = sgr(1), sgr(2)
    pl = (sgr(38, 2, 30, 30, 46) + sgr(48, 2, 137, 180, 250) + "  daoleno " + sgr(38, 2, 137, 180, 250) + sgr(48, 2, 49, 50, 68) + "\ue0b0"
          + sgr(38, 2, 205, 214, 244) + " ~/src/mewla " + sgr(38, 2, 49, 50, 68) + sgr(48, 2, 166, 227, 161) + "\ue0b0"
          + sgr(38, 2, 30, 30, 46) + " \ue0a0 main ✚2 " + RESET + sgr(38, 2, 166, 227, 161) + "\ue0b0" + RESET)
    out = [pl, sgr(32) + "❯ " + RESET + "git log --oneline --graph -6"]
    graph = [
        ("* ", "3cde66ce", " (HEAD -> main, origin/main)", " Client web: base href"),
        ("*   ", "fb1f6b82", "", " Merge remote-tracking branch"),
        ("|\\  ", "", "", ""),
        ("| * ", "01839b7d", " (tag: v0.3.2)", " libghostty-vt 1.0.1"),
        ("* | ", "6350ec0e", "", " Client web: pairing"),
        ("|/  ", "", "", ""),
        ("* ", "c3f0ca4b", "", " Session host engine"),
    ]
    for g, h, ref, msg in graph:
        line = sgr(31) + g.replace("|", sgr(31) + "|").replace("\\", sgr(32) + "\\").replace("/", sgr(32) + "/") + RESET
        if h:
            line += sgr(33) + h + RESET
        if ref:
            line += sgr(1, 36) + ref.replace("HEAD -> ", "HEAD -> " + sgr(32)) + RESET
        line += msg
        out.append(line)
    out.append(sgr(32) + "❯ " + RESET + "git diff --stat")
    out.append(" client/ui/terminal/TerminalPane.tsx | 12 " + sgr(32) + "++++++" + sgr(31) + "------" + RESET)
    out.append(" 1 file changed, 6 insertions(+), 6 deletions(-)")
    out.append(sgr(32) + "❯ " + RESET + "git diff")
    out.append(b + "diff --git a/main.go b/main.go" + RESET)
    out.append(b + "index 3f2a1c0..9b8e7d6 100644" + RESET)
    out.append(b + "--- a/main.go" + RESET)
    out.append(b + "+++ b/main.go" + RESET)
    out.append(sgr(36) + "@@ -10,7 +10,7 @@" + RESET + " func main() {")
    out.append("     fmt.Println(\"start\")")
    out.append(sgr(31) + "-    if err := run(); err != nil {" + RESET)
    out.append(sgr(32) + "+    if err := run(ctx); err != nil {" + RESET)
    out.append(sgr(31) + "-" + RESET + sgr(41) + "   " + RESET)
    out.append(sgr(32) + "+" + sgr(42) + "\t" + RESET + sgr(32) + "log.Fatal(err)" + RESET)
    out.append("     }")
    out.append(pl)
    out.append(sgr(32) + "❯ " + RESET + "echo done " + d + "# a dim comment" + RESET)
    return screen(out)


def htop_like():
    """An htop-style screen: meters with bracketed bars, an inverse header, a selected row."""
    out = []

    def meter(label, used, cache, total=40):
        u, c = int(total * used), int(total * cache)
        return (sgr(36) + f"{label:>3}" + RESET + sgr(1) + "[" + RESET + sgr(32) + "|" * u + sgr(34) + "|" * c + sgr(33) + "" + RESET
                + " " * (total - u - c) + sgr(2) + f"{int(used * 100):>3}%" + RESET + sgr(1) + "]" + RESET)

    for i, (a, b) in enumerate([(0.42, 0.1), (0.08, 0.02), (0.91, 0.05), (0.30, 0.2)]):
        out.append(meter(str(i), a, b))
    out.append(sgr(36) + "Mem" + RESET + sgr(1) + "[" + RESET + sgr(32) + "|" * 18 + sgr(34) + "|" * 6 + sgr(33) + "|" * 9 + RESET + " " * 2 + sgr(2) + "12.1G/30G" + RESET + sgr(1) + "]" + RESET)
    out.append(sgr(36) + "Swp" + RESET + sgr(1) + "[" + RESET + " " * 37 + sgr(2) + "0K/8G" + RESET + sgr(1) + "]" + RESET)
    out.append("")
    out.append(sgr(30, 42) + "  PID USER      PRI  NI  VIRT   RES S CPU% Command" + CSI + "K" + RESET)
    procs = [(1234, "daoleno", "20", "0", "2.1G", "312M", "S", "12.3", "claude"),
             (2345, "daoleno", "20", "0", "980M", "120M", "R", "8.0", "codex"),
             (3456, "root", "20", "0", "45M", "9M", "S", "0.7", "systemd"),
             (4567, "daoleno", "20", "0", "1.4G", "640M", "S", "3.1", "gradle")]
    for i, p in enumerate(procs):
        row = f"{p[0]:>5} {p[1]:<9} {p[2]:>3} {p[3]:>3} {p[4]:>5} {p[5]:>5} {p[6]} {p[7]:>4} {p[8]}"
        out.append((sgr(30, 46) + row + CSI + "K" + RESET) if i == 1 else row)
    footer = "".join(sgr(0) + f"F{n}" + sgr(30, 46) + f"{w:<6}" for n, w in [(1, "Help"), (2, "Setup"), (3, "Search"), (5, "Tree"), (9, "Kill"), (10, "Quit")])
    out.append(at(ROWS, 1) + footer + CSI + "K" + RESET)
    return screen(out)


def cursor_screen(code, label):
    """One cursor style per screen (DECSCUSR `code`), the cursor left mid-word."""
    return screen([f"cursor: {label}", "", "$ echo hello world"]) + at(3, 8) + f"{CSI}{code} q"


def alt_vim():
    """An editor on the alternate screen: tildes, an inverse status line, a scroll region."""
    lines = [sgr(33) + f"{n:>3} " + RESET + t for n, t in enumerate([
        "package main", "", "import \"fmt\"", "", "func main() {",
        "\t" + "fmt.Println(\"hello, 世界\")", "}"], 1)]
    body = lines + [sgr(34) + "~" + RESET] * (ROWS - 2 - len(lines))
    status = at(ROWS - 1, 1) + sgr(7) + " main.go [+]" + " " * 26 + "6,13  All " + RESET
    return CSI + "?1049h" + CSI + "H" + CSI + "2J" + "\r\n".join(body) + status + at(ROWS, 1) + "-- INSERT --" + at(6, 18) + CSI + "6 q"


def scroll_region():
    """DECSTBM: a fixed header and footer, the middle scrolled by 5."""
    s = CSI + "H" + CSI + "2J" + sgr(7) + " header (fixed)" + CSI + "K" + RESET + at(ROWS, 1) + sgr(7) + " footer (fixed)" + CSI + "K" + RESET
    s += f"{CSI}2;{ROWS - 1}r" + at(2, 1)
    s += "\r\n".join(f"line {i:02d} in the region" for i in range(1, ROWS + 4))
    s += CSI + "r" + at(ROWS - 1, 1)
    return s


def build_log():
    """Long build output: hundreds of lines, colors, a progress bar redrawn with \\r."""
    out = []
    for i in range(1, 401):
        tag = [sgr(32) + "   Compiling" + RESET, sgr(36) + "    Checking" + RESET, sgr(33) + "warning" + RESET + ":"][i % 3]
        out.append(f"{tag} crate-{i:03d} v0.{i % 10}.{i % 7} (/src/crates/crate-{i:03d})")
    s = "\r\n".join(out) + "\r\n"
    for p in range(0, 101, 10):
        s += "\r" + sgr(1) + "    Building" + RESET + " [" + "=" * (p // 4) + ">" + " " * (25 - p // 4) + f"] {p}/100"
    s += "\r\n" + sgr(1, 32) + "    Finished" + RESET + " dev [unoptimized] in 41.2s\r\n"
    return s


def reflow():
    """Long logical lines (soft wrapped) for resize and reflow."""
    return screen(["reflow: " + " ".join(f"w{i:02d}" for i in range(40)), "next " + "中文" * 20, "$ "])


SCREENS = {
    "sgr": (sgr_screen, "Colors (16/256/rgb) and every attribute"),
    "box": (box_screen, "Box drawing, blocks, braille, Powerline, Nerd and TUI symbols"),
    "cjk": (cjk_screen, "Wide CJK, emoji (VS16, ZWJ, flags), combining marks, wrap at the edge"),
    "prompt": (prompt_screen, "A Powerline prompt, git log --graph, git diff"),
    "meters": (htop_like, "htop-style meters, inverse header, selected row, footer"),
    "vim": (alt_vim, "Editor on the alternate screen, inverse status line, bar cursor"),
    "region": (scroll_region, "Scroll region with fixed header and footer"),
    "build": (build_log, "400 lines of build output and a \\r progress bar (history)"),
    "reflow": (reflow, "Soft-wrapped long lines (resize and reflow)"),
    "cursor-block": (lambda: cursor_screen(2, "steady block"), "Steady block cursor"),
    "cursor-bar": (lambda: cursor_screen(6, "steady bar"), "Steady bar cursor"),
    "cursor-underline": (lambda: cursor_screen(4, "steady underline"), "Steady underline cursor"),
    "cursor-blink": (lambda: cursor_screen(1, "blinking block"), "Blinking block cursor"),
}


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))
    manifest_path = os.path.join(out, "corpus.json")
    manifest = json.load(open(manifest_path)) if os.path.exists(manifest_path) else {}
    for name, (fn, what) in SCREENS.items():
        with open(os.path.join(out, name + ".vt"), "wb") as f:
            f.write(fn().encode("utf-8"))
        manifest[name] = {"cols": COLS, "rows": ROWS, "what": what, "source": "gen.py"}
    with open(manifest_path, "w") as f:
        json.dump(dict(sorted(manifest.items())), f, indent=2, ensure_ascii=False)
        f.write("\n")


if __name__ == "__main__":
    main()
