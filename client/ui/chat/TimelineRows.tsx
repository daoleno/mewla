import { Clipboard, computed, signal, Text, View } from "nsh";
import { Button, TextView } from "nsh/base";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import { activityFileRows, type ActivityFileRow } from "../../core/timeline/activityFiles";
import { splitActivityStatusDetail } from "../../core/timeline/activityModel";
import { toolPictures } from "../../core/timeline/threadPictures";
import { distinctivePath } from "../../core/timeline/toolCallSemantics";
import { compactPathLabel } from "../../core/timeline/pathDisplay";
import { ChatImage, type PictureLoader } from "./ChatImage";
import type { BrainWorkEventTimelineItem, PlanTimelineItem } from "../../core/timeline/types";
import { workCardModel, type WorkCardModel } from "../../core/timeline/workCardModel";
import { WORK_CARD_WORKERS, workDetailWorkerRow, workerName, type WorkWorkerLine } from "../../core/work/workers";
import type { TrayRow } from "../../core/tray/model";
import { RowEnd, RowText, RowTile } from "../tray/rowParts";
import { tray } from "../tray/state";
import { elapsedNow } from "../kit/elapsedClock";
import { answerPrompt, decideApproval } from "../../core/conversation/turn";
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
import { current, currentTransport, servers, switchTo } from "../session";

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
const toneColor = (tone: string | undefined) => (tone === "failed" || tone === "danger" ? "destructive" : tone === "running" || tone === "accent" ? "foreground" : "muted-foreground");

/**
 * A chat's code block (soft-final SPEC §11 Message, the mock's `.codeblock`): a surface card, radius 20,
 * with a 48 pt header (the language or "code" in soft mono, then Copy, which shows a check for a moment),
 * the code in mono 13/20 under it, scrolling sideways. NSH draws and colours the code.
 */
/**
 * Selected text in the app's ink, as in the terminal (SPEC §11): the handles take it and the highlight is it at
 * the platform's wash (NSH 0.5.0 draws it at 33 % on Android; SPEC's 20 % is not settable).
 */
export const SELECTION = "selection:bg-foreground";

/**
 * Code in a reply's text (SPEC §11 Inline code): the mono face a step under the 17 pt body (15), so the
 * x-heights meet, on the surface chip (radius 5) that shows on the canvas in both appearances. NSH 0.5.0
 * sizes it in points for the whole reply (no `em`: a heading's or a table cell's code is 15 too) and gives
 * spans no padding.
 */
export const INLINE_CODE = "font-mono text-[15px] text-foreground bg-card rounded-[5px]";

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
        <Text class="min-w-0 flex-1 select-none font-mono text-code text-muted-foreground line-clamp-1">{props.language || "code"}</Text>
        <Button testID="code-copy" accessibilityLabel={copied() ? "Copied" : "Copy"} onPress={copy} class="size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name={copied() ? "check" : "copy"} size={18} color="muted-foreground" />
        </Button>
      </View>
      <TextView document={{ blocks: [{ type: "codeBlock", code: props.code, language: props.language ?? "" }] }} selectable class={SELECTION}
        classes={{ codeBlock: "m-0 rounded-none border-0 bg-transparent p-0", codeBlockScroll: "m-0 p-0", codeBlockCode: "m-0 px-4 pb-4 pt-0" }} />
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
      <Icon name="document-text" size={18} color="muted-foreground" />
      <Text class={`min-w-0 shrink font-mono text-code line-clamp-1 ${props.onFile ? "text-foreground" : "text-muted-foreground"}`}>{shownPath(file.path)}</Text>
      <If when={file.added !== undefined}><Text testID="activity-file-added" class="font-mono text-code text-add">{`+${file.added}`}</Text></If>
      <If when={file.removed !== undefined}><Text testID="activity-file-removed" class="font-mono text-code text-del">{`−${file.removed}`}</Text></If>
    </Button>
  );
}

/** A tool row's words: what it did in 600, then the detail, soft, on one line that truncates. */
function ToolLine(props: { title: string; detail?: string; running?: boolean }) {
  return (
    <Text class={`min-w-0 flex-1 text-sub line-clamp-1 ${props.running ? "text-foreground" : "text-muted-foreground"}`}>
      <Text class="font-semibold">{props.title}</Text>
      {props.detail ? `  ${props.detail}` : ""}
    </Text>
  );
}

/** A row's detail without the "Done" a finished call needs no word for; a failed one says Failed (one word set). */
function shownDetail(item: ActivityTimelineItem): string | undefined {
  const { mark, rest } = splitActivityStatusDetail(item.detail, item.tone);
  if (mark === "failed") return rest ? `Failed · ${rest}` : "Failed";
  return rest && rest !== item.title ? rest : undefined;
}

