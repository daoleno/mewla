import { Clipboard, signal, Text, View } from "nsh";
import { ProgressBar } from "../kit/ProgressBar";
import { Button, TextView } from "nsh/base";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import { activityFileRows, type ActivityFileRow } from "../../core/timeline/activityFiles";
import { toolPictures } from "../../core/timeline/threadPictures";
import { distinctivePath } from "../../core/timeline/toolCallSemantics";
import { compactPathLabel } from "../../core/timeline/pathDisplay";
import { ChatImage, type PictureLoader } from "./ChatImage";
import type { BrainWorkEventTimelineItem, PlanTimelineItem } from "../../core/timeline/types";
import { workCardModel, type WorkCardModel } from "../../core/timeline/workCardModel";
import { WORK_CARD_WORKERS, type WorkWorkerLine } from "../../core/work/workers";
import type { ResultAction, WorkWorkerSession } from "../../core/work/types";
import { resultActionLabel } from "../../core/work/resultActions";
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

/**
 * A chat's code block (soft-final SPEC §11 Message, the mock's `.codeblock`): a surface card, radius 20,
 * with a 48 pt header (the language or "code" in soft mono, then Copy, which shows a check for a moment),
 * the code in mono 13/20 under it, scrolling sideways. NSH draws and colours the code.
 */
