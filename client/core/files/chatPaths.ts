/**
 * Files an agent names in a reply become links (owner 11-15: a PDF Brain named in inline code could not be
 * opened from Chat). A code span that is one home path (`/home/me/plan.pdf`, `~/notes.md`,
 * `C:\Users\me\a.png`) and such a path in the text become Markdown links to that path, which the chat's
 * link handler previews (`ui/chat/links.ts`); code blocks, links and URLs stay as they are.
 *
 * Only paths under a home (`/home/<name>/`, `/Users/<name>/`, `/root/`, `~/`, `<drive>:\Users\<name>\`)
 * that end in a file name with an extension are linked: route-like `/api/v1/x.json` in prose is not a file.
 */

const HOME_PATH = /^(?:~\/|\/(?:home|Users)\/[^/\s]+\/|\/root\/|[A-Za-z]:[\\/]Users[\\/][^\\/\s]+[\\/])/;
/** The file name's extension, at the end of the path or before a `:line(:col)` / `#Lline` suffix. */
const NAMED_FILE = /[^\\/]\.[A-Za-z0-9]{1,10}(?::\d+(?::\d+)?|#L\d+(?:C\d+)?)?$/;
const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const LIST_ITEM = /^\s*(?:[-*+]|\d{1,9}[.)])\s/;
/** What ends a path in running text: space, Markdown and table syntax, quotes and brackets. */
const BARE_STOP = /[\s`<>|"'()[\]{}*]/;
/** Punctuation a sentence puts after a path, never its end. */
const TRAILING = /[.,;:!?。，、；：！？…]+$/;
const BARE_EXTENSION = /\.[A-Za-z0-9]{1,10}(?::\d+(?::\d+)?)?(?![A-Za-z0-9._-])/g;

/** A code span's whole content, when it is one home file path. */
export function namedHomeFile(value: string): string | null {
  const path = value.trim();
  if (!path || path.length > 4096 || /[\n\0]/.test(path)) return null;
  return HOME_PATH.test(path) && NAMED_FILE.test(path) ? path : null;
}

/** The reply with the home files it names as links; the same string when it names none. */
export function linkFilePaths(markdown: string): string {
  if (!markdown || !/(?:~\/|\/home\/|\/Users\/|\/root\/|[A-Za-z]:[\\/]Users)/.test(markdown)) return markdown;
  const lines = markdown.split("\n");
  let fence: string | null = null;
  let blank = true;
  let inList = false;
  let inIndented = false;
  let changed = false;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const opened = FENCE.exec(line);
    if (fence) {
      if (opened && opened[1][0] === fence[0] && opened[1].length >= fence.length && !line.slice(opened[0].length).trim()) fence = null;
      continue;
    }
    if (opened) {
      fence = opened[1];
      continue;
    }
    if (!line.trim()) {
      blank = true;
      continue;
    }
    const indented = /^(?: {4}|\t)/.test(line);
    // An indented line after a blank line is code, unless it continues a list item.
    inIndented = indented && !inList && (blank || inIndented);
    if (!indented) inList = LIST_ITEM.test(line) || (inList && !blank);
    else if (LIST_ITEM.test(line)) inList = true;
    blank = false;
    if (inIndented) continue;
    const linked = linkLine(line);
    if (linked !== line) {
      lines[i] = linked;
      changed = true;
    }
  }
  return changed ? lines.join("\n") : markdown;
}

function linkLine(line: string): string {
  let out = "";
  let text = 0; // start of the plain text not yet copied
  let i = 0;
  const flush = (end: number) => {
    if (end > text) out += linkBare(line.slice(text, end));
  };
  while (i < line.length) {
    const c = line[i];
    if (c === "\\") {
      i += 2;
      continue;
    }
    if (c === "`") {
      let run = 1;
      while (line[i + run] === "`") run++;
      const close = findBackticks(line, i + run, run);
      if (close < 0) {
        i += run;
        continue;
      }
      flush(i);
      const span = line.slice(i, close + run);
      const path = namedHomeFile(stripCodeSpan(line.slice(i + run, close)));
      out += path ? `[${span}](${destination(path)})` : span;
      i = close + run;
      text = i;
      continue;
    }
    if (c === "[" || (c === "!" && line[i + 1] === "[")) {
      const end = linkEnd(line, c === "!" ? i + 1 : i);
      if (end > 0) {
        flush(i);
        out += line.slice(i, end);
        i = end;
        text = i;
        continue;
      }
    }
    if (c === "<") {
      const close = line.indexOf(">", i + 1);
      if (close > 0 && /^<\/?[A-Za-z][^<>]*>$|^<[a-z][a-z0-9+.-]*:[^\s<>]*>$/i.test(line.slice(i, close + 1))) {
        flush(i);
        out += line.slice(i, close + 1);
        i = close + 1;
        text = i;
        continue;
      }
    }
    i++;
  }
  flush(line.length);
  return out;
}

