export interface TerminalLink {
  kind: 'url' | 'path';
  /** The URL, or the path without its :line:column tail. */
  value: string;
  line?: number;
  column?: number;
  start: number;
  end: number;
}

/** Embedded verbatim in the renderer, and executed by the tests: the link
 * under character `index` of a line's text, or null. A line is a program's
 * logical line, so one that wrapped over rows is looked at whole. URLs are
 * http(s) and file; paths are absolute, home or dot-relative, or relative
 * with a directory and a file extension. Sentence punctuation and a closing
 * bracket without its opening one are not part of a link. */
export const TERMINAL_LINK_SOURCE = `(text, index) => {
  if (typeof text !== 'string' || !Number.isInteger(index) || index < 0 || index >= text.length) return null;
  const count = (s, c) => s.split(c).length - 1;
  const trim = (s) => {
    let t = s;
    for (;;) {
      const last = t[t.length - 1];
      if (last && /[.,;:!?'"\\u0060]/.test(last)) t = t.slice(0, -1);
      else if (last === ')' && count(t, '(') < count(t, ')')) t = t.slice(0, -1);
      else if (last === ']' && count(t, '[') < count(t, ']')) t = t.slice(0, -1);
      else if (last === '}' && count(t, '{') < count(t, '}')) t = t.slice(0, -1);
      else if (last === '>' && count(t, '<') < count(t, '>')) t = t.slice(0, -1);
      else return t;
    }
  };
  const taken = [];
  const urls = /(?:https?:\\/\\/|file:\\/\\/\\/)[^\\s<>"'\\u0060\\u00a0]+/gi;
  for (let m = urls.exec(text); m; m = urls.exec(text)) {
    const value = trim(m[0]);
    if (value.length < 9) continue;
    const start = m.index;
    const end = start + value.length;
    taken.push([start, end]);
    if (index >= start && index < end) return { kind: 'url', value, start, end };
  }
  const paths = /(^|[\\s(\\[{"'=<:,])((?:~|\\.{1,2})?\\/[\\w.@%+~\\-]+(?:\\/[\\w.@%+~\\-]+)*\\/?|[\\w.@%+\\-]+(?:\\/[\\w.@%+\\-]+)+\\.[A-Za-z][A-Za-z0-9]{0,9})((?::\\d+){1,2})?/g;
  for (let m = paths.exec(text); m; m = paths.exec(text)) {
    let body = m[2];
    const tail = m[3] || '';
    const start = m.index + m[1].length;
    if (tail === '') body = trim(body);
    if (body.length < 2 || body === '//' || /^\\/+$/.test(body)) continue;
    const end = start + body.length + (body === m[2] ? tail.length : 0);
    if (taken.some(([a, b]) => start < b && end > a)) continue;
    if (index >= start && index < end) {
      const link = { kind: 'path', value: body, start, end };
      const numbers = body === m[2] ? tail.split(':').filter(Boolean).map(Number) : [];
      if (numbers.length > 0) link.line = numbers[0];
      if (numbers.length > 1) link.column = numbers[1];
      return link;
    }
  }
  return null;
}`;

export const findTerminalLink = new Function(
  `return (${TERMINAL_LINK_SOURCE});`,
)() as (text: string, index: number) => TerminalLink | null;
