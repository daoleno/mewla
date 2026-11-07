/**
 * Copyright (c) Microsoft Corporation.
 * Licensed under the Apache License, Version 2.0 (see LICENSE-NOTICE.md).
 *
 * Derived from the Playwright Extension's relayConnection.ts, in plain
 * JavaScript. Changed: tabs the relay creates go to the agent's own window.
 */

// When each agent window was last driven by its agent (any command it sent): a
// focus change to that window right after one is the agent's doing.
export const drivenAt = new Map();

export function debugLog(...args) {
  console.log('[Mewla]', ...args);
}

// The chrome.* commands the Playwright MCP server may invoke, and the events
// forwarded to it.
const ALLOWED_CHROME_COMMANDS = new Set([
  'chrome.debugger.attach',
  'chrome.debugger.detach',
  'chrome.debugger.sendCommand',
  'chrome.tabs.create',
  'chrome.tabs.remove',
]);
const CHROME_EVENT_METHODS = [
  'chrome.debugger.onEvent',
  'chrome.debugger.onDetach',
  'chrome.tabs.onCreated',
  'chrome.tabs.onRemoved',
];
const REATTACH_DELAY_MS = 150;
const REATTACH_VERIFY_MS = 2500;
const REATTACH_COOLDOWN_MS = 3000;

export class RelayConnection {
  /** @param {WebSocket} ws @param {{windowId: number}} home the agent's window: tabs the server opens go there */
  constructor(ws, home) {
    this._ws = ws;
    this._home = home;
    this.attachedTabs = new Set();
    this._hasEverAttached = false;
    this._eventListeners = [];
    this._closed = false;
    this._pendingReattach = new Set();
    this._recentReattach = new Set();
    this.onclose = undefined;
    this.ontabattached = undefined;
    this.ontabdetached = undefined;
    this._installEventForwarders();
    this._ws.onmessage = event => this._onMessage(event);
    this._ws.onclose = () => this._onClose();
  }

  didInitialize() {
    this._sendMessage({ method: 'extension.initialized', params: [] });
  }

  close(message) {
    try { this._ws.close(1000, message); } catch {}
    this._onClose();
  }

  attachTab(tab) {
    if (this._closed || this.attachedTabs.has(tab.id))
      return;
    this._sendMessage({ method: 'chrome.tabs.onCreated', params: [tab] });
  }

  detachTab(tabId) {
    if (this._closed || !this.attachedTabs.has(tabId))
      return;
    detachDebugger(tabId);
    this._notifyTabDetached(tabId);
    this._sendMessage({ method: 'chrome.debugger.onDetach', params: [{ tabId }, 'target_closed'] });
    this._checkLastTabDetached();
  }

  _notifyTabAttached(tabId) {
    this.attachedTabs.add(tabId);
    this._hasEverAttached = true;
    this._pendingReattach.delete(tabId);
    this.ontabattached?.(tabId);
  }

  _notifyTabDetached(tabId) {
    this.attachedTabs.delete(tabId);
    this.ontabdetached?.(tabId);
  }

  _installEventForwarders() {
    for (const fullMethod of CHROME_EVENT_METHODS) {
      const target = resolveChromeMember(fullMethod);
      const listener = (...args) => this._onChromeEvent(fullMethod, args);
      target.obj[target.name].addListener(listener);
      this._eventListeners.push({ remove: () => target.obj[target.name].removeListener(listener) });
    }
  }

  _onClose() {
    if (this._closed)
      return;
    this._closed = true;
    this._pendingReattach.clear();
    this._recentReattach.clear();
    for (const l of this._eventListeners)
      l.remove();
    this._eventListeners = [];
    for (const tabId of [...this.attachedTabs]) {
      detachDebugger(tabId);
      this._notifyTabDetached(tabId);
    }
    this.onclose?.();
  }

  _checkLastTabDetached() {
    if (this._hasEverAttached && this.attachedTabs.size === 0 && this._pendingReattach.size === 0)
      this.close('All controlled tabs detached');
  }

  _onChromeEvent(fullMethod, args) {
    const tabId = tabIdForEventArgs(fullMethod, args);
    if (tabId === undefined || !this.attachedTabs.has(tabId))
      return;
    this._sendMessage({ method: fullMethod, params: args });
    if (fullMethod === 'chrome.debugger.onDetach') {
      this._notifyTabDetached(tabId);
      if (args[1] === 'target_closed' && this._maybeScheduleReattach(tabId))
        return;
      this._checkLastTabDetached();
    }
  }

  _maybeScheduleReattach(tabId) {
    if (this._closed)
      return false;
    if (this._recentReattach.has(tabId)) {
      debugLog(`Not re-attaching tab ${tabId}: re-detached within ${REATTACH_COOLDOWN_MS}ms`);
      return false;
    }
    this._recentReattach.add(tabId);
    setTimeout(() => this._recentReattach.delete(tabId), REATTACH_COOLDOWN_MS);
    this._pendingReattach.add(tabId);
    setTimeout(() => void this._tryReattach(tabId), REATTACH_DELAY_MS);
    return true;
  }

