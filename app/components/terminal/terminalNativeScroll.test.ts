import { expect, test } from 'bun:test';
import { buildGhosttyTerminalHtml } from './ghosttyWebViewHtml';

/** Execute the shipped WebView script with controlled DOM writes and clocks.
 * The Android reproduction lost the touch target on the first PTY redraw.
 * This checks the actual draw/gesture handlers, not a second gesture model.
 */
async function renderer() {
  const handlers = new Map<string, Function[]>();
  const frames: Function[] = [];
  const timers = new Map<number, Function>();
  const sent: any[] = [];
  let timerID = 0;
  let now = 1000;
  const animations: { keys: any; options: any; cancel: () => void }[] = [];
  const element = () => ({
    style: {} as Record<string, string>, innerHTML: '', textContent: '',
    clientWidth: 320, clientHeight: 480, scrollHeight: 480, scrollTop: 0,
    children: [] as any[], replaceChildren() { this.innerHTML = ''; this.children = []; },
    appendChild(node: any) {
      node.parent = this;
      this.children.push(node);
      this.innerHTML = this.children.map((child) => child.outerHTML).join('');
    },
    insertBefore() {},
    animate(keys: any, options: any) {
      const animation = { keys, options, cancel() {} };
      animations.push(animation);
      return animation;
    },
    getBoundingClientRect: () => ({ width: 320, height: 480, top: 0, left: 0 }),
    addEventListener: (name: string, fn: Function) => {
      handlers.set(name, [...(handlers.get(name) || []), fn]);
    },
    contains: () => true, querySelectorAll: () => [],
    scrollTo(options: { top: number }) { this.scrollTop = options.top; },
  });
  const elements = Object.fromEntries(['root', 'terminal-html', 'terminal-cursor', 'cell-measure',
    'terminal-live', 'terminal-wheel-frame', 'terminal-scroll-blend', 'terminal-history', 'history-rows', 'history-notice'].map((id) => [id, element()]));
  elements['cell-measure'].getBoundingClientRect = () => ({ width: 8, height: 16, top: 0, left: 0 });
  const document = { ...element(), body: element(), documentElement: element(),
    createElement: (tag: string) => {
      if (tag !== 'template') return element();
      const template = { innerHTML: '', content: {} as any };
      template.content.querySelectorAll = () => (template.innerHTML.match(/<div class="terminal-row"[^>]*>[\s\S]*?<\/div>/g) || []).map((html) => ({
        outerHTML: html, innerHTML: html.slice(html.indexOf('>') + 1, -6),
        textContent: html.replace(/<[^>]*>/g, ''),
      }));
      Object.defineProperty(template.content, 'firstElementChild', { get: () => ({
        outerHTML: template.innerHTML,
        parent: null as any,
        replaceWith(node: any) {
          const parent = this.parent;
          node.parent = parent;
          parent.children[parent.children.indexOf(this)] = node;
          parent.innerHTML = parent.children.map((child: any) => child.outerHTML).join('');
        },
      }) });
      return template;
    },
    getElementById: (id: string) => elements[id] };
  const window = { ...element(), getSelection: () => null,
    matchMedia: () => ({ matches: false }),
    ReactNativeWebView: { postMessage: (data: string) => sent.push(JSON.parse(data)) } } as any;
  const html = buildGhosttyTerminalHtml({ background: '#000', foreground: '#fff',
    cursor: '#fff', selectionBackground: '#333' } as any, null, 13, 0);
  const source = html.match(/<script>([\s\S]*)<\/script>/)![1];
  new Function('document', 'window', 'requestAnimationFrame', 'setTimeout', 'clearTimeout',
    'setInterval', 'ResizeObserver', 'performance', source)(document, window,
    (fn: Function) => { frames.push(fn); return frames.length; },
    (fn: Function) => { timers.set(++timerID, fn); return timerID; },
    (id: number) => timers.delete(id), () => 0, undefined, { now: () => now });
  await Promise.resolve();
  await Promise.resolve();
  const flush = (elapsed = 100) => { now += elapsed; while (frames.length) frames.shift()!(now); };
  const dispatch = (name: string, y: number) => {
    const touch = { clientX: 20, clientY: y };
    for (const fn of handlers.get(name) || []) fn({ touches: [touch], changedTouches: [touch], target: {} });
  };
  flush();
  window.__mewlaSetScrollContext('session', 'token', 'session-change');
  return { window, elements, sent, animations, flush, dispatch, idle() {
    const pending = [...timers.values()]; timers.clear(); pending.forEach((fn) => fn()); flush();
  } };
}

test('PTY redraw cannot detach the Android touch target during a drag or fling', async () => {
  const r = await renderer();
  const frame = (text: string, dirty = 'partial') => ({ dirty, rows: 1, cols: 80,
    dirtyLines: [0], lineHtml: [text], cursorVisible: false });
  r.window.__mewlaRenderSnapshot(frame('before', 'full')); r.flush();
  r.dispatch('touchstart', 100);
  r.dispatch('touchmove', 200);
  r.window.__mewlaRenderSnapshot(frame('after')); r.flush();
  expect(r.elements['terminal-html'].innerHTML).toBe('before');
  r.dispatch('touchend', 200); r.flush();
  expect(r.elements['terminal-html'].innerHTML).toBe('before');
  r.idle();
  expect(r.elements['terminal-html'].innerHTML).toBe('after');
  expect(r.sent.filter((event) => event.type === 'scroll' || event.type === 'mouse')).toEqual([]);
});

