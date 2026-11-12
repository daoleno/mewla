import { signal, Text, View } from "nsh";
import { ProgressBar } from "../kit/ProgressBar";
import { Button, TextView } from "nsh/base";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { BrainWorkEventTimelineItem, PlanTimelineItem } from "../../core/timeline/types";
import { workCardModel, type WorkCardModel } from "../../core/timeline/workCardModel";
import { WORK_CARD_WORKERS, type WorkWorkerLine } from "../../core/work/workers";
import type { WorkWorkerSession } from "../../core/work/types";
import type { AgentKind, TerminalFlavor } from "../../core/sessions/workerPresentation";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon as KitIcon } from "../kit/Icon";
import { clock } from "../calendar/clock";
import { serverForComputer, type ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import { Icon, type IconName } from "../icons/Icon";
import { ICON_ASSETS } from "../icons/assets";
import { current, servers, switchTo } from "../session";

/**
 * Which rows are open. A list recycles its cells, so a row's own signal would
 * follow the cell to another item: the open set lives with the chat, by id.
 */
export function createExpansion() {
  const open = signal<ReadonlySet<string>>(new Set());
  return {
    isOpen: (id: string) => open().has(id),
    toggle: (id: string) => {
      const next = new Set(open());
      if (next.has(id)) next.delete(id);
      else next.add(id);
      open.set(next);
    },
  };
}
export type Expansion = ReturnType<typeof createExpansion>;

const iconOf = (name: string | undefined, fallback: IconName): IconName => (name && name in ICON_ASSETS ? (name as IconName) : fallback);
const toneColor = (tone: string | undefined) => (tone === "failed" || tone === "danger" ? "destructive" : tone === "running" || tone === "accent" ? "foreground" : tone === "attention" ? "warning" : "muted-foreground");

const Mono = (props: { text: string }) => (
  <TextView document={{ blocks: [{ type: "codeBlock", code: props.text, language: "" }] }} selectable />
);

/** A tool call, a reasoning or update line, or a folded run of finished calls. */
export function ActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void }) {
  const item = props.item;
  const open = () => props.expansion.isOpen(item().id);
  const expandable = () => Boolean(item().commandText || item().queryText || item().body || item().files?.length || item().steps?.length || item().statusLine);
  return (
    <View class="px-4 py-0.5">
      <Button testID="activity" onPress={() => expandable() && props.expansion.toggle(item().id)} class="flex-row items-center gap-2 py-1">
        <Icon name={iconOf(item().icon, "construct")} color={toneColor(item().tone)} size={16} />
        <Text class={`flex-1 text-sm ${item().tone === "running" ? "" : "text-muted-foreground"} ${open() ? "" : "line-clamp-1"}`}>
          {item().title}{item().detail ? ` · ${item().detail}` : ""}
        </Text>
        {expandable() ? <Icon name={open() ? "chevron-up" : "chevron-down"} size={14} /> : null}
      </Button>
      {open() ? (
        <View class="gap-1.5 pb-2 pl-6">
          {item().steps?.length ? item().steps!.map((step) => (
            <View class="flex-row items-center gap-2">
              <Icon name={iconOf(step.icon, "construct")} color={toneColor(step.tone)} size={14} />
              <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{step.title}</Text>
            </View>
          )) : null}
          {item().commandText ? <Mono text={item().commandText!} /> : null}
          {item().queryText ? <TextView document={{ blocks: [{ type: "paragraph", content: [{ type: "text", text: item().queryText! }] }] }} selectable class="text-sm" /> : null}
          {item().statusLine ? <Text class="text-xs text-muted-foreground">{item().statusLine}</Text> : null}
          {item().body ? (item().bodyKind === "terminal" ? <Mono text={item().body!} /> : <TextView markdown={item().body!} selectable class="text-sm" />) : null}
          {item().files?.length ? item().files!.map((file) => (
            <Button testID="activity-file" accessibilityLabel={`Open ${file}`} disabled={!props.onFile} onPress={() => props.onFile?.(file)} class="self-start active:opacity-60">
              <Text class={props.onFile ? "font-mono text-xs text-primary" : "font-mono text-xs text-muted-foreground"}>{file}</Text>
            </Button>
          )) : null}
        </View>
      ) : null}
    </View>
  );
}

const STEP_ICON: Record<string, IconName> = { completed: "check-circle", in_progress: "time", pending: "circle" };

export function PlanRow(props: { item: () => PlanTimelineItem }) {
  return (
    <View class="mx-4 my-1 gap-1.5 rounded-xl border border-border px-3 py-2.5">
      <View class="flex-row items-center gap-2">
        <Icon name="list" size={16} />
        <Text class="text-sm font-semibold">Plan</Text>
      </View>
      {props.item().explanation ? <Text class="text-sm text-muted-foreground">{props.item().explanation}</Text> : null}
      {props.item().steps.map((step) => (
        <View class="flex-row items-start gap-2">
          <Icon name={STEP_ICON[step.status] ?? "circle"} color={step.status === "in_progress" ? "foreground" : "muted-foreground"} size={16} />
          <Text class={step.status === "completed" ? "flex-1 text-sm text-muted-foreground line-through" : "flex-1 text-sm"}>{step.step}</Text>
        </View>
      ))}
    </View>
  );
}