  _reattachAborted(tabId) {
    return this._closed || !this._pendingReattach.has(tabId);
  }

  async _tryReattach(tabId) {
    if (this._reattachAborted(tabId))
      return;
    let tab;
    try {
      tab = await chrome.tabs.get(tabId);
    } catch {
      this._pendingReattach.delete(tabId);
      this._checkLastTabDetached();
      return;
    }
    if (this._reattachAborted(tabId))
      return;
    if (this.attachedTabs.has(tabId)) {
      this._pendingReattach.delete(tabId);
      return;
    }
    this.attachTab(tab);
    setTimeout(() => {
      if (this._reattachAborted(tabId))
        return;
      this._pendingReattach.delete(tabId);
      if (!this.attachedTabs.has(tabId))
        this._checkLastTabDetached();
    }, REATTACH_VERIFY_MS);
  }

  async _onMessage(event) {
    let message;
    try {
      message = JSON.parse(event.data);
    } catch (error) {
      this._sendMessage({ error: { code: -32700, message: `Error parsing message: ${error.message}` } });
      return;
    }
    const response = { id: message.id };
    try {
      response.result = await this._handleCommand(message);
    } catch (error) {
      debugLog('Error handling command', message.method, error);
      response.error = error.message;
    }
    this._sendMessage(response);
  }

  async _handleCommand(message) {
    if (!ALLOWED_CHROME_COMMANDS.has(message.method))
      throw new Error(`Unknown method: ${message.method}`);
    const args = [...(message.params ?? [])];
    drivenAt.set(this._home.windowId, Date.now());
    if (message.method === 'chrome.tabs.create') {
      // The server opens its tabs in the agent's window, never the person's.
      args[0] = { ...(args[0] ?? {}), windowId: this._home.windowId };
      // A window kept out of the way stays so: a tab made the active one brings the window up.
      if (this._home.minimize) args[0].active = false;
    }
    const result = await invokeChromeMethod(message.method, args);
    if (message.method === 'chrome.debugger.attach') {
      const target = args[0];
      if (target?.tabId !== undefined) {
        if (this._closed)
          detachDebugger(target.tabId);
        else {
          // The page believes it has the focus without the window taking it
          // from the person's app: clicks and typing then don't change which
          // window or app is in front.
          await chrome.debugger.sendCommand(target, 'Emulation.setFocusEmulationEnabled', { enabled: true }).catch(() => {});
          this._notifyTabAttached(target.tabId);
        }
      }
    }
    return result ?? {};
  }

  _sendMessage(message) {
    if (this._ws.readyState === WebSocket.OPEN)
      this._ws.send(JSON.stringify(message));
  }
}

function tabIdForEventArgs(fullMethod, args) {
  switch (fullMethod) {
    case 'chrome.debugger.onEvent':
    case 'chrome.debugger.onDetach':
      return args[0]?.tabId;
    case 'chrome.tabs.onCreated':
      // Only popups opened by an attached tab are forwarded.
      return args[0].openerTabId;
    case 'chrome.tabs.onRemoved':
      return args[0];
  }
  return undefined;
}

function resolveChromeMember(fullMethod) {
  const parts = fullMethod.split('.');
  if (parts[0] !== 'chrome' || parts.length < 3)
    throw new Error(`Invalid chrome method: ${fullMethod}`);
  let obj = chrome;
  for (let i = 1; i < parts.length - 1; i++) {
    obj = obj?.[parts[i]];
    if (obj === undefined)
      throw new Error(`Unknown chrome path: ${parts.slice(0, i + 1).join('.')}, calling ${fullMethod}`);
  }
  return { obj, name: parts[parts.length - 1] };
}

function detachDebugger(tabId) {
  chrome.debugger.detach({ tabId }).catch(error => debugLog('Error detaching tab:', error));
}

async function invokeChromeMethod(fullMethod, args) {
  const { obj, name } = resolveChromeMember(fullMethod);
  if (typeof obj[name] !== 'function')
    throw new Error(`Not a function: ${fullMethod}`);
  return await obj[name].apply(obj, args);
}

export async function openRelayConnection(url, home) {
  const started = Date.now();
  const where = (() => { try { return new URL(url).host; } catch { return 'the relay'; } })();
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      socket.close();
      reject(new Error(`No answer from the relay at ${where} after ${Date.now() - started} ms (socket state ${socket.readyState})`));
    }, 10000);
    socket.onopen = () => { clearTimeout(timer); resolve(); };
    socket.onerror = () => {
      clearTimeout(timer);
      reject(new Error(`The relay at ${where} refused the connection after ${Date.now() - started} ms (socket state ${socket.readyState})`));
    };
    socket.onclose = event => {
      clearTimeout(timer);
      reject(new Error(`The relay at ${where} closed the connection (code ${event.code}${event.reason ? ', ' + event.reason : ''}) after ${Date.now() - started} ms`));
    };
  });
  socket.onclose = null;
  return new RelayConnection(socket, home);
}
