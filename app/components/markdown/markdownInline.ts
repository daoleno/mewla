import { Lexer, Tokenizer, type Token, type Tokens } from "marked";

export type MarkdownInlinePart = {
  text: string;
  kind?: "bold" | "code" | "italic" | "link" | "strike";
  url?: string;
  /** Present when a styled or link part holds more than one plain run. */
  children?: MarkdownInlinePart[];
};

// Full-width punctuation ends a bare URL: GFM runs a URL to the next space,
// which would swallow the rest of a Chinese or Japanese sentence.
const CJK_PUNCTUATION = /[　-〿＀-￯]/;

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
  const parts = mergeText(markdownInlineTokens(text).flatMap(toPart));
  if (cache.size >= CACHE_LIMIT) {
    cache.delete(cache.keys().next().value as string);
  }
  cache.set(text, parts);
  return parts;
}

function toPart(token: Token): MarkdownInlinePart[] {
  switch (token.type) {
    case "text":
    case "escape":
      return [{ text: token.text }];
    case "codespan":
      return [{ kind: "code", text: token.text }];
    case "strong":
      return [container("bold", token as Tokens.Strong)];
    case "em":
      return [container("italic", token as Tokens.Em)];
    case "del":
      return [container("strike", token as Tokens.Del)];
    case "link": {
      const link = token as Tokens.Link;
      return [{ ...container("link", link), url: link.href }];
    }
    case "image": {
      const image = token as Tokens.Image;
      return [{ kind: "link", text: image.text || image.href, url: image.href }];
    }
    case "br":
      return [{ text: "\n" }];
    default:
      return [{ text: token.raw }];
  }
}

function container(
  kind: "bold" | "italic" | "strike" | "link",
  token: Tokens.Strong | Tokens.Em | Tokens.Del | Tokens.Link,
): MarkdownInlinePart {
  const children = mergeText((token.tokens ?? []).flatMap(toPart));
  const text = children.map(plainText).join("");
  const plain = children.length === 1 && !children[0].kind;
  return plain || children.length === 0
    ? { kind, text }
    : { kind, text, children };
}

function plainText(part: MarkdownInlinePart): string {
  return part.text;
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