test('native scrolling reports only boundary/idle state, never tmux line batches', async () => {
  const r = await renderer();
  r.elements.root.scrollHeight = 4000;
  r.dispatch('touchstart', 100);
  r.dispatch('touchmove', 200);
  for (let i = 0; i < 120; i++) {
    r.elements.root.scrollTop = 100 + i * 2.5;
    r.dispatch('scroll', 200); r.flush();
  }
  r.dispatch('touchend', 200); r.idle();
  expect(r.sent.filter((event) => event.type === 'scroll')).toEqual([]);
  expect(r.sent.filter((event) => event.type === 'viewportScroll')).toHaveLength(1);
  expect(r.sent.filter((event) => event.type === 'historyInteraction').map((event) => event.active)).toEqual([true, false]);
});


test('a stationary live tap still focuses the pane and native IME', async () => {
  const r = await renderer();
  r.dispatch('touchstart', 100);
  r.dispatch('touchend', 100);
  expect(r.sent.filter((event) => event.type === 'mouse').map((event) => event.action)).toEqual(['press', 'release']);
  expect(r.sent.filter((event) => event.type === 'focusInput')).toHaveLength(1);
});

test('Brain history and mouse-reporting Worker use the same surface with distinct owners', async () => {
  for (const [alternate, mouse, route] of [
    [false, false, 'history'], [false, true, 'history'],
    [true, false, 'none'], [true, true, 'wheel'],
  ] as const) {
    const r = await renderer();
    r.window.__mewlaHistory({ rows: [], reset: true, removed: 0, paneId: '%1',
      panes: [{ id: '%1', left: 0, top: 0, cols: 80, rows: 40, alternate, mouse }] });
    r.dispatch('touchstart', 100);
    r.dispatch('touchmove', 102);
    expect(r.sent.filter((event) => event.type === 'wheel')).toHaveLength(0);
    r.dispatch('touchmove', 120);
    // No native scroll event or RAF is needed to start the real TUI round trip.
    const wheel = r.sent.filter((event) => event.type === 'wheel');
    expect(wheel).toHaveLength(route === 'wheel' ? 1 : 0);
    if (route === 'wheel') expect(wheel[0]).toMatchObject({ ticks: -1, sessionId: 'session', token: 'token' });
    expect(r.sent.filter((event) => event.type === 'focusInput' || event.type === 'mouse')).toEqual([]);
    r.window.__mewlaSetScrollContext('next-session', 'next-token', 'session-change');
    r.elements.root.scrollTop -= 100;
    r.dispatch('scroll', 120);
    r.flush();
    expect(r.sent.filter((event) => event.type === 'wheel')).toHaveLength(wheel.length);
  }
});


test('TUI old-frame interpolation finishes in one frame and cancellation reveals the latest grid', async () => {
  const r = await renderer();
  const frame = (offset: number, dirty = 'partial') => ({ dirty, rows: 20, cols: 80,
    dirtyLines: Array.from({ length: 20 }, (_, i) => i),
    lineHtml: Array.from({ length: 20 }, (_, i) => `<div class="terminal-row"><span>ANSI 中文 row ${i + offset}</span></div>`),
    cursorVisible: false });
  r.window.__mewlaHistory({ rows: [], paneId: '%1', panes: [
    { id: '%1', left: 0, top: 0, cols: 80, rows: 40, alternate: true, mouse: true },
  ] });
  r.window.__mewlaRenderSnapshot(frame(0, 'full')); r.flush();
  const touched = r.elements['terminal-html'].children[0];
  r.dispatch('touchstart', 100); r.dispatch('touchmove', 120);
  r.window.__mewlaRenderSnapshot(frame(1)); r.flush();
  r.window.__mewlaRenderSnapshot(frame(2)); r.flush();
  expect(r.animations).toHaveLength(2);
  expect(r.animations.map((a) => a.options.duration)).toEqual([16, 16]);
  // A slow PTY response cannot extend or accumulate the previous displacement.
  expect(r.animations[1].keys).toEqual(r.animations[0].keys);
  expect(r.elements['terminal-html'].children[0]).toBe(touched);
  expect(r.elements['terminal-wheel-frame'].style.display).toBe('block');
  r.dispatch('touchcancel', 120); r.flush();
  expect(r.elements['terminal-scroll-blend'].style.display).toBe('none');
  expect(r.elements['terminal-wheel-frame'].style.display).toBe('none');
  expect(r.elements['terminal-html'].style.opacity).toBe('1');
  expect(r.elements['terminal-html'].innerHTML).toContain('row 21');
  expect(r.sent.filter((e) => e.type === 'mouse' || e.type === 'focusInput')).toEqual([]);
});

test('a TUI consuming the first wheel without redraw cannot stall fresh movement for 100 ms', async () => {
  const r = await renderer();
  r.window.__mewlaHistory({ rows: [], paneId: '%1', panes: [
    { id: '%1', left: 0, top: 0, cols: 80, rows: 40, alternate: true, mouse: true },
  ] });
  r.dispatch('touchstart', 100); r.dispatch('touchmove', 120);
  const wheels = () => r.sent.filter((e) => e.type === 'wheel');
  expect(wheels()).toHaveLength(1);
  r.elements.root.scrollTop -= 30;
  r.dispatch('scroll', 140); r.flush(16);
  expect(wheels()).toHaveLength(1);
  r.elements.root.scrollTop -= 30;
  r.dispatch('scroll', 160); r.flush(16);
  expect(wheels()).toHaveLength(2);
  expect(wheels()[1].ticks).toBe(-2); // New 30 px plus only the fractional cell remainder.
  // No queued input may reappear when movement stops.
  r.flush(200);
  expect(wheels()).toHaveLength(2);
});
