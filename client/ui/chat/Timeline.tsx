import { cleanup, Clipboard, colorScheme, computed, effect, ErrorBoundary, List, platform, signal, Text, untracked, View, type JSX, type ListRef, type ViewRef } from "nsh";
import { delayed } from "../kit/wait";
import { motion } from "../../core/motion/tokens";
import { currentTransport } from "../session";
import type { CapsuleReading } from "../shell/Capsule";
import { noteScroll } from "../pets/hold";
import { elapsedShort } from "../../core/sessions/activity";
import { elapsedNow } from "../kit/elapsedClock";
import { Button, TextView } from "nsh/base";
import { isWorkingRow } from "../../core/conversation/timeline";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import type { AnsweredTimelineItem, BrainWorkEventTimelineItem, DateDividerItem, MessageTimelineItem, PlanTimelineItem, TimelineItem } from "../../core/timeline/types";
import { withDayDividers } from "../../core/timeline/dayDividers";
import { FOLDED_TEXT_HEIGHT, messageFolds } from "../../core/timeline/messageFold";
import { readingPlace, readingPlaceIndex, readingPlaceOf, rememberReadingPlace } from "../../core/timeline/readingPlace";
import { rowSteps } from "../../core/timeline/rowFeed";
import { keyedSplice, scrollTraceOn, traceScroll } from "../../core/timeline/scrollTrace";
import { Icon } from "../icons/Icon";
import { ChatImage } from "./ChatImage";
import { PictureViewer } from "./PictureViewer";
import { pictureKey, samePictures, sentPictures, threadPictures } from "../../core/timeline/threadPictures";
import { INLINE_CODE, SELECTION } from "./TimelineRows";
import { Caption } from "../kit/kit";
import { ActivityRow, ComputerMessageCard, createExpansion, PlanRow, renderCode, TaskNotificationCard, WorkCard, type Expansion, type WorkCardHandlers } from "./TimelineRows";
import { AnsweredRow } from "./RequestCard";
import { brainFiles, openChatLink, openChatPath } from "./links";


/**
 * A reply's table (soft-final SPEC §11 Table): a surface card, radius 20, inside the text's margins like a code
 * block; hair lines between rows, none between columns; cells 15/22 and at least 132 wide, so a short cell stays
 * on one line and three columns or more scroll sideways inside the card rather than squeeze, with the scroll
 * indicator; the header 13/600 soft. The SPEC's 24 pt right-edge fade is not drawn: TextView has no part over its
 * table scroll (NSH 0.5.0), and a cell's non-breaking spaces do not widen its column (the text is clipped).
 */
const TABLE = {
  table: "m-0 overflow-hidden rounded-[20px] border-0 bg-card",
  tableScroll: "m-0",
  tableRow: "border-0 bg-transparent",
  tableHead: "bg-transparent",
  tableCell: "min-w-[132px] border-0 border-t border-border px-4 py-3 text-[15px] leading-[22px]",
  tableHeaderCell: "border-t-0 text-[13px] font-semibold leading-4 text-muted-foreground",
  code: INLINE_CODE,
};


/**
 * The jump control floats over the thread, often over a bubble, so it must read as floating: the sheet-surface
 * fill (lighter than a bubble in dark) and the full float shadow (SPEC 11 Jump control).
 */
const jumpFloat = () => colorScheme() === "dark"
  ? "bg-sheet-surface shadow-[0_16px_48px_-12px_rgb(0_0_0_/_0.7)]"
  : "bg-sheet-surface shadow-[0_16px_48px_-12px_rgb(19_20_22_/_0.22)]";

/**
 * The 12 pt canvas fade under the page header (SPEC §11, the mock's `.nav::after`): the thread passes under
 * the header cleanly instead of a half line cut hard against its second line. NSH draws no gradient, so
 * six 2 pt strips of the page's paper step its opacity down. Taps go through it.
 */
const FADE = [0.94, 0.78, 0.6, 0.42, 0.24, 0.08];
function HeaderFade() {
  return (
    <View testID="header-fade" class="absolute left-0 right-0 top-0" style={{ pointerEvents: "none" }}>
      {FADE.map((opacity) => <View class="h-[2px] bg-background" style={{ opacity }} />)}
    </View>
  );
}

