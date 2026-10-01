import { expect, test } from 'bun:test';
import type { RenderSnapshot } from '../../modules/zen-terminal-vt/src';
import { TERMINAL_ROW_UPDATES_SOURCE, type TerminalRowUpdates } from './terminalRowUpdates';

class Row {
  parent!: Container;
  constructor(readonly html: string) {}
  replaceWith(row: Row) {
    row.parent = this.parent;
    this.parent.children[this.parent.children.indexOf(this)] = row;
  }
  remove() { this.parent.children.splice(this.parent.children.indexOf(this), 1); }
}
class Container {
  children: Row[] = [];
  get lastElementChild() { return this.children.at(-1)!; }
  appendChild(row: Row) { row.parent = this; this.children.push(row); }
}
function renderer() {
  let parsed = 0;
  const document = { createElement() {
    const template = { innerHTML: '', content: {} };
    Object.defineProperty(template.content, 'firstElementChild', {
      get() { parsed++; return new Row(template.innerHTML); },
    });
    return template;
  } };
  const updates = new Function('document', `return (${TERMINAL_ROW_UPDATES_SOURCE})();`)(document) as TerminalRowUpdates;
  const container = new Container();
  return { updates, container, parsed: () => parsed,
    paint: (target = container) => updates.paint(target as unknown as HTMLElement) };
}
function frame(dirty: 'full' | 'partial', rows: number, entries: [number, string][]): RenderSnapshot {
  return { dirty, rows, cols: 80, dirtyLines: entries.map(([row]) => row),
    lineHtml: entries.map(([, html]) => html), cursorCol: 0, cursorRow: 0, cursorVisible: true };
}

test('multiple deltas before one paint preserve all rows and replace only changed nodes', () => {
  const r = renderer();
  r.updates.apply(frame('full', 3, [[0, 'header'], [1, 'prompt'], [2, 'status']])); r.paint();
  const [header, , status] = r.container.children;
  r.updates.apply(frame('partial', 3, [[1, 'a']]));
  r.updates.apply(frame('partial', 3, [[1, 'ab']]));
  r.paint();
  expect(r.container.children.map((row) => row.html)).toEqual(['header', 'ab', 'status']);
  expect(r.container.children[0]).toBe(header);
  expect(r.container.children[2]).toBe(status);
  expect(r.parsed()).toBe(4);
  r.updates.apply(frame('partial', 3, [])); r.paint();
  expect(r.parsed()).toBe(4); // Cursor-only updates never parse cell HTML.
});

test('deferred selection/touch paint and wheel overlay independently catch up', () => {
  const r = renderer();
  r.updates.apply(frame('full', 2, [[0, 'one'], [1, 'two']])); r.paint();
  const overlay = new Container();
  r.updates.apply(frame('partial', 2, [[0, 'ONE']])); r.paint(overlay);
  r.updates.apply(frame('partial', 2, [[1, 'TWO']])); r.paint(overlay);
  expect(r.container.children.map((row) => row.html)).toEqual(['one', 'two']);
  r.paint();
  expect(r.container.children.map((row) => row.html)).toEqual(['ONE', 'TWO']);
  expect(overlay.children.map((row) => row.html)).toEqual(['ONE', 'TWO']);
});

test('reload requires a base, and resize/session full frames remove stale rows', () => {
  const r = renderer();
  expect(r.updates.apply(frame('partial', 2, [[0, 'stale']]))).toBe(false);
  r.updates.apply(frame('full', 2, [[0, 'first'], [1, 'second']])); r.paint();
  expect(r.updates.apply(frame('partial', 3, [[2, 'wrong geometry']]))).toBe(false);
  r.updates.apply(frame('full', 1, [[0, '<span>终端 é &amp;</span>']])); r.paint();
  expect(r.container.children.map((row) => row.html)).toEqual(['<span>终端 é &amp;</span>']);
  const parsed = r.parsed();
  r.updates.apply(frame('full', 1, [[0, '<span>终端 é &amp;</span>']])); r.paint();
  expect(r.parsed()).toBe(parsed);
});
