import { Lexer, type Token, type Tokens } from "marked";
import {
  MessageTokenizer,
  inlinePartsFromTokens,
  inlinePartsText,
  type MarkdownInlineOptions,
  type MarkdownInlinePart,
} from "../markdown/markdownInline";
import {
  isTimelineProjectionPerfEnabled,
  recordMarkdownParseSample,
} from "./timelineProjectionPerf";

/**
 * A chat message as blocks. marked owns the Markdown (CommonMark + GFM); this
 * file only maps its tokens to what the renderer draws and adds the few things
 * marked does not know: a bare JSON object as a code block, the open state of
 * a fence while a reply streams, and an image as a block of its own.
 */
export type MessageBlock =
  | { type: "heading"; level: number; inline: MarkdownInlinePart[] }
  | { type: "paragraph"; inline: MarkdownInlinePart[] }
  | { type: "image"; src: string; alt: string; title?: string }
  | MessageListBlock
  | MessageTableBlock
  | { type: "code"; text: string; language?: string; closed: boolean }
  | { type: "quote"; blocks: MessageBlock[] }
  | { type: "rule" };

export type MessageListBlock = {
  type: "list";
  ordered: boolean;
  /** Number of the first item of an ordered list. */
  start: number;
  items: MessageListItem[];
};

export type MessageListItem = {
  blocks: MessageBlock[];
  taskState?: "checked" | "unchecked";
};

export type MessageTableAlignment = "left" | "center" | "right";

export type MessageTableBlock = {
  type: "table";
  headers: MarkdownInlinePart[][];
  alignments: MessageTableAlignment[];
  rows: MarkdownInlinePart[][][];
};

export type MessageMarkdownOptions = {
  /**
   * A person's own message keeps the line breaks they typed (Shift+Enter, or
   * Return on a phone). An agent's Markdown wraps its prose at a fixed column,
   * so there a single newline is only a space.
   */
  keepLineBreaks?: boolean;
};

const MAX_BARE_JSON_CHARS = 140_000;
/**
 * marked reads a document in time that grows faster than its length (a few
 * hundred paragraphs take most of a second), so a long message is read in
 * pieces, split only where no block can run across. A piece whose text is
 * unchanged keeps its blocks: a reply that streams re-reads only its tail.
 */
const CHUNK_TARGET_CHARS = 1500;
const CHUNK_CACHE_LIMIT = 400;
const chunkCache = new Map<string, MessageBlock[]>();
const BLOCK_CACHE_LIMIT = 600;
const blockCache = new Map<string, MessageBlock>();

export function parseMessageMarkdown(
  value: string,
  { keepLineBreaks = false }: MessageMarkdownOptions = {},
): MessageBlock[] {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? perfNowMs() : 0;
  const blocks = parseUninstrumented(value, keepLineBreaks);
  if (measure) {
    recordMarkdownParseSample({
      durationMs: perfNowMs() - started,
      inputChars: value.length,
      blockCount: blocks.length,
    });
  }
  return blocks;
}

function parseUninstrumented(
  value: string,
  keepLineBreaks: boolean,
): MessageBlock[] {
  const source = value.replace(/<!--[\s\S]*?-->/g, "").replace(/\r\n/g, "\n");
  const options: MarkdownInlineOptions = {
    softBreak: keepLineBreaks ? "keep" : "space",
    straightQuotes: true,
  };
  const mode = keepLineBreaks ? "k" : "w";
  const blocks: MessageBlock[] = [];
  for (const chunk of splitIntoChunks(source)) {
    const key = `${mode}${chunk}`;
    const cached = chunkCache.get(key);
    if (cached) {
      blocks.push(...cached);
      continue;
    }
    const tokens = new Lexer({
      gfm: true,
      breaks: keepLineBreaks,
      tokenizer: new MessageTokenizer(),
    }).lex(chunk);
    // A reference link or image reads a definition from elsewhere, so a
    // message that has one is a single piece and its result is not reused.
    const reusable = !tokens.some((token) => token.type === "def");
    const made = tokens.flatMap((token) => reusable ? reusedBlocksOf(token, mode, options) : blocksOf(token, options));
    if (reusable && chunk.length < CHUNK_TARGET_CHARS * 4) {
      if (chunkCache.size >= CHUNK_CACHE_LIMIT) {
        chunkCache.delete(chunkCache.keys().next().value as string);
      }
      chunkCache.set(key, made);
    }
    blocks.push(...made);
  }
  return blocks;
}

/** A block whose text did not change is the same object, so a growing reply re-renders only the block still growing. */
function reusedBlocksOf(
  token: Token,
  mode: string,
  options: MarkdownInlineOptions,
): MessageBlock[] {
  if (token.type === "space") {
    return [];
  }
  const key = `${mode}${token.raw}`;
  const cached = blockCache.get(key);
  if (cached) {
    return [cached];
  }
  const made = blocksOf(token, options);
  if (made.length === 1) {
    if (blockCache.size >= BLOCK_CACHE_LIMIT) {
      blockCache.delete(blockCache.keys().next().value as string);
    }
    blockCache.set(key, made[0]);
  }
  return made;
}

