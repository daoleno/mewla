/**
 * Mewla extension: lets the agents Mewla starts on this computer work in a
 * window of their own in this Chrome, with its logged-in sites.
 *
 * The daemon asks over Chrome's native messaging (only this extension's ID is
 * allowed by the host manifest, so there is no key to copy or leak):
 *   {type: "connect", id, relayUrl, client, label?, bounds?: {left, top, width, height}}
 * and gets back {type: "connected" | "closed" | "error", id, ...}.
 * Each connection gets a new, unfocused window, placed on its bounds (the
 * agent's monitor on the agent screen; minimized when there are none), and a
 * window its pages open goes to the same place; the person's own windows and
 * tabs are never attached, moved, focused or listed. Its tabs are in a tab
 * group named for the Worker, so the person can see whose they are, and any
 * number of Workers can work in this Chrome at once, each in its own window.
 * When the agent is done its window closes with its tabs.
 *
 * In the agent browser (the daemon says so with {type: "role", agent: true})
 * the windows outlive a daemon that goes away for a restart or an update: they
 * keep working, the extension reconnects, and its hello lists them again
 * ({windows: [{id, worker, windowId}]}). With no daemon back in
 * AGENT_KEEP_MS they close. The person's Chrome closes them at once.
 */
import { debugLog, drivenAt, openRelayConnection } from './relay.js';

const HOST = 'com.mewla.browser';
const NON_DEBUGGABLE_SCHEMES = ['chrome:', 'edge:', 'devtools:', 'chrome-extension:'];
const isNonDebuggableUrl = url => !!url && NON_DEBUGGABLE_SCHEMES.some(s => url.startsWith(s));

// The last of the person's own windows that had the focus. If an agent's
// window takes the focus from it (some desktops focus a window on a click it
// is sent, even with no pointer), it goes straight back.
let personWindowId;
const agentWindowIds = new Set();
const minimizeAgentWindows = new Set();
// Windows being made right now: until they are known as agent windows, a focus
// change to a window nobody has seen yet could be theirs, so it waits.
// The person's window, and when focus left Chrome from it: a window that
// opens and takes the focus right then took it from the person, not from
// another app.
let lostPersonWindowId;
let lostAt = 0;
const createdAt = new Map();
let creating = 0;
let creatingWaiters = [];
function startCreating() { creating++; }
function doneCreating() {
  if (--creating === 0)
    creatingWaiters.splice(0).forEach(resume => resume());
}
chrome.windows.onFocusChanged.addListener(async windowId => {
  if (windowId === chrome.windows.WINDOW_ID_NONE) {
    // The person is in another app: there is no window of Chrome's to hand the
    // focus back to, and handing back a stale one would bring Chrome in front
    // of the app they are using. (Some desktops pass through "no window" for
    // an instant when one opens; see below.)
    if (personWindowId !== undefined) {
      lostPersonWindowId = personWindowId;
      lostAt = Date.now();
    }
    personWindowId = undefined;
    return;
  }
  if (creating > 0 && !agentWindowIds.has(windowId))
    await new Promise(resume => creatingWaiters.push(resume));
  if (!agentWindowIds.has(windowId)) {
    personWindowId = windowId;
    return;
  }
  // The window took the focus because of something the agent did to it (it
  // opened, or a command just reached it): the person's window had it a moment
  // ago, and some desktops pass through "no window" in between. It goes back.
  const agentCaused = Date.now() - (createdAt.get(windowId) ?? 0) < 2500 || Date.now() - (drivenAt.get(windowId) ?? 0) < 500;
  if (personWindowId === undefined && lostPersonWindowId !== undefined &&
      Date.now() - lostAt < 1500 && agentCaused)
    personWindowId = lostPersonWindowId;
  if (personWindowId !== undefined) {
    await chrome.windows.update(personWindowId, { focused: true }).catch(() => {});
    // Without an agent display to put it on, the window the person was in is the only place the agent's
    // can go that is not over theirs: out of the way, still working.
    if (minimizeAgentWindows.has(windowId))
      await chrome.windows.update(windowId, { state: 'minimized' }).catch(() => {});
  }
});