export type MessageHandlers = {
  /** A tap on one of the person's own sent messages: Edit, where it can be (the ✎ Editing chip). */
  onUserMessage?: (item: MessageTimelineItem) => void;
  /** When the running turn started (the provider's own clock): "Working · 1:05". */
  workingSince?: () => string | undefined;
  /** A link pressed in a reply: a web page, or a file the Session wrote. None: the chat's own (`openChatLink` on Brain's files). */
  onLink?: (url: string) => void;
  /** A file a tool call names, pressed. None: the chat's own (`openChatPath` on Brain's files). */
  onFile?: (path: string) => void;
  /** An image a reply shows by its path on the computer, as a data URL. */
  /** A picture by its path on the computer: phone-sized for a row, the original when `full` (the viewer). */
  loadImage?: (path: string, full?: boolean) => Promise<string | null>;
  /** Brain's thread: where its Work cards lead. */
  work?: WorkCardHandlers;
  /** A picture pressed (its `pictureKey`): the chat's viewer opens on it. Set by the chat itself. */
  openPicture?: (key: string) => void;
};

/** "Working" with the turn's elapsed time, on the clock the tray and Work rows read. */
function WorkingRow(props: { since: () => string | undefined }) {
  const label = () => elapsedShort(Date.parse(props.since() ?? ""), elapsedNow());
  return <View class="px-4"><Text testID="working" class="text-sub text-muted-foreground">{label() ? `Working · ${label()}` : "Working…"}</Text></View>;
}

/** The fade at a folded bubble's foot: its text going out into the bubble (the bubble's surface, stepping up). */
const BUBBLE_FADE = [0.12, 0.3, 0.5, 0.7, 0.86, 0.96];
function BubbleFade() {
  return (
    <View class="absolute bottom-0 left-0 right-0" style={{ pointerEvents: "none" }}>
      {BUBBLE_FADE.map((opacity) => <View class="h-[4px] bg-card" style={{ opacity }} />)}
    </View>
  );
}

function UserMessage(props: { item: () => MessageTimelineItem; handlers: MessageHandlers; expansion: Expansion }) {
  const it = props.item;
  // A long message folds (`messageFolds`): its first lines, fading out, and a chevron that opens and closes
  // it, open or not kept by id while the thread lives. The text stays whole under the fold, so Select all
  // copies the whole message.
  const textHeight = signal(0);
  const folds = () => Boolean(it().body) && messageFolds(textHeight());
  const open = () => props.expansion.isOpen(it().id);
  const folded = () => folds() && !open();
  const failed = () => it().pending && it().pendingLifecycle === "failed";
  const waiting = () => it().pending && !failed();
  // SPEC §16.4 Sending: the bubble at full strength at once; a clock beside it only once it is slow (3 s);
  // at the outbox's limit it dims and says "Not sent" with Try again.
  const slow = delayed(() => Boolean(waiting()), motion.wait.slow);
  // Sent pictures show as pictures above the bubble (SPEC §1.7); other files stay rows in it.
  const pictures = () => sentPictures(it().attachments);
  const files = () => it().attachments.filter((file) => !pictures().includes(file));
  const tap = () => !it().pending && props.handlers.onUserMessage?.(it());
  return (
    <View class="max-w-[84%] items-end gap-1 self-end">
      <If when={pictures().length}><View testID="sent-pictures" class={`flex-row flex-wrap justify-end gap-2 ${failed() ? "opacity-55" : ""}`}>
          {pictures().map((file) => (
            <ChatImage frame="sent" src={file.path} alt={file.name} load={props.handlers.loadImage} onOpen={() => props.handlers.openPicture?.(pictureKey(it().id, file.path))} />
          ))}
        </View></If>
      <If when={it().body || files().length}><View testID="user-message" selectable onPress={tap}
          class={`rounded-(--radius-bubble) bg-card px-4 py-3 ${failed() ? "opacity-55" : ""}`}>
          <If when={it().body}><View testID={folded() ? "user-message-folded" : "user-message-body"} class="overflow-hidden" style={{ maxHeight: folded() ? FOLDED_TEXT_HEIGHT : undefined }}>
              <Text testID="user-message-text" onPress={tap} onLayout={({ bounds }) => textHeight.set(bounds.size.height)}
                class={`shrink-0 text-message ${SELECTION}`}>{it().body}</Text>
              <If when={folded()}><BubbleFade /></If>
            </View></If>
          <If when={folds()}><Button testID="user-message-fold" accessibilityLabel={open() ? "Fold the message" : "Show the whole message"} onPress={() => props.expansion.toggle(untracked(it).id)}
              class="-mb-2 -mr-2 size-11 items-center justify-center self-end active:opacity-60">
              <Icon name={open() ? "chevron-up" : "chevron-down"} size={18} color="faint" />
            </Button></If>
          {files().map((file) => (
            <View class="flex-row items-center gap-1.5 pt-1">
              <Icon name="document" size={14} />
              <Text class="text-sub text-muted-foreground line-clamp-1">{file.name}</Text>
            </View>
          ))}
        </View></If>
      {/* Beside the bubble, so it takes no room in the thread when it comes. */}
      <If when={slow()}><View testID="message-slow" class="absolute -left-5 bottom-1"><Icon name="time" size={12} /></View></If>
      <If when={failed()}><View class="flex-row items-center gap-3">
          <Text class="text-sub font-semibold">Not sent ·</Text>
          <Button testID="retry" onPress={() => it().onRetryPending?.()} class="min-h-[44px] justify-center"><Text class="text-sub font-semibold">Try again</Text></Button>
          <Button testID="discard" onPress={() => it().onDiscardPending?.()} class="min-h-[44px] justify-center"><Text class="text-sub text-muted-foreground">Delete</Text></Button>
        </View></If>
    </View>
  );
}

