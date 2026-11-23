/**
 * What "Copy message" puts on the clipboard: a message whole, as plain text that reads as it showed. A reply is
 * Markdown and loses its marks: headings their `#`, emphasis its `*` and `_`, code its backticks and fences (the code
 * kept as written, indentation and all), a table its pipes (cells tab-separated, a row a line), a quote its `>`. A list
 * keeps its numbers, and a bullet is "•", as the thread draws them; a link keeps its address after its words. Blocks
 * stay a blank line apart. The person's own message is the text they typed, as it is.
 */
export function messageCopyText(body: string, markdown: boolean): string {
  if (!markdown) return body;
  const out: string[] = [];
  const lines = body.replace(/\r\n?/g, "\n").split("\n");
  let fence: { mark: string; indent: number } | null = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (fence) {
      if (new RegExp(`^ {0,3}${fence.mark[0]}{${fence.mark.length},}\\s*$`).test(line)) {
        fence = null;
        continue;
      }
      // The code as written, less the fence's own indent.
      out.push(line.replace(new RegExp(`^ {0,${fence.indent}}`), ""));
      continue;
    }
    const opening = /^( {0,3})(`{3,}|~{3,})/.exec(line);
    if (opening) {
      fence = { mark: opening[2], indent: opening[1].length };
      continue;
    }
    // A table: a row of cells, then its delimiter row.
    if (isTableRow(line) && i + 1 < lines.length && isDelimiterRow(lines[i + 1])) {
      out.push(tableCells(line).join("\t"));
      i++;
      while (i + 1 < lines.length && isTableRow(lines[i + 1])) out.push(tableCells(lines[++i]).join("\t"));
      continue;
    }
    // A setext heading's underline, or a rule: no text of its own.
    if (/^ {0,3}([-*_])( *\1){2,} *$/.test(line) || /^ {0,3}=+ *$/.test(line)) continue;
    out.push(blockLine(line));
  }
  return out.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

/** One line of a block: its heading, quote and list marks made plain, then its inline marks. */
function blockLine(line: string): string {
  let rest = line;
  let lead = "";
  const quote = /^ {0,3}(> ?)+/.exec(rest);
  if (quote) rest = rest.slice(quote[0].length);
  const heading = /^ {0,3}#{1,6}(?:\s+|$)/.exec(rest);
  if (heading) rest = rest.slice(heading[0].length).replace(/\s+#+\s*$/, "");
  const item = /^(\s*)([-*+]|\d{1,9}[.)])\s+(\[[ xX]\]\s+)?/.exec(rest);
  if (!heading && item) {
    const [, indent, marker, task] = item;
    const shown = task ? (task.trim() === "[ ]" ? "☐" : "☑") : /\d/.test(marker) ? marker.replace(")", ".") : "•";
    lead = `${indent}${shown} `;
    rest = rest.slice(item[0].length);
  }
  return lead + inline(rest);
}

/** A line's inline Markdown made plain; code spans keep their text as written. */
function inline(text: string): string {
  let out = "";
  let at = 0;
  const code = /(`+)([\s\S]*?[^`])\1(?!`)/g;
  for (let m = code.exec(text); m; m = code.exec(text)) {
    out += prose(text.slice(at, m.index));
    const inner = m[2];
    out += /^ .* $/.test(inner) && inner.trim() ? inner.slice(1, -1) : inner;
    at = m.index + m[0].length;
  }
  return out + prose(text.slice(at));
}

/** Text outside code spans: pictures, links, emphasis and escapes made plain. */
function prose(text: string): string {
  // An escaped mark is the character itself: kept out of the marks' way until the end.
  return text
    .replace(/\\([\\`*_{}[\]()#+\-.!|>~<])/g, (_, c: string) => String.fromCharCode(ESCAPED + c.charCodeAt(0)))
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\(\s*<?([^)\s>]*)>?(?:\s+"[^"]*")?\s*\)/g, (_, words: string, url: string) => linkText(words, url))
    .replace(/<((?:https?|mailto):[^>\s]+)>/g, "$1")
    .replace(/(\*\*|__|~~)(?=\S)([\s\S]*?\S)\1/g, "$2")
    // Italic: a mark against a word on each side ("*so*", "_so_"), never one inside a word (snake_case) or alone.
    .replace(/(^|[^\w*])[*_](?=\S)([^*_\n]*?\S)[*_](?![\w*])/g, "$1$2")
    .replace(/[\uE000-\uE07F]/g, (c) => String.fromCharCode(c.charCodeAt(0) - ESCAPED));
}

/** Where an escaped ASCII mark waits (a private-use character) while the marks are taken out. */
const ESCAPED = 0xe000;

/** A link's words, then its address unless the words already are it. */
function linkText(words: string, url: string): string {
  const plain = prose(words);
  if (!url || url.startsWith("#")) return plain;
  const bare = (s: string) => s.replace(/^(https?:\/\/|mailto:)/, "").replace(/\/$/, "");
  return bare(plain) === bare(url) ? plain : `${plain} (${url})`;
}

function isTableRow(line: string): boolean {
  return /^ {0,3}\|.*\|\s*$/.test(line) || (line.includes("|") && /^ {0,3}\S/.test(line) && tableCells(line).length > 1 && !/^ {0,3}([-*+]|\d+[.)])\s/.test(line));
}

function isDelimiterRow(line: string): boolean {
  return /^ {0,3}\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/.test(line) && line.includes("-");
}

/** A row's cells: split at its unescaped pipes, outer ones dropped, each made plain. */
function tableCells(line: string): string[] {
  const cells: string[] = [];
  let cell = "";
  let ticks = 0;
  const row = line.trim().replace(/^\|/, "").replace(/(?<!\\)\|$/, "");
  for (let i = 0; i < row.length; i++) {
    const c = row[i];
    if (c === "`") ticks = ticks ? 0 : 1;
    if (c === "\\" && row[i + 1] === "|") {
      cell += "|";
      i++;
      continue;
    }
    if (c === "|" && !ticks) {
      cells.push(cell);
      cell = "";
      continue;
    }
    cell += c;
  }
  cells.push(cell);
  return cells.map((c) => inline(c.trim()));
}