function findBackticks(line: string, from: number, run: number): number {
  for (let j = from; j < line.length; j++) {
    if (line[j] !== "`") continue;
    let n = 1;
    while (line[j + n] === "`") n++;
    if (n === run) return j;
    j += n - 1;
  }
  return -1;
}

/** CommonMark: one space on each side of a code span's content is not content. */
function stripCodeSpan(content: string): string {
  return content.length > 2 && content.startsWith(" ") && content.endsWith(" ") && content.trim() ? content.slice(1, -1) : content;
}

/** Where a `[text](dest)` or `[text][ref]` starting at `start` ends; 0 when it is none. */
function linkEnd(line: string, start: number): number {
  let depth = 0;
  let j = start;
  for (; j < line.length; j++) {
    const ch = line[j];
    if (ch === "\\") {
      j++;
    } else if (ch === "`") {
      let run = 1;
      while (line[j + run] === "`") run++;
      const close = findBackticks(line, j + run, run);
      j = close < 0 ? j + run - 1 : close + run - 1;
    } else if (ch === "[") depth++;
    else if (ch === "]" && --depth === 0) break;
  }
  if (depth !== 0 || j >= line.length) return 0;
  const after = line[j + 1];
  if (after === "(") {
    let parens = 0;
    for (let k = j + 1; k < line.length; k++) {
      if (line[k] === "\\") k++;
      else if (line[k] === "(") parens++;
      else if (line[k] === ")" && --parens === 0) return k + 1;
    }
    return 0;
  }
  if (after === "[") {
    const close = line.indexOf("]", j + 2);
    return close > 0 ? close + 1 : 0;
  }
  return 0;
}

/** Home file paths in running text: after a space, a bracket, a quote or a non-ASCII letter (Chinese has no spaces). */
function linkBare(text: string): string {
  if (!/(?:~\/|\/home\/|\/Users\/|\/root\/|[A-Za-z]:[\\/]Users)/.test(text)) return text;
  let out = "";
  let copied = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c !== "/" && c !== "~" && !/[A-Za-z]/.test(c)) continue;
    const before = i === 0 ? "" : text[i - 1];
    if (before && (/[A-Za-z0-9_./\\:~@%+=-]/.test(before))) continue;
    if (!HOME_PATH.test(text.slice(i, i + 300))) continue;
    let end = i;
    while (end < text.length && !BARE_STOP.test(text[end])) end++;
    const path = fileEnd(text.slice(i, end));
    if (!path) {
      i = end;
      continue;
    }
    out += text.slice(copied, i) + `[${path}](${destination(path)})`;
    copied = i + path.length;
    i = copied - 1;
  }
  return copied ? out + text.slice(copied) : text;
}

/** The longest start of `run` that is a file path ending in a name with an extension. */
function fileEnd(run: string): string | null {
  const trimmed = run.replace(TRAILING, "");
  let best = -1;
  BARE_EXTENSION.lastIndex = 0;
  for (let m = BARE_EXTENSION.exec(trimmed); m; m = BARE_EXTENSION.exec(trimmed)) best = m.index + m[0].length;
  if (best < 0) return null;
  const path = trimmed.slice(0, best);
  return namedHomeFile(path);
}

/** A link destination that keeps the path as it is: spaces, backslashes and brackets included. */
function destination(path: string): string {
  return `<${path.replace(/[\\<>]/g, (ch) => `\\${ch}`)}>`;
}
