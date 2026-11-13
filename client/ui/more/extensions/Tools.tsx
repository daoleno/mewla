import { effect, signal, Text, untracked, View } from "nsh";
import { Switch } from "nsh/base";
import type { ConnectionTool, PluginAccount } from "../../../core/extensions/connections";
import { toolSections, toolsSwitch, type ToolsSwitch } from "../../../core/extensions/pluginConnectionsModel";
import { formatDateTime } from "../../../core/more/dates";
import { daemonState } from "../daemon";
import { EmptyState, Row, Section, Spinner, SwitchRow } from "../../kit/kit";
import { StateLine, tile } from "../../kit/RowTile";
import { callStateTone } from "../../../core/extensions/colour";
import { Icon } from "../../kit/Icon";
import type { PageNav } from "../routes";
import { PluginsFrame } from "./Pages";
import { flowState, plugins } from "./plugins";

/**
 * One account's tools: its on/off switch, then every tool under a switch for all
 * of them and one per group, and the calls Brain made. Every switch takes effect
 * on the tap, with no confirm: each row already says what its tool does.
 */
export function ToolsPage(props: { service: string; account?: string; nav: PageNav }) {
  const target = (): PluginAccount | null => {
    const state = flowState();
    const id = props.account ?? state.accounts.find((account) => account.integration === props.service && account.status !== "disconnected")?.id;
    if (!id) return null;
    return state.details[id] ?? null;
  };
  const accountId = () => props.account ?? flowState().accounts.find((account) => account.integration === props.service && account.status !== "disconnected")?.id;
  // The list says which accounts there are; the account's own answer carries its tools.
  // Once, after the list (one request at a time); pull to refresh asks again.
  const asked = new Set<string>();
  effect(() => {
    const id = accountId();
    const ready = daemonState() === "connected" && flowState().loaded && flowState().running === null;
    if (id && ready && !asked.has(id)) {
      asked.add(id);
      untracked(() => void plugins()?.load(id));
    }
  });
  return (
    <PluginsFrame title="Tools & activity" testID="tools-page" nav={props.nav} onRefresh={() => accountId() && void plugins()?.load(accountId()!)}>
      {/* Loading only while the account's answer is on its way; a failed one says so above. */}
      {/* The account's answer is drawn once per answer: a new one (a tool switched, a check) draws it again, other news does not. */}
      <Each of={target() ? [target()!] : []} key={(account) => account}>{(account) => <ToolsView account={account()} nav={props.nav} />}</Each>
      <If when={!target() && (flowState().running === `load:${accountId()}` || !flowState().loaded || (accountId() && !asked.has(accountId()!)))}><EmptyState busy title="Loading tools" /></If>
    </PluginsFrame>
  );
}

function ToolsView(props: { account: PluginAccount; nav: PageNav }) {
  const a = props.account;
  const running = () => flowState().running;
  const busy = () => running() !== null;
  // A tool request in flight already shows its result, so it doesn't grey the switches out.
  const locked = () => busy() && running() !== `tools:${a.id}`;
  const tools = a.tools ?? [];
  const sections = toolSections(a);
  const allowed = tools.filter((tool) => tool.allowed).length;
  const all = toolsSwitch(a, tools);
  const flip = (state: ToolsSwitch) => {
    if (state.tap === "set") plugins()?.setTools(a, state.names, !state.on);
    else if (state.tap === "consent") void plugins()?.connect(a.integration, { writes: true });
  };
  return (
    <View>
      <Section title={a.name}>
        <SwitchRow testID="account-on" accessibilityLabel="Account on" title="On" checked={a.enabled} busy={running() === `enabled:${a.id}`} disabled={busy() && running() !== `enabled:${a.id}`} onChange={(on) => plugins()?.setEnabled(a, on)} />
        <Row
          testID="check-tools"
          lead={tile("pulse")}
          title="Check service and tools"
          subtitle={a.verified_at ? `Last checked ${formatDateTime(a.verified_at)}` : undefined}
          trailing={<If when={running() === `refresh:${a.id}`}><Spinner size={16} /></If>}
          disabled={busy()}
          onPress={() => plugins()?.refresh(a)}
        />
      </Section>
      <Section title="Tools">
        <If when={tools.length} else={<Row title="No tools reported" />}>{(length) => <View>
            <SwitchRow testID="all-tools" title="All tools" subtitle={`${allowed} of ${length()} allowed`} checked={all.on} disabled={locked() || all.tap === "none"} onChange={() => flip(all)} />
            {sections.flatMap((section) => {
              const state = toolsSwitch(a, section.tools, section.consent);
              const rows = section.tools.map((tool) => <ToolRow tool={tool} locked={locked()} onToggle={(on) => plugins()?.setTools(a, [tool.name], on)} />);
              // A single group is the whole list; All tools already covers it.
              return sections.length > 1
                ? [<SwitchRow title={`${section.title} · ${section.allowed}/${section.tools.length}`} accessibilityLabel={`All of ${section.title}`} checked={state.on} disabled={locked() || state.tap === "none"} onChange={() => flip(state)} />, ...rows]
                : rows;
            })}
          </View>}</If>
      </Section>
      <Section title="Recent calls">
        <If when={a.history.length} else={<Row title="No calls yet" />}>{a.history.map((event) => <Row title={event.tool} subtitle={event.message || formatDateTime(event.at)} trailing={<StateLine tone={callStateTone(event.status)} label={event.status} />} />)}</If>
      </Section>
    </View>
  );
}

