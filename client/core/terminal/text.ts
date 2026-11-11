// What the terminal's history says as text: rows without their styling, the
// matches of a search, the prompts to jump between and the link under a tap.

/** HISTORY_ROW_PROMPT of history.ts (which imports this file): the row starts a shell prompt (OSC 133). */
const ROW_PROMPT = 4;

/** CSI, OSC (BEL or ST), the other string sequences and two-byte escapes; C0 controls but tab. */
const ESCAPES = /\u001b\[[0-?]*[ -/]*[@-~]|\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)?|\u001b[P^_X][^\u001b]*(?:\u001b\\)?|\u001b[ -/]*[0-~]|[\u0000-\u0008\u000a-\u001f\u007f]/g;

/** A styled history row as plain text. */
export function rowText(ansi: string): string {
  return ansi.indexOf("\u001b") < 0 && !/[\u0000-\u0008\u000a-\u001f\u007f]/.test(ansi) ? ansi : ansi.replace(ESCAPES, "");
}

export type SearchState = {
  query: string;
  /** History rows that hold the query, oldest first. */
  hits: number[];
  /** The row shown among the hits, or -1. */
  current: number;
};

export const NO_SEARCH: SearchState = { query: "", hits: [], current: -1 };

/**
 * Rows whose text holds `query` (any case). `step` 0 is a new query and lands
 * on the newest match; 1 and -1 move to the next newer and older match,
 * wrapping; null keeps the row shown after the history changed.
 */
export function searchHistory(text: readonly string[], query: string, step: -1 | 0 | 1 | null, previous: SearchState = NO_SEARCH): SearchState {
  const needle = query.toLowerCase();
  if (!needle) return NO_SEARCH;
  const hits: number[] = [];
  for (let i = 0; i < text.length; i++) if (text[i].toLowerCase().includes(needle)) hits.push(i);
  if (!hits.length) return { query, hits, current: -1 };
  const held = hits.indexOf(previous.current);
  let at: number;
  if (step === 0) at = hits.length - 1;
  else if (step === null) at = held;
  else at = held < 0 ? hits.length - 1 : (held + step + hits.length) % hits.length;
  return { query, hits, current: at < 0 ? -1 : hits[at] };
}

/** What the search bar shows of the matches: where the shown row stands, or none. */
export function searchCount(state: SearchState): string {
  if (!state.query) return "";
  if (!state.hits.length) return "0";
  return `${Math.max(0, state.hits.indexOf(state.current)) + 1}/${state.hits.length}`;
}

