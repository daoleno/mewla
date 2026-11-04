// Loaded by Playwright MCP for every tab (--init-page). Playwright's click,
// hover and check wait for the element to stop moving, which needs two
// animation frames; a tab an agent works in is never the one on screen, and
// Chrome gives such a tab only a frame or two a second, so every click took
// one to two seconds. This does the same checks without frames, in the page,
// then clicks with `force`: the element is attached and visible, enabled, not
// covered by another element, and scrolled into view. A call that already
// passes `force` is left alone.
const ACTIONS = { click: 0, dblclick: 0, hover: 0, tap: 0, check: 0, uncheck: 0, setChecked: 1 };

// Returns why the element can't be acted on yet, or '' when it can.
function blocker(el) {
  if (el.disabled || el.getAttribute('aria-disabled') === 'true') return 'is disabled';
  el.scrollIntoView({ block: 'center', inline: 'center' });
  const box = el.getBoundingClientRect();
  if (box.width === 0 || box.height === 0 || getComputedStyle(el).visibility === 'hidden') return 'is not visible';
  const top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
  if (!top || !(el.contains(top) || top.contains(el))) return 'is covered by <' + (top ? top.tagName.toLowerCase() : 'nothing') + '>';
  return '';
}

// Resolves after this document's first animation frame (once per document).
function firstFrame() {
  if (window.__mewlaPainted) return true;
  return new Promise(resolve => {
    const done = () => { window.__mewlaPainted = true; resolve(true); };
    requestAnimationFrame(done);
    setTimeout(done, 1500);
  });
}

module.exports.default = async function fastActions({ page }) {
  // A script's guess that is wrong (a heading the page doesn't have) fails in
  // five seconds, as the tools' own actions do, not after Playwright's 30.
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const proto = Object.getPrototypeOf(page.locator('html'));
  if (proto.__mewlaFastActions) return;
  proto.__mewlaFastActions = true;
  for (const [name, optionsAt] of Object.entries(ACTIONS)) {
    const original = proto[name];
    if (typeof original !== 'function') continue;
    proto[name] = async function (...args) {
      const options = args[optionsAt] || {};
      if (options.force !== undefined || options.trial) return original.apply(this, args);
      const timeout = options.timeout ?? 5000;
      const deadline = Date.now() + timeout;
      // A page that has not painted a frame yet drops input: a click right
      // after a navigation does nothing. One frame per page load, not per
      // click; a tab that never paints is let through after 1.5 s.
      await this.evaluate(firstFrame, undefined, { timeout: Math.max(100, deadline - Date.now()) });
      let why = '';
      for (;;) {
        why = await this.evaluate(blocker, undefined, { timeout: Math.max(100, deadline - Date.now()) });
        if (!why || Date.now() + 50 >= deadline) break;
        await new Promise(resolve => setTimeout(resolve, 50));
      }
      if (why) throw new Error(`locator.${name}: element ${why} (waited ${timeout}ms)`);
      args[optionsAt] = { ...options, force: true };
      return original.apply(this, args);
    };
  }
};
