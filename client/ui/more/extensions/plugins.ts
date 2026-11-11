import { cleanup, Clipboard, effect, host, lifecycle, openUrl, signal, untracked } from "nsh";
import { ConnectionsFlow, initialFlowState, type FlowState } from "../../../core/extensions/connectionsFlow";
import type { ConnectionResponse } from "../../../core/extensions/connections";
import { isPluginReturnUrl, retainPluginReturn } from "../../../core/extensions/pluginOnboarding";
import { requestConnections } from "../../../core/extensions/requests";
import { platform } from "../../../core/platform";
import { servers } from "../../session";
import { daemon, daemonState } from "../daemon";

/**
 * The Plugins of the current computer: one connect flow per computer, made when
 * it becomes current and dropped when another does. A sign-in's return link
 * (`mewla://plugins?state=…`) reaches the flow it belongs to; one for a computer
 * that is not current is kept for it, and Extensions says so.
 */

export const flowState = signal<FlowState>(initialFlowState());
/** The computer a return was kept for while another one was current. */
export const deferredFor = signal<string | null>(null);
let deferredId: string | null = null;

let flow: ConnectionsFlow | null = null;
/** A return link that came before the computer it belongs to was ready (a cold start). */
let earlyReturn: string | null = null;

export function plugins(): ConnectionsFlow | null {
  return flow;
}

/**
 * How a sign-in's return opens Extensions (More, then Extensions), so the card
 * that finishes it is on screen. The app's shell sets it.
 */
let openExtensions: (() => void) | null = null;
export function setOpenExtensions(open: (() => void) | null): void {
  openExtensions = open;
}

/** A socket is offline for a moment on every start; only an offline that lasts means the computer can't be reached. */
const OFFLINE_SETTLE_MS = 1_500;
export const offlineSettled = signal(false);
effect(() => {
  const state = daemonState();
  untracked(() => offlineSettled.set(false));
  // Still connecting is not offline yet.
  if (state === "connected" || state === "connecting") return;
  const timer = setTimeout(() => offlineSettled.set(true), OFFLINE_SETTLE_MS);
  cleanup(() => clearTimeout(timer));
});

// The computer a kept return belongs to is current now: it finishes there, so the notice goes.
effect(() => {
  const id = daemon()?.server.id ?? null;
  untracked(() => {
    if (deferredId && deferredId === id) {
      deferredId = null;
      deferredFor.set(null);
    }
  });
});

const secure = {
  get: (key: string) => platform().secure.getItem(key),
  set: (key: string, value: string) => platform().secure.setItem(key, value),
  remove: (key: string) => platform().secure.removeItem(key),
};

effect(() => {
  const current = daemon();
  untracked(() => {
    flow?.dispose();
    flow = null;
    flowState.set(initialFlowState());
    if (!current) return;
    const next = new ConnectionsFlow({
      serverId: current.server.id,
      request: (request) => requestConnections(current, request),
      store: secure,
      openUrl: (url) => openUrl(url).then(() => undefined),
      copy: (text) => Clipboard.writeText(text),
      now: () => Date.now(),
      every: (ms, run) => {
        const timer = setInterval(run, ms);
        return () => clearInterval(timer);
      },
      active: () => lifecycle() === "active",
    });
    flow = next;
    next.onChange((state) => flowState.set(state));
    const stop = current.on("connections_changed", (frame) => {
      const connections = frame.connections as ConnectionResponse | undefined;
      if (connections && typeof connections === "object") next.apply(connections);
    });
    const stopState = current.onState((state) => {
      if (state === "connected") {
        void next.list();
        void next.check();
      }
    });
    const dispose = next.dispose.bind(next);
    next.dispose = () => {
      stop();
      stopState();
      dispose();
    };
    if (current.state === "connected") void next.list();
    const initial = earlyReturn;
    earlyReturn = null;
    void next.restore(initial);
  });
});

// Back in front: a sign-in left waiting may have finished in the browser.
effect(() => {
  if (lifecycle() === "active") untracked(() => void flow?.check());
});

host.on<{ url?: string }>("links.open", (payload) => {
  const url = payload?.url;
  if (!url || !isPluginReturnUrl(url) && url !== "mewla://plugins") return;
  openExtensions?.();
  if (flow?.handleLink(url)) return;
  if (!flow) {
    earlyReturn = url;
    return;
  }
  // Not this computer's sign-in: keep it for the computer it belongs to.
  const others = servers().map((server) => server.id).filter((id) => id !== daemon()?.server.id);
  void retainPluginReturn(others, url, { getItemAsync: secure.get, setItemAsync: secure.set }).then((serverId) => {
    if (!serverId || serverId === daemon()?.server.id) return;
    deferredId = serverId;
    deferredFor.set(servers().find((server) => server.id === serverId)?.name ?? "another computer");
  });
});

/** Whether the list is still on its way (the computer has not answered it yet, and may still). */
export function catalogLoading(): boolean {
  const state = flowState();
  return !state.catalog.length && (state.running === "list" || (!state.loaded && !offlineSettled()));
}

/** Why there is no list: the computer's refusal, or a computer that stayed out of reach. */
export function catalogProblem(): string {
  const state = flowState();
  if (state.catalog.length) return "";
  return state.error || (!state.loaded && offlineSettled() ? "Can't reach your computer." : "");
}
