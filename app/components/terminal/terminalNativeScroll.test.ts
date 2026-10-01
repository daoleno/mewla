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
  const element = () => ({
    style: {} as Record<string, string>, innerHTML: '', textContent: '',
    clientWidth: 320, clientHeight: 480, scrollHeight: 480, scrollTop: 0,
    children: [], replaceChildren() { this.innerHTML = ''; },
    getBoundingClientRect: () => ({ width: 320, height: 480, top: 0, left: 0 }),
    addEventListener: (name: string, fn: Function) => {
      handlers.set(name, [...(handlers.get(name) || []), fn]);
    },
    contains: () => true, querySelectorAll: () => [],
    scrollTo(options: { top: number }) { this.scrollTop = options.top; },
  });
  const elements = Object.fromEntries(['root', 'terminal-html', 'terminal-cursor', 'cell-measure',
    'terminal-live', 'terminal-scroll-blend', 'terminal-history', 'history-rows', 'history-notice'].map((id) => [id, element()]));
  elements['cell-measure'].getBoundingClientRect = () => ({ width: 8, height: 16, top: 0, left: 0 });
  const document = { ...element(), body: element(), documentElement: element(),
    getElementById: (id: string) => elements[id] };
  const window = { ...element(), getSelection: () => null,
    matchMedia: () => ({ matches: false }),
    ReactNativeWebView: { postMessage: (data: string) => sent.push(JSON.parse(data)) } } as any;
  const html = buildGhosttyTerminalHtml({ background: '#000', foreground: '#fff',
    cursor: '#fff', selectionBackground: '#333' } as any, null, 13, 0);
  const source = html.match(/<script>([\s\S]*)<\/script>/)![1];
  new Function('document', 'window', 'requestAnimationFrame', 'setTimeout', 'clearTimeout',
    'setInterval', 'ResizeObserver', source)(document, window,
    (fn: Function) => { frames.push(fn); return frames.length; },
    (fn: Function) => { timers.set(++timerID, fn); return timerID; },
    (id: number) => timers.delete(id), () => 0, undefined);
  await Promise.resolve();
  await Promise.resolve();
  const flush = () => { while (frames.length) frames.shift()!(100); };
  const dispatch = (name: string, y: number) => {
    const touch = { clientX: 20, clientY: y };
    for (const fn of handlers.get(name) || []) fn({ touches: [touch], changedTouches: [touch], target: {} });
  };
  flush();
  window.__zenSetScrollContext('session', 'token', 'session-change');
  return { window, elements, sent, flush, dispatch, idle() {
    const pending = [...timers.values()]; timers.clear(); pending.forEach((fn) => fn()); flush();
  } };
}

test('PTY redraw cannot detach the Android touch target during a drag or fling', async () => {
  const r = await renderer();
  r.window.__zenRenderSnapshot({ html: 'before', cursorVisible: false }); r.flush();
  r.dispatch('touchstart', 100);
  r.dispatch('touchmove', 200);
  r.window.__zenRenderSnapshot({ html: 'after', cursorVisible: false }); r.flush();
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
