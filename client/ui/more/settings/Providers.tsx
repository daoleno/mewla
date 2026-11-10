import { Clipboard, effect, signal, Text, untracked, View } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import type { ProviderError } from "../../../core/providers/errors";
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
  discoverProviderModels,
  setProviderConnection,
  setProviderModels,
  testProviderConnection,
  upsertProviderConnection,
} from "../../../core/providers/requests";
import type { ProviderClient, ProviderConnection, ProviderModel } from "../../../core/providers/types";
import { connected, daemon } from "../daemon";
import { Icon } from "../kit/Icon";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner, SwitchRow, Tag } from "../kit/kit";
import { Segmented } from "../kit/Segmented";
import type { PageNav } from "../routes";
import { change, changing, forgetProviders, locked, modelsAge, providers, refreshProviders, snapshot, warning } from "./providers";

const CLIENTS = [
  { key: "codex", label: "Codex" },
  { key: "claude", label: "Claude Code" },
] as const;

const agent = signal<ProviderClient>("codex");
const rowNote = signal<Record<string, string>>({});
/** Models a connection reported on its last discovery, for the model access page. */
export const discovered = signal<Record<string, { models: ProviderModel[]; warning?: string }>>({});

function failureWords(error: unknown): string {
  return (error as ProviderError | null)?.message || "The Provider request failed. Refresh and try again.";
}

