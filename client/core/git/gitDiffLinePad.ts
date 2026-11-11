/**
 * Two trailing spaces after every line Git review hands NSH's DiffView (FAILURES R4#3).
 *
 * NSH 0.5.0 sizes a DiffView row for one line (or, with `softWrap`, for the parts it counts from
 * the width of "0"), and Android lays each line's text out at the sum of its advances. A line
 * that ends in an italic glyph (a syntax comment: NSH's `Token::Comment` is italic, not
 * something the app can restyle) needs a little more than that sum: its last word broke onto a
 * second line inside the one-line row and drew over the next row. Spaces add advance and no ink,
 * so the line ends with room to spare; NSH never starts a wrapped part with whitespace, so they
 * add no row. Goes away when NSH's DiffView keeps a line's text on one line (upstream issue
 * drafted in nsh-upstream-1111; number to follow).
 *
 * Only what the view draws is padded: Git review's own model (copy, find, ask the agent) keeps
 * the text as it is.
 */
export const GIT_DIFF_LINE_PAD = "  ";

/** `text` (a whole file) with each non-empty line padded; line count and endings unchanged. */
export function padGitDiffText(text: string): string {
  return text.replace(/[^\r\n](?=\r?\n|\r?$)/g, `$&${GIT_DIFF_LINE_PAD}`);
}

/**
 * `patch` (unified, git's) with each non-empty hunk line padded. A hunk ends when its header's
 * counts are used up, so file headers (`--- a/…`, `+++ b/…`, `diff --git`) and `\ No newline`
 * marks are never touched.
 */
export function padGitDiffPatch(patch: string): string {
  let oldLeft = 0;
  let newLeft = 0;
  return patch
    .split("\n")
    .map((line) => {
      const header = /^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@/.exec(line);
      if (header) {
        oldLeft = header[1] === undefined ? 1 : Number(header[1]);
        newLeft = header[2] === undefined ? 1 : Number(header[2]);
        return line;
      }
      // A short-counted hunk still ends at the next file.
      if (line.startsWith("diff --git ")) oldLeft = newLeft = 0;
      if (oldLeft <= 0 && newLeft <= 0) return line;
      const mark = line[0];
      if (mark === " ") {
        oldLeft--;
        newLeft--;
      } else if (mark === "-") oldLeft--;
      else if (mark === "+") newLeft--;
      else return line;
      // An empty line keeps only its mark: no glyph, nothing to make room for.
      return line.length > 1 ? padGitDiffText(line) : line;
    })
    .join("\n");
}
