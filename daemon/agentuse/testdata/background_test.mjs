// Run by TestBackground: node testdata/background_test.mjs <path to extension/background.js>
//
// The extension's service worker with a stand-in `chrome`: the real background.js and relay.js, driven the
// way the daemon drives them (messages on the native-messaging port) and the way Chrome does (events on
// chrome.tabs and chrome.windows). It checks what the extension asks Chrome to do, never Chrome itself.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

// The extension logs to the console; this script's one line of output is "ok".
console.log = (...args) => console.error(...args);

const calls = [];
const record = (name, ...args) => calls.push({ name, args });
const callsOf = name => calls.filter(call => call.name === name);

function event() {
  const listeners = new Set();
  return {
    addListener: listener => listeners.add(listener),
    removeListener: listener => listeners.delete(listener),
    fire: (...args) => { for (const listener of [...listeners]) listener(...args); },
  };
}

// Windows and tabs as Chrome keeps them: a window has a top bar of 60 px that its page does not.
const TOP_BAR = 60;
const windows = new Map();
const tabs = new Map();
const zoom = new Map();
let nextWindow = 100;
let nextTab = 500;
const makeTab = windowId => {
  const tab = { id: nextTab++, windowId, active: true, url: 'about:blank' };
  tabs.set(tab.id, tab);
  zoom.set(tab.id, 1);
  return tab;
};
const pageOf = tab => {
  const window = windows.get(tab.windowId);
  return { ...tab, width: window.width, height: window.height - TOP_BAR };
};

const port = { sent: [], onMessage: event(), onDisconnect: event(), postMessage(message) { this.sent.push(message); } };
let manifestVersion = '0.4.1';
let refuseInput = '';
const chromeEvents = {
  windowsFocus: event(), windowsRemoved: event(), tabsCreated: event(), tabsUpdated: event(), tabsActivated: event(),
  tabsZoom: event(), tabsRemoved: event(), debuggerEvent: event(), debuggerDetach: event(),
};
globalThis.chrome = {
  runtime: {
    id: 'mewla-test',
    lastError: undefined,
    connectNative: () => port,
    getManifest: () => ({ version: manifestVersion }),
    reload: () => record('runtime.reload'),
    onMessage: event(), onStartup: event(), onInstalled: event(),
  },
  windows: {
    WINDOW_ID_NONE: -1,
    onFocusChanged: chromeEvents.windowsFocus,
    onRemoved: chromeEvents.windowsRemoved,
    getLastFocused: async () => ({ focused: false }),
    create: async options => {
      const window = { id: nextWindow++, left: options.left ?? 0, top: options.top ?? 0, width: options.width ?? 1000, height: options.height ?? 800 };
      windows.set(window.id, window);
      return { ...window, tabs: [makeTab(window.id)] };
    },
    get: async id => ({ ...windows.get(id) }),
    update: async (id, info) => {
      record('windows.update', id, info);
      const { state, focused, ...place } = info;
      Object.assign(windows.get(id), place);
    },
    remove: async id => { record('windows.remove', id); windows.delete(id); },
  },
  tabs: {
    onCreated: chromeEvents.tabsCreated,
    onUpdated: chromeEvents.tabsUpdated,
    onActivated: chromeEvents.tabsActivated,
    onZoomChange: chromeEvents.tabsZoom,
    onRemoved: chromeEvents.tabsRemoved,
    group: async () => 7,
    query: async ({ windowId, active }) => [...tabs.values()].filter(tab => tab.windowId === windowId && (active === undefined || tab.active === active)).map(pageOf),
    get: async id => {
      if (!tabs.has(id)) throw new Error('No tab with id ' + id);
      return pageOf(tabs.get(id));
    },
    setZoomSettings: async (id, settings) => record('tabs.setZoomSettings', id, settings),
    getZoom: async id => zoom.get(id),
    setZoom: async (id, factor) => { record('tabs.setZoom', id, factor); zoom.set(id, factor || 1); },
  },
  tabGroups: { update: async () => {} },
  debugger: {
    onEvent: chromeEvents.debuggerEvent, onDetach: chromeEvents.debuggerDetach, detach: async () => {}, attach: async () => {},
    // The page's offset in its window takes a while to come back, as a real page's evaluation does.
    sendCommand: async (target, method, params) => {
      if (method === 'Runtime.evaluate') {
        await new Promise(resolve => setTimeout(resolve, 20));
        return { result: { value: [0, TOP_BAR] } };
      }
      if (refuseInput) throw new Error(refuseInput);
      record('debugger.sendCommand', target, method, params);
      return {};
    },
  },
};
// The relay's WebSocket to the daemon opens at once.
const sockets = [];
globalThis.WebSocket = class {
  static OPEN = 1;
  readyState = 0;
  constructor() { sockets.push(this); queueMicrotask(() => { this.readyState = 1; this.onopen?.(); }); }
  send() {}
  close() { this.readyState = 3; }
};