const GROUP_COLORS = ['blue', 'green', 'orange', 'purple', 'cyan', 'pink', 'yellow', 'red', 'grey'];
// The same Worker gets the same color each time.
function groupColor(label) {
  let h = 0;
  for (const c of String(label || '')) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return GROUP_COLORS[h % GROUP_COLORS.length];
}

/** @type {Map<string, AgentWindow>} */
const sessions = new Map();
/** @type {chrome.runtime.Port | undefined} */
let port;
let backoff = 1000;
// This is the agent browser's extension: its windows outlive the daemon.
let agentBrowser = false;
const AGENT_KEEP_MS = 10 * 60_000;
// What the person's input from Screen may be (AgentWindow.input).
const INPUT_METHODS = new Set(['Input.dispatchMouseEvent', 'Input.dispatchKeyEvent']);
let keepTimer;

function tell(message) {
  try { port?.postMessage(message); } catch (error) { debugLog('host gone', error); }
}

const hostOf = url => {
  try { return new URL(url).host; } catch { return ''; }
};

const options_bounds = bounds => ({
  left: Math.round(bounds.left),
  top: Math.round(bounds.top),
  width: Math.round(bounds.width),
  height: Math.round(bounds.height),
});

class AgentWindow {
  constructor(id) {
    this.id = id;
    this.worker = '';
    this.bounds = undefined;
    this.windowId = undefined;
    this.groupId = undefined;
    this.connection = undefined;
    this.closed = false;
    this._onUpdated = (tabId, changeInfo, tab) => {
      if (this.reflowed && changeInfo.url !== undefined && tab.active && (tab.windowId === this.windowId || this.reflowed.popups.has(tab.windowId)))
        tell({ type: 'page', id: this.id, host: hostOf(changeInfo.url) });
      if (tab.windowId !== this.windowId || this.connection?.attachedTabs.has(tabId))
        return;
      if (changeInfo.url !== undefined && !isNonDebuggableUrl(changeInfo.url))
        this.connection?.attachTab(tab);
    };
    // A tab the agent's tools open in its window joins its tab group.
    this._onCreated = tab => {
      if (tab.windowId === this.windowId && this.groupId !== undefined && tab.groupId !== this.groupId)
        void chrome.tabs.group({ groupId: this.groupId, tabIds: [tab.id] }).catch(() => {});
      // A window the page opened (a popup, a sign-in) appears at the screen's
      // corner, which is not this agent's monitor: it goes there.
      else if (tab.windowId !== this.windowId && tab.openerTabId !== undefined && this.bounds)
        void this._follow(tab);
    };
    this._onRemoved = windowId => {
      if (windowId === this.windowId)
        this.close('The agent window was closed');
      this.reflowed?.popups.delete(windowId);
    };
    // While the window is laid out for the phone, a tab that comes to the front is zoomed the same way.
    this._onActivated = ({ tabId, windowId }) => {
      if (this.reflowed && windowId === this.windowId)
        void this._zoom(tabId, this.reflowed.zoom).catch(() => {});
    };
  }

  /** Puts a window that one of this agent's pages opened where the agent's window is. */
  async _follow(tab) {
    const opener = await chrome.tabs.get(tab.openerTabId).catch(() => undefined);
    if (opener?.windowId !== this.windowId || !this.bounds)
      return;
    await chrome.windows.update(tab.windowId, { ...(this.reflowed?.window ?? this.bounds), state: 'normal' }).catch(() => {});
    if (this.reflowed) {
      this.reflowed.popups.add(tab.windowId);
      await this._zoom(tab.id, this.reflowed.zoom);
    }
  }

