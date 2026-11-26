import { effect, signal, Text, untracked, View, type JSX } from "nsh";
import { Menu, MenuItem } from "nsh/base";
import { Icon } from "../../kit/Icon";
import { listDetail, wide } from "../../shell/layout";
import { presentProviderError } from "../../../core/providers/errors";
import { providerEditorCanSave, providerEditorInitialBaseUrl, providerEditorInitialName, providerEditorRequiresBaseUrl, type ProvidersEditorState } from "../../../core/providers/editorModel";
import {
  advancedConnectionInput,
  connectionsForClient,
  createdConnectionFromMutation,
  enabledModelIds,
  modelSupportToggleTurnsOff,
  providerBaseUrlHostname,
  providerNameIssue,
  toggleModelSupport,
} from "../../../core/providers/presentation";
import {
  deleteProviderConnection,
  setProviderConnection,
  setProviderModels,
  testProviderConnection,
  upsertProviderConnection,
} from "../../../core/providers/requests";
import type { ProviderClient, ProviderConnection, ProviderModel, ProvidersSnapshot } from "../../../core/providers/types";
import { AGENT_ASSETS } from "../../icons/agents/assets";
import { connected, daemon } from "../daemon";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner, SwitchRow } from "../../kit/kit";
import { LEAD_SLOT, StateLine, tile, type Lead } from "../../kit/RowTile";
import { AgentIcon } from "../../icons/AgentIcon";
import { officialLoginLead, providerLead, type ProviderLead } from "../../../core/providers/lead";
import { Segmented } from "../../kit/Segmented";
import type { PageNav } from "../routes";
import { change, changing, discover, forgetProviders, locked, modelsAge, providers, refreshProviders, snapshot, warning } from "./providerState";

const CLIENTS = [
  { key: "codex", label: "Codex", mark: AGENT_ASSETS.codex },
  { key: "claude", label: "Claude Code", mark: AGENT_ASSETS.claude },
] as const;

/** Search earns its place only once the list is long enough to scan. */
export const PROVIDER_SEARCH_THRESHOLD = 6;

const query = signal("");
const rowNote = signal<Record<string, string>>({});
/** Models a connection reported on its last discovery, for the model access page. */
export const discovered = signal<Record<string, { models: ProviderModel[]; warning?: string }>>({});

function failureWords(error: unknown): string {
  return presentProviderError(error).message || "The Provider request failed. Refresh and try again.";
}

function note(id: string, text: string): void {
  rowNote.set({ ...rowNote(), [id]: text });
}

/** Where a connection points: its endpoint's host, its preset, or the agent's own endpoint. */
function whereOf(s: ProvidersSnapshot | null, c: ProviderConnection): string {
  return c.base_url ? providerBaseUrlHostname(c.base_url) : s?.presets.find((p) => p.id === c.preset_id)?.label ?? "Official endpoint";
}

/** The client a connection serves (each connection serves one). */
function clientOf(c: ProviderConnection): ProviderClient {
  return c.clients.includes("claude") ? "claude" : "codex";
}

/** What an agent uses now: the official login or one connection's name. */
function chosenName(client: ProviderClient): string {
  const id = snapshot()?.defaults[client]?.connection_id ?? "";
  return id ? snapshot()?.connections.find((c) => c.id === id)?.name ?? "Official login" : "Official login";
}

function modelCount(c: ProviderConnection): string {
  const n = enabledModelIds(snapshot(), c.id).length;
  return `${n} ${n === 1 ? "model" : "models"}`;
}

/** A slot rebuilt only when `key` says something new (as Screen's): the shown Provider's panel, made per Provider. */
function keyed<K>(key: () => K, render: (key: K) => JSX.Element): JSX.Element {
  return ((value: K) => untracked(() => render(value)))(key());
}

/** The Provider shown beside the lists on a wide pane; "" for the first one an agent uses. */
const shown = signal("");