/** A tool call, a reasoning or update line, or a folded run of finished calls. */
export function ActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void; pictures?: { load?: PictureLoader; open: (src: string) => void } }) {
  const item = props.item;
  const open = () => props.expansion.isOpen(item().id);
  const files = () => activityFileRows(item());
  const expandable = () => Boolean(item().commandText || item().queryText || item().body || files().length || item().steps?.length || item().statusLine);
  return (
    <View class="px-4 py-0.5">
      {/* A tool row (soft-final SPEC §11): 44 tall, an 18 pt icon, what it did in 600 then where in soft, one line, a chevron. */}
      <Button testID="activity" onPress={() => expandable() && props.expansion.toggle(item().id)} class="min-h-[44px] flex-row items-center gap-2">
        <Icon name={iconOf(item().icon, "construct")} color={toneColor(item().tone)} size={18} />
        <ToolLine title={item().title} detail={shownDetail(item())} running={item().tone === "running"} />
        <If when={expandable()}><Icon name={open() ? "chevron-up" : "chevron-down"} size={18} color="faint" /></If>
      </Button>
      {/* The pictures the call produced, under its row whether or not it is open (SPEC §1.7). */}
      <If when={toolPictures(item()).length}><View testID="tool-pictures" class="flex-row flex-wrap items-start gap-2 pb-1.5 pl-6">
          {toolPictures(item()).map((src) => (
            <ChatImage frame="tool" src={src} alt={item().title} load={props.pictures?.load} onOpen={() => props.pictures?.open(src)} />
          ))}
        </View></If>
      <If when={open()}><View class="gap-1.5 pb-2 pl-6">
          <If when={item().steps?.length}>{item().steps!.map((step) => (
            <View testID="activity-step" class="min-h-[44px] flex-row items-center gap-2">
              <Icon name={iconOf(step.icon, "construct")} color={toneColor(step.tone)} size={18} />
              <ToolLine title={step.title} detail={shownDetail(step)} />
            </View>
          ))}</If>
          <If when={item().commandText}><Mono text={item().commandText ?? ""} /></If>
          <If when={item().queryText}><TextView document={{ blocks: [{ type: "paragraph", content: [{ type: "text", text: item().queryText ?? "" }] }] }} selectable class="text-sm" /></If>
          <If when={item().statusLine}><Text class="text-xs text-muted-foreground">{item().statusLine}</Text></If>
          <If when={item().body}><If when={item().bodyKind === "terminal"} else={<TextView markdown={item().body ?? ""} selectable renderCodeBlock={renderCode} class={`text-sm ${SELECTION}`} />}><Mono text={item().body ?? ""} /></If></If>
          {files().map((file) => <FileRow file={file} onFile={props.onFile} />)}
        </View></If>
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
      <If when={props.item().explanation}><Text class="text-sm text-muted-foreground">{props.item().explanation}</Text></If>
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
        <If when={props.status}>{(status) => <Text class="text-xs text-muted-foreground">{status()}</Text>}</If>
      </View>
      <If when={props.title}>{(title) => <Text class="text-base font-semibold">{title()}</Text>}</If>
      <If when={open()} else={<If when={props.preview}>{(preview) => <Text class="text-sm text-muted-foreground line-clamp-3">{preview()}</Text>}</If>}><TextView markdown={props.body || props.preview} selectable class={`text-sm ${SELECTION}`} /></If>
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
      <If when={target()}><Button testID="computer-message-open" accessibilityLabel={`Open on ${it().computer}`} onPress={() => { const server = target(); if (server) switchTo(server); }}
          class="mx-4 mb-1 self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
          <Text class="text-sm font-semibold text-primary">Open on {it().computer}</Text>
        </Button></If>
    </View>
  );
}

/** Where a Work card leads outside the thread; none (a past thread) leaves it to read. */
export type WorkCardHandlers = {
  /** The agent that ran a Session, for the card's logo and "Agent · Done 11:20". */
  agentOf: (sessionId: string | undefined) => { kind: AgentKind; flavor: TerminalFlavor; name: string } | null;
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
  return [workerName(agent) || undefined, [model.state, at].filter(Boolean).join(" ")].filter(Boolean).join(" · ");
}