type ToolInput = { name: string; type: string; required: boolean; description?: string };

/** The schema's top-level properties, in declared order, one line each. */
function toolInputs(schema: Record<string, unknown> | undefined): ToolInput[] {
  const properties = schema?.properties;
  if (!properties || typeof properties !== "object") return [];
  const required = new Set(Array.isArray(schema?.required) ? (schema.required as string[]) : []);
  return Object.entries(properties as Record<string, Record<string, unknown>>).map(([name, prop]) => {
    const type = Array.isArray(prop?.enum) ? (prop.enum as unknown[]).map(String).join(" | ") : Array.isArray(prop?.type) ? (prop.type as string[]).join(" | ") : typeof prop?.type === "string" ? prop.type : "any";
    return { name, type, required: required.has(name), description: typeof prop?.description === "string" ? prop.description : undefined };
  });
}

/** One tool: its switch is the only control; the row itself discloses its inputs. */
function ToolRow(props: { tool: ConnectionTool; locked: boolean; onToggle: (allowed: boolean) => void }) {
  const expanded = signal(false);
  const inputs = toolInputs(props.tool.input_schema);
  const description = props.tool.description.replace(/\s+/g, " ").trim();
  const expandable = inputs.length > 0 || description.length > 120;
  return (
    <View>
      <View
        role={expandable ? "button" : undefined}
        accessibilityLabel={expandable ? `${props.tool.name}, ${inputs.length} input${inputs.length === 1 ? "" : "s"}` : undefined}
        accessibilityHint={expandable ? (expanded() ? "Hides details" : "Shows details") : undefined}
        accessibilityState={expandable ? { expanded: expanded() } : undefined}
        onPress={expandable ? () => expanded.set(!expanded()) : undefined}
        class="flex-row items-center gap-2 px-4 py-3 active:bg-accent"
      >
        <View class="w-4"><If when={expandable}><Icon name={expanded() ? "chevron-down" : "chevron-right"} size={14} tone="faint" /></If></View>
        <View class="min-w-0 flex-1">
          <Text class="font-mono text-code">{props.tool.name}</Text>
          <Text class={`mt-0.5 text-sub text-muted-foreground ${expanded() ? "" : "line-clamp-2"}`}>{`${props.tool.updated ? "Updated, waits for your confirmation · " : ""}${description}`}</Text>
        </View>
        <View class="-mr-4">
          <SwitchRowInline testID={`tool-${props.tool.name}`} label={`Allow ${props.tool.name}`} checked={props.tool.allowed} disabled={props.locked} onChange={props.onToggle} />
        </View>
      </View>
      <If when={expanded() && inputs.length}><View class="mb-2 ml-10 mr-4 gap-1 rounded-(--radius-control) bg-background px-3 py-2">
          {inputs.map((input) => (
            <View>
              <Text class="font-mono text-code">{`${input.name}: ${input.type}${input.required ? " (required)" : ""}`}</Text>
              <If when={input.description}>{(description) => <Text class="text-caption text-muted-foreground">{description()}</Text>}</If>
            </View>
          ))}
        </View></If>
    </View>
  );
}


function SwitchRowInline(props: { label: string; checked: boolean; disabled?: boolean; testID?: string; onChange: (on: boolean) => void }) {
  return (
    <View class="pr-4">
      <Switch
        testID={props.testID}
        accessibilityLabel={props.label}
        checked={props.checked}
        disabled={props.disabled}
        onChange={({ checked }) => props.onChange(checked)}
        class="disabled:opacity-50"
        trackClass="h-[30px] w-[50px] rounded-full bg-secondary border border-border checked:bg-primary checked:border-primary"
        thumbClass="m-[2px] size-[24px] rounded-full bg-card shadow-sm checked:translate-x-[20px]"
      />
    </View>
  );
}