/**
 * A delegated Worker's brief (BRAIN-LOOK item 6): the task Brain gave it, two lines at most and a tap
 * opens the rest, never Mewla's protocol and never as if the person had typed it. Open or not lives in
 * `expansion` by id, so a recycled cell does not carry it to another row.
 */
function DelegatedBrief(props: { item: () => MessageTimelineItem; expansion: Expansion }) {
  const task = () => props.item().delegatedTask ?? "";
  const open = () => props.expansion.isOpen(props.item().id);
  // A chevron where the task likely runs past two lines; a tap opens any task, since a line's width is not known here.
  const long = () => task().length > 80 || task().includes("\n");
  return (
    <Button testID="delegated-brief" accessibilityLabel={`Task from Brain: ${task()}`} onPress={() => props.expansion.toggle(props.item().id)}
      class="min-h-[44px] flex-row items-start gap-2 py-2.5 active:opacity-70">
      <Icon name="brain" size={18} />
      <Text testID={open() ? "delegated-task-open" : "delegated-task"} class={`min-w-0 flex-1 text-sub ${open() ? "" : "line-clamp-2"}`}>{task()}</Text>
      <If when={long()}><Icon name={open() ? "chevron-up" : "chevron-down"} size={18} color="faint" /></If>
    </Button>
  );
}

/**
 * One recycled cell. Every branch reads `item()` inside JSX, so a reused cell
 * shows the item it was given next; per-row state (open or not) lives in
 * `expansion`, by id.
 */
/**
 * A row whose views threw: nothing in its place, the error to the log and the computer (as a crash's summary
 * goes), and the row drawn again once its cell is handed another row or this one changes.
 */
function FailedRow(props: { error: unknown; reset: () => void; id: () => string }) {
  const failedOn = untracked(props.id);
  const error = props.error instanceof Error ? props.error : new Error(String(props.error));
  console.error(`chat row ${failedOn} failed: ${error.message}\n${error.stack ?? ""}`);
  try {
    currentTransport()?.send({ type: "client_diagnostic", name: "chat_row_error", request_id: `row-${Date.now().toString(36)}`, body: `${failedOn}: ${error.message}\n${error.stack ?? ""}`.slice(0, 4000) });
  } catch {
    // offline: the log has it
  }
  effect(() => {
    if (props.id() !== failedOn) untracked(props.reset);
  });
  return <View testID="row-failed" />;
}