function note(id: string, text: string): void {
  rowNote.set({ ...rowNote(), [id]: text });
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
      }
      if (live) refreshProviders();
    });
  });

  const client = () => agent();
  const list = () => (snapshot() ? connectionsForClient(snapshot()!, client()) : []);
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
      note(id || "official", (error as ProviderError).kind === "busy" ? failureWords(error) : "Could not select this Provider connection. Retry.");
    }
  }

  async function openModels(connection: ProviderConnection): Promise<void> {
    const current = daemon();
    if (!current) return;
    note(connection.id, "");
    try {
      const result = await discoverProviderModels(current, connection.id);
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
    if (!(await confirmDestructive("Delete Provider?", connection.name, "Delete"))) return;
    try {
      await change(`delete:${connection.id}`, () => deleteProviderConnection(current, connection.id, s.revision));
    } catch (error) {
      note(connection.id, failureWords(error));
    }
  }

  const subtitle = (c: ProviderConnection) => {
    const where = c.base_url ? providerBaseUrlHostname(c.base_url) : snapshot()?.presets.find((p) => p.id === c.preset_id)?.label ?? "Official endpoint";
    const models = enabledModelIds(snapshot(), c.id).length;
    return `${where} · ${models} ${models === 1 ? "model" : "models"} · ${modelsAge(c.models_fetched_at, c.models_stale)}`;
  };

  return (
    <Page title="Model Providers" testID="providers" refreshing={providers().loading && Boolean(snapshot())} onRefresh={refreshProviders}>
      {!daemon() ? (
        <EmptyState icon="key" title="No current server" detail="Providers are managed on the connected computer." />
      ) : !connected() && !snapshot() ? (
        <EmptyState icon="key" title="Server offline" detail="Providers are managed on the connected computer." />
      ) : !snapshot() && providers().error ? (
        providers().error!.kind === "unavailable" ? (
          <EmptyState icon="key" title="Unavailable" detail="Model connections are not available on this daemon." />
        ) : (
          <EmptyState testID="providers-failed" icon="alert" title="Could not load" detail={failureWords(providers().error)} action="Refresh" onAction={refreshProviders} />
        )
      ) : !snapshot() ? (
        <EmptyState busy title="Loading Providers" />
      ) : (
        <View>
          {locked() ? <Notice title="Refresh required" detail="Changes are paused until this list is refreshed." action="Refresh" onAction={refreshProviders} /> : null}
          {warning() ? <Notice title="Saved with a warning" detail={warning()} /> : null}
          {providers().error ? <Notice tone="failed" title="Refresh required" detail={failureWords(providers().error)} action="Refresh" onAction={refreshProviders} /> : null}
          <View class="mt-3">
            <Segmented testID="provider-agent" accessibilityLabel="Agent" options={CLIENTS} value={client()} onChange={(key) => agent.set(key as ProviderClient)} />
          </View>
          <Section
            title={`${label()} uses`}
            detail={<Button testID="provider-add" onPress={() => props.nav.push({ name: "settings-provider-editor", client: client() })} class="active:opacity-60"><Text class="text-sm font-semibold">Add</Text></Button>}
            footer={list().length === 0 ? "Add a Provider to route this Agent through your own API key." : undefined}
          >
            <Row
              testID="provider-official"
              title="Official login"
              subtitle="Direct · uses the native account"
              check={selectedId() === ""}
              trailing={changing() === "select:" ? <Spinner size={16} /> : undefined}
              disabled={Boolean(changing())}
              onPress={() => void select(null)}
            />
            {rowNote().official ? <Text class="px-4 pb-3 text-sm text-destructive">{rowNote().official}</Text> : null}
            {list().map((c) => (
              <View>
                <Row
                  testID={`provider-${c.id}`}
                  title={c.name}
                  subtitle={subtitle(c)}
                  check={selectedId() === c.id}
                  disabled={Boolean(changing())}
                  onPress={() => void select(c)}
                  trailing={
                    <View class="flex-row items-center gap-2">
                      {!c.credential_ready ? <Tag label="Key required" /> : null}
                      {changing() === `select:${c.id}` ? <Spinner size={16} /> : null}
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
                {rowNote()[c.id] ? <Text class="px-4 pb-3 text-sm text-muted-foreground">{rowNote()[c.id]}</Text> : null}
              </View>
            ))}
          </Section>
          {snapshot()!.gateway ? <Gateway /> : null}
        </View>
      )}
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
  const failure = signal("");
  const testLine = signal("");
  const requiresBaseUrl = () => providerEditorRequiresBaseUrl(editor());
  const nameIssue = () => (name().trim() ? providerNameIssue({ name: name(), snapshot: snapshot(), exceptConnectionId: existing()?.id }) : null);
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

  async function save(): Promise<void> {
    const current = daemon();
    const before = snapshot();
    if (!current || !before || !canSave()) return;
    failure.set("");
    const key = apiKey();
    apiKey.set("");
    try {
      const connection = existing()
        ? advancedConnectionInput({ existingId: existing()!.id, name: name(), client: props.client, baseUrl: baseUrl(), presetId: existing()!.preset_id, advanced: Boolean(existing()!.base_url) })
        : advancedConnectionInput({ name: name(), client: props.client, baseUrl: baseUrl() });
      const result = await change("save", () =>
        upsertProviderConnection(current, { connection, revision: before.revision, operation: existing() ? "update" : "create", credential: key }),
      );
      const saved = existing() ? result.snapshot.connections.find((c) => c.id === existing()!.id) : createdConnectionFromMutation(before, result.snapshot, connection.preset_id);
      if (!existing() && saved && !saved.credential_ready) {
        props.nav.pop();
        return props.nav.push({ name: "settings-provider-editor", client: props.client, connectionId: saved.id, retry: true });
      }
      props.nav.pop();
    } catch (error) {
      failure.set(failureWords(error));
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
    <Page title={existing() ? "Edit Provider" : "Add Provider"} testID="provider-editor">
      {props.retry ? <Notice title="API key not ready" detail="The Provider was added, but its key did not take. Paste it again." /> : null}
      <Section footer={nameIssue() ?? undefined}>
        <Field testID="provider-name" label="Provider name" value={name()} placeholder="e.g. Work gateway" onChange={(v) => name.set(v)} />
        {requiresBaseUrl() ? <Field testID="provider-url" label="Base URL" mono keyboardType="url" value={baseUrl()} placeholder="https://api.example.com/v1" onChange={(v) => baseUrl.set(v)} /> : null}
        <Field testID="provider-key" label="API Key" masked value={apiKey()} placeholder={existing()?.credential_hint || "Paste API key"} onChange={(v) => apiKey.set(v)} />
      </Section>
      {existing()?.credential_ready ? <Text class="mt-1.5 px-4 text-xs text-muted-foreground">Leave empty to keep the current key.</Text> : null}
      {failure() ? <Notice tone="failed" title="Providers" detail={failure()} /> : null}
      {testLine() ? <Text testID="provider-test-line" class="mt-3 px-4 text-sm text-muted-foreground">{testLine()}</Text> : null}
      <View class="mt-4 flex-row gap-3">
        <PillButton testID="provider-save" filled title={changing() === "save" ? "Saving…" : existing() ? "Save changes" : "Add Provider"} busy={changing() === "save"} disabled={!canSave()} onPress={() => void save()} />
        <PillButton testID="provider-test" title="Test connection" disabled={!(baseUrl().trim() || existing()) || (!existing() && !apiKey().trim())} onPress={() => void test()} />
      </View>
    </Page>
  );
}

/** Which of a connection's models agents may use. Turning one off asks first: Sessions using it start failing. */
export function ProviderModels(props: { connectionId: string }) {
  const query = signal("");
  const failure = signal("");
  const connection = () => snapshot()?.connections.find((c) => c.id === props.connectionId) ?? null;
  const models = () => {
    const live = snapshot()?.models[props.connectionId] ?? [];
    const found = discovered()[props.connectionId]?.models ?? [];
    const byId = new Map(live.map((m) => [m.id, m]));
    for (const m of found) if (!byId.has(m.id)) byId.set(m.id, m);
    const q = query().trim().toLowerCase();
    return [...byId.values()].filter((m) => !q || m.id.toLowerCase().includes(q) || (m.display_name ?? "").toLowerCase().includes(q));
  };
  const on = () => enabledModelIds(snapshot(), props.connectionId);

  async function flip(model: ProviderModel): Promise<void> {
    const current = daemon();
    if (!current) return;
    const turnsOff = modelSupportToggleTurnsOff(snapshot(), props.connectionId, model.id);
    if (turnsOff && !(await confirmDestructive(`Turn off ${model.display_name || model.id}?`, "Sessions using it will start failing.", "Turn off"))) return;
    const next = toggleModelSupport(snapshot(), props.connectionId, model.id);
    failure.set("");
    try {
      await change(`model:${model.id}`, () => setProviderModels(current, props.connectionId, next));
    } catch (error) {
      failure.set(failureWords(error));
    }
  }

  return (
    <Page title="Model access" testID="provider-models">
      <Text class="mt-3 px-1 text-sm text-muted-foreground">{`${connection()?.name ?? ""} · ${on().length} of ${(snapshot()?.models[props.connectionId] ?? []).length} on`}</Text>
      {discovered()[props.connectionId] && discovered()[props.connectionId].models.length === 0 ? (
        <Notice title="Upstream models are unavailable" detail="Showing the local model catalog; Sync can refresh it." />
      ) : null}
      {failure() ? <Notice tone="failed" title="Providers" detail={failure()} /> : null}
      <Section>
        <Field testID="model-search" value={query()} placeholder="Search model IDs or names" onChange={(v) => query.set(v)} />
      </Section>
      <Section footer={changing().startsWith("model:") ? "Saving…" : undefined}>
        {models().length === 0 ? (
          <Text class="px-4 py-4 text-sm text-muted-foreground">{query() ? "No models match this search." : "No models were reported by this endpoint."}</Text>
        ) : (
          models().map((m) => (
            <SwitchRow
              testID={`model-${m.id}`}
              title={m.display_name || m.id}
              subtitle={[m.id, m.source, m.context_window_tokens ? `${Math.round(m.context_window_tokens / 1000)}k ctx` : ""].filter(Boolean).join(" · ")}
              checked={on().includes(m.id)}
              busy={changing() === `model:${m.id}`}
              disabled={Boolean(changing()) && changing() !== `model:${m.id}`}
              onChange={() => void flip(m)}
            />
          ))
        )}
      </Section>
    </Page>
  );
}
