import { effect, signal, Text, untracked, View } from "nsh";
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

/**
 * Model Providers, learned from Magpie's Agents / Providers split: each agent once, with
 * what it uses (one choice, made on its own page), then every Provider once, each opening
 * one page with its key, its test and its models.
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
      }
      if (live) refreshProviders();
    });
  });

  return (
    <Page display title="Model Providers" testID="providers" refreshing={providers().loading && Boolean(snapshot())} onRefresh={refreshProviders}>
      <>
        <If when={!daemon()} else={<If when={!connected() && !snapshot()} else={<If when={!snapshot() && providers().error} else={<If when={!snapshot()} else={<View>
            <If when={locked()}><Notice testID="providers-locked" title="Refresh required" detail="Changes are paused until this list is refreshed." action="Refresh" onAction={refreshProviders} /></If>
            <If when={warning()}><Notice testID="providers-warning" title="Saved with a warning" detail={warning()} /></If>
            <If when={providers().error}><Notice tone="failed" title={presentProviderError(providers().error).title === "Providers" ? "Refresh required" : presentProviderError(providers().error).title} detail={failureWords(providers().error)} action="Refresh" onAction={refreshProviders} /></If>
            <Agents nav={props.nav} />
            <ProviderList nav={props.nav} />
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

/** Each agent and what it uses now; a row opens its choice. */
function Agents(props: { nav: PageNav }) {
  return (
    <Section title="Agents">
      {CLIENTS.map((c) => (
        <Row
          testID={`provider-agent-${c.key}`}
          lead={{ kind: "logo", node: <AgentIcon kind={c.key} size={28} slot={LEAD_SLOT} /> }}
          title={c.label}
          value={chosenName(c.key)}
          chevron
          accessibilityLabel={`${c.label} uses ${chosenName(c.key)}`}
          onPress={() => props.nav.push({ name: "settings-provider-agent", client: c.key })}
        />
      ))}
    </Section>
  );
}

/** Every Provider once: where it points and how many models it gives. */
function ProviderList(props: { nav: PageNav }) {
  const all = () => snapshot()?.connections ?? [];
  const q = () => query().trim().toLowerCase();
  const list = () => all().filter((c) => !q() || `${c.name} ${whereOf(snapshot(), c)}`.toLowerCase().includes(q()));
  const showSearch = () => all().length >= PROVIDER_SEARCH_THRESHOLD || q().length > 0;
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
            accessibilityLabel={`${c.name}, ${whereOf(snapshot(), c)}, for ${CLIENTS.find((k) => k.key === clientOf(c))!.label}${c.credential_ready ? "" : ", API key required"}`}
            chevron
            onPress={() => props.nav.push(c.credential_ready ? { name: "settings-provider", connectionId: c.id } : { name: "settings-provider-editor", client: clientOf(c), connectionId: c.id })}
            trailing={
              <View class="flex-row items-center gap-2">
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

/** What one agent uses: its official login or one of its Providers. */
export function AgentProviders(props: { client: ProviderClient; nav: PageNav }) {
  const client = () => props.client;
  const all = () => (snapshot() ? connectionsForClient(snapshot()!, client()) : []);
  const selectedId = () => snapshot()?.defaults[client()]?.connection_id ?? "";
  const label = () => CLIENTS.find((c) => c.key === client())!.label;

  async function select(connection: ProviderConnection | null): Promise<void> {
    const current = daemon();
    const s = snapshot();
    if (!current || !s) return;
    if (connection && !connection.credential_ready) return props.nav.push({ name: "settings-provider-editor", client: client(), connectionId: connection.id });
    const id = connection?.id ?? "";
    note(id || "official", "");
    try {
      await change(`select:${id}`, () => setProviderConnection(current, { client: client(), connectionId: id, revision: s.revision }));
    } catch (error) {
      const kind = presentProviderError(error).kind;
      note(id || "official", kind === "busy" || kind === "offline" || kind === "conflict" ? failureWords(error) : "Could not select this Provider. Retry.");
    }
  }

  return (
    <Page display title={label()} testID={`provider-agent-page-${client()}`}>
      <If when={locked()}><Notice testID="providers-locked" title="Refresh required" detail="Changes are paused until this list is refreshed." action="Refresh" onAction={refreshProviders} /></If>
      <Section title="Uses">
        <Row
          testID="provider-official"
          lead={lead(officialLoginLead(client()))}
          title="Official login"
          check={selectedId() === ""}
          role="radio"
          selected={selectedId() === ""}
          trailing={<If when={changing() === "select:"}><Spinner size={16} /></If>}
          disabled={Boolean(changing()) || selectedId() === ""}
          onPress={() => void select(null)}
        />
        <If when={rowNote().official}><View class="px-4 pb-3"><Text class="text-sm text-destructive" android={{ accessibilityLiveRegion: "polite" }}>{rowNote().official}</Text></View></If>
        {all().map((c) => (
          <View>
            <Row
              testID={`provider-choose-${c.id}`}
              lead={lead(providerLead(c))}
              title={c.name}
              subtitle={`${whereOf(snapshot(), c)} · ${modelCount(c)}`}
              subtitleOneLine
              check={selectedId() === c.id}
              role="radio"
              selected={selectedId() === c.id}
              accessibilityLabel={`${c.name}, ${whereOf(snapshot(), c)}${c.credential_ready ? "" : ", API key required"}`}
              disabled={Boolean(changing())}
              onPress={() => void select(c)}
              trailing={
                <View class="flex-row items-center gap-2">
                  <If when={!c.credential_ready}><StateLine tone="warn" label="Key required" /></If>
                  <If when={changing() === `select:${c.id}`}><Spinner size={16} /></If>
                </View>
              }
            />
            <If when={rowNote()[c.id]}><View class="px-4 pb-3"><Text class="text-sm text-destructive" android={{ accessibilityLiveRegion: "polite" }}>{rowNote()[c.id]}</Text></View></If>
          </View>
        ))}
        <Row testID={`provider-add-${client()}`} lead={tile("add")} title="Add Provider" disabled={Boolean(changing())} onPress={() => props.nav.push({ name: "settings-provider-editor", client: client() })} />
      </Section>
    </Page>
  );
}

/** A provider's lead as the row draws it: its vendor's mark, its agent's logo, or the key on the tile. */
function lead(l: ProviderLead): Lead {
  if (l.kind === "agent") return { kind: "logo", node: <AgentIcon kind={l.agent} size={28} slot={LEAD_SLOT} /> };
  return l;
}

/**
 * One Provider: where it points, its key, a test, and which of its models agents may use.
 * Opening it asks the Provider for its model list again, so a model it added since shows.
 */
export function ProviderDetail(props: { connectionId: string; nav: PageNav }) {
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
      props.nav.pop();
    } catch (error) {
      failure.set({ title: presentProviderError(error).title, detail: failureWords(error) });
    }
  }

  return (
    <Page display title={connection()?.name ?? "Provider"} testID="provider-detail">
      <If when={connection()} else={<EmptyState icon="key" title="Provider removed" />}>
        <View>
          <If when={warning()}><Notice title="Saved with a warning" detail={warning()} /></If>
          <If when={failure()}><Notice testID="provider-failure" tone="failed" title={failure()!.title} detail={failure()!.detail} /></If>
          <Section>
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
    </Page>
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