  /**
   * Lays the window out for the person's phone while they hold it: the page's content takes `bounds.width` x
   * `bounds.height` pixels with the window's top left at `bounds.left`, `bounds.top`, and the tab is zoomed by
   * `zoom`, so the page lays itself out at the phone's width and is drawn at its sharpness. zoom 0 restores the
   * window's place and the tabs' zoom. Answers with the content's area in the screen's pixels and the page's host.
   */
  async reflow({ req, bounds, zoom } = {}) {
    try {
      if (!(zoom > 0)) {
        await this._restore();
        tell({ type: 'reflowed', id: this.id, req });
        return;
      }
      const [tab] = await chrome.tabs.query({ windowId: this.windowId, active: true });
      if (!tab)
        throw new Error('the agent window has no page');
      const before = await chrome.windows.get(this.windowId);
      this.reflowed ??= { saved: { left: before.left, top: before.top, width: before.width, height: before.height }, tabs: new Set(), popups: new Set() };
      this.reflowed.zoom = zoom;
      await this._zoom(tab.id, zoom);
      // What the window draws above and beside its page (tabs, toolbar, an infobar) stays at its own size.
      const chromeTop = Math.max(0, before.height - (tab.height ?? before.height));
      const chromeSide = Math.max(0, before.width - (tab.width ?? before.width));
      const window = { left: Math.round(bounds.left), top: Math.round(bounds.top), width: Math.round(bounds.width) + chromeSide, height: Math.round(bounds.height) + chromeTop };
      await chrome.windows.update(this.windowId, { ...window, state: 'normal' });
      // The toolbar can change height with the window: measure once more and make the page exactly the size asked.
      let placed = await chrome.windows.get(this.windowId);
      let page = await chrome.tabs.get(tab.id);
      const short = Math.round(bounds.height) - (page.height ?? 0);
      if (short !== 0 && page.height) {
        window.height += short;
        await chrome.windows.update(this.windowId, { height: window.height });
        placed = await chrome.windows.get(this.windowId);
        page = await chrome.tabs.get(tab.id);
      }
      this.reflowed.window = window;
      const content = {
        left: placed.left + (placed.width - (page.width ?? placed.width)),
        top: placed.top + (placed.height - (page.height ?? placed.height)),
        width: page.width ?? placed.width,
        height: page.height ?? placed.height,
      };
      tell({ type: 'reflowed', id: this.id, req, bounds: content, host: hostOf(page.url) });
    } catch (error) {
      tell({ type: 'reflowed', id: this.id, req, message: String(error?.message || error) });
    }
  }

  async _zoom(tabId, zoom) {
    await chrome.tabs.setZoomSettings(tabId, { mode: 'automatic', scope: 'per-tab' }).catch(() => {});
    await chrome.tabs.setZoom(tabId, zoom);
    this.reflowed?.tabs.add(tabId);
  }

  async _restore() {
    const reflowed = this.reflowed;
    if (!reflowed)
      return;
    this.reflowed = undefined;
    // Every tab of the window and its popups, not only the ones zoomed here: one may have come in since.
    const tabs = new Set(reflowed.tabs);
    for (const windowId of [this.windowId, ...reflowed.popups])
      for (const tab of await chrome.tabs.query({ windowId }).catch(() => []))
        tabs.add(tab.id);
    for (const tabId of tabs)
      await chrome.tabs.setZoom(tabId, 0).catch(() => {});
    for (const windowId of reflowed.popups)
      await chrome.windows.update(windowId, { ...this.bounds, state: 'normal' }).catch(() => {});
    await chrome.windows.update(this.windowId, { ...(this.bounds ?? reflowed.saved), state: 'normal' }).catch(() => {});
  }