/** The Provider the split shows: the one picked, else the one Codex uses, else Claude Code's, else the first. */
function shownId(): string {
  const s = snapshot();
  if (!s) return "";
  const ids = s.connections.map((c) => c.id);
  if (shown() && ids.includes(shown())) return shown();
  for (const client of ["codex", "claude"] as const) {
    const id = s.defaults[client]?.connection_id ?? "";
    if (id && ids.includes(id)) return id;
  }
  return ids[0] ?? "";
}

/**
 * Model Providers, design A (Magpie's Agents / Providers split): each agent once, with what it uses in a chip that
 * picks in place; every Provider once. On a pane wide enough for a list beside its detail (`listDetail()`), the
 * agents sit beside the shown Provider's page and the Providers run under both; narrower, a Provider opens its page.
 */
export function Providers(props: { nav: PageNav }) {
  let forServer = "";
  effect(() => {
    const id = daemon()?.server.id ?? "";
    const live = Boolean(daemon()) && connected();
    untracked(() => {
      if (id !== forServer) {
        forServer = id;
        forgetProviders();
        discovered.set({});
        rowNote.set({});
        query.set("");
        shown.set("");
      }
      if (live) refreshProviders();
    });
  });

  // The panel is made again only when the shown Provider changes, not on every new list (its open asks for models,
  // and that answer brings a new list).
  const shownKey = signal("");
  // The ring is the pull's answer only: the list read again after a panel asks for models stays quiet.
  const pulled = signal(false);
  effect(() => {
    if (!providers().loading) untracked(() => pulled.set(false));
  });
  effect(() => {
    const id = shownId();
    if (id !== untracked(shownKey)) shownKey.set(id);
  });

  return (
    <Page display full={listDetail()} title="Model Providers" testID="providers" refreshing={pulled() && providers().loading && Boolean(snapshot())} onRefresh={() => { pulled.set(true); refreshProviders(); if (!untracked(providers).loading) pulled.set(false); }}>
      <>
        <If when={!daemon()} else={<If when={!connected() && !snapshot()} else={<If when={!snapshot() && providers().error} else={<If when={!snapshot()} else={<View>
            <If when={locked()}><Notice testID="providers-locked" title="Refresh required" detail="Changes are paused until this list is refreshed." action="Refresh" onAction={refreshProviders} /></If>
            <If when={warning()}><Notice testID="providers-warning" title="Saved with a warning" detail={warning()} /></If>
            <If when={providers().error}><Notice tone="failed" title={presentProviderError(providers().error).title === "Providers" ? "Refresh required" : presentProviderError(providers().error).title} detail={failureWords(providers().error)} action="Refresh" onAction={refreshProviders} /></If>
            <If when={listDetail()} else={<View><Agents nav={props.nav} /><ProviderList nav={props.nav} /></View>}>
              <View testID="providers-split">
                <View class="flex-row gap-6">
                  <View class="min-w-0 flex-1"><Agents nav={props.nav} /></View>
                  <View testID="providers-shown" class="min-w-0 flex-1">
                    {keyed(shownKey, (cid) => <If when={cid}><ProviderPanel connectionId={cid} nav={props.nav} label /></If>)}
                  </View>
                </View>
                <ProviderList nav={props.nav} />
              </View>
            </If>
          </View>}><EmptyState busy title="Loading Providers" /></If>}><If when={providers().error!.kind === "unavailable"} else={<EmptyState
              testID="providers-failed"
              icon="alert"
              title={providers().error!.refreshable ? "Refresh required" : "Could not load"}
              detail={failureWords(providers().error)}
              action="Refresh"
              onAction={refreshProviders}
            />}><EmptyState icon="key" title="Unavailable" detail="Model connections are not available on this daemon." /></If></If>}><EmptyState icon="key" title="Server offline" detail="Providers are managed on the connected computer." /></If>}><EmptyState testID="providers-no-server" icon="key" title="No current server" detail="Providers are managed on the connected computer." action="Open Settings" onAction={() => props.nav.pop()} /></If>
      </>
    </Page>
  );
}

const MENU_POPUP = "rounded-[18px] bg-popover p-1.5 shadow-float";
const MENU_ITEM = "min-h-[44px] flex-row items-center gap-3 rounded-xl px-3 py-2 hover:bg-accent active:bg-accent focus:bg-accent disabled:opacity-40";

