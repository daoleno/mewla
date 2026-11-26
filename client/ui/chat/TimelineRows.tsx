import { computed, signal, Text, untracked, View } from "nsh";
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
import { sameWorkWorkerLines, WORK_CARD_WORKERS, workDetailWorkerRow, workerName, type WorkWorkerLine } from "../../core/work/workers";
import { derived } from "../kit/derived";
import type { TrayRow } from "../../core/tray/model";
import { RowEnd, RowText, RowTile } from "../tray/rowParts";
import { tray } from "../tray/state";
import { elapsedNow } from "../kit/elapsedClock";
import { answerPrompt, decideApproval } from "../../core/conversation/turn";
import type { BrainCurrentWork, ResultAction, WorkWorkerSession } from "../../core/work/types";
import { resultActionLabel } from "../../core/work/resultActions";
import type { AgentKind, TerminalFlavor } from "../../core/sessions/workerPresentation";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon as KitIcon } from "../kit/Icon";
import { clock } from "../calendar/clock";
import { serverForComputer, type ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import { grantPermissionName, grantRequestView, grantTitle, type GrantRequestTimelineItem } from "../../core/timeline/grantRequestModel";
import type { PluginNoticeTimelineItem } from "../../core/timeline/pluginNoticeModel";
import { grantRequestAction, type GrantRequestAction } from "../../core/screen/grantRequest";
import { showPermissionGuide } from "../mac/permissionGuide";
import { StateLine } from "../kit/RowTile";
import { signInFromChat } from "../more/extensions/plugins";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import { Icon, type IconName } from "../icons/Icon";
import { ICON_ASSETS } from "../icons/assets";
import { connection, current, currentTransport, servers, switchTo, workers } from "../session";
import { isWorkerActivelyRunning } from "../../core/sessions/workerStatusPresentation";
import { workAction } from "../../core/work/brainWork";
import { brainWorkTitle } from "../../core/work/surface";
import { delayed, RING, Spinner } from "../kit/wait";
import { haptic } from "../shell/haptics";
import { nav } from "../shell/nav";
import { agentDisplays, displayOfWorker } from "../screenfloat/agentDisplays";
import { isMermaidLanguage } from "../../core/mermaid/mermaid";
import { MermaidBlock } from "../mermaid/MermaidBlock";
import { density, requestCardSize, threadType } from "../kit/density";
import { writeClipboard } from "../kit/clipboard";

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
 * the platform's wash (NSH 0.5.1 draws it at 33 % on Android; SPEC's 20 % is not settable).
 */
export const SELECTION = "selection:bg-foreground";

/**
 * Code in a reply's text (SPEC §11 Inline code): the mono face a step under the 17 pt body (15), so the
 * x-heights meet, on the surface chip (radius 5) that shows on the canvas in both appearances. NSH 0.5.1
 * sizes it in points for the whole reply (no `em`: a heading's or a table cell's code is 15 too) and gives
 * spans no padding (longbridge/nsh#1901, #1902).
 */
export const INLINE_CODE = "font-mono text-[15px] text-foreground bg-card rounded-[5px]";