await import(pathToFileURL(process.argv[2]).href);

const settle = async () => { for (let i = 0; i < 8; i++) await new Promise(resolve => setImmediate(resolve)); };
const settleInput = async () => { await new Promise(resolve => setTimeout(resolve, 200)); await settle(); };
const hostMessage = async message => { port.onMessage.fire(message); await settle(); };
const told = type => port.sent.filter(message => message.type === type);
const lastTold = type => told(type).at(-1);
const phone = (extra = {}) => ({ bounds: { left: 40, top: 20, width: 390, height: 844 }, zoom: 2.5, ...extra });
const connect = async (id, bounds = { left: 0, top: 0, width: 1000, height: 800 }) => {
  await hostMessage({ type: 'connect', id, relayUrl: 'ws://127.0.0.1:1/relay', worker: id, label: id, bounds });
  const { windowId } = lastTold('connected');
  return { windowId, tabId: [...tabs.values()].find(tab => tab.windowId === windowId).id };
};

// The extension says hello with its version when it connects to the host.
assert.deepEqual(port.sent[0], { type: 'hello', version: '0.4.1', windows: [] });

// ---- the extension updates itself: isNewer reads numbers, not text -------------------------------------
// With no agent window open, a newer daemon's extension reloads this one; anything else does not.
for (const [version, reloads] of [['0.4.2', true], ['0.4.1', false], ['0.4.0', false], ['0.10.0', true], ['0.4.1.1', true], ['0.4', false], ['1', true], ['0.5.x', false], ['', false], [undefined, false], ['-1', false]]) {
  calls.length = 0;
  await hostMessage({ type: 'reload', version });
  assert.equal(callsOf('runtime.reload').length, reloads ? 1 : 0, `reload for version ${JSON.stringify(version)}`);
}
manifestVersion = '0.9.9';
calls.length = 0;
await hostMessage({ type: 'reload', version: '0.10.0' });
assert.equal(callsOf('runtime.reload').length, 1, '0.10.0 is newer than 0.9.9 (a text comparison says it is not)');
manifestVersion = '0.4.1';

// An open agent window is never closed by a reload: it waits until the last one is gone.
let agent = await connect('update-wait');
calls.length = 0;
await hostMessage({ type: 'reload', version: '0.4.2' });
assert.equal(callsOf('runtime.reload').length, 0, 'a reload closed an agent window');
await hostMessage({ type: 'disconnect', id: 'update-wait' });
assert.equal(callsOf('runtime.reload').length, 1, 'the reload did not follow the last window');

// ---- connect, ping ------------------------------------------------------------------------------------
agent = await connect('phone');
assert.deepEqual(lastTold('connected'), { type: 'connected', id: 'phone', windowId: agent.windowId });
await hostMessage({ type: 'ping' });
assert.deepEqual(lastTold('pong'), { type: 'pong', sessions: ['phone'], version: '0.4.1' });
await hostMessage({ type: 'connect', id: 'phone', relayUrl: 'ws://127.0.0.1:1/relay' });
assert.equal(lastTold('error').message, 'already connected');

// ---- the person's input from Screen ----------------------------------------------------------------------
// 2026-11-21: a click's page offset takes a moment; typing right after it reached the page first, into no
// field. Events reach the page in the order they came, a click at the page's own point.
// The agent's browser tools attach the window's tab, as Playwright does through the relay.
sockets.at(-1).onmessage({ data: JSON.stringify({ id: 1, method: 'chrome.debugger.attach', params: [{ tabId: agent.tabId }, '1.3'] }) });
await settle();
calls.length = 0;
const mouse = type => ({ type: 'input', id: 'phone', input: { method: 'Input.dispatchMouseEvent', params: { type, x: 200, y: 100 + TOP_BAR, button: 'left', clickCount: 1 } } });
const key = (type, text) => ({ type: 'input', id: 'phone', input: { method: 'Input.dispatchKeyEvent', params: { type, key: text, text } } });
for (const message of [mouse('mousePressed'), mouse('mouseReleased'), key('keyDown', 'a'), key('keyUp', 'a'), key('keyDown', 'b')])
  port.onMessage.fire(message);