function Card(props: { id: string; expansion: Expansion; icon: IconName; tone: string; kind: string; title: string; status?: string; preview: string; body: string; extra?: () => unknown }) {
  const open = () => props.expansion.isOpen(props.id);
  return (
    <Button onPress={() => props.expansion.toggle(props.id)} class="mx-4 my-1 gap-1.5 rounded-xl border border-border bg-card px-3 py-2.5 active:bg-accent">
      <View class="flex-row items-center gap-2">
        <Icon name={props.icon} color={toneColor(props.tone)} size={16} />
        <Text class="flex-1 text-xs font-semibold uppercase text-muted-foreground line-clamp-1">{props.kind}</Text>
        {props.status ? <Text class="text-xs text-muted-foreground">{props.status}</Text> : null}
      </View>
      {props.title ? <Text class="text-base font-semibold">{props.title}</Text> : null}
      {open() ? <TextView markdown={props.body || props.preview} selectable class="text-sm" /> : props.preview ? <Text class="text-sm text-muted-foreground line-clamp-3">{props.preview}</Text> : null}
      {open() && props.extra ? (props.extra() as never) : null}
    </Button>
  );
}

/** A background task, subagent or monitor that reported back. */
export function TaskNotificationCard(props: { item: () => TaskNotificationTimelineItem; expansion: Expansion }) {
  const it = props.item;
  return (
    <Card id={it().id} expansion={props.expansion} icon={iconOf(it().icon, "pulse")} tone={it().tone} kind={it().kindLabel} title={it().title}
      status={it().statusLabel} preview={[it().facts.join(" · "), it().preview].filter(Boolean).join("\n")} body={it().body}
      extra={() => it().details.map((d) => <Text class="text-xs text-muted-foreground">{`${d.label}: ${d.value}`}</Text>)} />
  );
}

/** A task sent to, or answered by, another computer. */
export function ComputerMessageCard(props: { item: () => ComputerMessageTimelineItem; expansion: Expansion }) {
  const it = props.item;
  // "Open on <computer>": that computer becomes current, where its own Brain and Sessions show what happened.
  const target = () => serverForComputer(servers(), it().daemonId, current()?.id ?? null);
  return (
    <View>
      <Card id={it().id} expansion={props.expansion} icon="desktop" tone={it().tone} kind={`${it().direction === "to" ? "To" : "From"} ${it().computer} · ${it().kindLabel}`}
        title={it().userSaid ? `“${it().userSaid}”` : ""} status={it().statusLabel} preview={it().preview} body={it().body} />
      {target() ? (
        <Button testID="computer-message-open" accessibilityLabel={`Open on ${it().computer}`} onPress={() => { const server = target(); if (server) switchTo(server); }}
          class="mx-4 mb-1 self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
          <Text class="text-sm font-semibold text-primary">Open on {it().computer}</Text>
        </Button>
      ) : null}
    </View>
  );
}

/** Where a Work card leads outside the thread; none (a past thread) leaves it to read. */
export type WorkCardHandlers = {
  /** The agent that ran a Session, for the card's logo and "Agent · Done 11:20". */
  agentOf: (sessionId: string | undefined) => { kind: AgentKind; flavor: TerminalFlavor; name: string; progress?: number } | null;
  /** The title of a Work: its detail page. */
  onTitle: (workId: string) => void;
  /** The Workers that joined a Work, as the Session list has them now. */
  workers: (rows: readonly WorkWorkerSession[] | undefined) => WorkWorkerLine[];
  /** A Worker line: its Session. */
  onSession: (sessionId: string) => void;
  /** The quiet line: the result sheet. */
  onLine: (model: WorkCardModel) => void;
};

/** "Agent · Done 11:20". */
function resultMeta(model: WorkCardModel, agent: string | undefined): string {
  const at = model.at ? clock().format.time(model.at, clock().zones.device) : "";
  return [agent, [model.state, at].filter(Boolean).join(" ")].filter(Boolean).join(" · ");
}

function WorkLogo(props: { agent: { kind: AgentKind; flavor: TerminalFlavor } | null; size: number }) {
  return props.agent
    ? <AgentIcon kind={props.agent.kind} flavor={props.agent.flavor} size={props.size - 8} />
    : <View class="items-center justify-center rounded-lg bg-foreground" style={{ width: props.size, height: props.size }}><KitIcon name="brain" size={props.size - 10} tone="canvas" /></View>;
}

/** A Worker of a Work on its card (SPEC §11): 44 pt, its logo, name, what it does, the time; pushes its Session. */
function WorkerLine(props: { line: WorkWorkerLine; onPress: () => void }) {
  const line = props.line;
  return (
    <Button testID="work-card-worker" accessibilityLabel={[line.name, line.summary, line.time].filter(Boolean).join(", ")} disabled={!line.openable} onPress={props.onPress}
      class="min-h-[44px] flex-row items-center gap-2.5 border-t border-border px-4 py-2 active:opacity-70">
      <AgentIcon kind={line.agent} flavor={line.flavor} size={20} />
      <View class="min-w-0 flex-1">
        <Text class="text-sub line-clamp-1"><Text class="font-semibold">{line.name}</Text><Text class="text-muted-foreground">{line.summary ? `  ${line.summary}` : ""}</Text></Text>
        {line.progress !== undefined ? <ProgressBar filled={line.progress} testID="work-card-progress" /> : null}
      </View>
      <Text class="text-caption text-muted-foreground">{line.time === "live" ? "" : line.time}</Text>
    </Button>
  );
}

