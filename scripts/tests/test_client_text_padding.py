"""No horizontal padding on a client Text (UPSTREAM-NSH §8, round-5 S2).

NSH 0.5.0 on Android measures a Text's wrapping without its own horizontal padding and then lays it out
in the narrower content box: a paragraph whose last break falls within the padding's width is measured one
line short, and that line is clipped with no "…" ("…the generated" / "reference." gone at 390). Padding
belongs on a View around the Text. This scans every `<Text …>` opening tag in client/ui (tags may span
lines) for a p-, px-, pl-, pr-, ps- or pe- class.
"""

from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
UI = ROOT / "client/ui"
# client/ui/terminal belongs to the terminal lane; its one padded Text (a single-line error) waits for it.
KNOWN = {"client/ui/terminal/TerminalPane.tsx: px-3 py-1 text-xs text-destructive"}
PADDING = re.compile(r"(?<![\w-])-?(?:p|px|pl|pr|ps|pe)-(?:\[[^\]]+\]|[\d.]+|px)\b")


def text_tags(source: str):
    """Yield (line, class string) for each <Text ...> opening tag's class attribute."""
    for match in re.finditer(r"<Text\b", source):
        depth, i = 0, match.end()
        while i < len(source):
            ch = source[i]
            if ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
            elif ch == ">" and depth == 0:
                break
            i += 1
        tag = source[match.start():i]
        for cls in re.finditer(r"\bclass(?:Name)?=(?:\"([^\"]*)\"|\{`([^`]*)`\})", tag):
            yield source.count("\n", 0, match.start()) + 1, cls.group(1) or cls.group(2) or ""


def offenders():
    found = []
    for path in sorted(UI.rglob("*.tsx")):
        if ".test." in path.name:
            continue
        for line, classes in text_tags(path.read_text()):
            if PADDING.search(classes) and f"{path.relative_to(ROOT)}: {classes}" not in KNOWN:
                found.append(f"{path.relative_to(ROOT)}:{line}: {classes}")
    return found


class TextHasNoHorizontalPadding(unittest.TestCase):
    def test_no_text_carries_horizontal_padding(self):
        self.assertEqual(offenders(), [], "move the padding to a View around the Text (UPSTREAM-NSH §8)")

    def test_the_scan_sees_what_it_must(self):
        caught = list(text_tags('<Text testID="x" class="px-1 pt-3 text-sub">a</Text>\n<Text\n  class={`p-4 ${a ? "b" : "c"}`}>b</Text>'))
        self.assertEqual([bool(PADDING.search(c)) for _, c in caught], [True, True])
        self.assertFalse(PADDING.search("pt-3 pb-1.5 text-sub top-1"))


if __name__ == "__main__":
    unittest.main()
