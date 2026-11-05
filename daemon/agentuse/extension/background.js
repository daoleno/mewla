/**
 * Mewla for Chrome: lets the agents Mewla starts on this computer work in a
 * window of their own in this Chrome, with its logged-in sites.
 *
 * The daemon asks over Chrome's native messaging (only this extension's ID is
 * allowed by the host manifest, so there is no key to copy or leak):
 *   {type: "connect", id, relayUrl, client, label?, bounds?: {left, top, width, height}}
 * and gets back {type: "connected" | "closed" | "error", id, ...}.
 * Each connection gets a new, unfocused window; the person's own windows and
 * tabs are never attached, moved, focused or listed. Its tabs are in a tab
 * group named for the Worker, so the person can see whose they are, and any
 * number of Workers can work in this Chrome at once, each in its own window.
 * When the agent is done its window closes with its tabs.
 */
import { debugLog, openRelayConnection } from './relay.js';

const HOST = 'com.mewla.browser';
const NON_DEBUGGABLE_SCHEMES = ['chrome:', 'edge:', 'devtools:', 'chrome-extension:'];
const isNonDebuggableUrl = url => !!url && NON_DEBUGGABLE_SCHEMES.some(s => url.startsWith(s));

// The last of the person's own windows that had the focus. If an agent's
// window takes the focus from it (some desktops focus a window on a click it
// is sent, even with no pointer), it goes straight back.
let personWindowId;
const agentWindowIds = new Set();
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
  if (personWindowId === undefined && lostPersonWindowId !== undefined &&
      Date.now() - lostAt < 1500 && Date.now() - (createdAt.get(windowId) ?? 0) < 2500)
    personWindowId = lostPersonWindowId;
  if (personWindowId !== undefined) {
    await chrome.windows.update(personWindowId, { focused: true }).catch(() => {});
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

function tell(message) {
  try { port?.postMessage(message); } catch (error) { debugLog('host gone', error); }
}

class AgentWindow {
  constructor(id) {
    this.id = id;
    this.windowId = undefined;
    this.groupId = undefined;
    this.connection = undefined;
    this.closed = false;
    this._onUpdated = (tabId, changeInfo, tab) => {
      if (tab.windowId !== this.windowId || this.connection?.attachedTabs.has(tabId))
        return;
      if (changeInfo.url !== undefined && !isNonDebuggableUrl(changeInfo.url))
        this.connection?.attachTab(tab);
    };
    // A tab the agent's tools open in its window joins its tab group.
    this._onCreated = tab => {
      if (tab.windowId === this.windowId && this.groupId !== undefined && tab.groupId !== this.groupId)
        void chrome.tabs.group({ groupId: this.groupId, tabIds: [tab.id] }).catch(() => {});
    };
    this._onRemoved = windowId => {
      if (windowId === this.windowId)
        this.close('The agent window was closed');
    };
  }

  async open({ relayUrl, bounds, label }) {
    const options = { url: 'about:blank', type: 'normal', focused: false };
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
      agentWindowIds.add(window.id);
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

  async close(reason, quiet) {
    if (this.closed)
      return;
    this.closed = true;
    chrome.tabs.onCreated.removeListener(this._onCreated);
    chrome.tabs.onUpdated.removeListener(this._onUpdated);
    chrome.windows.onRemoved.removeListener(this._onRemoved);
    this.connection?.close(reason);
    sessions.delete(this.id);
    agentWindowIds.delete(this.windowId);
    createdAt.delete(this.windowId);
    if (this.windowId !== undefined)
      await chrome.windows.remove(this.windowId).catch(() => {});
    if (!quiet)
      tell({ type: 'closed', id: this.id, reason });
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
    case 'disconnect':
      await sessions.get(message.id)?.close('The daemon asked to disconnect');
      return;
    case 'ping':
      tell({ type: 'pong', sessions: [...sessions.keys()], version: chrome.runtime.getManifest().version });
      return;
  }
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
    for (const session of [...sessions.values()])
      void session.close('The Mewla daemon disconnected');
    scheduleReconnect();
  });
  tell({ type: 'hello', version: chrome.runtime.getManifest().version });
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
