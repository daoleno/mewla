import { Clipboard, effect, signal, Text, untracked, View } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
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
import { Icon } from "../../kit/Icon";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner, StatusMark, SwitchRow, Tag } from "../../kit/kit";
import { Segmented } from "../../kit/Segmented";
import type { PageNav } from "../routes";
import { change, changing, discover, forgetProviders, locked, modelsAge, providers, refreshProviders, snapshot, warning } from "./providerState";

const CLIENTS = [
  { key: "codex", label: "Codex", mark: AGENT_ASSETS.codex },
  { key: "claude", label: "Claude Code", mark: AGENT_ASSETS.claude },
] as const;

/** Search earns its place only once the list is long enough to scan. */
export const PROVIDER_SEARCH_THRESHOLD = 6;

const agent = signal<ProviderClient>("codex");
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

/** Model Providers: which endpoint each agent uses on the current computer, and the keys and models of each. */
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

  const client = () => agent();
  const all = () => (snapshot() ? connectionsForClient(snapshot()!, client()) : []);
  const q = () => query().trim().toLowerCase();
  const list = () => all().filter((c) => !q() || `${c.name} ${whereOf(snapshot(), c)}`.toLowerCase().includes(q()));
  const showSearch = () => all().length >= PROVIDER_SEARCH_THRESHOLD || q().length > 0;
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
      note(id || "official", kind === "busy" || kind === "offline" || kind === "conflict" ? failureWords(error) : "Could not select this Provider connection. Retry.");
    }
  }

  async function openModels(connection: ProviderConnection): Promise<void> {
    note(connection.id, "");
    try {
      const result = await discover(connection.id);
      discovered.set({ ...discovered(), [connection.id]: { models: result.models, warning: result.discoveryWarning } });
      if (result.models.length === 0) note(connection.id, "The upstream model list is unavailable. Mewla is using its local model catalog; Sync can refresh it.");
      props.nav.push({ name: "settings-provider-models", connectionId: connection.id });
    } catch (error) {
      note(connection.id, failureWords(error));
    }
  }

  async function test(connection: ProviderConnection): Promise<void> {
    const current = daemon();
    if (!current) return;
    note(connection.id, "Testing connection…");
    try {
      const result = await testProviderConnection(current, { connectionId: connection.id, client: client() });
      note(connection.id, `Connected · ${result.latencyMs} ms · ${result.modelCount} models found`);
    } catch (error) {
      note(connection.id, failureWords(error) || "Connection test failed.");
    }
  }

  async function remove(connection: ProviderConnection): Promise<void> {
    const current = daemon();
    const s = snapshot();
    if (!current || !s) return;
    if (!(await confirmDestructive(`Delete ${connection.name}?`, "This can't be undone.", "Delete"))) return;
    try {
      await change(`delete:${connection.id}`, () => deleteProviderConnection(current, connection.id, s.revision));
    } catch (error) {
      note(connection.id, failureWords(error));
    }
  }

  const subtitle = (c: ProviderConnection) => {
    const models = enabledModelIds(snapshot(), c.id).length;
    return `${whereOf(snapshot(), c)} · ${models} ${models === 1 ? "model" : "models"} · ${modelsAge(c.models_fetched_at, c.models_stale)}`;
  };

  return (
    <Page display title="Model Providers" testID="providers" refreshing={providers().loading && Boolean(snapshot())} onRefresh={refreshProviders}>
      <>
        {!daemon() ? (
          <EmptyState testID="providers-no-server" icon="key" title="No current server" detail="Providers are managed on the connected computer." action="Open Settings" onAction={() => props.nav.pop()} />
        ) : !connected() && !snapshot() ? (
          <EmptyState icon="key" title="Server offline" detail="Providers are managed on the connected computer." />
        ) : !snapshot() && providers().error ? (
          providers().error!.kind === "unavailable" ? (
            <EmptyState icon="key" title="Unavailable" detail="Model connections are not available on this daemon." />
          ) : (
            <EmptyState
              testID="providers-failed"
              icon="alert"
              title={providers().error!.refreshable ? "Refresh required" : "Could not load"}
              detail={failureWords(providers().error)}
              action="Refresh"
              onAction={refreshProviders}
            />
          )
        ) : !snapshot() ? (
          <EmptyState busy title="Loading Providers" />
        ) : (
          <View>
            {locked() ? <Notice testID="providers-locked" title="Refresh required" detail="Changes are paused until this list is refreshed." action="Refresh" onAction={refreshProviders} /> : null}
            {warning() ? <Notice testID="providers-warning" title="Saved with a warning" detail={warning()} /> : null}
            {providers().error ? <Notice tone="failed" title={presentProviderError(providers().error).title === "Providers" ? "Refresh required" : presentProviderError(providers().error).title} detail={failureWords(providers().error)} action="Refresh" onAction={refreshProviders} /> : null}
            <View class="mt-3">
              <Segmented testID="provider-agent" accessibilityLabel="Agent" options={CLIENTS} value={client()} onChange={(key) => agent.set(key as ProviderClient)} />
            </View>
            {showSearch() ? (
              <Section>
                <Field testID="provider-search" accessibilityLabel="Search Providers" value={query()} placeholder={`Search ${label()} Providers`} onChange={(v) => query.set(v)} />
              </Section>
            ) : null}
            <Section
              title={`${label()} uses`}
              footer={list().length === 0 && q() ? "No Providers match this search." : undefined}
            >
              <Row
                testID="provider-official"
                title="Official login"
                subtitle="Direct · uses the native account"
                check={selectedId() === ""}
                role="radio"
                selected={selectedId() === ""}
                accessibilityLabel="Official login, Direct · uses the native account"
                trailing={changing() === "select:" ? <Spinner size={16} /> : undefined}
                disabled={Boolean(changing()) || selectedId() === ""}
                onPress={() => void select(null)}
              />
              {rowNote().official ? <View class="px-4 pb-3"><Text class="text-sm text-destructive" android={{ accessibilityLiveRegion: "polite" }}>{rowNote().official}</Text></View> : null}
              {list().map((c) => (
                <View>
                  <Row
                    testID={`provider-${c.id}`}
                    title={c.name}
                    subtitle={subtitle(c)}
                    check={selectedId() === c.id}
                    role="radio"
                    selected={selectedId() === c.id}
                    accessibilityLabel={`${c.name}, ${whereOf(snapshot(), c)}${c.credential_ready ? "" : ", API key required"}`}
                    disabled={Boolean(changing())}
                    onPress={() => void select(c)}
                    trailing={
                      <View class="flex-row items-center gap-2">
                        {!c.credential_ready ? <StatusMark status="warning" label="Key required" /> : null}
                        {changing() === `select:${c.id}` || changing() === `discover:${c.id}` ? <Spinner size={16} /> : null}
                        <Menu
                          trigger={<View testID={`provider-menu-${c.id}`} accessibilityLabel={`${c.name} actions`} class="p-1"><Icon name="more-horizontal" size={18} /></View>}
                          popupClass="rounded-2xl bg-popover p-2"
                          backdropClass="bg-black/30"
                        >
                          <MenuItem disabled={!c.credential_ready} onSelect={() => void openModels(c)} class="rounded-lg px-4 py-3 active:bg-accent disabled:opacity-50">
                            <Text class="text-base">{`Model access · ${enabledModelIds(snapshot(), c.id).length} of ${(snapshot()?.models[c.id] ?? []).length} on`}</Text>
                          </MenuItem>
                          <MenuItem onSelect={() => void test(c)} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Test connection</Text></MenuItem>
                          <MenuItem onSelect={() => props.nav.push({ name: "settings-provider-editor", client: client(), connectionId: c.id })} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base">Edit</Text></MenuItem>
                          <MenuItem onSelect={() => void remove(c)} class="rounded-lg px-4 py-3 active:bg-accent"><Text class="text-base text-destructive">Delete</Text></MenuItem>
                        </Menu>
                      </View>
                    }
                  />
                  {rowNote()[c.id] ? <View class="px-4 pb-3"><Text class="text-sm text-muted-foreground" android={{ accessibilityLiveRegion: "polite" }}>{rowNote()[c.id]}</Text></View> : null}
                </View>
              ))}
              <Row testID="provider-add" icon="add" title={`Add ${label()} Provider`} disabled={Boolean(changing())} onPress={() => props.nav.push({ name: "settings-provider-editor", client: client() })} />
            </Section>
            {snapshot()!.gateway ? <Gateway /> : null}
          </View>
        )}
      </>
    </Page>
  );
}