export function CodeBlock(props: { code: string; language?: string }) {
  const copied = signal(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const copy = () => {
    void writeClipboard(props.code).then(() => {
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

/** How a chat's Markdown draws its code blocks: as `CodeBlock`, a `mermaid` one as its diagram. */
export const renderCode = (block: { code: string; language?: string }) => {
  if (isMermaidLanguage(block.language)) return <MermaidBlock code={block.code} asCode={asChatCode} />;
  return <CodeBlock code={block.code} language={block.language} />;
};

const asChatCode = (code: string) => <CodeBlock code={code} language="mermaid" />;

/** `renderCode` for a reply that may still stream: its diagrams are drawn once it stops. */
export const renderCodeWhile = (streaming: () => boolean) => (block: { code: string; language?: string }) => {
  if (isMermaidLanguage(block.language)) return <MermaidBlock code={block.code} streaming={streaming} asCode={asChatCode} />;
  return <CodeBlock code={block.code} language={block.language} />;
};

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
  const type = threadType();
  return (
    <Text class={`min-w-0 flex-1 ${type.line} line-clamp-1 ${props.running ? "text-foreground" : "text-muted-foreground"}`}>
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

/** How long a step took, in the Ledger's right column: "46 ms", "1.8s", "15s", "4m 12s", "1h 3m". */
export function formatStepDuration(ms: number | undefined): string {
  if (ms === undefined) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`;
  return `${Math.floor(ms / 3_600_000)}h ${Math.round((ms % 3_600_000) / 60_000)}m`;
}

const TABULAR: { fontVariant: ["tabular-nums"] } = { fontVariant: ["tabular-nums"] };

/** Whether a Ledger row is open: a failed step starts open (owner 11-27), and a tap turns either the other way. */
const ledgerOpen = (expansion: Expansion, item: ActivityTimelineItem) => expansion.isOpen(stepKey(item)) !== Boolean(item.defaultExpanded && item.tone === "failed");
/** A step's own key in the open set: a folded run carries its first step's id. */
const stepKey = (item: ActivityTimelineItem) => `step:${item.id}`;

/** The Ledger's right column: what came back (failure in del, +/− in add and del) and how long it took. */
function LedgerEnd(props: { item: () => ActivityTimelineItem }) {
  const type = threadType();
  const facts = () => props.item().ledger ?? {};
  const failed = () => props.item().tone === "failed";
  const result = () => facts().result ?? (failed() ? "failed" : "");
  const time = () => (props.item().tone === "running" ? "" : formatStepDuration(facts().durationMs));
  return (
    <Text class={`shrink-0 ${type.line} text-muted-foreground`} style={TABULAR}>
      <If when={facts().added !== undefined || facts().removed !== undefined}>
        <Text class="text-add">{`+${facts().added ?? 0}`}</Text><Text>{" "}</Text><Text class="text-del">{`−${facts().removed ?? 0}`}</Text><Text>{time() ? " · " : ""}</Text>
      </If>
      <If when={result()}><Text class={failed() ? "font-semibold text-del" : ""}>{result()}</Text><Text>{time() ? " · " : ""}</Text></If>
      {time()}
    </Text>
  );
}

/**
 * One Ledger step (owner 11-27, variant A): its icon, the agent's words or what it did to what, the target in soft
 * mono, and on the right the result and the time. Open, it shows the command and the output (a failed step's end).
 */
function LedgerStep(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void; inRun?: boolean }) {
  const item = props.item;
  const type = threadType();
  const open = () => ledgerOpen(props.expansion, item());
  const failed = () => item().tone === "failed";
  const running = () => item().tone === "running";
  const files = () => activityFileRows(item());
  const expandable = () => Boolean(item().commandText || item().body || files().length);
  const target = () => item().ledger?.target;
  const more = () => item().ledger?.more;
  return (
    <View testID="ledger-step">
      <Button testID={failed() ? "ledger-step-failed" : "ledger-step-row"} accessibilityLabel={item().accessibilityLabel ?? item().title}
        onPress={() => expandable() && props.expansion.toggle(stepKey(item()))} class={`${props.inRun ? "min-h-[36px]" : "min-h-[44px]"} flex-row items-center gap-2`}>
        <Icon name={iconOf(item().icon, "construct")} color={failed() ? "del" : running() ? "foreground" : "faint"} size={16} />
        <Text class={`min-w-0 flex-1 ${type.line} line-clamp-1 text-foreground`}>
          {item().title}
          <If when={target()}><Text class={`font-mono ${type.mono} text-muted-foreground`}>{`  ${target()}`}</Text></If>
          <If when={more()}><Text class="text-muted-foreground">{`  +${more()}`}</Text></If>
        </Text>
        <If when={running()} else={<LedgerEnd item={item} />}><Spinner size={14} /></If>
      </Button>
      <If when={open()}>
        <View class="gap-1.5 pb-2 pl-6">
          <If when={item().commandText || item().body}>
            <View testID="ledger-output" selectable class="gap-1 rounded-xl bg-card px-3 py-2">
              <If when={item().commandText}><Text class={`font-mono ${type.mono} text-foreground ${SELECTION}`}>{`$ ${item().commandText ?? ""}`}</Text></If>
              <If when={item().body}><Text class={`font-mono ${type.mono} text-muted-foreground ${SELECTION}`}>{item().body ?? ""}</Text></If>
            </View>
          </If>
          {files().map((file) => <FileRow file={file} onFile={props.onFile} />)}
        </View>
      </If>
    </View>
  );
}

/** The agent's reasoning inside a run: its first line, soft; open, all of it. */
function LedgerThought(props: { item: () => ActivityTimelineItem; expansion: Expansion }) {
  const type = threadType();
  const open = () => props.expansion.isOpen(stepKey(props.item()));
  const firstLine = () => (props.item().body ?? "").trim().split("\n")[0];
  return (
    <Button testID="ledger-thought" onPress={() => props.expansion.toggle(stepKey(props.item()))} class="min-h-[36px] justify-center">
      <If when={open()} else={<Text class={`${type.line} italic text-muted-foreground line-clamp-1`}>{firstLine()}</Text>}>
        <TextView markdown={props.item().body ?? ""} selectable class={`text-sm text-muted-foreground ${SELECTION}`} />
      </If>
    </Button>
  );
}

/**
 * A run of calls between two messages (variant A, the Ledger): one quiet line, "Ran 4 commands, read 1 file", with
 * its time on the right; its failures (the only accent) show under it closed or open. Open, every step is a line
 * under a hairline rail.
 */
function LedgerRun(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void }) {
  const item = props.item;
  const type = threadType();
  const open = () => props.expansion.isOpen(item().id);
  const failed = () => item().ledger?.failed ?? 0;
  const shown = () => (open() ? item().steps ?? [] : (item().steps ?? []).filter((step) => step.tone === "failed"));
  return (
    <View testID="ledger-run">
      <Button testID="activity" accessibilityLabel={`${item().title}${failed() ? `, ${failed()} failed` : ""}`} onPress={() => props.expansion.toggle(item().id)}
        class="min-h-[44px] flex-row items-center gap-2">
        <Icon name={iconOf(item().icon, "construct")} color="muted-foreground" size={16} />
        <Text class={`min-w-0 flex-1 ${type.line} line-clamp-1 text-foreground`}>
          {item().title}
          <If when={failed()}><Text testID="ledger-run-failed" class="text-del">{` · ${failed()} failed`}</Text></If>
        </Text>
        <Text class={`shrink-0 ${type.line} text-muted-foreground`} style={TABULAR}>{formatStepDuration(item().ledger?.durationMs)}</Text>
        <Icon name={open() ? "chevron-up" : "chevron-down"} size={16} color="faint" />
      </Button>
      <If when={shown().length}>
        <View class="ml-2 border-l border-border pl-3.5">
          <Each of={shown()} key={(step) => step.id}>{(step) => (
            <If when={step().activityKind === "reasoning"} else={<LedgerStep item={step} expansion={props.expansion} onFile={props.onFile} inRun />}>
              <LedgerThought item={step} expansion={props.expansion} />
            </If>
          )}</Each>
        </View>
      </If>
    </View>
  );
}

/** A tool call, a reasoning or update line, or a folded run of finished calls. */
export function ActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void; pictures?: { load?: PictureLoader; open: (src: string) => void } }) {
  const item = props.item;
  // The agent's finished reasoning reads as its first line, soft, like the thoughts inside a run.
  const thought = () => item().activityKind === "reasoning" && item().tone !== "running" && !item().streaming;
  return (
    <If when={!thought()} else={<View class="px-4 py-0.5"><LedgerThought item={item} expansion={props.expansion} /></View>}>
    <If when={item().ledger} else={<ClassicActivityRow item={item} expansion={props.expansion} onFile={props.onFile} pictures={props.pictures} />}>
      <View class="px-4 py-0.5">
        <If when={item().steps?.length} else={<LedgerStep item={item} expansion={props.expansion} onFile={props.onFile} />}>
          <LedgerRun item={item} expansion={props.expansion} onFile={props.onFile} />
        </If>
        <If when={toolPictures(item()).length}><View testID="tool-pictures" class="flex-row flex-wrap items-start gap-2 pb-1.5 pl-6">
            {toolPictures(item()).map((src) => (
              <ChatImage frame="tool" src={src} alt={item().title} load={props.pictures?.load} onOpen={() => props.pictures?.open(src)} />
            ))}
          </View></If>
      </View>
    </If>
    </If>
  );
}

/** A row an older daemon's events made (no `tool`): the row as it was before the Ledger. */
function ClassicActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion; onFile?: (path: string) => void; pictures?: { load?: PictureLoader; open: (src: string) => void } }) {
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
          <If when={item().queryText}><TextView document={{ blocks: [{ type: "paragraph", content: [{ type: "text", text: item().queryText ?? "" }] }] }} selectable class={`text-sm ${SELECTION}`} /></If>
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

/**
 * An Extensions account whose sign-in ended or is about to (an AWS profile, Slack), in Chat: what happened, and
 * Sign in, which opens Extensions on that account's own sign-in (for AWS Identity Center, a code to approve).
 */
export function PluginNoticeCard(props: { item: () => PluginNoticeTimelineItem }) {
  const it = props.item;
  const missing = signal(false);
  const signIn = async () => {
    haptic.impact();
    missing.set(!(await signInFromChat(it().accountId)));
  };
  return (
    <View testID="plugin-notice" class="mx-4 my-1 gap-1.5 rounded-xl border border-border bg-card px-3 py-2.5">
      <View class="flex-row items-center gap-2">
        <Icon name="warning" color={toneColor("failed")} size={16} />
        <Text class="flex-1 text-base font-semibold" role="header">{it().title}</Text>
      </View>
      <If when={it().body}>{(body) => <View selectable><Text class={`text-sm text-muted-foreground ${SELECTION}`}>{body()}</Text></View>}</If>
      <View class="flex-row items-center gap-3">
        <Button testID="plugin-notice-sign-in" accessibilityLabel={`Sign in: ${it().title}`} onPress={() => void signIn()}
          class="self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
          <Text class="text-sm font-semibold text-primary">Sign in</Text>
        </Button>
        <If when={missing()}><Text testID="plugin-notice-missing" class="text-sm text-muted-foreground">This computer no longer has that account</Text></If>
      </View>
    </View>
  );
}

/**
 * What a Worker needs on this Mac that only the person can give in System Settings (DESIGN-MAC.md §2.4, `m-perm`): the
 * reason in one line, and Not now / Open System Settings; sealed once the grant counts or the person says Not now. A grant
 * that is given but not yet in effect ends in one button, Restart Mewla. The button's answer is the card's next state, which
 * the thread pushes; a failed one is said in a line under the buttons.
 */
export function GrantRequestCard(props: { item: () => GrantRequestTimelineItem }) {
  const it = props.item;
  const view = () => grantRequestView(it());
  const failed = signal("");
  const busy = signal(false);
  const press = async (action: GrantRequestAction) => {
    const server = current();
    if (!server || untracked(busy)) return;
    haptic.impact();
    busy.set(true);
    failed.set("");
    try {
      // On this Mac the card's Open System Settings also puts the panel to drag Mewla in beside it.
      if (action === "open_settings") showPermissionGuide(server, it().permission);
      await grantRequestAction(server, it().id, action);
    } catch (error) {
      failed.set(error instanceof Error ? error.message : String(error));
    } finally {
      busy.set(false);
    }
  };
  // The Mac's card is `m-perm`'s: the seal dot and "Allow screen recording", the reason, then Not now and Open System
  // Settings side by side, full width, at the request card's sizes. Phones keep their card.
  if (density() !== "phone") return <MacGrantCard item={it} view={view} busy={busy} failed={failed} press={press} />;
  return (
    <View testID="grant-request" accessibilityLabel={`${grantPermissionName(it().permission)}: ${it().reason}`} class="mx-4 my-1 gap-2.5 rounded-xl border border-border bg-card px-3 py-2.5">
      <View selectable><Text class={`text-base ${SELECTION}`}>{it().reason}</Text></View>
      <If when={view().sealed}>
        <StateLine testID="grant-request-sealed" tone={it().state === "granted" ? "ok" : "off"} label={view().word ?? ""} />
      </If>
      <If when={view().buttons.length > 0}>
        <View class="flex-row items-center gap-2">
          <Each of={view().buttons} key={(button) => button.id}>{(button) => (
            <Button testID={`grant-request-${button().id}`} accessibilityLabel={button().label} onPress={() => void press(button().id)}
              class={`self-start rounded-full px-3.5 py-1.5 active:opacity-70 ${button().id === "not_now" ? "border border-border" : "bg-primary"}`}>
              <Text class={`text-sm font-semibold ${button().id === "not_now" ? "text-primary" : "text-primary-foreground"}`}>{button().label}</Text>
            </Button>
          )}</Each>
        </View>
      </If>
      <If when={failed()}><Text testID="grant-request-failed" class="text-sm text-muted-foreground">{failed()}</Text></If>
    </View>
  );
}

function MacGrantCard(props: { item: () => GrantRequestTimelineItem; view: () => ReturnType<typeof grantRequestView>; busy: () => boolean; failed: () => string; press: (action: GrantRequestAction) => Promise<void> }) {
  const it = props.item;
  const S = requestCardSize();
  const type = threadType();
  return (
    <View testID="grant-request" accessibilityLabel={`${grantPermissionName(it().permission)}: ${it().reason}`} class={`mx-4 my-1 ${S.card}`}>
      <View testID="grant-request-title" class="flex-row items-center gap-2">
        <If when={!props.view().sealed}><View class="size-1.5 rounded-full bg-seal" /></If>
        <Text class={`${type.line} font-semibold text-foreground`}>{grantTitle(it().permission)}</Text>
      </View>
      <View selectable><Text class={`${type.question} ${SELECTION}`}>{it().reason}</Text></View>
      <If when={props.view().sealed}>
        <StateLine testID="grant-request-sealed" tone={it().state === "granted" ? "ok" : "off"} label={props.view().word ?? ""} />
      </If>
      <If when={props.view().buttons.length > 0}>
        <View class="flex-row items-center gap-2">
          <Each of={props.view().buttons} key={(button) => button.id}>{(button) => (
            <Button testID={`grant-request-${button().id}`} accessibilityLabel={button().label} disabled={props.busy()} onPress={() => void props.press(button().id)}
              class={`${S.button} flex-1 items-center justify-center active:opacity-70 ${button().id === "not_now" ? "bg-accent" : "bg-seal"}`}>
              <Text class={`${S.label} font-semibold ${button().id === "not_now" ? "text-foreground" : "text-seal-foreground"}`}>{button().label}</Text>
            </Button>
          )}</Each>
        </View>
      </If>
      <If when={props.failed()}><Text testID="grant-request-failed" class={`${type.line} text-muted-foreground`}>{props.failed()}</Text></If>
    </View>
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
 * A page or app a Worker has open on its display (a sign-in it left for the person, after its turn too): the one tap
 * to it in Screen, where the person sees and takes it over.
 */
export function WorkerScreenButton(props: { worker: () => string | undefined }) {
  const open = computed(() => !!displayOfWorker(agentDisplays(), props.worker()));
  // A page the agent left for the person (its turn is over, its window still open): one step takes it over.
  const left = computed(() => {
    const worker = workers().find((w) => w.id === props.worker());
    return open() && !!worker && !isWorkerActivelyRunning(worker.status);
  });
  return (
    <If when={open()}><Button testID="work-card-screen" accessibilityLabel={left() ? "Take over its screen" : "Open its screen"} onPress={() => { const worker = props.worker(); if (worker) nav.push(left() ? { name: "screen", worker, takeOver: true } : { name: "screen", worker }); }}
        class="h-[30px] flex-row items-center gap-1 rounded-full bg-background px-3 active:opacity-70">
        <KitIcon name={left() ? "hand" : "desktop"} size={16} tone="ink" />
        <Text class="text-caption font-semibold">{left() ? "Take over" : "Screen"}</Text>
      </Button></If>
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
      <WorkerScreenButton worker={() => line.sessionId} />
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
  if (model.state === "Waiting" || lines.some((line) => line.state === "waiting")) return { words: "Waiting", dot: null };
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
    // The Work's judged progress, as its tray row draws it (§11 Progress); never a Worker's own. Named as the
    // judge wrote it in the person's language; the Session's code name and its Worker's words only before
    // (owner 2026-11-19: "看不懂这个"). A parked Work says it asks nothing until the judge says what it waits on.
    if (line) {
      const worker = workDetailWorkerRow(line, elapsedNow());
      return { ...worker, title: props.model().shownTitle || worker.title, line: props.model().shownLine || worker.line, mark: props.model().mark };
    }
    const agent = props.agent();
    return {
      key: `work:${props.model().workId}`, title: props.model().shownTitle || workerName(agent?.name) || props.model().sessionName || props.model().title, line: props.model().summary,
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
      <WorkerScreenButton worker={sessionId} />
      <If when={request()} else={<RowEnd row={row} />}><Button testID={request()?.oneTap ? "work-card-approve" : "work-card-answer"} disabled={sending()} onPress={() => (request()?.oneTap ? void approve() : press())}
          class="h-[30px] items-center justify-center rounded-full bg-seal px-3 active:opacity-70 disabled:opacity-40">
          <Text class="text-caption font-semibold text-seal-foreground">{request()?.oneTap ? "Approve" : "Answer"}</Text>
        </Button></If>
    </Button>
  );
}

/**
 * A Work that waits on Brain's question (SPEC §11 Work card, "A Work that asks"; owner 2026-11-13, shot 11): the
 * question whole, then its choices as rows that answer at once (Brain's pick tagged), then "Something else…",
 * which opens Work detail with its reply box. It stays where the Work is in the thread, never pinned at its foot;
 * the source at the top line opens Work detail, where Snooze puts it aside (the tray keeps it).
 */
function WorkQuestion(props: { work: () => BrainCurrentWork; handlers?: WorkCardHandlers }) {
  const busy = signal("");
  const failed = signal(false);
  const offline = () => connection() !== "connected";
  const workId = () => props.work().work_id;
  const answer = async (choice: string) => {
    const transport = currentTransport();
    if (!transport || transport.state !== "connected" || busy()) return;
    busy.set(choice);
    failed.set(false);
    try {
      await workAction(transport, workId(), "reply", { text: choice });
      // A request answered and acknowledged: the one light tick (SPEC §16.3 Haptics).
      haptic.impact("light");
    } catch {
      failed.set(true);
    } finally {
      busy.set("");
    }
  };
  const choices = () => props.work().choices ?? [];
  return (
    <View testID="work-asks" class="overflow-hidden rounded-(--radius-group) bg-card">
      <View class="gap-2 px-4 pb-4 pt-1">
        <View class="min-h-[44px] flex-row items-center gap-2">
          <View class="size-2 rounded-full bg-seal" />
          <Text class="text-caption font-semibold">Brain asks</Text>
          <Button testID="work-asks-source" accessibilityLabel={`Open ${brainWorkTitle(props.work().title)}`} disabled={!props.handlers} onPress={() => props.handlers?.onTitle(workId())}
            class="min-h-[44px] min-w-0 flex-1 flex-row items-center justify-end gap-1 active:opacity-70">
            <Text class="min-w-0 shrink text-caption text-muted-foreground line-clamp-1">{brainWorkTitle(props.work().title)}</Text>
            <KitIcon name="chevron-right" size={14} tone="faint" />
          </Button>
        </View>
        <View selectable><Text testID="work-asks-question" class="text-body font-semibold">{props.work().question ?? ""}</Text></View>
        <If when={failed()} else={<If when={offline()}><Text testID="work-asks-offline" class="text-sub text-muted-foreground">{`${current()?.name || "Your computer"} is offline`}</Text></If>}><Text testID="work-asks-failed" class="text-sub">{`Didn't reach ${current()?.name || "your computer"}`}</Text></If>
      </View>
      <Each of={choices()} key={(choice) => choice}>{(choice) => <WorkChoiceRow label={untracked(choice)} pick={untracked(choice) === props.work().recommended} busy={() => busy() === choice()} disabled={() => offline() || Boolean(busy())} onPress={() => void answer(untracked(choice))} />}</Each>
      <Button testID="work-asks-other" disabled={!props.handlers} onPress={() => props.handlers?.onTitle(workId())}
        class="min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 active:bg-accent">
        <Text class="min-w-0 flex-1 text-body">{choices().length ? "Something else…" : "Reply"}</Text>
        <KitIcon name="chevron-right" size={16} tone="faint" />
      </Button>
    </View>
  );
}

/** A choice of a Work's question: a 54 pt row with a ring; Brain's pick wears the request card's tag. */
function WorkChoiceRow(props: { label: string; pick: boolean; busy: () => boolean; disabled: () => boolean; onPress: () => void }) {
  // Not optimistic (SPEC §16.2 pattern 8): the press, then the ring in the row's circle after `wait.quiet`.
  const ring = delayed(props.busy);
  return (
    <Button testID={`work-asks-choice-${props.label}`} disabled={props.disabled()} onPress={props.onPress}
      class={`min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 py-2 active:bg-accent ${props.busy() ? "" : "disabled:opacity-40"}`}>
      <Text class="min-w-0 flex-1 text-body">{props.label}</Text>
      <If when={props.pick}><Text testID="work-asks-pick" class="text-caption text-muted-foreground">Brain's pick</Text></If>
      <View class="size-[22px] items-center justify-center">
        <If when={ring()} else={<View class="size-[22px] rounded-full border-2 border-faint" />}>
          <Spinner size={RING.button} label={props.label} />
        </If>
      </View>
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
  // Its Sessions' lines, made once per change of the computer's Sessions or this Work and news only when they say
  // something else. Read as a function, a card made them some eight times a draw, each making every Session's row
  // model again, and every card of Brain's thread did so on every Session update: on the owner's phone (11-14,
  // v0.4.2) such a draw passed NSH's 2 s call budget (sessionRowModel under a Work card, "Error: interrupted").
  const lines = derived(() => props.handlers?.workers(props.item().currentWork?.worker_sessions) ?? [], { equals: sameWorkWorkerLines });
  const state = () => workCardState(model(), lines());
  const open = () => props.item().onPress?.();
  // Which card, as one word: made again only when it changes. `lines()` is new on every change of the
  // computer's Sessions, which drew every Work card of the thread anew when read in the branch.
  const shape = computed(() => (model().kind === "line" ? "line" : model().kind === "result" ? "result" : lines().length <= 1 && (lines().length === 1 || model().sessionId) ? "one" : "many"));
  // Brain's question on this Work: the card is the question (an open Work only; the model says when it asks).
  const asks = computed(() => (model().kind === "needs" && props.item().currentWork?.question ? props.item().currentWork : undefined));
  return (
    <View class="px-4 py-1">
      <If when={asks()} else={<If when={shape() === "line"} else={<If when={shape() === "result"} else={<If when={shape() === "one"} else={<View testID={state().dot === "seal" ? "work-needs" : "work-card"} class="overflow-hidden rounded-(--radius-group) bg-card">
          <Button testID="work-card-title" disabled={!props.handlers} onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[52px] flex-row items-center gap-2 px-4 active:opacity-70">
            <Text class="min-w-0 flex-1 text-body font-semibold line-clamp-1">{model().title}</Text>
            <If when={state().dot}><View class={`size-2 rounded-full ${state().dot === "seal" ? "bg-seal" : "bg-foreground"}`} /></If>
            <Text testID="work-card-state" class="text-sub text-muted-foreground">{state().words}</Text>
            <KitIcon name="chevron-right" size={16} tone="faint" />
          </Button>
          {/* Several Workers: the judge's line for the Work, then how many Workers, which Work detail lists (owner's
              phone 11-19 22:45: each Session's code name and last words read as nothing). Before the judge, the Workers. */}
          <If when={lines().length && !model().shownLine} else={<>
              <If when={model().summary}><View class="border-t border-border px-4 py-3"><Text testID="work-card-line" class="text-sub text-muted-foreground line-clamp-2">{model().summary}</Text></View></If>
              <If when={lines().length}><Button testID="work-card-workers" onPress={() => props.handlers?.onTitle(model().workId)} class="min-h-[44px] flex-row items-center border-t border-border px-4 active:opacity-70">
                  <Text class="min-w-0 flex-1 text-sub text-muted-foreground">{`${lines().length} Workers`}</Text>
                  <KitIcon name="chevron-right" size={16} tone="faint" />
                </Button></If>
            </>}><>
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
          <If when={model().summary}><View selectable><Text class={`text-sub ${SELECTION}`}>{model().summary}</Text></View></If>
          <If when={model().actions.length}><ResultActions actions={model().actions} disabled={!props.handlers} onPress={(action) => props.handlers?.onAction(model(), action)} /></If>
        </View></If>}><Button testID="work-line" accessibilityLabel={`${model().title}, ${model().state}`} disabled={!props.handlers} onPress={() => props.handlers?.onLine(model())}
          class="min-h-[44px] flex-row items-center gap-2.5 active:opacity-70">
          <WorkLogo agent={agent()} size={20} />
          <Text class="min-w-0 flex-1 text-sub text-muted-foreground line-clamp-1"><Text class="font-semibold text-foreground">{model().title}</Text>{model().summary ? ` · ${model().summary}` : ""}</Text>
          <KitIcon name="check" size={18} tone="soft" />
        </Button></If>}>{(work) => <WorkQuestion work={work} handlers={props.handlers} />}</If>
    </View>
  );
}
