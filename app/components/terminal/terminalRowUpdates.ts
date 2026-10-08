import type { RenderSnapshot } from '../../modules/terminal-vt/src';

/** Embedded verbatim so the device renderer and behavioral tests execute the
 * same accumulator. Apply every delta on arrival; paint only the latest grid. */
export const TERMINAL_ROW_UPDATES_SOURCE = `function createTerminalRowUpdates() {
  let lines = [];
  let cols = 0;
  let revision = 0;
  let hasBase = false;
  const painted = new WeakMap();
  return {
    get lines() { return lines; },
    get revision() { return revision; },
    apply(snapshot) {
      if (snapshot.dirty === 'none') return false;
      if (snapshot.dirty === 'full') {
        lines = new Array(snapshot.rows).fill('');
        cols = snapshot.cols;
        hasBase = true;
        revision++;
      } else if (!hasBase || snapshot.rows !== lines.length || snapshot.cols !== cols) {
        return false;
      }
      for (let i = 0; i < snapshot.dirtyLines.length; i++) {
        const row = snapshot.dirtyLines[i];
        if (lines[row] !== snapshot.lineHtml[i]) {
          lines[row] = snapshot.lineHtml[i];
          revision++;
        }
      }
      return true;
    },
    paint(container) {
      let previous = painted.get(container);
      if (!previous) { previous = []; painted.set(container, previous); }
      for (let row = 0; row < lines.length; row++) {
        if (previous[row] === lines[row]) continue;
        const template = document.createElement('template');
        template.innerHTML = lines[row];
        const node = template.content.firstElementChild;
        if (!node) continue;
        const existing = container.children[row];
        if (existing) existing.replaceWith(node);
        else container.appendChild(node);
        previous[row] = lines[row];
      }
      while (container.children.length > lines.length) container.lastElementChild.remove();
      previous.length = lines.length;
    },
  };
}`;

export interface TerminalRowUpdates {
  readonly lines: string[];
  readonly revision: number;
  apply(snapshot: RenderSnapshot): boolean;
  paint(container: HTMLElement): void;
}