/** An agent's state as its chip's dot says it: on its Provider and ready, waiting for a key, or on its own login. */
function agentState(client: ProviderClient): { tone: "ok" | "warn" | "off"; words: string } {
  const s = snapshot();
  const id = s?.defaults[client]?.connection_id ?? "";
  if (!id) return { tone: "off", words: "official login" };
  const c = s?.connections.find((x) => x.id === id);
  if (!c?.credential_ready) return { tone: "warn", words: "key required" };
  if (s?.gateway && !s.gateway.running) return { tone: "warn", words: "gateway unavailable" };
  return { tone: "ok", words: "ready" };
}

const DOT: Record<"ok" | "warn" | "off", string> = { ok: "bg-ok", warn: "bg-warning", off: "bg-faint" };

/** Each agent with what it uses in a chip; the chip's menu picks another in place. */
function Agents(props: { nav: PageNav }) {
  return (
    <Section title="Agents">
      {CLIENTS.map((c) => <AgentRow client={c.key} nav={props.nav} />)}
    </Section>
  );
}

function AgentRow(props: { client: ProviderClient; nav: PageNav }) {
  const client = props.client;
  const label = CLIENTS.find((c) => c.key === client)!.label;
  const options = () => (snapshot() ? connectionsForClient(snapshot()!, client) : []);
  const selectedId = () => snapshot()?.defaults[client]?.connection_id ?? "";

  async function select(connection: ProviderConnection | null): Promise<void> {
    const current = daemon();
    const s = snapshot();
    if (!current || !s) return;
    if (connection && !connection.credential_ready) return props.nav.push({ name: "settings-provider-editor", client, connectionId: connection.id });
    const id = connection?.id ?? "";
    if (id === selectedId()) return;
    note(`agent:${client}`, "");
    try {
      await change(`select:${id}`, () => setProviderConnection(current, { client, connectionId: id, revision: s.revision }));
    } catch (error) {
      const kind = presentProviderError(error).kind;
      note(`agent:${client}`, kind === "busy" || kind === "offline" || kind === "conflict" ? failureWords(error) : "Could not select this Provider. Retry.");
    }
  }

  const chip = (
    <View testID={`provider-pick-${client}`} role="button" accessibilityLabel={`${label} uses ${chosenName(client)}, ${agentState(client).words}. Change`} class={`${wide() ? "max-w-[240px]" : "max-w-[156px]"} shrink-0 flex-row items-center gap-2 rounded-xl bg-muted px-3 py-2`}>
      <View testID={`provider-dot-${client}-${agentState(client).tone}`} class={`size-2 rounded-full ${DOT[agentState(client).tone]}`} />
      <Text class="shrink text-body line-clamp-1">{chosenName(client)}</Text>
      <If when={changing().startsWith("select:")} else={<Icon name="chevron-down" size={14} tone="faint" />}><Spinner size={14} /></If>
    </View>
  );

  return (
    <View>
      <Row
        testID={`provider-agent-${client}`}
        lead={{ kind: "logo", node: <AgentIcon kind={client} size={28} slot={LEAD_SLOT} /> }}
        title={label}
        trailing={
          <Menu trigger={chip} popupClass={wide() ? `${MENU_POPUP} w-[288px]` : MENU_POPUP} backdropClass="bg-scrim">
            <MenuItem testID={`provider-pick-${client}-official`} disabled={Boolean(changing())} onSelect={() => void select(null)} class={MENU_ITEM}>
              <Text class="flex-1 text-body">Official login</Text>
              <If when={selectedId() === ""}><Icon name="check" size={18} tone="ink" /></If>
            </MenuItem>
            {options().map((c) => (
              <MenuItem testID={`provider-pick-${client}-${c.id}`} disabled={Boolean(changing())} onSelect={() => void select(c)} class={MENU_ITEM}>
                <View class="min-w-0 flex-1">
                  <Text class="text-body line-clamp-1">{c.name}</Text>
                  <Text class="text-sub text-muted-foreground line-clamp-1">{c.credential_ready ? whereOf(snapshot(), c) : "Key required"}</Text>
                </View>
                <If when={selectedId() === c.id}><Icon name="check" size={18} tone="ink" /></If>
              </MenuItem>
            ))}
            <MenuItem testID={`provider-pick-${client}-add`} disabled={Boolean(changing())} onSelect={() => props.nav.push({ name: "settings-provider-editor", client })} class={MENU_ITEM}>
              <Text class="text-body">Add Provider</Text>
            </MenuItem>
          </Menu>
        }
      />
      <If when={rowNote()[`agent:${client}`]}><View class="px-4 pb-3"><Text class="text-sm text-destructive" android={{ accessibilityLiveRegion: "polite" }}>{rowNote()[`agent:${client}`]}</Text></View></If>
    </View>
  );
}