/**
 * A Work in Brain's thread (SPEC §1.2, §11 Work card and Work result card): live while it runs (its
 * title opens Work detail, its Session's line the Session); waiting on a decision, the card says so
 * and opens Work detail; back and new, the result card (the header opens the Session, a next step
 * goes to Brain); read, its quiet line, which opens the result sheet.
 */
export function WorkCard(props: { item: () => BrainWorkEventTimelineItem; handlers?: WorkCardHandlers }) {
  const model = () => workCardModel(props.item());
  const agent = () => props.handlers?.agentOf(model().sessionId) ?? null;
  const lines = () => props.handlers?.workers(props.item().currentWork?.worker_sessions) ?? [];
  const open = () => props.item().onPress?.();
  return (
    <View class="px-4 py-1">
      {model().kind === "line" ? (
        <Button testID="work-line" accessibilityLabel={`${model().title}, ${model().state}`} disabled={!props.handlers} onPress={() => props.handlers?.onLine(model())}
          class="min-h-[44px] flex-row items-center gap-2.5 active:opacity-70">
          <WorkLogo agent={agent()} size={20} />
          <Text class="min-w-0 flex-1 text-sub text-muted-foreground line-clamp-1"><Text class="font-semibold text-foreground">{model().title}</Text>{model().summary ? ` · ${model().summary}` : ""}</Text>
          <KitIcon name="check" size={18} tone="soft" />
        </Button>
      ) : model().kind === "result" ? (
        <View testID="work-result" class="gap-3 rounded-(--radius-group) bg-card p-4">
          <Button testID="work-result-open" accessibilityLabel={`Open ${model().title}`} disabled={!props.item().onPress} onPress={open} class="-my-1 min-h-[52px] flex-row items-center gap-3 active:opacity-70">
            <WorkLogo agent={agent()} size={28} />
            <View class="min-w-0 flex-1">
              <Text class="text-body font-semibold line-clamp-1">{model().title}</Text>
              <Text class="text-caption text-muted-foreground line-clamp-1">{resultMeta(model(), agent()?.name)}</Text>
            </View>
            <View testID="work-unread" class="size-2 rounded-full bg-foreground" />
            <KitIcon name="chevron-right" size={18} tone="faint" />
          </Button>
          {model().summary ? <View selectable><Text class="text-sub">{model().summary}</Text></View> : null}
        </View>
      ) : (
        <View testID={model().kind === "needs" ? "work-needs" : "work-card"} class="overflow-hidden rounded-(--radius-group) bg-card">
          <Button testID="work-card-title" disabled={!props.handlers} onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[52px] flex-row items-center gap-2 px-4 active:opacity-70">
            <Text class="min-w-0 flex-1 text-body font-semibold line-clamp-1">{model().title}</Text>
            <View class={`size-2 rounded-full ${model().kind === "needs" ? "bg-seal" : "bg-foreground"}`} />
            <Text class="text-sub text-muted-foreground">{model().state}</Text>
            <KitIcon name="chevron-right" size={16} tone="faint" />
          </Button>
          {lines().length ? (
            <>
              {lines().slice(0, WORK_CARD_WORKERS).map((line) => <WorkerLine line={line} onPress={() => props.handlers?.onSession(line.sessionId)} />)}
              {lines().length > WORK_CARD_WORKERS ? (
                <Button testID="work-card-more" onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[44px] flex-row items-center border-t border-border px-4 active:opacity-70">
                  <Text class="min-w-0 flex-1 text-sub text-muted-foreground">{`${lines().length - WORK_CARD_WORKERS} more`}</Text>
                  <KitIcon name="chevron-right" size={16} tone="faint" />
                </Button>
              ) : null}
            </>
          ) : model().sessionId ? (
            <Button testID="work-card-session" disabled={!props.item().onPress} onPress={open} class="min-h-[44px] flex-row items-center gap-2.5 border-t border-border px-4 active:opacity-70">
              <WorkLogo agent={agent()} size={20} />
              <View class="min-w-0 flex-1">
                <Text class="text-sub line-clamp-1"><Text class="font-semibold">{agent()?.name ?? model().sessionName ?? "Session"}</Text><Text class="text-muted-foreground">{model().summary ? `  ${model().summary}` : ""}</Text></Text>
                {model().kind === "live" && agent()?.progress !== undefined ? <ProgressBar filled={agent()!.progress!} testID="work-card-progress" /> : null}
              </View>
            </Button>
          ) : model().summary ? <Text class="border-t border-border px-4 py-3 text-sub text-muted-foreground line-clamp-2">{model().summary}</Text> : null}
        </View>
      )}
    </View>
  );
}