/** Which view a row is drawn with (`TimelineRow`). */
function rowKind(item: TimelineItem | DateDividerItem): string {
  if (item.type === "date-divider") return "divider";
  if (item.type === "message") return item.delegatedTask ? "brief" : item.role === "user" ? "user" : "reply";
  if (isWorkingRow(item)) return "working";
  return item.type;
}

export function TimelineRow(props: { item: () => TimelineItem | DateDividerItem; expansion: Expansion; handlers: MessageHandlers }) {
  const it = props.item as () => TimelineItem;
  // The reply's width, which its pictures take.
  const textWidth = signal(0);
  // What a view of one type reads: the row's item while it is of that type, else the last one that was. A
  // recycled cell handed a row of another type reaches the views already drawn one step before the branch
  // above is made again; read as it is, a Work card read a message's `event` (owner 11-13 10:22).
  const as = <T extends TimelineItem["type"]>(type: T) => {
    let last: Extract<TimelineItem, { type: T }> | undefined;
    return () => {
      const now = it();
      if (now.type === type || last === undefined) last = now as Extract<TimelineItem, { type: T }>;
      return last;
    };
  };
  // Which view the row is, as one word: the branch is made again only when the word changes. A new object
  // for the same row (a streaming reply's next words, a status) reaches the views already drawn, which read
  // `item()` themselves; chosen in the JSX, each one drew the whole row anew (owner 11-13: rows flashed).
  const kind = computed(() => rowKind(it()));
  // Made once per row, here: written in a prop (`item={as(…)}`) the call becomes the prop's getter, and every
  // read made a new accessor that held nothing (the 10:27 crash came through that).
  const reply = as("message");
  const activity = as("activity") as () => ActivityTimelineItem;
  const plan = as("plan") as () => PlanTimelineItem;
  const task = as("task-notification") as () => TaskNotificationTimelineItem;
  const computerMessage = as("computer-message") as () => ComputerMessageTimelineItem;
  const answered = as("answered") as () => AnsweredTimelineItem;
  const work = as("brain-work-event") as () => BrainWorkEventTimelineItem;
  return (
    <View class="py-1">
      <If when={kind() === "divider"} else={<If when={kind() === "brief"} else={<If when={kind() === "user"} else={<If when={kind() === "reply"} else={<If when={kind() === "working"} else={<If when={kind() === "activity"} else={<If when={kind() === "plan"} else={<If when={kind() === "task-notification"} else={<If when={kind() === "computer-message"} else={<If when={kind() === "answered"} else={<If when={kind() === "brain-work-event"}><WorkCard item={work} handlers={props.handlers.work} /></If>}><AnsweredRow item={answered} /></If>}><ComputerMessageCard item={computerMessage} expansion={props.expansion} /></If>}><TaskNotificationCard item={task} expansion={props.expansion} /></If>}><PlanRow item={plan} /></If>}><ActivityRow item={activity} expansion={props.expansion} onFile={props.handlers.onFile}
          pictures={{ load: props.handlers.loadImage, open: (src) => props.handlers.openPicture?.(pictureKey(it().id, src)) }} /></If>}><WorkingRow since={() => props.handlers.workingSince?.()} /></If>}><View class="px-4">
          {/* A reply is its text alone (soft-final SPEC §11 Message): a long press selects it, code and table cells too,
              with the system's handles and menu (Copy, Select all, Share). */}
          <View testID="reply" onLayout={({ bounds }) => textWidth.set(bounds.size.width)}>
            <TextView markdown={reply().body ?? ""} options={{ streaming: Boolean(reply().streaming) }} selectable class={`text-message ${SELECTION}`}
              renderCodeBlock={renderCode}
              classes={TABLE}
              onLinkPress={(event) => props.handlers.onLink?.(event.url)}
              renderImage={(image) => {
                const key = pictureKey(reply().id, image.src);
                return <ChatImage src={image.src} alt={image.alt} load={props.handlers.loadImage} width={textWidth} onOpen={() => props.handlers.openPicture?.(key)} />;
              }} />
          </View>
        </View></If>}><View class="px-4"><UserMessage item={reply} handlers={props.handlers} expansion={props.expansion} /></View></If>}><View class="px-4"><DelegatedBrief item={reply} expansion={props.expansion} /></View></If>}><View testID="date-divider" class="py-2">
          {/* The whole row's width, centred, one line: sized to its own word, a release build's measure wrapped "Toda / y". */}
          <Caption plain center text={(it() as unknown as DateDividerItem).label} />
        </View></If>
    </View>
  );
}

