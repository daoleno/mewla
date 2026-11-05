/**
 * Mewla for Chrome: lets the agents Mewla starts on this computer work in a
 * window of their own in this Chrome, with its logged-in sites.
 *
 * The daemon asks over Chrome's native messaging (only this extension's ID is
 * allowed by the host manifest, so there is no key to copy or leak):
 *   {type: "connect", id, relayUrl, client, bounds?: {left, top, width, height}}
 * and gets back {type: "connected" | "closed" | "error", id, ...}.
 * Each connection gets a new, unfocused window; the person's own windows and
 * tabs are never attached, moved, focused or listed. When the agent is done
 * its window closes with its tabs.
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
chrome.windows.onFocusChanged.addListener(async windowId => {
  if (windowId === chrome.windows.WINDOW_ID_NONE)
    return;
  if (!agentWindowIds.has(windowId)) {
    personWindowId = windowId;
    return;
  }
  if (personWindowId !== undefined) {
    await chrome.windows.update(personWindowId, { focused: true }).catch(() => {});
  }
});

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
    this.connection = undefined;
    this.closed = false;
    this._onUpdated = (tabId, changeInfo, tab) => {
      if (tab.windowId !== this.windowId || this.connection?.attachedTabs.has(tabId))
        return;
      if (changeInfo.url !== undefined && !isNonDebuggableUrl(changeInfo.url))
        this.connection?.attachTab(tab);
    };
    this._onRemoved = windowId => {
      if (windowId === this.windowId)
        this.close('The agent window was closed');
    };
  }

  async open({ relayUrl, bounds }) {
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
    const window = await chrome.windows.create(options);
    this.windowId = window.id;
    agentWindowIds.add(window.id);
    if (before?.focused && personWindowId === undefined)
      personWindowId = before.id;
    if (before?.focused && before.id !== window.id)
      await chrome.windows.update(before.id, { focused: true }).catch(() => {});
    const tab = window.tabs?.[0] ?? (await chrome.tabs.query({ windowId: window.id }))[0];
    chrome.tabs.onUpdated.addListener(this._onUpdated);
    chrome.windows.onRemoved.addListener(this._onRemoved);
    try {
      this.connection = await openRelayConnection(relayUrl, this);
    } catch (error) {
      await this.close(error.message);
      throw error;
    }
    this.connection.onclose = () => void this.close('The agent finished');
    this.connection.attachTab(tab);
    this.connection.didInitialize();
  }

  async close(reason) {
    if (this.closed)
      return;
    this.closed = true;
    chrome.tabs.onUpdated.removeListener(this._onUpdated);
    chrome.windows.onRemoved.removeListener(this._onRemoved);
    this.connection?.close(reason);
    sessions.delete(this.id);
    agentWindowIds.delete(this.windowId);
    if (this.windowId !== undefined)
      await chrome.windows.remove(this.windowId).catch(() => {});
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
        tell({ type: 'error', id: message.id, message: error.message });
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

chrome.runtime.onStartup.addListener(() => {});
chrome.runtime.onInstalled.addListener(() => {});
connectHost();