await settleInput();
const sent = callsOf('debugger.sendCommand').map(call => [call.args[1], call.args[2].type, call.args[2].key ?? '']);
assert.deepEqual(sent, [
  ['Input.dispatchMouseEvent', 'mousePressed', ''], ['Input.dispatchMouseEvent', 'mouseReleased', ''],
  ['Input.dispatchKeyEvent', 'keyDown', 'a'], ['Input.dispatchKeyEvent', 'keyUp', 'a'], ['Input.dispatchKeyEvent', 'keyDown', 'b'],
], 'the keys reached the page before the click');
assert.deepEqual([callsOf('debugger.sendCommand')[0].args[2].x, callsOf('debugger.sendCommand')[0].args[2].y], [200, 100]);
assert.deepEqual(callsOf('debugger.sendCommand')[0].args[0], { tabId: agent.tabId });
assert.equal(told('input').length, 0, 'input that reached the page was reported as a problem');

// Input that cannot reach the page is said once, and so is its coming back.
refuseInput = 'Cannot access a chrome:// URL';
port.onMessage.fire(key('keyDown', 'c'));
port.onMessage.fire(key('keyUp', 'c'));
await settleInput();
assert.deepEqual(told('input'), [{ type: 'input', id: 'phone', message: 'Cannot access a chrome:// URL' }]);
refuseInput = '';
port.onMessage.fire(key('keyDown', 'd'));
await settleInput();
assert.deepEqual(told('input').at(-1), { type: 'input', id: 'phone' });
// The page in front is not one the tools hold (the person opened a tab of their own there).
const own = makeTab(agent.windowId);
tabs.get(agent.tabId).active = false;
port.onMessage.fire(key('keyDown', 'e'));
await settleInput();
assert.deepEqual(told('input').at(-1), { type: 'input', id: 'phone', message: "the page in front is not one the agent's browser tools hold" });
tabs.delete(own.id);
tabs.get(agent.tabId).active = true;
// A window the extension does not have.
await hostMessage({ type: 'input', id: 'nobody', input: key('keyDown', 'g').input });
assert.deepEqual(told('input').at(-1), { type: 'input', id: 'nobody', message: 'the agent window is gone' });

// ---- laying the window out for the phone ---------------------------------------------------------------
calls.length = 0;
await hostMessage({ type: 'reflow', id: 'phone', req: 1, ...phone() });
let reflowed = lastTold('reflowed');
assert.equal(reflowed.req, 1);
assert.equal(reflowed.message, undefined, reflowed.message);
// The page is exactly the size asked for, its top left where the window's is plus the bar's size.
assert.deepEqual(reflowed.bounds, { left: 40, top: 20 + TOP_BAR, width: 390, height: 844 });
assert.deepEqual(callsOf('tabs.setZoom').map(call => call.args), [[agent.tabId, 2.5]]);
assert.deepEqual(callsOf('tabs.setZoomSettings').map(call => call.args), [[agent.tabId, { mode: 'automatic', scope: 'per-tab' }]]);

// 2026-11-17 (da7864539): Chrome shows its zoom bubble over the page on every setZoom, so a refit at the
// same scale (a taller view when the key bar comes up) sets nothing.
calls.length = 0;
await hostMessage({ type: 'reflow', id: 'phone', req: 2, ...phone({ bounds: { left: 40, top: 20, width: 390, height: 500 } }) });
assert.deepEqual(lastTold('reflowed').bounds, { left: 40, top: 20 + TOP_BAR, width: 390, height: 500 });
assert.equal(callsOf('tabs.setZoom').length, 0, 'a refit at the same zoom set the zoom again (Chrome shows its zoom bubble)');
// A different scale does set it.
calls.length = 0;
await hostMessage({ type: 'reflow', id: 'phone', req: 3, ...phone({ zoom: 3 }) });
assert.deepEqual(callsOf('tabs.setZoom').map(call => call.args), [[agent.tabId, 3]]);
await hostMessage({ type: 'reflow', id: 'phone', req: 4, ...phone() });
assert.equal(zoom.get(agent.tabId), 2.5);

