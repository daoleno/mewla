/**
 * Files an agent names in a reply become links (owner 11-15: a PDF Brain named in inline code could not be
 * opened from Chat). A code span that is one file path and such a path in the text become Markdown links to
 * that path, which the chat's link handler previews (`ui/chat/links.ts`); code blocks, links and URLs stay as
 * they are.
 *
 * A path under a home (`/home/<name>/`, `/Users/<name>/`, `/root/`, `~/`, `<drive>:\Users\<name>\`) that ends
 * in a file name with an extension is linked as it is. Any other path (owner 11-16: Brain's
 * `worklog/…/DESIGN-v2.md` could not be opened) is linked only once the speaking Session's computer said it is
 * a file there (`existing`, by `fileKey`): relative paths are its cwd's, and route-like `/api/v1/x.json` in
 * prose names no file. `chatFileCandidates` lists the paths to ask about: in text only paths with a folder,
 * in a code span a bare `RESULT.md` too.
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
const HOME_HINT = /(?:~\/|\/home\/|\/Users\/|\/root\/|[A-Za-z]:[\\/]Users)/;
/**
 * A path the computer is asked about: no space, quote, bracket or glob; not a URL or an elided `…/a.md`; its
 * extension has a letter (`127.0.0.1:8317` and `v0.3.0` are not files).
 */
const OTHER_FILE = /^(?![a-z][a-z0-9+.-]*:\/\/)(?!.*(?:\.\.\.|…))[^\s`<>|"'()[\]{}*?,;=\\]*[^\\/\s.]\.(?=[A-Za-z0-9]*[A-Za-z])[A-Za-z0-9]{1,10}(?::\d+(?::\d+)?|#L\d+(?:C\d+)?)?$/i;
/** A relative folder named with its closing slash, as an agent names a folder report: no space, quote, bracket, glob or dots-only part. */
const FOLDER = /^(?![a-z][a-z0-9+.-]*:)(?!.*(?:\.\.\.|…))(?:\.\/)?(?:[^\s`<>|"'()[\]{}*?,;=\\/.][^\s`<>|"'()[\]{}*?,;=\\/]*\/)+$/i;
const LINE_SUFFIX = /(?::\d+(?::\d+)?|#L\d+(?:C\d+)?)$/i;
/** At most this many paths of one reply are asked about. */
export const MAX_CHAT_FILE_CANDIDATES = 64;
const NONE: ReadonlySet<string> = new Set();

/** What decides whether a path the reply names is linked; `null`: it is not a file path at all. */
type Accept = (path: string, inCode: boolean) => boolean;

/** A code span's whole content, when it is one home file path. */
export function namedHomeFile(value: string): string | null {
  const path = value.trim();
  if (!path || path.length > 4096 || /[\n\0]/.test(path)) return null;
  return HOME_PATH.test(path) && NAMED_FILE.test(path) ? path : null;
}

/** A path as the computer is asked about it: its `:line(:col)` or `#Lline` suffix left off. */
export function fileKey(path: string): string {
  return path.replace(LINE_SUFFIX, "");
}

/** A path that is not a home's, when it could name a file: in text only one with a folder (`a/b.md`, `./x.sh`). */
function otherFile(path: string, inCode: boolean): string | null {
  if (!path || path.length > 4096 || HOME_PATH.test(path)) return null;
  // A folder in code (`reports/q3/`): linked when the computer says it holds an index.html, which it opens.
  if (inCode && FOLDER.test(path)) return path;
  if (!OTHER_FILE.test(path)) return null;
  return inCode || path.includes("/") ? path : null;
}

/**
 * The reply with the files it names as links: home paths, and the other paths in `existing` (by `fileKey`);
 * the same string when it names none.
 */
export function linkFilePaths(markdown: string, existing: ReadonlySet<string> = NONE): string {
  if (!markdown) return markdown;
  if (!existing.size && !HOME_HINT.test(markdown)) return markdown;
  return walk(markdown, (path, inCode) => (namedHomeFile(path) ? true : Boolean(otherFile(path, inCode)) && existing.has(fileKey(path))));
}

/**
 * The paths a reply names that are not a home's (by `fileKey`, each once, at most MAX_CHAT_FILE_CANDIDATES):
 * what to ask the speaking Session's computer about before they are linked.
 */
export function chatFileCandidates(markdown: string): string[] {
  if (!markdown || (!markdown.includes("/") && !markdown.includes("`"))) return [];
  const found = new Set<string>();
  walk(markdown, (path, inCode) => {
    const other = otherFile(path, inCode);
    if (other && found.size < MAX_CHAT_FILE_CANDIDATES) found.add(fileKey(other));
    return false;
  });
  return [...found];
}

function walk(markdown: string, accept: Accept): string {
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
    const linked = linkLine(line, accept);
    if (linked !== line) {
      lines[i] = linked;
      changed = true;
    }
  }
  return changed ? lines.join("\n") : markdown;
}

function linkLine(line: string, accept: Accept): string {
  let out = "";
  let text = 0; // start of the plain text not yet copied
  let i = 0;
  const flush = (end: number) => {
    if (end > text) out += linkBare(line.slice(text, end), accept);
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
      const content = stripCodeSpan(line.slice(i + run, close)).trim();
      out += content && accept(content, true) ? `[${span}](${destination(content)})` : span;
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

/** File paths in running text: after a space, a bracket, a quote or a non-ASCII letter (Chinese has no spaces). */
function linkBare(text: string, accept: Accept): string {
  if (!text.includes("/") && !text.includes("\\")) return text;
  let out = "";
  let copied = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (!/[A-Za-z0-9_./~]/.test(c)) continue;
    const before = i === 0 ? "" : text[i - 1];
    if (before && (/[A-Za-z0-9_./\\:~@%+=-]/.test(before))) continue;
    let end = i;
    while (end < text.length && !BARE_STOP.test(text[end])) end++;
    const run = text.slice(i, end);
    const path = run.includes("/") || HOME_PATH.test(run) ? fileEnd(run) : null;
    if (!path || !accept(path, false)) {
      // Past this path only: Chinese after it can be followed by another (`docs/a.md和/home/me/b.pdf`).
      i = path ? i + path.length - 1 : end;
      continue;
    }
    out += text.slice(copied, i) + `[${path}](${destination(path)})`;
    copied = i + path.length;
    i = copied - 1;
  }
  return copied ? out + text.slice(copied) : text;
}

/** Chinese, Japanese or Korean text: written with no space after a path, it ends the path at its extension. */
const CJK = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uf900-\ufaff\uff00-\uffef]/;

/**
 * The start of `run` that is a file path ending in a name with an extension: the longest, or the first that CJK
 * text follows (`/a.pdf和/b.pdf` is two paths).
 */
function fileEnd(run: string): string | null {
  const trimmed = run.replace(TRAILING, "");
  let best = -1;
  BARE_EXTENSION.lastIndex = 0;
  for (let m = BARE_EXTENSION.exec(trimmed); m; m = BARE_EXTENSION.exec(trimmed)) {
    best = m.index + m[0].length;
    if (CJK.test(trimmed[best] ?? "")) break;
  }
  if (best < 0) return null;
  return trimmed.slice(0, best);
}

/** A link destination that keeps the path as it is: spaces, backslashes and brackets included. */
function destination(path: string): string {
  return `<${path.replace(/[\\<>]/g, (ch) => `\\${ch}`)}>`;
}