  async open({ relayUrl, bounds, label }) {
    const options = { url: 'about:blank', type: 'normal', focused: false };
    this.minimize = !bounds;
    if (!bounds) options.state = 'minimized';
    if (bounds) {
      options.left = Math.round(bounds.left);
      options.top = Math.round(bounds.top);
      options.width = Math.round(bounds.width);
      options.height = Math.round(bounds.height);
    }
    // A new window can take the focus even when asked not to (some desktops
    // give it to any window that appears). If the person's Chrome window had
    // it, hand it straight back; if Chrome wasn't the app they were using,
    // nothing is touched.
    const before = await chrome.windows.getLastFocused().catch(() => undefined);
    startCreating();
    let window;
    try {
      window = await chrome.windows.create(options);
      this.windowId = window.id;
      if (bounds) {
        // A new window ignores the position it is created at on a screen with
        // no window manager; moving it does not.
        this.bounds = options_bounds(bounds);
        await chrome.windows.update(window.id, { ...this.bounds, state: 'normal' }).catch(() => {});
      }
      agentWindowIds.add(window.id);
      if (this.minimize) minimizeAgentWindows.add(window.id);
      createdAt.set(window.id, Date.now());
    } finally {
      doneCreating();
    }
    // The window the person was in: the one known to be theirs, or else the
    // one that had the focus before, if it is not another agent's window.
    if (personWindowId === undefined && before?.focused && !agentWindowIds.has(before.id))
      personWindowId = before.id;
    if (personWindowId !== undefined)
      await chrome.windows.update(personWindowId, { focused: true }).catch(() => {});
    const tab = window.tabs?.[0] ?? (await chrome.tabs.query({ windowId: window.id }))[0];
    await this._group(tab, label);
    chrome.tabs.onCreated.addListener(this._onCreated);
    chrome.tabs.onUpdated.addListener(this._onUpdated);
    chrome.windows.onRemoved.addListener(this._onRemoved);
    chrome.tabs.onActivated.addListener(this._onActivated);
    try {
      this.connection = await openRelayConnection(relayUrl, this);
    } catch (error) {
      await this.close(error.message, true);
      throw error;
    }
    this.connection.onclose = () => void this.close('The agent finished');
    this.connection.attachTab(tab);
    this.connection.didInitialize();
  }

  // quiet: the open failed, and the failure itself is what the daemon hears.
  // The window's first tab goes in a group titled with the Worker's name.
  async _group(tab, label) {
    try {
      this.groupId = await chrome.tabs.group({ tabIds: [tab.id], createProperties: { windowId: this.windowId } });
      await chrome.tabGroups.update(this.groupId, { title: String(label || 'Mewla agent').slice(0, 40), color: groupColor(label) });
    } catch (error) {
      debugLog('tab group', error);
    }
  }

  /**
   * The person's input from Mewla's Screen, to this window's page: a DevTools Input command sent through the
   * debugger its tab is attached with, so it reaches that tab only, wherever the window is and whichever app is in
   * front. A mouse event comes in points from the window's top left and goes to the page at the same place.
   */
  async input({ method, params } = {}) {
    if (!INPUT_METHODS.has(method) || this.windowId === undefined || !this.connection)
      return;
    const [tab] = await chrome.tabs.query({ windowId: this.windowId, active: true }).catch(() => []);
    if (!tab || !this.connection.attachedTabs.has(tab.id))
      return;
    const target = { tabId: tab.id };
    const event = { ...params };
    if (method === 'Input.dispatchMouseEvent') {
      // What lies above and beside the page in the window: its tabs, toolbar and any infobar.
      const offset = await chrome.debugger.sendCommand(target, 'Runtime.evaluate', {
        expression: '[window.outerWidth - window.innerWidth, window.outerHeight - window.innerHeight]',
        returnByValue: true,
      }).catch(() => undefined);
      const [left, top] = offset?.result?.value ?? [0, 0];
      event.x = Number(event.x) - left;
      event.y = Number(event.y) - top;
      if (event.y < 0 || event.x < 0)
        return; // on the window's own controls, not the page
    }
    await chrome.debugger.sendCommand(target, method, event).catch(error => debugLog('input', error));
  }

  async close(reason, quiet) {
    if (this.closed)
      return;
    this.closed = true;
    chrome.tabs.onCreated.removeListener(this._onCreated);
    chrome.tabs.onUpdated.removeListener(this._onUpdated);
    chrome.windows.onRemoved.removeListener(this._onRemoved);
    chrome.tabs.onActivated.removeListener(this._onActivated);
    this.connection?.close(reason);
    sessions.delete(this.id);
    agentWindowIds.delete(this.windowId);
    minimizeAgentWindows.delete(this.windowId);
    drivenAt.delete(this.windowId);
    createdAt.delete(this.windowId);
    if (this.windowId !== undefined)
      await chrome.windows.remove(this.windowId).catch(() => {});
    if (!quiet)
      tell({ type: 'closed', id: this.id, reason });
    reloadWhenIdle();
  }
}