// 2026-11-19 (2fd5d0891): Chrome puts a tab's own zoom back on every navigation (a reload, the next step of a
// sign-in). The page keeps the phone's zoom: Chrome's zoom change event, a navigation's status and a tab
// coming to the front each bring it back.
calls.length = 0;
zoom.set(agent.tabId, 1);
chromeEvents.tabsZoom.fire({ tabId: agent.tabId, newZoomFactor: 1 });
await settle();
assert.deepEqual(callsOf('tabs.setZoom').map(call => call.args), [[agent.tabId, 2.5]], "a navigation's zoom reset was not undone");
calls.length = 0;
chromeEvents.tabsZoom.fire({ tabId: agent.tabId, newZoomFactor: 2.5 });
await settle();
assert.equal(callsOf('tabs.setZoom').length, 0, "the phone's own zoom change was answered");
zoom.set(agent.tabId, 1);
chromeEvents.tabsUpdated.fire(agent.tabId, { status: 'loading' }, { ...tabs.get(agent.tabId) });
await settle();
assert.deepEqual(callsOf('tabs.setZoom').map(call => call.args), [[agent.tabId, 2.5]], 'a page load did not get the phone zoom back');
const second = makeTab(agent.windowId);
second.active = false;
chromeEvents.tabsActivated.fire({ tabId: second.id, windowId: agent.windowId });
await settle();
assert.deepEqual(callsOf('tabs.setZoom').at(-1).args, [second.id, 2.5], 'a tab brought to the front was not zoomed');
// A tab in a window that is not this agent's keeps its own zoom.
calls.length = 0;
const stranger = (await chrome.windows.create({})).tabs[0];
chromeEvents.tabsZoom.fire({ tabId: stranger.id, newZoomFactor: 1.5 });
chromeEvents.tabsUpdated.fire(stranger.id, { status: 'complete' }, { ...stranger });
chromeEvents.tabsActivated.fire({ tabId: stranger.id, windowId: stranger.windowId });
await settle();
assert.equal(callsOf('tabs.setZoom').length, 0, "another window's tab was zoomed for the phone");

// ---- letting go: zoom 0 ---------------------------------------------------------------------------------
// Every tab of the window comes back to zoom 0, also one that came in after the layout, and the window goes
// back to where it was; afterwards a zoom change is nobody's business.
calls.length = 0;
const late = makeTab(agent.windowId);
late.active = false;
await hostMessage({ type: 'reflow', id: 'phone', req: 5, zoom: 0 });
assert.equal(lastTold('reflowed').req, 5);
assert.equal(lastTold('reflowed').message, undefined);
assert.deepEqual(new Set(callsOf('tabs.setZoom').map(call => call.args[0])), new Set([agent.tabId, second.id, late.id]));
assert.ok(callsOf('tabs.setZoom').every(call => call.args[1] === 0), 'a tab was zoomed instead of put back');
assert.deepEqual(callsOf('windows.update').at(-1).args, [agent.windowId, { left: 0, top: 0, width: 1000, height: 800, state: 'normal' }]);
calls.length = 0;
chromeEvents.tabsZoom.fire({ tabId: agent.tabId, newZoomFactor: 1 });
await settle();
assert.equal(callsOf('tabs.setZoom').length, 0, 'a window let go of was zoomed again');

// A reflow of a window that is gone says so.
await hostMessage({ type: 'reflow', id: 'nobody', req: 6, ...phone() });
assert.deepEqual(lastTold('reflowed'), { type: 'reflowed', id: 'nobody', req: 6, message: 'the agent window is gone' });

// ---- Mewla goes away while a window is laid out ---------------------------------------------------------
// Nobody holds the window for a phone any more: it is put back, and in the person's own Chrome the agent's
// window closes with the daemon.
const held = await connect('held');
await hostMessage({ type: 'reflow', id: 'held', req: 7, ...phone() });
assert.equal(zoom.get(held.tabId), 2.5);
calls.length = 0;
port.onDisconnect.fire();
await settle();
assert.equal(zoom.get(held.tabId), 1, 'the window stayed laid out for a phone that is gone');
assert.ok(callsOf('windows.remove').some(call => call.args[0] === held.windowId), 'the agent window outlived the daemon');

console.log = () => {};
process.stdout.write('ok\n');
process.exit(0);