/** The shape of a prompt line, for a history the program did not mark (OSC 133). */
const PROMPT_SHAPE = /^\s*(?:[❯›»➜>$%#]\s|\S+@\S+[:~ ].*[$%#]\s)/;

/** Prompt rows: those the program marked, else those shaped like a prompt. */
export function promptRows(text: readonly string[], flags: readonly number[]): number[] {
  const marked: number[] = [];
  for (let i = 0; i < flags.length; i++) if (flags[i] & ROW_PROMPT) marked.push(i);
  if (marked.length) return marked;
  const shaped: number[] = [];
  for (let i = 0; i < text.length; i++) if (PROMPT_SHAPE.test(text[i])) shaped.push(i);
  return shaped;
}

/**
 * The prompt row to show at the top for a jump from the one at `top`
 * (`direction` -1 older, 1 newer), or "bottom" when there is no newer one,
 * or null when there is no older one.
 */
export function promptTarget(prompts: readonly number[], top: number, direction: -1 | 1): number | "bottom" | null {
  if (direction < 0) {
    for (let i = prompts.length - 1; i >= 0; i--) if (prompts[i] < top) return prompts[i];
    return null;
  }
  for (const row of prompts) if (row > top) return row;
  return "bottom";
}

export type TerminalLink = {
  kind: "url" | "path";
  /** The URL, or the path without its :line:column tail. */
  value: string;
  line?: number;
  column?: number;
  start: number;
  end: number;
};

const count = (s: string, c: string) => s.split(c).length - 1;

/** Sentence punctuation and a closing bracket without its opening one are not part of a link. */
function trimLink(s: string): string {
  let t = s;
  for (;;) {
    const last = t[t.length - 1];
    if (last && /[.,;:!?'"`]/.test(last)) t = t.slice(0, -1);
    else if (last === ")" && count(t, "(") < count(t, ")")) t = t.slice(0, -1);
    else if (last === "]" && count(t, "[") < count(t, "]")) t = t.slice(0, -1);
    else if (last === "}" && count(t, "{") < count(t, "}")) t = t.slice(0, -1);
    else if (last === ">" && count(t, "<") < count(t, ">")) t = t.slice(0, -1);
    else return t;
  }
}

/**
 * The link under character `index` of a line's text, or null (Expo:
 * terminalLinks.ts). URLs are http(s) and file; paths are absolute, home or
 * dot-relative, or relative with a directory and a file extension, with an
 * optional :line:column.
 */
export function findTerminalLink(text: string, index: number): TerminalLink | null {
  if (typeof text !== "string" || !Number.isInteger(index) || index < 0 || index >= text.length) return null;
  const taken: Array<[number, number]> = [];
  const urls = /(?:https?:\/\/|file:\/\/\/)[^\s<>"'` ]+/gi;
  for (let m = urls.exec(text); m; m = urls.exec(text)) {
    const value = trimLink(m[0]);
    if (value.length < 9) continue;
    const start = m.index;
    const end = start + value.length;
    taken.push([start, end]);
    if (index >= start && index < end) return { kind: "url", value, start, end };
  }
  const paths = /(^|[\s(\[{"'=<:,])((?:~|\.{1,2})?\/[\w.@%+~\-]+(?:\/[\w.@%+~\-]+)*\/?|[\w.@%+\-]+(?:\/[\w.@%+\-]+)+\.[A-Za-z][A-Za-z0-9]{0,9})((?::\d+){1,2})?/g;
  for (let m = paths.exec(text); m; m = paths.exec(text)) {
    let body = m[2];
    const tail = m[3] || "";
    const start = m.index + m[1].length;
    if (tail === "") body = trimLink(body);
    if (body.length < 2 || body === "//" || /^\/+$/.test(body)) continue;
    const end = start + body.length + (body === m[2] ? tail.length : 0);
    if (taken.some(([a, b]) => start < b && end > a)) continue;
    if (index >= start && index < end) {
      const link: TerminalLink = { kind: "path", value: body, start, end };
      const numbers = body === m[2] ? tail.split(":").filter(Boolean).map(Number) : [];
      if (numbers.length > 0) link.line = numbers[0];
      if (numbers.length > 1) link.column = numbers[1];
      return link;
    }
  }
  return null;
}

/** An http(s) URL the system may open; anything else stays in the terminal. */
export function openableUrl(value: string): string | null {
  return /^https?:\/\/[^\s]+$/i.test(value) ? value : null;
}

/** The path a file: URL names, which opens as a path does. */
export function fileUrlPath(value: string): string | null {
  if (!/^file:\/\/\/[^\s]+$/i.test(value)) return null;
  try {
    return decodeURIComponent(value.slice("file://".length));
  } catch {
    return null;
  }
}

/** Where a tap on a link goes: the browser, or the Session's file preview. */
export type LinkAction =
  | { kind: "url"; url: string }
  | { kind: "path"; path: string; line?: number; column?: number };

/** What a tap at `index` of `text` opens, or null for a tap on no link. */
export function linkAction(text: string, index: number): LinkAction | null {
  const link = findTerminalLink(text, index);
  if (!link) return null;
  if (link.kind === "url") {
    const path = fileUrlPath(link.value);
    if (path) return { kind: "path", path };
    const url = openableUrl(link.value);
    return url ? { kind: "url", url } : null;
  }
  return { kind: "path", path: link.value, ...(link.line !== undefined ? { line: link.line } : {}), ...(link.column !== undefined ? { column: link.column } : {}) };
}