async function onHostMessage(message) {
  switch (message?.type) {
    case 'connect': {
      if (sessions.has(message.id)) {
        tell({ type: 'error', id: message.id, message: 'already connected' });
        return;
      }
      const session = new AgentWindow(message.id);
      session.worker = String(message.worker || '');
      sessions.set(message.id, session);
      try {
        await session.open(message);
        tell({ type: 'connected', id: message.id, windowId: session.windowId });
      } catch (error) {
        sessions.delete(message.id);
        tell({ type: 'error', id: message.id, message: String(error?.message || error) });
      }
      return;
    }
    case 'role':
      // The daemon is back: windows kept while it was gone carry on.
      agentBrowser = !!message.agent;
      clearTimeout(keepTimer);
      keepTimer = undefined;
      return;
    case 'disconnect':
      await sessions.get(message.id)?.close('The daemon asked to disconnect');
      return;
    case 'input':
      await sessions.get(message.id)?.input(message.input);
      return;
    case 'reflow': {
      const session = sessions.get(message.id);
      if (session)
        await session.reflow(message);
      else
        tell({ type: 'reflowed', id: message.id, req: message.req, message: 'the agent window is gone' });
      return;
    }
    case 'ping':
      tell({ type: 'pong', sessions: [...sessions.keys()], version: chrome.runtime.getManifest().version });
      return;
    case 'reload':
      // The daemon carries a newer extension than this one. Unpacked, its files on disk are the new
      // ones and a reload is the update; from the store, the store updates it and a reload changes
      // nothing (the daemon asks once per version, so that does not repeat). Never while an agent
      // has a window open: a reload would close it.
      if (isNewer(message.version, chrome.runtime.getManifest().version)) {
        reloadForVersion = message.version;
        reloadWhenIdle();
      }
      return;
  }
}

let reloadForVersion;
function reloadWhenIdle() {
  if (reloadForVersion === undefined || sessions.size > 0)
    return;
  reloadForVersion = undefined;
  chrome.runtime.reload();
}

/** Whether dotted version `a` is newer than `b`; anything that is not numbers is not. */
function isNewer(a, b) {
  const parse = v => String(v ?? '').split('.').map(Number);
  const x = parse(a), y = parse(b);
  if (x.some(n => !Number.isInteger(n) || n < 0) || y.some(n => !Number.isInteger(n) || n < 0))
    return false;
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const p = x[i] ?? 0, q = y[i] ?? 0;
    if (p !== q)
      return p > q;
  }
  return false;
}

function connectHost() {
  try {
    port = chrome.runtime.connectNative(HOST);
  } catch (error) {
    scheduleReconnect();
    return;
  }
  port.onMessage.addListener(message => void onHostMessage(message));
  port.onDisconnect.addListener(() => {
    const error = chrome.runtime.lastError?.message;
    debugLog('native host disconnected', error);
    port = undefined;
    // Nobody holds a window for a phone while Mewla is gone: put laid-out windows back for the agents.
    for (const session of sessions.values())
      void session._restore().catch(() => {});
    if (agentBrowser) {
      // Mewla is restarting or updating: the agents' windows keep working, for a while.
      keepTimer ??= setTimeout(() => {
        keepTimer = undefined;
        for (const session of [...sessions.values()])
          void session.close('Mewla did not come back');
      }, AGENT_KEEP_MS);
    } else {
      for (const session of [...sessions.values()])
        void session.close('The Mewla daemon disconnected');
    }
    scheduleReconnect();
  });
  const windows = [...sessions.values()]
    .filter(session => session.connection && !session.closed)
    .map(session => ({ id: session.id, worker: session.worker, windowId: session.windowId }));
  tell({ type: 'hello', version: chrome.runtime.getManifest().version, windows });
  backoff = 1000;
}

function scheduleReconnect() {
  // Chrome starts the host with the daemon's `mewla agent-use native-host`;
  // when Mewla isn't installed or isn't running the host exits at once. Keep
  // trying, slowly.
  setTimeout(connectHost, backoff);
  backoff = Math.min(backoff * 2, 60_000);
}

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type === 'status' && sender.id === chrome.runtime.id)
    sendResponse({ connected: !!port, sessions: sessions.size });
});
chrome.runtime.onStartup.addListener(() => {});
chrome.runtime.onInstalled.addListener(() => {});
connectHost();
