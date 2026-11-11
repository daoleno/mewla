import { accountSignInInput, connectsWithForm, enabledRequest, reconnectsWithWrites, toolPolicyRequest, toolUpdatesRequest, withToolsAllowed, type AccountRecoveryAction } from "./pluginConnectionsModel";
import type { ConnectionRequest, ConnectionResponse, PluginAccount, PluginIntegration } from "./connections";
import { connectInput, finishPluginReturn, isPluginCallbackUrl, matchesPluginReturn, pendingConnectionKey, pluginReturnError, type ConnectPhase, type PendingConnection } from "./pluginOnboarding";

/**
 * The Plugins of one computer: its catalog and accounts, the one sign-in in
 * progress and how it ended. Framework-free; the page wraps it in a signal and
 * makes a new one when the current computer changes, so nothing of one computer
 * reaches another.
 *
 * A sign-in opens in the system browser and comes back on `mewla://plugins?state=…`
 * (or is polled, for a code or an address to paste). The flow is kept in secure
 * storage so a return after the app was killed still finishes it.
 */

export interface FlowStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

export interface FlowDeps {
  serverId: string;
  /** One `connections` request; a refusal by the computer rejects with an Error carrying `code`. */
  request(request: ConnectionRequest): Promise<ConnectionResponse>;
  store: FlowStore;
  openUrl(url: string): void | Promise<void>;
  copy(text: string): void | Promise<void>;
  now(): number;
  /** Repeats `run` every `ms` until the returned stop is called. */
  every(ms: number, run: () => void): () => void;
  /** Whether the app is in front (polling waits while it is not). */
  active(): boolean;
  /**
   * In a browser: the page's origin, which `connect_start` sends so the
   * provider returns this tab to the web UI's `/plugins` (`web_return`), and
   * how the sign-in page opens in this tab. Absent in the apps, whose sign-in
   * returns on `mewla://plugins`.
   */
  web?: WebDeps;
}

export interface WebDeps {
  origin(): Promise<string | null> | string | null;
  openHere(url: string): void | Promise<void>;
}

/** How a web sign-in ended, as the computer's callback sent the tab back: `/plugins?connected=<account>` or `?plugin_error=<step>`, with `service=`. */
export interface WebReturn {
  connected?: string;
  plugin_error?: string;
  service?: string;
}

export interface FlowState {
  catalog: PluginIntegration[];
  accounts: PluginAccount[];
  /** Accounts with their tools and history, for the Tools page. */
  details: Record<string, PluginAccount>;
  loaded: boolean;
  /** The request in flight (`connect:github`, `enabled:<id>`…), so only the control that started it spins. */
  running: string | null;
  /** A request that failed outside a sign-in. */
  error: string;
  phase: ConnectPhase;
  /** The service the sign-in is (or was last) for. */
  service: string | null;
  connectError: string;
  connectedId: string | null;
  flow: PendingConnection | null;
}

export type ConnectOptions = {
  writes?: boolean;
  input?: NonNullable<ConnectionRequest["input"]>;
  /** False for a custom service reached with a token or none: it connects without a sign-in. */
  signIn?: boolean;
};

const POLL_MS = 3_000;

export function initialFlowState(): FlowState {
  return { catalog: [], accounts: [], details: {}, loaded: false, running: null, error: "", phase: "idle", service: null, connectError: "", connectedId: null, flow: null };
}

export class ConnectionsFlow {
  state: FlowState = initialFlowState();
  private listeners = new Set<(state: FlowState) => void>();
  private alive = true;
  private mutation = false;
  private completion = false;
  private starting = false;
  private openingBrowser = false;
  private checking: Promise<void> | null = null;
  private attempt = 0;
  private stopPoll: (() => void) | null = null;
  private readonly key: string;

  constructor(private readonly deps: FlowDeps) {
    this.key = pendingConnectionKey(deps.serverId);
  }