const LIST_MARKER_START = /^(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)/;
/** A link definition, or a raw HTML block that can hold blank lines, outside any fence. */
const SINGLE_PIECE_LINE = /^ {0,3}(?:\[[^\]\n]+\]:|<(?:pre|script|style|textarea)\b|<[?!])/i;

/** The fence a line opens or closes: `fence` is the open fence's marker, or null. */
function fenceAfter(line: string, fence: string | null): string | null {
  const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line);
  if (!marker) {
    return fence;
  }
  if (!fence) {
    return marker[1];
  }
  return marker[1][0] === fence[0] &&
    marker[1].length >= fence.length &&
    /^ {0,3}(`{3,}|~{3,})\s*$/.test(line)
    ? null
    : fence;
}

function mustStayWhole(lines: readonly string[]): boolean {
  let fence: string | null = null;
  for (const line of lines) {
    if (!fence && SINGLE_PIECE_LINE.test(line)) {
      return true;
    }
    fence = fenceAfter(line, fence);
  }
  return false;
}

/**
 * Pieces of the message that read the same alone as in place: a piece ends at
 * a blank line outside a fence, and the next line (which must be complete)
 * starts at the margin and cannot continue a list. A message with a link
 * definition or a raw HTML block that can hold blank lines stays whole.
 */
export function splitIntoChunks(source: string): string[] {
  if (source.length <= CHUNK_TARGET_CHARS * 2) {
    return [source];
  }
  const lines = source.split("\n");
  if (mustStayWhole(lines)) {
    return [source];
  }
  const chunks: string[] = [];
  let start = 0;
  let size = 0;
  let fence: string | null = null;
  for (let index = 0; index < lines.length - 1; index += 1) {
    const line = lines[index];
    size += line.length + 1;
    fence = fenceAfter(line, fence);
    if (fence || size < CHUNK_TARGET_CHARS || line.trim() !== "") {
      continue;
    }
    // The next non-blank line must be complete, so a marker still being typed
    // is not mistaken for text at the margin.
    let next = index + 1;
    while (next < lines.length - 1 && lines[next].trim() === "") {
      next += 1;
    }
    if (next >= lines.length - 1) {
      break;
    }
    const nextLine = lines[next];
    if (/^[ \t]/.test(nextLine) || LIST_MARKER_START.test(nextLine)) {
      continue;
    }
    chunks.push(lines.slice(start, index + 1).join("\n"));
    start = index + 1;
    size = 0;
  }
  chunks.push(lines.slice(start).join("\n"));
  return chunks;
}

function blocksOf(
  token: Token,
  options: MarkdownInlineOptions,
  inList = false,
): MessageBlock[] {
  switch (token.type) {
    case "space":
    case "def":
      return [];
    case "heading": {
      const heading = token as Tokens.Heading;
      return [
        {
          type: "heading",
          level: heading.depth,
          inline: inlinePartsFromTokens(heading.tokens, options),
        },
      ];
    }
    case "paragraph":
    case "text":
      return paragraphBlocks(
        token as Tokens.Paragraph | Tokens.Text,
        options,
        !inList,
      );
    case "code":
      return [codeBlock(token as Tokens.Code)];
    case "blockquote": {
      const quote = token as Tokens.Blockquote;
      const blocks = quote.tokens.flatMap((child) =>
        blocksOf(child, options, inList),
      );
      return blocks.length > 0 ? [{ type: "quote", blocks }] : [];
    }
    case "list":
      return [listBlock(token as Tokens.List, options)];
    case "table":
      return [tableBlock(token as Tokens.Table, options)];
    case "hr":
      return [{ type: "rule" }];
    case "html": {
      const text = token.raw.trim();
      return text
        ? [{ type: "paragraph", inline: [{ text }] }]
        : [];
    }
    default:
      return [];
  }
}

/** Prose around an image stays prose; the image becomes a block between. */
function paragraphBlocks(
  token: Tokens.Paragraph | Tokens.Text,
  options: MarkdownInlineOptions,
  allowBareJson: boolean,
): MessageBlock[] {
  const tokens = token.tokens ?? [{ type: "text", raw: token.raw, text: token.text } as Tokens.Text];
  const json = allowBareJson ? bareJsonBlock(token.raw) : null;
  if (json) {
    return [json];
  }
  const blocks: MessageBlock[] = [];
  let run: Token[] = [];
  const flush = () => {
    const inline = trimInline(inlinePartsFromTokens(run, options));
    if (inline.length > 0) {
      blocks.push({ type: "paragraph", inline });
    }
    run = [];
  };
  for (const child of tokens) {
    if (child.type === "image") {
      flush();
      const image = child as Tokens.Image;
      blocks.push({
        type: "image",
        src: image.href,
        alt: image.text,
        title: image.title || undefined,
      });
    } else {
      run.push(child);
    }
  }
  flush();
  return blocks;
}

function trimInline(parts: MarkdownInlinePart[]): MarkdownInlinePart[] {
  const trimmed = parts.map((part) => ({ ...part }));
  const first = trimmed[0];
  if (first && !first.kind) {
    first.text = first.text.replace(/^[ \t\n]+/, "");
  }
  const last = trimmed[trimmed.length - 1];
  if (last && !last.kind) {
    last.text = last.text.replace(/[ \t\n]+$/, "");
  }
  return trimmed.filter((part) => part.kind || part.text.length > 0);
}

function codeBlock(token: Tokens.Code): MessageBlock {
  return {
    type: "code",
    text: token.text.replace(/\n+$/, ""),
    language:
      token.codeBlockStyle === "indented"
        ? undefined
        : normalizeCodeFenceLanguage(token.lang ?? ""),
    closed: token.codeBlockStyle === "indented" || fenceIsClosed(token.raw),
  };
}

/** marked takes a fence still open at the end of its input as code to the end; whether it was closed shows only in its raw text. */
function fenceIsClosed(raw: string): boolean {
  const lines = raw.replace(/\n+$/, "").split("\n");
  const opening = /^ {0,3}(`{3,}|~{3,})/.exec(lines[0]);
  if (!opening || lines.length < 2) {
    return false;
  }
  const closing = /^ {0,3}(`{3,}|~{3,})\s*$/.exec(lines[lines.length - 1]);
  return Boolean(
    closing &&
      closing[1][0] === opening[1][0] &&
      closing[1].length >= opening[1].length,
  );
}

export function normalizeCodeFenceLanguage(value: string): string | undefined {
  const token = value
    .trim()
    .split(/\s+/)[0]
    ?.replace(/^language-/, "")
    .replace(/[{}]/g, "")
    .replace(/^\./, "")
    .trim();
  return token ? token.toLowerCase() : undefined;
}

function listBlock(
  list: Tokens.List,
  options: MarkdownInlineOptions,
): MessageListBlock {
  return {
    type: "list",
    ordered: list.ordered,
    start: typeof list.start === "number" ? list.start : 1,
    items: list.items.map((item) => ({
      blocks: item.tokens.flatMap((child) => blocksOf(child, options, true)),
      taskState: item.task ? (item.checked ? "checked" : "unchecked") : undefined,
    })),
  };
}

function tableBlock(
  table: Tokens.Table,
  options: MarkdownInlineOptions,
): MessageTableBlock {
  const cell = (value: Tokens.TableCell) =>
    trimInline(inlinePartsFromTokens(value.tokens, options));
  return {
    type: "table",
    headers: table.header.map(cell),
    alignments: table.align.map((align) =>
      align === "center" || align === "right" ? align : "left",
    ),
    rows: table.rows.map((row) => row.map(cell)),
  };
}

/** The plain text of one table cell. */
export function messageTableCellText(cell: readonly MarkdownInlinePart[]): string {
  return inlinePartsText(cell);
}

/** Every image in the message, in reading order: the gallery a tap on one opens. */
export function messageImages(
  blocks: readonly MessageBlock[],
): Extract<MessageBlock, { type: "image" }>[] {
  const images: Extract<MessageBlock, { type: "image" }>[] = [];
  const visit = (list: readonly MessageBlock[]) => {
    for (const block of list) {
      if (block.type === "image") {
        images.push(block);
      } else if (block.type === "quote") {
        visit(block.blocks);
      } else if (block.type === "list") {
        for (const item of block.items) {
          visit(item.blocks);
        }
      }
    }
  };
  visit(blocks);
  return images;
}

/** A JSON object or array a reply pastes without a fence reads as the code it is. */
function bareJsonBlock(raw: string): MessageBlock | null {
  const text = raw.trim();
  if (
    text.length > MAX_BARE_JSON_CHARS ||
    !(
      (text.startsWith("{") && text.endsWith("}")) ||
      (text.startsWith("[") && text.endsWith("]"))
    )
  ) {
    return null;
  }
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object") {
      return null;
    }
  } catch {
    return null;
  }
  if (!isProbablyStandaloneJson(text)) {
    return null;
  }
  return { type: "code", text, language: "json", closed: true };
}

function isProbablyStandaloneJson(value: string) {
  if (value === "{}" || value === "[]") {
    return true;
  }
  if (value.startsWith("{")) {
    return value.includes(":");
  }
  return /[,:{[]/.test(value.slice(1, -1));
}

function perfNowMs() {
  const perf = (globalThis as { performance?: { now(): number } }).performance;
  return perf?.now?.() ?? Date.now();
}
