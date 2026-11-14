/**
 * How much of one event a timeline row reads.
 *
 * A thread's rows are projected again on changes to it, all in one call into JS, and NSH cuts such a call off after
 * 2 s ("Error: interrupted": the thread stops updating, and can fail the same way each time it opens). So no row may
 * cost more for a longer command, input or output: a 30 KB heredoc, a 30 KB file write or a 30 KB log costs what a
 * short one does. Rows read a command's head and an output's start and end; the event keeps the full text (the
 * expanded row's command and the terminal show it).
 */

/** The start of a command (or a tool's input) that its row's label, target, files and query are read from. */
export const ROW_SCAN_HEAD = 512;

/** Of a longer text, a row reads this much of its start and this much of its end (whole lines). */
export const ROW_TEXT_EDGE = 2048;

export function scanHead(text: string): string {
  if (text.length <= ROW_SCAN_HEAD) {
    return text;
  }
  const head = text.slice(0, ROW_SCAN_HEAD);
  return isHighSurrogate(head.charCodeAt(head.length - 1)) ? head.slice(0, -1) : head;
}

/**
 * A command's head (scanHead), with a quote it cut off closed again: `gh pr create --body "…` reads as the whole
 * command does, its body one quoted word.
 */
export function commandHead(command: string): string {
  let head = scanHead(command);
  if (head.length === command.length) {
    return head;
  }
  let quote = "";
  for (let i = 0; i < head.length; i += 1) {
    const ch = head[i];
    if (ch === "\\" && quote !== "'") {
      if (i === head.length - 1) {
        head = head.slice(0, -1);
        break;
      }
      i += 1;
    } else if (quote) {
      if (ch === quote) quote = "";
    } else if (ch === '"' || ch === "'") {
      quote = ch;
    }
  }
  return head + quote;
}

// A surrogate pair is one character, as Array.from counts; matched natively, without an array of every character.
const SURROGATE_PAIR_RE = /[\uD800-\uDBFF][\uDC00-\uDFFF]/g;

export function codePointLength(value: string): number {
  return value.length - (value.match(SURROGATE_PAIR_RE)?.length ?? 0);
}

function isHighSurrogate(code: number): boolean {
  return code >= 0xd800 && code <= 0xdbff;
}

function isLowSurrogate(code: number): boolean {
  return code >= 0xdc00 && code <= 0xdfff;
}

/**
 * The JSON body of a long output after its transport header ("Wall time: …", "Output:"), or null. A row keeps a
 * JSON payload whole, since it is parsed natively; only lines cleaned one by one in JS are windowed.
 */
export function jsonAfterOutputHeader(value: string): string | null {
  const marker = value.startsWith("Output:\n") ? 0 : value.indexOf("\nOutput:\n");
  if (marker < 0) {
    return null;
  }
  const body = value.slice(marker + (marker === 0 ? 0 : 1) + "Output:\n".length).trim();
  if (!/^[[{]/.test(body)) {
    return null;
  }
  try {
    JSON.parse(body);
    return body;
  } catch {
    return null;
  }
}

export type TextWindow = {
  /** The start and end, joined by a newline; the text itself when it is short. */
  text: string;
  /** How many characters (as Array.from counts them) of the original `text` leaves out. */
  omitted: number;
};

/** A long text's start and end in whole lines, and how much was left out between them. */
export function textWindow(text: string): TextWindow {
  if (text.length <= ROW_TEXT_EDGE * 2) {
    return { text, omitted: 0 };
  }
  let head = text.slice(0, ROW_TEXT_EDGE);
  const headBreak = head.lastIndexOf("\n");
  if (headBreak >= ROW_TEXT_EDGE / 2) {
    head = head.slice(0, headBreak);
  } else if (isHighSurrogate(head.charCodeAt(head.length - 1))) {
    head = head.slice(0, -1);
  }
  let tail = text.slice(text.length - ROW_TEXT_EDGE);
  const tailBreak = tail.indexOf("\n");
  if (tailBreak >= 0 && tailBreak < ROW_TEXT_EDGE / 2) {
    tail = tail.slice(tailBreak + 1);
  } else if (isLowSurrogate(tail.charCodeAt(0))) {
    tail = tail.slice(1);
  }
  const joined = `${head}\n${tail}`;
  return { text: joined, omitted: codePointLength(text) - codePointLength(joined) };
}