/** Every Provider once: where it points and how many models it gives. In the split a row shows it beside the agents. */
function ProviderList(props: { nav: PageNav }) {
  const all = () => snapshot()?.connections ?? [];
  const q = () => query().trim().toLowerCase();
  const list = () => all().filter((c) => !q() || `${c.name} ${whereOf(snapshot(), c)}`.toLowerCase().includes(q()));
  const showSearch = () => all().length >= PROVIDER_SEARCH_THRESHOLD || q().length > 0;
  const open = (c: ProviderConnection) => {
    if (!c.credential_ready) return props.nav.push({ name: "settings-provider-editor", client: clientOf(c), connectionId: c.id });
    if (listDetail()) return shown.set(c.id);
    props.nav.push({ name: "settings-provider", connectionId: c.id });
  };
  return (
    <View>
      <If when={showSearch()}><Section>
          <Field testID="provider-search" accessibilityLabel="Search Providers" value={query()} placeholder="Search Providers" onChange={(v) => query.set(v)} />
        </Section></If>
      <Section title="Providers" footer={list().length === 0 && q() ? "No Providers match this search." : undefined}>
        {list().map((c) => (
          <Row
            testID={`provider-${c.id}`}
            lead={lead(providerLead(c))}
            title={c.name}
            subtitle={`${whereOf(snapshot(), c)} · ${modelCount(c)}`}
            subtitleOneLine
            selected={listDetail() && shownId() === c.id}
            accessibilityLabel={`${c.name}, ${whereOf(snapshot(), c)}, for ${CLIENTS.find((k) => k.key === clientOf(c))!.label}${c.credential_ready ? "" : ", API key required"}`}
            chevron
            onPress={() => open(c)}
            trailing={
              <View class="flex-row items-center gap-2">
                <If when={listDetail() && shownId() === c.id}><View testID={`provider-${c.id}-shown`} class="size-1.5 rounded-full bg-foreground" /></If>
                <If when={!c.credential_ready}><StateLine testID={`provider-${c.id}-key`} tone="warn" label="Key required" /></If>
                <AgentIcon kind={clientOf(c)} size={16} slot={24} />
              </View>
            }
          />
        ))}
        <Row testID="provider-add" lead={tile("add")} title="Add Provider" disabled={Boolean(changing())} onPress={() => props.nav.push({ name: "settings-provider-editor", client: "codex", choose: true })} />
      </Section>
    </View>
  );
}

/** A provider's lead as the row draws it: its vendor's mark, its agent's logo, or the key on the tile. */
function lead(l: ProviderLead): Lead {
  if (l.kind === "agent") return { kind: "logo", node: <AgentIcon kind={l.agent} size={28} slot={LEAD_SLOT} /> };
  return l;
}

/** One Provider's page as its own route (a narrow pane): the panel under the Provider's name. */
export function ProviderDetail(props: { connectionId: string; nav: PageNav }) {
  const connection = () => snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null;
  return (
    <Page display title={connection()?.name ?? "Provider"} testID="provider-detail">
      <ProviderPanel connectionId={props.connectionId} nav={props.nav} onDeleted={() => props.nav.pop()} />
    </Page>
  );
}

/**
 * One Provider: where it points, its key, a test, and which of its models agents may use. Showing it asks the Provider
 * for its model list again, so a model it added since shows. `label`: under a section label with its name (the split).
 */