function WorkLogo(props: { agent: { kind: AgentKind; flavor: TerminalFlavor } | null; size: number }) {
  return <If when={props.agent} else={<View class="items-center justify-center rounded-lg bg-foreground" style={{ width: props.size, height: props.size }}><KitIcon name="brain" size={props.size - 10} tone="canvas" /></View>}>{(agent) => <AgentIcon kind={agent()?.kind ?? "terminal"} flavor={agent()?.flavor} size={props.size - 8} />}</If>;
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

/**
 * A Worker of a Work on its card (SPEC §11 Work card): 44 pt, its logo, its task name, what it does, then its state as
 * a tray row ends ("● 10m", a red dot and "Needs you"), never a clock time while it works. Pushes its Session.
 */
function WorkerLine(props: { line: WorkWorkerLine; onPress: () => void }) {
  const line = props.line;
  const row = () => workDetailWorkerRow(line, elapsedNow());
  return (
    <Button testID="work-card-worker" accessibilityLabel={[row().title, row().line, row().end].filter(Boolean).join(", ")} disabled={!line.openable} onPress={props.onPress}
      class="min-h-[44px] flex-row items-center gap-2.5 border-t border-border px-4 py-2 active:opacity-70">
      <AgentIcon kind={line.agent} flavor={line.flavor} size={20} />
      <View class="min-w-0 flex-1">
        <Text class="text-sub line-clamp-1"><Text class="font-semibold">{row().title}</Text><Text class="text-muted-foreground">{row().line ? `  ${row().line}` : ""}</Text></Text>
      </View>
      <RowEnd row={row} />
    </Button>
  );
}

/**
 * The Work's state, its most urgent Worker's (SPEC §11 Work card): a red dot and "Needs you" if any Worker waits on
 * you; else "● N running"; else Queued or Waiting; never Running while something inside it needs you.
 */
function workCardState(model: WorkCardModel, lines: readonly WorkWorkerLine[]): { words: string; dot: "seal" | "ink" | null } {
  if (model.kind === "needs" || lines.some((line) => line.state === "blocked")) return { words: "Needs you", dot: "seal" };
  const running = lines.filter((line) => line.state === "running").length;
  if (running > 0) return { words: `${running} running`, dot: "ink" };
  if (model.state === "Waiting") return { words: "Waiting", dot: null };
  if (model.state === "Running") return lines.length ? { words: "Queued", dot: null } : { words: "1 running", dot: "ink" };
  return { words: model.state, dot: null };
}

/**
 * A Work with one Session (SPEC §11 Work card): that Session's tray row inside a card, so its title is written once.
 * Needing you, its line is the ask and it ends in the red tag: Approve answers at once, Answer opens the Session.
 */
function OneSessionWork(props: { model: () => WorkCardModel; line: () => WorkWorkerLine | undefined; agent: () => ReturnType<WorkCardHandlers["agentOf"]>; handlers?: WorkCardHandlers; open: () => void }) {
  const sessionId = () => props.line()?.sessionId ?? props.model().sessionId;
  const request = () => (sessionId() ? tray().needs.find((r) => r.workerId === sessionId()) : undefined);
  const sending = signal(false);
  const row = (): TrayRow => {
    const line = props.line();
    // The Work's judged progress, as its tray row draws it (§11 Progress); never a Worker's own.
    if (line) return { ...workDetailWorkerRow(line, elapsedNow()), mark: props.model().mark };
    const agent = props.agent();
    return {
      key: `work:${props.model().workId}`, title: workerName(agent?.name) || props.model().sessionName || props.model().title, line: props.model().summary,
      end: "", tile: agent ? { kind: "agent", agent: agent.kind, flavor: agent.flavor } : { kind: "brain" }, target: { open: "work", workId: props.model().workId },
      mark: props.model().mark,
    };
  };
  const approve = async () => {
    const tap = request()?.oneTap;
    const transport = currentTransport();
    if (!tap || !transport || sending()) return;
    sending.set(true);
    try {
      if (tap.kind === "approval") await decideApproval(transport, tap.workerId, tap.approvalId, tap.optionId);
      else await answerPrompt(transport, tap.workerId, tap.promptId, { number: tap.number });
    } catch {
      // The card in the thread still asks; the tray says why.
    } finally {
      sending.set(false);
    }
  };
  const press = () => (sessionId() ? props.handlers?.onSession(sessionId()!) : props.open());
  return (
    <Button testID="work-card-one" accessibilityLabel={[row().title, request()?.ask ?? row().line].filter(Boolean).join(", ")} disabled={!props.handlers} onPress={press}
      class="min-h-[68px] flex-row items-center gap-3.5 overflow-hidden rounded-(--radius-group) bg-card px-4 py-3 active:opacity-70">
      <RowTile tile={() => row().tile} mark={() => row().mark} />
      <RowText row={() => { const r = request(); return r ? { ...row(), line: r.ask ?? r.title, mark: undefined } : row(); }} />
      <If when={request()} else={<RowEnd row={row} />}><Button testID={request()?.oneTap ? "work-card-approve" : "work-card-answer"} disabled={sending()} onPress={() => (request()?.oneTap ? void approve() : press())}
          class="h-[30px] items-center justify-center rounded-full bg-seal px-3 active:opacity-70 disabled:opacity-40">
          <Text class="text-caption font-semibold text-seal-foreground">{request()?.oneTap ? "Approve" : "Answer"}</Text>
        </Button></If>
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
  const state = () => workCardState(model(), lines());
  const open = () => props.item().onPress?.();
  // Which card, as one word: made again only when it changes. `lines()` is new on every change of the
  // computer's Sessions, which drew every Work card of the thread anew when read in the branch.
  const shape = computed(() => (model().kind === "line" ? "line" : model().kind === "result" ? "result" : lines().length <= 1 && (lines().length === 1 || model().sessionId) ? "one" : "many"));
  return (
    <View class="px-4 py-1">
      <If when={shape() === "line"} else={<If when={shape() === "result"} else={<If when={shape() === "one"} else={<View testID={state().dot === "seal" ? "work-needs" : "work-card"} class="overflow-hidden rounded-(--radius-group) bg-card">
          <Button testID="work-card-title" disabled={!props.handlers} onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[52px] flex-row items-center gap-2 px-4 active:opacity-70">
            <Text class="min-w-0 flex-1 text-body font-semibold line-clamp-1">{model().title}</Text>
            <If when={state().dot}><View class={`size-2 rounded-full ${state().dot === "seal" ? "bg-seal" : "bg-foreground"}`} /></If>
            <Text testID="work-card-state" class="text-sub text-muted-foreground">{state().words}</Text>
            <KitIcon name="chevron-right" size={16} tone="faint" />
          </Button>
          <If when={lines().length} else={<If when={model().summary}><View class="border-t border-border px-4 py-3"><Text class="text-sub text-muted-foreground line-clamp-2">{model().summary}</Text></View></If>}><>
              {lines().slice(0, WORK_CARD_WORKERS).map((line) => <WorkerLine line={line} onPress={() => props.handlers?.onSession(line.sessionId)} />)}
              <If when={lines().length > WORK_CARD_WORKERS}><Button testID="work-card-more" onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[44px] flex-row items-center border-t border-border px-4 active:opacity-70">
                  <Text class="min-w-0 flex-1 text-sub text-muted-foreground">{`${lines().length - WORK_CARD_WORKERS} more`}</Text>
                  <KitIcon name="chevron-right" size={16} tone="faint" />
                </Button></If>
            </></If>
        </View>}><OneSessionWork model={model} line={() => lines()[0]} agent={agent} handlers={props.handlers} open={() => props.handlers?.onTitle(model().workId)} /></If>}><View testID="work-result" class="gap-3 rounded-(--radius-group) bg-card p-4">
          <Button testID="work-result-open" accessibilityLabel={`Open ${model().title}`} disabled={!props.item().onPress} onPress={open} class="-my-1 min-h-[52px] flex-row items-center gap-3 active:opacity-70">
            <WorkLogo agent={agent()} size={28} />
            <View class="min-w-0 flex-1">
              <Text class="text-body font-semibold line-clamp-1">{model().title}</Text>
              <Text class="text-caption text-muted-foreground line-clamp-1">{resultMeta(model(), agent()?.name)}</Text>
            </View>
            <View testID="work-unread" class="size-2 rounded-full bg-foreground" />
            <KitIcon name="chevron-right" size={18} tone="faint" />
          </Button>
          <If when={model().summary}><View selectable><Text class="text-sub">{model().summary}</Text></View></If>
          <If when={model().actions.length}><ResultActions actions={model().actions} disabled={!props.handlers} onPress={(action) => props.handlers?.onAction(model(), action)} /></If>
        </View></If>}><Button testID="work-line" accessibilityLabel={`${model().title}, ${model().state}`} disabled={!props.handlers} onPress={() => props.handlers?.onLine(model())}
          class="min-h-[44px] flex-row items-center gap-2.5 active:opacity-70">
          <WorkLogo agent={agent()} size={20} />
          <Text class="min-w-0 flex-1 text-sub text-muted-foreground line-clamp-1"><Text class="font-semibold text-foreground">{model().title}</Text>{model().summary ? ` · ${model().summary}` : ""}</Text>
          <KitIcon name="check" size={18} tone="soft" />
        </Button></If>
    </View>
  );
}