/**
 * Within this of its end the list is at its end (following): it follows new rows there, and further up (reading) the
 * jump control shows. One distance for both: between two, the list did neither.
 */
const AT_END = 120;

/** The thread's foot as a row of its list. */
type FootRow = { type: "thread-foot"; id: string };
const FOOT: FootRow = { type: "thread-foot", id: "thread-foot" };

export function ChatTimeline(props: {
  testID: string;
  items: () => TimelineItem[];
  empty: () => unknown;
  handlers?: MessageHandlers;
  /** The chat's identity for its reading position (a Session's key); none keeps no position. */
  scope?: string;
  /** Counts the person's sends: each brings the chat to its newest end, the sent message there and its reply under it. */
  sent?: () => number;
  /** Whether the chat is on screen: its tab or page can stay mounted, hidden, under another. */
  shown?: () => boolean;
  /**
   * The thread's foot, after the newest row: its open requests and what was just answered (SPEC §1.2).
   * A row of the list (not its footer), so it measures again as cards open, stack and seal. `anchor`
   * takes the open card's view, which the thread centres.
   */
  foot?: { shown: () => boolean; render: (anchor: (node: ViewRef) => void) => JSX.Element };
  /** The open requests: with the foot out of view, the jump control shows a red dot and their count. */
  requests?: () => number;
  /** Bumped to bring the foot (the open request) into view: a tray row, the cold start with a request open. */
  reveal?: () => number;
  /**
   * Under a capsule header (`shell/Capsule.tsx`): the room above the first row, so nothing hides under the
   * capsule at rest, and the person's scrolling reported to it. The capsule draws the fade then.
   */
  capsule?: { top: () => number; reading: CapsuleReading };
}) {
  // A note at the foot of the chat for a moment (SPEC §11 Toast): why a link did not open.
  const notice = signal("");
  let noticeTimer: ReturnType<typeof setTimeout> | undefined;
  const say = (text: string) => {
    notice.set(text);
    clearTimeout(noticeTimer);
    noticeTimer = setTimeout(() => notice.set(""), 1500);
  };
  cleanup(() => clearTimeout(noticeTimer));
  // The page's handlers, read live (Brain's are getters); links and paths it does not handle open as
  // Brain's files: a page without its own is Brain's thread.
  const given = props.handlers ?? {};
  const handlers: MessageHandlers = Object.create(given);
  const files = brainFiles(say);
  if (!given.onLink) handlers.onLink = (url) => openChatLink(url, files);
  if (!given.onFile) handlers.onFile = (path) => openChatPath(path, files);
  // The picture viewer: every picture of the thread, in order; the one shown by its key.
  const viewing = signal<string | null>(null);
  const pictures = computed(() => threadPictures(props.items()), { equals: samePictures });
  handlers.openPicture = (key) => viewing.set(key);
  const expansion = createExpansion();
  let list: ListRef | undefined;
  const scope = props.scope ?? props.testID;
  const trace = (kind: string, data: Record<string, unknown> = {}) => traceScroll(scope, kind, data);
  trace("make");
  // Read back from the newest row: the jump button brings it into view again.
  const away = signal(false);
  // A day's label is the same object while its day and its label are: made anew on every update, each one was a new row.
  const dividers = new Map<string, DateDividerItem>();
  type Row = TimelineItem | DateDividerItem | FootRow;
  // The thread's rows as they are now.
  const target = computed((): Row[] => {
    const all = withDayDividers(props.items()).map((row) => {
      if (row.type !== "date-divider") return row;
      const kept = dividers.get(row.id);
      if (kept?.label === row.label) return kept;
      dividers.set(row.id, row);
      return row;
    });
    return props.foot?.shown() ? [...all, FOOT] : all;
  });
  /*
   * The rows the list holds: the thread's, one narrow change at a time (core/timeline/rowFeed.ts). NSH's List lays
   * out again every row between the first and the last key that changed; a full thread changes at both ends with
   * each event (the newest in, the oldest out), and the reader's place went with it. A change at both ends reaches
   * the list as two, a moment apart, and NSH keeps the rows on screen where they are through each.
   */
  const rows = signal<readonly Row[]>(untracked(target));
  let feeding: ReturnType<typeof setTimeout> | undefined;
  const feed = () => {
    feeding = undefined;
    const steps = rowSteps(untracked(rows), untracked(target));
    rows.set(steps[0]);
    // The rest from what the list holds then to the rows as they are then (more may have come meanwhile).
    if (steps.length > 1) feeding = setTimeout(feed, 0);
  };
  effect(() => {
    target();
    untracked(() => feeding === undefined && feed());
  });
  cleanup(() => clearTimeout(feeding));
  // The trace: each change of the rows' keys as NSH's List splices it, and whether it covers the rows on screen.
  let tracedKeys: string[] = [];
  let firstShown = -1;
  let lastShown = -1;
  if (scrollTraceOn()) effect(() => {
    const keys = rows().map((row) => row.id);
    const old = tracedKeys;
    const splice = keyedSplice(old, keys);
    tracedKeys = keys;
    if (!splice) return;
    const covers = firstShown >= 0 && splice.removed > 0 && firstShown >= splice.at && firstShown < splice.at + splice.removed;
    trace("splice", { index: splice.at, removed: splice.removed, inserted: splice.inserted, rows: keys.length, firstShown, lastShown, covers, removedKeys: old.slice(splice.at, splice.at + Math.min(splice.removed, 6)), insertedKeys: keys.slice(splice.at, splice.at + Math.min(splice.inserted, 6)) });
  });
  /*
   * One owner of where the chat is, in two states: following (within `AT_END` of its newest end: NSH's List keeps
   * that end in view as rows come and grow) and reading (anywhere else: nothing moves it, NSH keeps the rows on
   * screen where they are as rows change around them). Only the person's acts change it: their scrolling, a send
   * and the jump control (to the end), a tray row or link to a request (to its card). Where it opens is decided
   * once, when its first rows are in: the list is made then, at the row the person last read to or at its end.
   */
  let opening: { index?: number; centre: boolean } = { centre: false };
  const ready = signal(false);
  const openOnce = () => {
    if (untracked(ready) || untracked(rows).length === 0) return;
    // Read when the rows are in, not when the list is made: places kept from before a restart load meanwhile.
    const saved = props.scope ? readingPlaceOf(props.scope) : undefined;
    opening = { index: untracked(() => readingPlaceIndex(rows(), saved)), centre: untracked(openRequests) };
    trace("open", { saved, index: opening.index, rows: untracked(rows).length, centre: opening.centre });
    ready.set(true);
  };
  let touched = false;
  // How far the list is from its newest end, as it last scrolled (for the capsule's rest).
  let fromEnd = 0;
  let shownFirst: string | undefined;
  const mounted = signal(false);
  const anchor = signal<ViewRef | undefined>(undefined);
  /**
   * Brings the foot's open card to the middle of the list, as far as the content lets it (it is near the
   * end): a card taller than the list shows from its top. Measured where it is laid out; from far up the
   * foot is not, so the list goes to it first. Without an open card, the end.
   */
  const centreRequest = async (animated: boolean) => {
    const all = untracked(rows);
    const index = all.length - 1;
    const view = untracked(anchor);
    trace("cmd", { by: "centreRequest", cmd: "scrollToEnd?", foot: all[index] === FOOT, view: Boolean(view), rows: all.length });
    if (!list || all[index] !== FOOT || !view) return void list?.scrollToEnd({ animated });
    let card = await view.measure();
    if (!card) {
      list.scrollToIndex({ index, viewPosition: 1, animated: false });
      card = (await untracked(anchor)?.measure()) ?? null;
    }
    const box = await list?.measure();
    if (!list || !card || !box) return void list?.scrollToEnd({ animated });
    // In the band below a capsule header: the list runs under it.
    const under = untracked(() => props.capsule?.top() ?? 0);
    const top = under + Math.max(0, (box.bounds.size.height - under - card.bounds.size.height) / 2);
    trace("cmd", { by: "centreRequest", cmd: "scrollToIndex", index, rows: untracked(rows).length });
    list.scrollToIndex({ index, viewPosition: 0, viewOffset: top - card.bounds.origin.y, animated });
  };
  const openRequests = () => Boolean(props.foot?.shown()) && (props.requests?.() ?? 0) > 0;
  openOnce();
  effect(() => {
    rows();
    openOnce();
  });
  // Opened with a request open and no place to go back to, the chat shows it centred (SPEC §3.1 cold
  // start), when its card is first laid out. A request that comes later only shows in the jump control.
  let centred = false;
  effect(() => {
    if (!mounted() || !anchor() || centred) return;
    centred = true;
    if (opening.centre && opening.index === undefined) untracked(() => void centreRequest(false));
  });
  // The person's acts, counted by the page: each new one moves the list (a count already there when the
  // list is made is not: back in the chat from the terminal, it does not jump to the last message sent).
  const seenReveal = props.reveal ? untracked(props.reveal) : 0;
  effect(() => {
    if (!props.reveal || props.reveal() === seenReveal || !mounted()) return;
    untracked(() => void centreRequest(true));
  });
  /*
   * A send and the jump control: to the newest end, following from then on. From further than a screen away the
   * list goes there at once: an animated scroll is dropped by NSH at the first change of the rows (the send's echo
   * comes within a moment), short of the end.
   */
  let viewport = 0;
  const toEnd = (by: string) => {
    const animated = fromEnd <= viewport;
    trace("cmd", { by, cmd: "scrollToEnd", animated, rows: untracked(rows).length });
    list?.scrollToEnd({ animated });
  };
  const seenSent = props.sent ? untracked(props.sent) : 0;
  effect(() => {
    if (!props.sent || props.sent() === seenSent) return;
    untracked(() => toEnd("send"));
  });
  // Where the list was while on screen. A browser drops a hidden list's scroll (its tab is
  // `display: none`), so on the web a return brings back that place, or the newest row
  // (FAILURES R4 #14: Brain came back some twenty screens up after every tab switch).
  const shown = () => (props.shown ? props.shown() : true);
  let last: { offset: number; atEnd: boolean } | null = null;
  if (platform.os === "web" && props.shown) {
    let wasShown = untracked(shown);
    effect(() => {
      const on = shown();
      const target = on && !wasShown ? last : null;
      wasShown = on;
      if (!target) return;
      const back = () => (target.atEnd ? list?.scrollToEnd({ animated: false }) : list?.scrollToOffset({ offset: target.offset, animated: false }));
      // Once the list is laid out again, and once more after its rows have measured.
      const first = setTimeout(back, 0);
      const settled = setTimeout(back, 120);
      cleanup(() => {
        clearTimeout(first);
        clearTimeout(settled);
      });
    });
  }
  let dragging = false;
  let lastTraced = Number.NaN;
  return (
    <View class="flex-1">
      <PictureViewer pictures={pictures} open={viewing} load={handlers.loadImage} />
      <If when={ready()} else={<View testID={props.testID} class="flex-1">{props.empty() as never}</View>}><List keyboardShouldPersistTaps="handled" testID={props.testID} class="flex-1" each={rows()} keyExtractor={(item) => item.id} ref={(node) => {
          list = node;
          trace("ref", { set: Boolean(node) });
          mounted.set(true);
        }}
        onScrollDragStart={() => {
          dragging = true;
          trace("drag", { phase: "start" });
          touched = true;
          props.capsule?.reading.dragStart();
        }}
        onScrollDragEnd={() => {
          dragging = false;
          trace("drag", { phase: "end" });
        }}
        onScrollEnd={({ offset }) => props.capsule?.reading.rest(offset.y, fromEnd)}
        // Opens at its newest row (NSH 0.5.0, before the first frame), or at the row it was read back to.
        initialScrollAtEnd={opening.index === undefined}
        initialScrollIndex={opening.index}
        onViewableItemsChange={({ viewableItems }) => {
          // A hidden list's rows (a browser resets its scroll) are not where the person was.
          if (untracked(shown)) shownFirst = readingPlace(viewableItems);
          const shownIndices = viewableItems.filter((token) => token.isViewable).map((token) => token.index);
          firstShown = shownIndices.length ? Math.min(...shownIndices) : -1;
          lastShown = shownIndices.length ? Math.max(...shownIndices) : -1;
          trace("viewable", { first: firstShown, last: lastShown, firstKey: shownFirst });
        }}
        // Following: the newest end stays in view as rows come and grow (and the keyboard takes room).
        maintainVisibleContentPosition={{ autoscrollToBottomThreshold: AT_END }} keyboardDismissMode="interactive"
        contentStyle={{ paddingTop: (props.capsule?.top() ?? 0) + 12, paddingBottom: 12 }}
        onScroll={({ offset, contentSize, size }) => {
          // A hidden list's scroll (a browser resets it) is not where the person was.
          if (!untracked(shown)) return;
          fromEnd = contentSize.height - (offset.y + size.height);
          viewport = size.height;
          if (scrollTraceOn() && (Number.isNaN(lastTraced) || Math.abs(offset.y - lastTraced) > 1)) {
            trace("scroll", { offset: Math.round(offset.y), content: Math.round(contentSize.height), viewport: Math.round(size.height), fromEnd: Math.round(fromEnd), dragging, jump: Number.isNaN(lastTraced) ? 0 : Math.round(offset.y - lastTraced) });
            lastTraced = offset.y;
          }
          props.capsule?.reading.scroll(offset.y, fromEnd);
          // The person's own scrolling holds the header cat still.
          if (touched) noteScroll();
          const far = fromEnd > AT_END;
          away.set(far);
          last = { offset: offset.y, atEnd: !far };
          if (!props.scope || !touched) return;
          rememberReadingPlace(props.scope, far ? shownFirst : undefined);
        }}
        footer={<>
          <If when={rows().length === 0}>{props.empty() as never}</If>
        </>}>
        {(item) => {
          // A recycled cell takes whatever row comes next: the foot's cell can become a row and back,
          // so which of the two it draws follows its item, not the item it was made for.
          const foot = computed(() => (item() as FootRow).type === "thread-foot");
          return (
            <View>
              <If when={foot()} else={<ErrorBoundary fallback={(error: unknown, reset: () => void) => <FailedRow error={error} reset={reset} id={() => (item() as TimelineItem).id} />}>
                  <TimelineRow item={item as () => TimelineItem | DateDividerItem} expansion={expansion} handlers={handlers} />
                </ErrorBoundary>}>{props.foot!.render((node) => anchor.set(node)) as never}</If>
            </View>
          );
        }}
      </List></If>
      <If when={!props.capsule}><HeaderFade /></If>
      <If when={away()}><If when={(props.requests?.() ?? 0) > 0} else={<Button testID="jump-latest" accessibilityLabel="Jump to latest" onPress={() => toEnd("jump")}
            class={`absolute bottom-3 right-4 size-11 items-center justify-center rounded-full active:opacity-70 ${jumpFloat()}`}>
            <Icon name="arrow-down" size={18} color="foreground" />
          </Button>}><Button testID="jump-requests" accessibilityLabel={`${props.requests!()} open, jump to ${props.requests!() === 1 ? "it" : "them"}`} onPress={() => void centreRequest(true)}
            class={`absolute bottom-3 right-4 h-[40px] flex-row items-center gap-1.5 rounded-full px-3.5 active:opacity-70 ${jumpFloat()}`}>
            <View class="size-2 rounded-full bg-seal" />
            <Text class="text-sub font-semibold">{String(props.requests!())}</Text>
            <Icon name="arrow-down" size={16} color="foreground" />
          </Button></If></If>
      <If when={notice()}><View class="absolute bottom-16 left-4 right-4 items-center">
          <View testID="chat-notice" onPress={() => notice.set("")} class="rounded-full bg-foreground px-4 py-2.5"><Text class="text-sub font-semibold text-background">{notice()}</Text></View>
        </View></If>
    </View>
  );
}
