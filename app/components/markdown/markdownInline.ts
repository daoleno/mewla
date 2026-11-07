import { Lexer, Tokenizer, type Token, type Tokens } from "marked";

export type MarkdownInlinePart = {
  text: string;
  kind?: "bold" | "code" | "italic" | "link" | "strike";
  url?: string;
  /** Present when a styled or link part holds more than one plain run. */
  children?: MarkdownInlinePart[];
};

// CJK text and full-width punctuation end a bare URL: GFM runs a URL to the
// next space, which would swallow the rest of a Chinese, Japanese or Korean
// sentence. A URL that really has such characters writes them as %xx.
const CJK_PUNCTUATION =
  /[\u3000-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uf900-\ufaff\uff00-\uffef]/;

export class MessageTokenizer extends Tokenizer {
  url(src: string): Tokens.Link | undefined {
    const link = super.url(src);
    if (!link) {
      return undefined;
    }
    if (link.href.startsWith("mailto:") && /^[:/]/.test(src.slice(link.raw.length))) {
      // git@github.com:org/repo is an SSH remote, not an address.
      return undefined;
    }
    const cut = link.raw.search(CJK_PUNCTUATION);
    if (cut < 0) {
      return link;
    }
    return cut === 0 ? undefined : super.url(src.slice(0, cut));
  }
}

export function markdownInlineTokens(text: string): Token[] {
  return new Lexer({ gfm: true, tokenizer: new MessageTokenizer() }).inlineTokens(text);
}

export type MarkdownInlineOptions = {
  /** What a soft line break inside a paragraph becomes. A person's own message keeps it. */
  softBreak?: "keep" | "space";
  /** Curly quotes become straight ones so one font draws the whole message alike. */
  straightQuotes?: boolean;
};

const CACHE_LIMIT = 400;
const cache = new Map<string, MarkdownInlinePart[]>();

/**
 * Inline Markdown as a tree of parts. marked's tokenizer owns what is a link,
 * emphasis or code, so bare URLs, `<https://…>`, balanced parentheses and
 * nesting follow CommonMark and GFM instead of a hand-written pattern.
 */
export function tokenizeMarkdownInline(text: string): MarkdownInlinePart[] {
  const cached = cache.get(text);
  if (cached) {
    return cached;
  }
  const parts = inlinePartsFromTokens(markdownInlineTokens(text));
  if (cache.size >= CACHE_LIMIT) {
    cache.delete(cache.keys().next().value as string);
  }
  cache.set(text, parts);
  return parts;
}

/** The parts of already-lexed inline tokens, e.g. a paragraph's `tokens`. */
export function inlinePartsFromTokens(
  tokens: readonly Token[],
  options: MarkdownInlineOptions = {},
): MarkdownInlinePart[] {
  return mergeText(tokens.flatMap((token) => toPart(token, options)));
}

/** What the parts read as without their styling. */
export function inlinePartsText(parts: readonly MarkdownInlinePart[]): string {
  return parts.map((part) => part.text).join("");
}

const NAMED_ENTITIES: Record<string, string> = {
  amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: "\u00a0",
  copy: "\u00a9", reg: "\u00ae", trade: "\u2122", hellip: "\u2026",
  mdash: "\u2014", ndash: "\u2013", lsquo: "\u2018", rsquo: "\u2019",
  ldquo: "\u201c", rdquo: "\u201d", laquo: "\u00ab", raquo: "\u00bb",
  bull: "\u2022", middot: "\u00b7", times: "\u00d7", divide: "\u00f7",
  plusmn: "\u00b1", deg: "\u00b0", para: "\u00b6", sect: "\u00a7",
  euro: "\u20ac", pound: "\u00a3", yen: "\u00a5", cent: "\u00a2",
  larr: "\u2190", rarr: "\u2192", uarr: "\u2191", darr: "\u2193",
  harr: "\u2194", le: "\u2264", ge: "\u2265", ne: "\u2260",
  frac12: "\u00bd", frac14: "\u00bc", frac34: "\u00be",
};

/**
 * marked leaves entities in text as written (its HTML renderer re-escapes
 * them), so `&amp;` would show as typed. Numeric ones arrive decoded already.
 */
export function decodeHtmlEntities(text: string): string {
  if (!text.includes("&")) {
    return text;
  }
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z][a-z0-9]*);/gi, (match, name: string) => {
    if (name[0] === "#") {
      const code = name[1] === "x" || name[1] === "X"
        ? Number.parseInt(name.slice(2), 16)
        : Number.parseInt(name.slice(1), 10);
      return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : match;
    }
    return NAMED_ENTITIES[name] ?? NAMED_ENTITIES[name.toLowerCase()] ?? match;
  });
}

function proseText(text: string, options: MarkdownInlineOptions): string {
  let value = decodeHtmlEntities(text);
  if (options.softBreak === "space") {
    value = value.replace(/ *\n */g, " ");
  }
  if (options.straightQuotes) {
    value = value
      .replace(/[\u2018\u2019\u02BC]/g, "'")
      .replace(/[\u201C\u201D]/g, '"');
  }
  return value;
}

function toPart(token: Token, options: MarkdownInlineOptions): MarkdownInlinePart[] {
  switch (token.type) {
    case "text":
      return [{ text: proseText(token.text, options) }];
    case "escape":
      return [{ text: token.text }];
    case "codespan":
      return [{ kind: "code", text: token.text }];
    case "strong":
      return [container("bold", token as Tokens.Strong, options)];
    case "em":
      return [container("italic", token as Tokens.Em, options)];
    case "del":
      return [container("strike", token as Tokens.Del, options)];
    case "link": {
      const link = token as Tokens.Link;
      return [{ ...container("link", link, options), url: link.href }];
    }
    case "image": {
      const image = token as Tokens.Image;
      return [{ kind: "link", text: image.text || image.href, url: image.href }];
    }
    case "br":
      return [{ text: "\n" }];
    case "checkbox":
      return [];
    case "html":
      return /^<br\s*\/?>$/i.test(token.raw) ? [{ text: "\n" }] : [{ text: token.raw }];
    default:
      return [{ text: token.raw }];
  }
}

function container(
  kind: "bold" | "italic" | "strike" | "link",
  token: Tokens.Strong | Tokens.Em | Tokens.Del | Tokens.Link,
  options: MarkdownInlineOptions,
): MarkdownInlinePart {
  const children = inlinePartsFromTokens(token.tokens ?? [], options);
  const text = inlinePartsText(children);
  const plain = children.length === 1 && !children[0].kind;
  return plain || children.length === 0
    ? { kind, text }
    : { kind, text, children };
}

function mergeText(parts: MarkdownInlinePart[]): MarkdownInlinePart[] {
  const merged: MarkdownInlinePart[] = [];
  for (const part of parts) {
    const last = merged[merged.length - 1];
    if (!part.kind && last && !last.kind) {
      last.text += part.text;
    } else {
      merged.push({ ...part });
    }
  }
  return merged;
}