function ProviderPanel(props: { connectionId: string; nav: PageNav; label?: boolean; onDeleted?: () => void }) {
  const search = signal("");
  const failure = signal<{ title: string; detail: string } | null>(null);
  const tested = signal<{ tone: "ok" | "error" | "off"; label: string } | null>(null);
  const connection = () => snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null;
  const all = () => {
    const live = snapshot()?.models[props.connectionId] ?? [];
    const found = discovered()[props.connectionId]?.models ?? [];
    const byId = new Map(live.map((m) => [m.id, m]));
    for (const m of found) if (!byId.has(m.id)) byId.set(m.id, m);
    return [...byId.values()];
  };
  const models = () => {
    const q = search().trim().toLowerCase();
    return all().filter((m) => !q || m.id.toLowerCase().includes(q) || (m.display_name ?? "").toLowerCase().includes(q));
  };
  const on = () => enabledModelIds(snapshot(), props.connectionId);

  let asked = false;
  effect(() => {
    if (asked || !connection()?.credential_ready || !connected()) return;
    asked = true;
    untracked(() => {
      discover(props.connectionId)
        .then((result) => discovered.set({ ...discovered(), [props.connectionId]: { models: result.models, warning: result.discoveryWarning } }))
        .catch(() => undefined);
    });
  });

  async function test(): Promise<void> {
    const current = daemon();
    const c = connection();
    if (!current || !c) return;
    tested.set({ tone: "off", label: "Testing…" });
    try {
      const result = await testProviderConnection(current, { connectionId: c.id, client: clientOf(c) });
      tested.set({ tone: "ok", label: `${result.latencyMs} ms` });
    } catch (error) {
      tested.set({ tone: "error", label: "Failed" });
      failure.set({ title: "Test failed", detail: failureWords(error) });
    }
  }

  async function flip(model: ProviderModel): Promise<void> {
    const current = daemon();
    if (!current) return;
    const turnsOff = modelSupportToggleTurnsOff(snapshot(), props.connectionId, model.id);
    if (turnsOff && !(await confirmDestructive(`Turn off ${model.display_name || model.id}?`, "Sessions using it will start failing.", "Turn off"))) return;
    const next = toggleModelSupport(snapshot(), props.connectionId, model.id);
    failure.set(null);
    try {
      await change(`model:${model.id}`, () => setProviderModels(current, props.connectionId, next));
    } catch (error) {
      failure.set({ title: presentProviderError(error).title, detail: failureWords(error) });
    }
  }

  async function remove(): Promise<void> {
    const current = daemon();
    const s = snapshot();
    const c = connection();
    if (!current || !s || !c) return;
    if (!(await confirmDestructive(`Delete ${c.name}?`, "This can't be undone.", "Delete"))) return;
    try {
      await change(`delete:${c.id}`, () => deleteProviderConnection(current, c.id, s.revision));
      props.onDeleted?.();
    } catch (error) {
      failure.set({ title: presentProviderError(error).title, detail: failureWords(error) });
    }
  }

  return (
    <If when={connection()} else={<EmptyState icon="key" title="Provider removed" />}>
      <View testID="provider-panel">
        <If when={failure()}><Notice testID="provider-failure" tone="failed" title={failure()!.title} detail={failure()!.detail} /></If>
        <Section title={props.label ? connection()!.name : undefined}>
          <Row testID="provider-endpoint" title="Endpoint" value={whereOf(snapshot(), connection()!)} />
          <Row
            testID="provider-key-row"
            title="API key"
            value={connection()!.credential_ready ? connection()!.credential_hint || "Saved" : "Required"}
            chevron
            onPress={() => props.nav.push({ name: "settings-provider-editor", client: clientOf(connection()!), connectionId: connection()!.id })}
          />
          <Row
            testID="provider-test-row"
            title="Test connection"
            disabled={!connection()!.credential_ready || tested()?.label === "Testing…"}
            onPress={() => void test()}
            trailing={<If when={tested()}><StateLine testID="provider-test-state" tone={tested()!.tone} label={tested()!.label} /></If>}
          />
        </Section>
        <Section testID="provider-models" title={`Models · ${on().length} of ${all().length}`} footer={changing().startsWith("model:") ? "Saving…" : undefined}>
          <>
            <If when={all().length > PROVIDER_SEARCH_THRESHOLD * 2}><Field testID="model-search" accessibilityLabel="Search models" value={search()} placeholder="Search models" onChange={(v) => search.set(v)} /></If>
            <If when={models().length === 0} else={<>{models().map((m) => (
                <SwitchRow
                  testID={`model-${m.id}`}
                  title={m.display_name || m.id}
                  accessibilityLabel={`${m.display_name || m.id} access`}
                  subtitle={m.display_name && m.display_name !== m.id ? m.id : undefined}
                  checked={on().includes(m.id)}
                  busy={changing() === `model:${m.id}`}
                  disabled={Boolean(changing()) && changing() !== `model:${m.id}`}
                  onChange={() => void flip(m)}
                />
              ))}</>}><View class="px-4 py-4"><Text class="text-sm text-muted-foreground">{search() ? "No models match." : "No models reported."}</Text></View></If>
          </>
        </Section>
        <Section>
          <Row testID="provider-delete" title="Delete Provider" destructive disabled={Boolean(changing())} onPress={() => void remove()} />
        </Section>
      </View>
    </If>
  );
}