export function CodeBlock(props: { code: string; language?: string }) {
  const copied = signal(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const copy = () => {
    void Clipboard.writeText(props.code).then(() => {
      copied.set(true);
      clearTimeout(timer);
      timer = setTimeout(() => copied.set(false), 1500);
    }).catch(() => {});
  };
  return (
    <View testID="code-block" class="my-1 overflow-hidden rounded-(--radius-group) bg-card">
      <View class="h-12 flex-row items-center gap-2 pl-4 pr-1">
        <Text class="min-w-0 flex-1 font-mono text-code text-muted-foreground line-clamp-1">{props.language || "code"}</Text>
        <Button testID="code-copy" accessibilityLabel={copied() ? "Copied" : "Copy"} onPress={copy} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name={copied() ? "check" : "copy"} size={18} color="muted-foreground" />
        </Button>
      </View>
      <TextView document={{ blocks: [{ type: "codeBlock", code: props.code, language: props.language ?? "" }] }}
        classes={{ codeBlock: "m-0 rounded-none border-0 bg-transparent px-4 pb-4 pt-0" }} />
    </View>
  );
}

/** How a chat's Markdown draws its code blocks: as `CodeBlock`. */
export const renderCode = (block: { code: string; language?: string }) => <CodeBlock code={block.code} language={block.language} />;

const Mono = (props: { text: string }) => <CodeBlock code={props.text} />;

/** A file as the row titles show it: from its project down (`daemon/watcher/watcher.go`), not the whole absolute path, whose end a line cuts off. */
const shownPath = (path: string) => distinctivePath(path) ?? compactPathLabel(path);

/** An edited file (SPEC §1.7, §11 Diff card): its path in mono, then +/− in add and del; a rename shows and opens its new path. */
function FileRow(props: { file: ActivityFileRow; onFile?: (path: string) => void }) {
  const file = props.file;
  return (
    <Button testID="activity-file" accessibilityLabel={[`Open ${file.path}`, file.added !== undefined ? `${file.added} added` : "", file.removed !== undefined ? `${file.removed} removed` : ""].filter(Boolean).join(", ")}
      disabled={!props.onFile} onPress={() => props.onFile?.(file.path)} class="min-h-[44px] flex-row items-center gap-2 active:opacity-60">
      <Text class={`min-w-0 shrink font-mono text-code line-clamp-1 ${props.onFile ? "text-foreground" : "text-muted-foreground"}`}>{shownPath(file.path)}</Text>
      {file.added !== undefined ? <Text testID="activity-file-added" class="font-mono text-code text-add">{`+${file.added}`}</Text> : null}
      {file.removed !== undefined ? <Text testID="activity-file-removed" class="font-mono text-code text-del">{`−${file.removed}`}</Text> : null}
    </Button>
  );
}

/** A tool call, a reasoning or update line, or a folded run of finished calls. */
export function ActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void; pictures?: { load?: PictureLoader; open: (src: string) => void } }) {
  const item = props.item;
  const open = () => props.expansion.isOpen(item().id);
  const files = () => activityFileRows(item());
  const expandable = () => Boolean(item().commandText || item().queryText || item().body || files().length || item().steps?.length || item().statusLine);
  return (
    <View class="px-4 py-0.5">
      <Button testID="activity" onPress={() => expandable() && props.expansion.toggle(item().id)} class="flex-row items-center gap-2 py-1">
        <Icon name={iconOf(item().icon, "construct")} color={toneColor(item().tone)} size={16} />
        <Text class={`flex-1 text-sm ${item().tone === "running" ? "" : "text-muted-foreground"} ${open() ? "" : "line-clamp-1"}`}>
          {item().title}{item().detail ? ` · ${item().detail}` : ""}
        </Text>
        {expandable() ? <Icon name={open() ? "chevron-up" : "chevron-down"} size={14} /> : null}
      </Button>
      {/* The pictures the call produced, under its row whether or not it is open (SPEC §1.7). */}
      {toolPictures(item()).length ? (
        <View testID="tool-pictures" class="flex-row flex-wrap items-start gap-2 pb-1.5 pl-6">
          {toolPictures(item()).map((src) => (
            <ChatImage frame="tool" src={src} alt={item().title} load={props.pictures?.load} onOpen={() => props.pictures?.open(src)} />
          ))}
        </View>
      ) : null}
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
          {item().body ? (item().bodyKind === "terminal" ? <Mono text={item().body!} /> : <TextView markdown={item().body!} selectable renderCodeBlock={renderCode} class="text-sm" />) : null}
          {files().map((file) => <FileRow file={file} onFile={props.onFile} />)}
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
  /** A result's next step: it goes where the step names, and the card becomes its line. */
  onAction: (model: WorkCardModel, action: ResultAction) => void;
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

/** A result's next steps (SPEC §11): small ghost buttons, canvas fill, an 18 pt glyph, ink words, wrapping. */
export function ResultActions(props: { actions: readonly ResultAction[]; disabled?: boolean; onPress: (action: ResultAction) => void }) {
  return (
    <View testID="work-actions" class="flex-row flex-wrap gap-2">
      {props.actions.map((action) => {
        const { label, icon } = resultActionLabel(action);
        return (
          <Button testID={`work-action-${action.kind}`} accessibilityLabel={label} disabled={props.disabled} onPress={() => props.onPress(action)}
            class="h-[36px] max-w-full flex-row items-center gap-1.5 rounded-full bg-background px-3 active:opacity-70 disabled:opacity-40">
            <KitIcon name={icon} size={18} tone="ink" />
            <Text class="shrink text-sub font-semibold line-clamp-1">{label}</Text>
          </Button>
        );
      })}
    </View>
  );
}

function workerLineText(line: WorkWorkerLine): string {
  const text = [line.stateLabel, line.summary].filter(Boolean).join(" · ");
  return text ? `  ${text}` : "";
}

/** A Worker of a Work on its card (SPEC §11): 44 pt, its logo, name, what it does, the time; pushes its Session. */
function WorkerLine(props: { line: WorkWorkerLine; onPress: () => void }) {
  const line = props.line;
  return (
    <Button testID="work-card-worker" accessibilityLabel={[line.name, line.stateLabel, line.summary, line.time].filter(Boolean).join(", ")} disabled={!line.openable} onPress={props.onPress}
      class="min-h-[44px] flex-row items-center gap-2.5 border-t border-border px-4 py-2 active:opacity-70">
      <AgentIcon kind={line.agent} flavor={line.flavor} size={20} />
      <View class="min-w-0 flex-1">
        <Text class="text-sub line-clamp-1"><Text class="font-semibold">{line.name}</Text><Text class="text-muted-foreground">{workerLineText(line)}</Text></Text>
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
          {model().actions.length ? <ResultActions actions={model().actions} disabled={!props.handlers} onPress={(action) => props.handlers?.onAction(model(), action)} /> : null}
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