  onChange(listener: (state: FlowState) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** The computer is no longer current: nothing it answers changes anything. */
  dispose(): void {
    this.alive = false;
    this.stopPoll?.();
    this.stopPoll = null;
    this.listeners.clear();
  }

  private set(patch: Partial<FlowState>): void {
    if (!this.alive) return;
    this.state = { ...this.state, ...patch };
    this.syncPolling();
    for (const listener of this.listeners) listener(this.state);
  }

  /** Polls while a sign-in waits on the browser (not while a CLI waits to be installed). */
  private syncPolling(): void {
    const wants = this.state.phase === "waiting" && !!this.state.flow && !this.state.flow.flow.install_required;
    if (wants && !this.stopPoll) this.stopPoll = this.deps.every(POLL_MS, () => { if (this.deps.active()) void this.check(); });
    else if (!wants && this.stopPoll) { this.stopPoll(); this.stopPoll = null; }
  }

  private async remember(value: PendingConnection | null): Promise<void> {
    this.set({ flow: value });
    if (value) await this.deps.store.set(this.key, JSON.stringify(value)).catch(() => {});
    else await this.deps.store.remove(this.key).catch(() => {});
  }

  /** What any answer says about the catalog and accounts; a broadcast `connections_changed` too. */
  apply(response: ConnectionResponse): void {
    if (!this.alive) return;
    const patch: Partial<FlowState> = {};
    if (response.catalog) { patch.catalog = response.catalog; patch.loaded = true; }
    let accounts = response.accounts ?? this.state.accounts;
    if (response.account) {
      const next = response.account;
      accounts = [...accounts.filter((item) => item.id !== next.id), { ...next, tools: undefined }];
      if (next.tools) patch.details = { ...this.state.details, [next.id]: next };
    }
    if (response.accounts || response.account) patch.accounts = accounts;
    this.set(patch);
  }

  /** Sends one request at a time, marking `key` as running. Resolves undefined when refused or failed. */
  async send(request: ConnectionRequest, key?: string): Promise<ConnectionResponse | undefined> {
    if (!this.alive || this.mutation) return;
    this.mutation = true;
    this.set({ running: key ?? request.action, error: "" });
    try {
      const result = await this.deps.request(request);
      if (!this.alive) return;
      this.apply(result);
      return result;
    } catch (failure) {
      // A failed first list still ends loading; the list shows the error.
      this.set({ error: failure instanceof Error ? failure.message : "Could not reach this server. Try again.", ...(request.action === "list" ? { loaded: true } : {}) });
    } finally {
      this.mutation = false;
      this.set({ running: null });
    }
  }

  list(): Promise<ConnectionResponse | undefined> {
    return this.send({ action: "list" });
  }

  private fail(message: string, status: "failed" | "cancelled" = "failed"): void {
    this.set({ phase: status, connectError: message });
  }

  private async settle(result: ConnectionResponse): Promise<void> {
    if (!this.alive) return;
    this.apply(result);
    const flow = result.flow;
    if (flow?.status === "connected" && result.account) {
      this.set({ service: flow.integration, connectedId: result.account.id, phase: "connected", connectError: "" });
      await this.remember(null);
    } else if (flow?.status === "failed" || flow?.status === "cancelled") {
      this.fail(flow.message ?? "Connection cancelled. No new account was connected.", flow.status);
      await this.remember(null);
    } else if (flow?.status === "waiting") {
      // A waiting flow can carry why its last check didn't get through; the next check is in a few seconds.
      this.set({ phase: "waiting", connectError: flow.message ?? "" });
      if (this.state.flow) await this.remember({ ...this.state.flow, flow });
    }
  }

  /** A `mewla://plugins?state=…` return for this flow. */
  async finish(url: string): Promise<void> {
    const active = this.state.flow;
    if (!active || !this.alive || !matchesPluginReturn(url, active.flow)) return;
    if (this.completion) { await this.remember({ ...active, callback: url }); return; }
    this.completion = true;
    this.set({ phase: "verifying", connectError: "" });
    try {
      await this.remember({ ...active, callback: url });
      const response = await finishPluginReturn(active, this.deps.serverId, url, (_id, request) => this.deps.request(request));
      await this.settle(response);
    } catch (failure) {
      this.fail(failure instanceof Error ? failure.message : "Could not verify authorization. Try again.");
    } finally {
      this.completion = false;
    }
  }

  /**
   * A link the system opened the app with. True when it was this flow's return
   * (or the bare callback), so nothing else acts on it.
   */
  handleLink(url: string): boolean {
    const active = this.state.flow;
    if (active && matchesPluginReturn(url, active.flow)) { void this.finish(url); return true; }
    if (isPluginCallbackUrl(url)) { void this.check(); return true; }
    return false;
  }

  /** Asks the computer how the waiting sign-in stands. One check at a time. */
  check(): Promise<void> {
    if (this.checking) return this.checking;
    const run = async () => {
      const active = this.state.flow;
      if (!active || !this.alive || this.completion) return;
      if (Date.parse(active.flow.expires) <= this.deps.now()) {
        this.fail("This sign-in expired. Start again.");
        await this.remember(null);
        return;
      }
      if (active.callback) { await this.finish(active.callback); return; }
      this.completion = true;
      try {
        const result = await this.deps.request({ action: "connect_status", flow_id: active.flow.id });
        if (this.state.flow?.flow.id === active.flow.id) await this.settle(result);
      } catch (failure) {
        if (this.state.flow?.flow.id !== active.flow.id) return;
        // The computer answered: it no longer has this sign-in (it expired or restarted).
        if ((failure as { code?: string }).code === "plugin_request_failed") {
          this.fail(failure instanceof Error ? failure.message : "This sign-in expired. Start again.");
          await this.remember(null);
          // It can't be asked yet (a reconnect): the sign-in still waits there.
        } else if (this.state.flow) this.set({ phase: "waiting" });
      } finally {
        this.completion = false;
        if (this.state.flow?.callback && this.alive) void this.finish(this.state.flow.callback);
      }
    };
    this.checking = run().finally(() => { this.checking = null; });
    return this.checking;
  }

  /**
   * Picks up a sign-in left waiting (the app was closed while the browser was
   * open), then checks it, or finishes it with the link the app opened with.
   */
  async restore(initialUrl?: string | null, returned?: WebReturn | null): Promise<void> {
    // A web sign-in came back through the computer's callback, which settled it there; the stored flow only confirms it.
    const settled = !!returned && !!(returned.connected || returned.plugin_error);
    if (returned && settled) {
      if (returned.connected) this.set({ service: returned.service ?? null, connectedId: returned.connected, phase: "connected", connectError: "" });
      else {
        this.set({ service: returned.service ?? null });
        this.fail(pluginReturnError(returned.plugin_error) ?? "", returned.plugin_error === "cancelled" ? "cancelled" : "failed");
      }
    }
    const raw = await this.deps.store.get(this.key).catch(() => null);
    if (!raw || !this.alive) return;
    try {
      const stored = JSON.parse(raw) as PendingConnection;
      if (stored.serverId !== this.deps.serverId || !(Date.parse(stored.flow.expires) > this.deps.now())) { await this.remember(null); return; }
      // The return ended this flow; one for another service keeps waiting.
      if (returned && settled && (!returned.service || stored.flow.integration === returned.service)) { await this.remember(null); return; }
      this.set({ flow: stored, service: stored.flow.integration, phase: stored.flow.status === "confirm" ? "idle" : "waiting" });
      if (initialUrl && matchesPluginReturn(initialUrl, stored.flow)) await this.finish(initialUrl);
      else await this.check();
    } catch {
      this.fail("The previous sign-in could not be restored. Start again.");
      await this.remember(null);
    }
  }

  async cancel(): Promise<void> {
    this.attempt++;
    const active = this.state.flow;
    if (active && this.alive) {
      try {
        const response = await this.deps.request({ action: "connect_cancel", flow_id: active.flow.id });
        if (response.account) { await this.settle(response); return; }
      } catch {
        // A pending flow expires on its computer; it cannot run.
      }
    }
    if (this.alive) {
      await this.remember(null);
      this.fail("Sign-in cancelled. Nothing was connected.", "cancelled");
    }
  }

  /**
   * Opens the sign-in page in the system browser. A code is copied first. The
   * page returns on the app's link, or (a code or an address to paste) the flow
   * is polled until the computer has it.
   */
  async openBrowser(): Promise<void> {
    const active = this.state.flow;
    // A second tap while the browser is still opening opens nothing more.
    if (!active?.flow.authorization_url || !this.alive || this.openingBrowser) return;
    this.openingBrowser = true;
    try {
      this.set({ phase: "waiting" });
      // The code is on screen too; a clipboard that refuses never keeps the page closed.
      if (active.flow.user_code) await Promise.resolve(this.deps.copy(active.flow.user_code)).catch(() => {});
      try {
        // In a browser, a provider that returns to the web UI takes this tab there (a popup would outlive its opener);
        // any other sign-in (a code, an address to paste) opens in a tab of its own while this one polls.
        if (this.deps.web && active.flow.web_return) await this.deps.web.openHere(active.flow.authorization_url);
        else await this.deps.openUrl(active.flow.authorization_url);
      } catch {
        this.fail("The browser could not open. Try again.");
        return;
      }
    } finally {
      this.openingBrowser = false;
    }
    await this.check();
  }

  /**
   * The one connect action: a new service, another account, Reconnect, or
   * allowing changes the service has to approve. Returns "form" when the
   * service brings its own details from its page instead.
   */
  async connect(serviceId: string, options: ConnectOptions = {}): Promise<"form" | void> {
    if (this.mutation || this.starting || !this.alive) return;
    if (!options.input && connectsWithForm(serviceId)) return "form";
    this.starting = true;
    try {
      if (this.state.flow) await this.cancel();
      const attempt = ++this.attempt;
      this.set({ service: serviceId, connectedId: null, phase: "opening", connectError: "" });
      const input = options.input ?? { integration: serviceId, allow_writes: !!options.writes };
      if (options.signIn === false) {
        const added = await this.send({ action: "add", input }, `connect:${serviceId}`);
        if (added?.account) this.set({ connectedId: added.account.id, phase: "connected" });
        else this.set({ phase: "idle" });
        return;
      }
      // A browser names its origin, so the sign-in comes back to this page.
      const origin = this.deps.web ? await Promise.resolve(this.deps.web.origin()).catch(() => null) : null;
      const start = connectInput(input, this.deps.web ? "web" : "native", origin ?? undefined);
      const result = await this.send({ action: "connect_start", input: start }, `connect:${serviceId}`);
      if (!result?.flow) { this.set({ phase: "idle" }); return; }
      // A profile still signed in connects without a browser.
      if (result.flow.status === "connected") { await this.settle(result); return; }
      if (attempt !== this.attempt) {
        await this.deps.request({ action: "connect_cancel", flow_id: result.flow.id }).catch(() => undefined);
        return;
      }
      await this.remember({ serverId: this.deps.serverId, flow: result.flow });
      // A code to enter, a CLI to install, or nothing to open: the card shows it first.
      if (result.flow.user_code || result.flow.install_required || !result.flow.authorization_url) this.set({ phase: "waiting" });
      else await this.openBrowser();
    } catch (failure) {
      this.fail(failure instanceof Error ? failure.message : "Could not start this connection. Try again.");
    } finally {
      this.starting = false;
    }
  }

  /** GitHub only: the account `gh` is signed in to on the computer, instead of a code. */
  async useServerGitHub(): Promise<void> {
    if (this.state.flow) await this.cancel();
    this.set({ service: "github", connectError: "" });
    const result = await this.send({ action: "github_preview" }, "connect:github");
    if (result?.flow) { await this.remember({ serverId: this.deps.serverId, flow: result.flow }); this.set({ phase: "idle" }); }
    else this.fail("No GitHub login was found on this server.");
  }

  async importGitHub(): Promise<void> {
    if (!this.state.flow || this.mutation) return;
    this.set({ phase: "verifying" });
    const result = await this.send({ action: "github_import", flow_id: this.state.flow.flow.id, input: { integration: "github" } }, "connect:github");
    if (result) await this.settle(result);
    else this.fail("GitHub couldn't be verified. Start again.");
  }

  /**
   * Hands what the person pasted (the address the browser ended on, a CLI code,
   * a client) to the waiting sign-in. Throws the computer's refusal so the field
   * keeps the text and shows why; the flow keeps waiting.
   */
  async submitAddress(address: string): Promise<void> {
    const active = this.state.flow;
    if (!active || !this.alive) return;
    // Wait for polling, but never send this to a sign-in that replaced it.
    await this.checking;
    if (this.state.flow?.flow.id !== active.flow.id || !this.alive || this.completion) return;
    this.completion = true;
    this.set({ phase: "verifying", connectError: "" });
    try {
      const result = await this.deps.request({ action: "connect_finish", flow_id: active.flow.id, callback: address });
      if (this.state.flow?.flow.id === active.flow.id) await this.settle(result);
    } catch (failure) {
      if (this.alive && this.state.flow?.flow.id === active.flow.id) {
        this.set({ phase: "waiting" });
        throw failure;
      }
    } finally {
      this.completion = false;
    }
  }

  /** Clears the outcome of the last sign-in once it has been seen. */
  dismiss(): void {
    if (!this.state.flow) this.set({ phase: "idle", connectError: "", connectedId: null, service: null });
  }

  /** Read and search, or Make changes. Allowing changes the service never granted is the connect action again. */
  setAccess(account: PluginAccount, group: "read" | "write", allowed: boolean): void {
    if (group === "write" && allowed && account.access?.write_consent) { void this.connect(account.integration, { writes: true }); return; }
    void this.send({ action: "permissions", id: account.id, group, allowed }, `${group}:${account.id}`);
  }

  async renameProfile(account: PluginAccount, name: string): Promise<boolean> {
    const response = await this.send({ action: "cloud_rename", id: account.id, profile: name }, `rename:${account.id}`);
    if (response) {
      if (this.state.connectedId === account.id && response.account) this.set({ connectedId: response.account.id });
      await this.list();
    }
    return !!response;
  }

  async disconnect(account: PluginAccount): Promise<boolean> {
    const request: ConnectionRequest = account.profile ? { action: "cloud_delete", id: account.id, confirm_profile: account.profile } : { action: "disconnect", id: account.id };
    const response = await this.send(request, `disconnect:${account.id}`);
    if (response) {
      if (this.state.connectedId === account.id) this.dismiss();
      if (account.profile) await this.list();
    }
    return !!response;
  }

  recover(account: PluginAccount, action: AccountRecoveryAction): void {
    // A cloud profile signs in again by the same flow, naming itself so it is renewed in place.
    const signIn = action === "reconnect" ? accountSignInInput(account) : null;
    if (signIn) void this.connect(account.integration, { input: signIn });
    else if (action === "refresh") void this.send({ action: "refresh", id: account.id }, `recover:${account.id}`);
    else if (action === "disconnect") void this.disconnect(account);
    else void this.connect(account.integration, { writes: reconnectsWithWrites(account) });
  }

  /** Hyperliquid: deletes the saved API wallet key and leaves the account read only. */
  async removeKey(account: PluginAccount): Promise<boolean> {
    return !!(await this.send({ action: "remove_key", id: account.id }, `key:${account.id}`));
  }

  /** The account's own switch. Off keeps its sign-in and tool choices. */
  setEnabled(account: PluginAccount, on: boolean): void {
    void this.send(enabledRequest(account, on), `enabled:${account.id}`);
  }

  refresh(account: PluginAccount): void {
    void this.send({ action: "refresh", id: account.id }, `refresh:${account.id}`);
  }

  /** One account's tools and history, for its Tools page. */
  load(id: string): Promise<ConnectionResponse | undefined> {
    return this.send({ action: "get", id }, `load:${id}`);
  }

  /** Allows or blocks tools with no confirm; the switches move at once and go back if the computer refuses. */
  setTools(target: PluginAccount, names: readonly string[], allowed: boolean): void {
    if (!names.length || this.mutation || !this.alive) return;
    const previous = this.state.details[target.id];
    this.set({ details: { ...this.state.details, [target.id]: withToolsAllowed(this.state.details[target.id] ?? target, names, allowed) } });
    void this.send(toolPolicyRequest(target, names, allowed), `tools:${target.id}`).then((result) => {
      if (!result && previous) this.set({ details: { ...this.state.details, [target.id]: previous } });
    });
  }

  confirmUpdates(account: PluginAccount, names: readonly string[]): void {
    if (names.length) void this.send(toolUpdatesRequest(account, names, true), `updates:${account.id}`);
  }

  plugin(id: string | null): PluginIntegration | null {
    return this.state.catalog.find((item) => item.id === id) ?? null;
  }

  /** How a notice names an account's service: a custom service by its own name. */
  serviceLabel(account: PluginAccount): string {
    return account.integration === "mcp" || account.integration === "openapi" ? account.name : this.plugin(account.integration)?.name ?? account.name;
  }
}