/** Add or edit one Provider: its name, its endpoint (custom ones) and its key, saved together. */
export function ProviderEditor(props: { client: ProviderClient; connectionId?: string; retry?: boolean; choose?: boolean; nav: PageNav }) {
  const forClient = signal<ProviderClient>(props.client);
  const existing = () => (props.connectionId ? snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null : null);
  const editor = (): ProvidersEditorState => (existing() ? { kind: "edit", connection: existing()!, retry: props.retry } : { kind: "create", client: forClient() });
  const name = signal(providerEditorInitialName(editor()));
  const baseUrl = signal(providerEditorInitialBaseUrl(editor()));
  const apiKey = signal("");
  const failure = signal<{ title: string; detail: string } | null>(null);
  const testLine = signal("");
  // What a failed Save or a test said is about the fields as they were: a change takes it away.
  const edit = (field: { set(value: string): void }, value: string) => {
    field.set(value);
    failure.set(null);
    testLine.set("");
  };
  const requiresBaseUrl = () => providerEditorRequiresBaseUrl(editor());
  const nameIssue = () => providerNameIssue({ name: name(), snapshot: snapshot(), exceptConnectionId: existing()?.id });
  const canSave = () =>
    providerEditorCanSave({
      mutating: Boolean(changing()),
      name: name(),
      baseUrl: baseUrl(),
      apiKey: apiKey(),
      nameIssue: nameIssue(),
      createMode: !existing(),
      editMode: Boolean(existing()),
      requiresBaseUrl: requiresBaseUrl(),
    });

  /** A new connection with its key in place goes straight to choosing its models. */
  async function discoverNew(saved: ProviderConnection): Promise<boolean> {
    try {
      const result = await discover(saved.id);
      discovered.set({ ...discovered(), [saved.id]: { models: result.models, warning: result.discoveryWarning } });
      return result.models.length > 0;
    } catch (error) {
      warning.set(`Provider saved. Model discovery can be retried: ${failureWords(error)}`);
      return false;
    }
  }

  async function save(): Promise<void> {
    const current = daemon();
    const before = snapshot();
    if (!current || !before || !canSave()) return;
    failure.set(null);
    const key = apiKey();
    apiKey.set("");
    const editing = existing();
    try {
      const connection = editing
        ? advancedConnectionInput({ existingId: editing.id, name: name(), client: forClient(), baseUrl: baseUrl(), presetId: editing.preset_id, advanced: Boolean(editing.base_url) })
        : advancedConnectionInput({ name: name(), client: forClient(), baseUrl: baseUrl() });
      const result = await change("save", () =>
        upsertProviderConnection(current, { connection, revision: before.revision, operation: editing ? "update" : "create", credential: key }),
      );
      let saved: ProviderConnection | undefined;
      try {
        saved = editing ? result.snapshot.connections.find((c) => c.id === editing.id) : createdConnectionFromMutation(before, result.snapshot, connection.preset_id);
      } catch (identity) {
        // The new one can't be told apart: writes wait for a fresh list, which starts now.
        locked.set(true);
        refreshProviders();
        throw identity;
      }
      if (key.trim() && saved && !saved.credential_ready) {
        // Saved, but the key did not take: the same form again, on that connection.
        refreshProviders();
        props.nav.pop();
        return props.nav.push({ name: "settings-provider-editor", client: forClient(), connectionId: saved.id, retry: true });
      }
      if (!editing && saved?.credential_ready && (await discoverNew(saved))) {
        props.nav.pop();
        return props.nav.push({ name: "settings-provider", connectionId: saved.id });
      }
      refreshProviders();
      props.nav.pop();
    } catch (error) {
      const shown = presentProviderError(error);
      failure.set({ title: shown.title, detail: failureWords(error) });
      refreshProviders();
    }
  }

  async function test(): Promise<void> {
    const current = daemon();
    if (!current) return;
    testLine.set("Testing connection…");
    try {
      const result = existing() && !apiKey().trim()
        ? await testProviderConnection(current, { connectionId: existing()!.id, client: forClient() })
        : await testProviderConnection(current, { client: forClient(), baseUrl: baseUrl() || existing()?.base_url || "", apiKey: apiKey() });
      testLine.set(`Connected · ${result.latencyMs} ms · ${result.modelCount} models found`);
    } catch (error) {
      testLine.set(failureWords(error) || "Connection test failed.");
    }
  }

  return (
    <Page display title={existing() ? "Edit Provider" : "Add Provider"} testID="provider-editor">
      <If when={props.retry}><Notice testID="provider-retry" tone="failed" title="The API key was not saved. Check it and try again." /></If>
      <If when={props.choose && !existing()}><View class="mt-3">
          <Segmented testID="provider-for" accessibilityLabel="For" options={CLIENTS} value={forClient()} onChange={(key) => forClient.set(key as ProviderClient)} />
        </View></If>
      <Section footer={nameIssue() ?? undefined}>
        <Field testID="provider-name" label="Provider name" value={name()} placeholder="e.g. Work gateway" onChange={(v) => edit(name, v)} />
        <If when={requiresBaseUrl()}><Field testID="provider-url" label="Base URL" mono keyboardType="url" value={baseUrl()} placeholder="https://api.example.com/v1" onChange={(v) => edit(baseUrl, v)} /></If>
        <Field testID="provider-key" label="API Key" masked value={apiKey()} placeholder="Paste API key" onChange={(v) => edit(apiKey, v)} />
      </Section>
      <If when={existing()?.credential_hint} else={<If when={existing()?.credential_ready}><View class="mt-1.5 px-4"><Text testID="provider-key-hint" class="text-xs text-muted-foreground">Leave empty to keep the current key.</Text></View></If>}><View class="mt-1.5 px-4"><Text testID="provider-key-hint" class="text-xs text-muted-foreground line-clamp-1">{`Stored key · ${existing()!.credential_hint} — leave empty to keep it`}</Text></View></If>
      <If when={failure()}><Notice testID="provider-failure" tone="failed" title={failure()!.title} detail={failure()!.detail} /></If>
      <If when={testLine()}><View class="mt-3 px-4"><Text testID="provider-test-line" class="text-sm text-muted-foreground" android={{ accessibilityLiveRegion: "polite" }}>{testLine()}</Text></View></If>
      <View class="mt-4 flex-row gap-3">
        <PillButton testID="provider-save" filled title={changing() === "save" ? "Saving…" : existing() ? "Save changes" : "Add Provider"} busy={changing() === "save"} disabled={!canSave()} onPress={() => void save()} />
        <PillButton testID="provider-test" title="Test connection" disabled={!(baseUrl().trim() || existing()) || (!existing() && !apiKey().trim())} onPress={() => void test()} />
      </View>
    </Page>
  );
}
