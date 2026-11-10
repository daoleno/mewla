import { describe, expect, test } from 'bun:test';
import { findTerminalLink } from './terminalLinks';

const at = (text: string, needle: string, offset = 1) =>
  findTerminalLink(text, text.indexOf(needle) + offset);

describe('terminal links', () => {
  test('finds a URL and drops the punctuation around it', () => {
    expect(at('see https://example.com/a?b=1&c=2, then', 'https')).toMatchObject({
      kind: 'url', value: 'https://example.com/a?b=1&c=2',
    });
    expect(at('(https://example.com/wiki/Foo_(bar))', 'https')?.value).toBe('https://example.com/wiki/Foo_(bar)');
    expect(at('(https://example.com/x)', 'https')?.value).toBe('https://example.com/x');
    expect(at('"https://example.com/x".', 'https')?.value).toBe('https://example.com/x');
    expect(at('open file:///tmp/a.png now', 'file')?.value).toBe('file:///tmp/a.png');
  });

  test('only the characters of the link are the link', () => {
    const text = 'go to https://example.com/x now';
    expect(findTerminalLink(text, 0)).toBeNull();
    expect(findTerminalLink(text, text.indexOf('https') - 1)).toBeNull();
    expect(findTerminalLink(text, text.indexOf(' now'))).toBeNull();
    expect(findTerminalLink(text, text.length)).toBeNull();
    expect(findTerminalLink(text, -1)).toBeNull();
  });

  test('finds absolute, home, dot and relative paths with their line and column', () => {
    expect(at('error in /home/me/app/src/main.ts:42:7: bad', '/home')).toMatchObject({
      kind: 'path', value: '/home/me/app/src/main.ts', line: 42, column: 7,
    });
    expect(at('wrote ~/notes/today.md', '~/')?.value).toBe('~/notes/today.md');
    expect(at('run ./scripts/ci-local.py --all', './s')?.value).toBe('./scripts/ci-local.py');
    expect(at('see ../docs/readme.md.', '../')?.value).toBe('../docs/readme.md');
    expect(at(' M app/components/terminal/terminalLinks.ts', 'app')).toMatchObject({
      kind: 'path', value: 'app/components/terminal/terminalLinks.ts',
    });
    expect(at('daemon/server/server.go:1160', 'daemon')).toMatchObject({ value: 'daemon/server/server.go', line: 1160 });
  });

  test('leaves ordinary text and URL parts alone', () => {
    expect(at('and/or either', 'and')).toBeNull();
    expect(at('w/ the fix', 'w/')).toBeNull();
    expect(at('1/2 of it', '1/2')).toBeNull();
    expect(at('a / b', '/')).toBeNull();
    expect(at('see https://example.com/a/b.html', 'a/b')?.kind).toBe('url');
    expect(at('ratio 3/4', '3/4')).toBeNull();
  });
});