function Gateway() {
  const copied = signal(false);
  const g = () => snapshot()!.gateway!;
  return (
    <Section title="Gateway" footer={`${g().protocols.length ? g().protocols.join(" / ") : "No protocols"} · ${g().model_count} exposed models`}>
      <Row
        title="Mewla Provider Gateway"
        subtitle={copied() ? "Endpoint copied" : g().endpoint || g().address}
        mono={false}
        trailing={<Tag label={g().running ? "Ready" : "Unavailable"} />}
        accessibilityLabel={g().endpoint ? "Copy gateway endpoint" : undefined}
        onPress={g().endpoint ? () => void Clipboard.writeText(g().endpoint!).then(() => { copied.set(true); setTimeout(() => copied.set(false), 1_600); }) : undefined}
      />
    </Section>
  );
}

/** Add or edit one Provider: its name, its endpoint (custom ones) and its key, saved together. */
export function ProviderEditor(props: { client: ProviderClient; connectionId?: string; retry?: boolean; nav: PageNav }) {
  const existing = () => (props.connectionId ? snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null : null);
  const editor = (): ProvidersEditorState => (existing() ? { kind: "edit", connection: existing()!, retry: props.retry } : { kind: "create", client: props.client });
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
        ? advancedConnectionInput({ existingId: editing.id, name: name(), client: props.client, baseUrl: baseUrl(), presetId: editing.preset_id, advanced: Boolean(editing.base_url) })
        : advancedConnectionInput({ name: name(), client: props.client, baseUrl: baseUrl() });
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
        return props.nav.push({ name: "settings-provider-editor", client: props.client, connectionId: saved.id, retry: true });
      }
      if (!editing && saved?.credential_ready && (await discoverNew(saved))) {
        props.nav.pop();
        return props.nav.push({ name: "settings-provider-models", connectionId: saved.id });
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
        ? await testProviderConnection(current, { connectionId: existing()!.id, client: props.client })
        : await testProviderConnection(current, { client: props.client, baseUrl: baseUrl() || existing()?.base_url || "", apiKey: apiKey() });
      testLine.set(`Connected · ${result.latencyMs} ms · ${result.modelCount} models found`);
    } catch (error) {
      testLine.set(failureWords(error) || "Connection test failed.");
    }
  }

  return (
    <Page display title={existing() ? "Edit Provider" : "Add Provider"} testID="provider-editor">
      {props.retry ? <Notice testID="provider-retry" tone="failed" title="The API key was not saved. Check it and try again." /> : null}
      <Section footer={nameIssue() ?? undefined}>
        <Field testID="provider-name" label="Provider name" value={name()} placeholder="e.g. Work gateway" onChange={(v) => edit(name, v)} />
        {requiresBaseUrl() ? <Field testID="provider-url" label="Base URL" mono keyboardType="url" value={baseUrl()} placeholder="https://api.example.com/v1" onChange={(v) => edit(baseUrl, v)} /> : null}
        <Field testID="provider-key" label="API Key" masked value={apiKey()} placeholder="Paste API key" onChange={(v) => edit(apiKey, v)} />
      </Section>
      {existing()?.credential_hint ? (
        <View class="mt-1.5 px-4"><Text testID="provider-key-hint" class="text-xs text-muted-foreground line-clamp-1">{`Stored key · ${existing()!.credential_hint} — leave empty to keep it`}</Text></View>
      ) : existing()?.credential_ready ? (
        <View class="mt-1.5 px-4"><Text testID="provider-key-hint" class="text-xs text-muted-foreground">Leave empty to keep the current key.</Text></View>
      ) : null}
      {failure() ? <Notice testID="provider-failure" tone="failed" title={failure()!.title} detail={failure()!.detail} /> : null}
      {testLine() ? <View class="mt-3 px-4"><Text testID="provider-test-line" class="text-sm text-muted-foreground" android={{ accessibilityLiveRegion: "polite" }}>{testLine()}</Text></View> : null}
      <View class="mt-4 flex-row gap-3">
        <PillButton testID="provider-save" filled title={changing() === "save" ? "Saving…" : existing() ? "Save changes" : "Add Provider"} busy={changing() === "save"} disabled={!canSave()} onPress={() => void save()} />
        <PillButton testID="provider-test" title="Test connection" disabled={!(baseUrl().trim() || existing()) || (!existing() && !apiKey().trim())} onPress={() => void test()} />
      </View>
    </Page>
  );
}

