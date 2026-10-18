import { Lexer, type Token, type Tokens } from "marked";
import { MessageTokenizer } from "./markdownInline";

/**
 * Prepares Markdown for the native renderer, which links whatever it is given.
 *  - A link that would do nothing when pressed (a blocked scheme, a file this
 *    surface cannot open) becomes its text.
 *  - A bare URL becomes an explicit <url> autolink: the renderer only links a
 *    bare URL with whitespace or ASCII punctuation on both sides, so a URL
 *    beside Chinese text would stay plain.
 * Code, other links and every other character are left as written.
 */
export function prepareMarkdownLinks(
  markdown: string,
  openable?: (href: string) => boolean,
): string {
  if (!openable && !/(?:https?:\/\/|ftp:\/\/|www\.)/i.test(markdown)) {
    return markdown;
  }
  const edits: { start: number; end: number; text: string }[] = [];
  const visit = (tokens: Token[], parent: string, base: number) => {
    let cursor = 0;
    for (const token of tokens) {
      const offset = parent.indexOf(token.raw, cursor);
      if (offset < 0 || !token.raw) continue;
      cursor = offset + token.raw.length;
      const start = base + offset;
      if (token.type === "link") {
        const link = token as Tokens.Link;
        if (openable && link.raw !== link.text && !openable(link.href)) {
          edits.push({
            start,
            end: start + link.raw.length,
            text: link.tokens.map((child) => child.raw).join(""),
          });
        } else if (link.raw === link.text && /^www\./i.test(link.raw)) {
          edits.push({ start, end: start + link.raw.length, text: `[${link.raw}](<${link.href}>)` });
        } else if (link.raw === link.text && /^(?:https?|ftp):\/\//i.test(link.href)) {
          edits.push({ start, end: start + link.raw.length, text: `<${link.href}>` });
        }
      } else if (token.type === "list") {
        visit((token as Tokens.List).items, token.raw, start);
      } else if (token.type === "table") {
        const table = token as Tokens.Table;
        const cells = [...table.header, ...table.rows.flat()];
        visit(cells.flatMap((cell) => cell.tokens), token.raw, start);
      } else if ("tokens" in token && Array.isArray(token.tokens)) {
        visit(token.tokens, token.raw, start);
      }
    }
  };
  visit(
    new Lexer({ gfm: true, tokenizer: new MessageTokenizer() }).lex(markdown),
    markdown,
    0,
  );

  let result = markdown;
  for (const edit of edits.sort((a, b) => b.start - a.start)) {
    result = result.slice(0, edit.start) + edit.text + result.slice(edit.end);
  }
  return result;
}
