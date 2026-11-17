/**
 * A file name cut for one line that keeps its end: `head` gives way (it ends in "…" when the line is short) and
 * `tail` always shows, so `20261117185847-转写_美元债蒸馏讨论-逐字稿文本-1 (2).md` reads `20261117185847-转写_美元债…(2).md`.
 * The tail is the extension and the last few characters before it. A short name, or one without a plain
 * extension, is all head. Counted in code points, so a character is never cut in two.
 */
export function fileNameParts(name: string): { head: string; tail: string } {
  const chars = Array.from(name);
  if (chars.length <= SHORT) return { head: name, tail: "" };
  const dot = name.lastIndexOf(".");
  const extension = dot > 0 ? name.slice(dot) : "";
  if (!/^\.[\p{L}\p{N}_-]{1,8}$/u.test(extension)) return { head: name, tail: "" };
  const stem = Array.from(name.slice(0, dot));
  const kept = stem.slice(-STEM_KEPT).join("").trimStart();
  const tail = kept + extension;
  return { head: name.slice(0, name.length - tail.length), tail };
}

/** Names up to this many characters stay one piece. */
const SHORT = 16;
/** How much of the name before the extension the tail keeps. */
const STEM_KEPT = 4;

/** The text with a word joiner (U+2060) between its characters, so a line breaks nowhere in it. */
export function unbroken(text: string): string {
  return Array.from(text).join("⁠");
}