/** Which of a connection's models agents may use. Turning one off asks first: Sessions using it start failing. */
export function ProviderModels(props: { connectionId: string }) {
  const search = signal("");
  const failure = signal<{ title: string; detail: string } | null>(null);
  const connection = () => snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null;
  const models = () => {
    const live = snapshot()?.models[props.connectionId] ?? [];
    const found = discovered()[props.connectionId]?.models ?? [];
    const byId = new Map(live.map((m) => [m.id, m]));
    for (const m of found) if (!byId.has(m.id)) byId.set(m.id, m);
    const q = search().trim().toLowerCase();
    return [...byId.values()].filter((m) => !q || m.id.toLowerCase().includes(q) || (m.display_name ?? "").toLowerCase().includes(q) || m.source.toLowerCase().includes(q));
  };
  const on = () => enabledModelIds(snapshot(), props.connectionId);

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

  return (
    <Page display title="Model access" testID="provider-models">
      <View class="mt-3 px-1"><Text class="text-sm text-muted-foreground">{`${connection()?.name ?? ""} · ${on().length} of ${(snapshot()?.models[props.connectionId] ?? []).length} on`}</Text></View>
      {discovered()[props.connectionId] && discovered()[props.connectionId].models.length === 0 ? (
        <Notice title="Upstream models are unavailable" detail="Showing the local model catalog; Sync can refresh it." />
      ) : null}
      {warning() ? <Notice title="Saved with a warning" detail={warning()} /> : null}
      {failure() ? <Notice tone="failed" title={failure()!.title} detail={failure()!.detail} /> : null}
      <Section>
        <Field testID="model-search" accessibilityLabel="Search model IDs or names" value={search()} placeholder="Search model IDs or names" onChange={(v) => search.set(v)} />
      </Section>
      <Section footer={changing().startsWith("model:") ? "Saving…" : undefined}>
        <>
          {models().length === 0 ? (
            <View class="px-4 py-4"><Text class="text-sm text-muted-foreground">{search() ? "No models match this search." : "No models were reported by this endpoint."}</Text></View>
          ) : (
            models().map((m) => (
              <SwitchRow
                testID={`model-${m.id}`}
                title={m.display_name || m.id}
                accessibilityLabel={`${m.display_name || m.id} access`}
                subtitle={[m.id, m.source, m.context_window_tokens ? `${Math.round(m.context_window_tokens / 1000)}k ctx` : ""].filter(Boolean).join(" · ")}
                checked={on().includes(m.id)}
                busy={changing() === `model:${m.id}`}
                disabled={Boolean(changing()) && changing() !== `model:${m.id}`}
                onChange={() => void flip(m)}
              />
            ))
          )}
        </>
      </Section>
    </Page>
  );
}
