import { cleanup, Clipboard, computed, effect, List, platform, signal, Text, untracked, View, type JSX, type ListRef, type ViewRef } from "nsh";
import { noteScroll } from "../pets/hold";
import { providerActivityElapsedLabels } from "../../core/timeline/providerActivityElapsed";
import { Button, TextView } from "nsh/base";
import { GuardedSheet as Sheet } from "../shell/AppSheet";
import { isWorkingRow } from "../../core/conversation/timeline";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import type { AnsweredTimelineItem, BrainWorkEventTimelineItem, DateDividerItem, MessageTimelineItem, PlanTimelineItem, TimelineItem } from "../../core/timeline/types";
import { withDayDividers } from "../../core/timeline/dayDividers";
import { readingPlace, readingPlaceIndex } from "../../core/timeline/readingPlace";
import { Icon } from "../icons/Icon";
import { ChatImage } from "./ChatImage";
import { PictureViewer } from "./PictureViewer";
import { pictureKey, samePictures, sentPictures, threadPictures } from "../../core/timeline/threadPictures";
import { PlainText } from "./PlainText";
import { selectingText, showMessageMenu } from "./messageEdit";
import { Caption } from "../kit/kit";
import { ActivityRow, ComputerMessageCard, createExpansion, PlanRow, renderCode, TaskNotificationCard, WorkCard, type Expansion, type WorkCardHandlers } from "./TimelineRows";
import { AnsweredRow } from "./RequestCard";
import { brainFiles, openChatLink, openChatPath } from "./links";

export type MessageHandlers = {
  /** A tap on one of the person's own messages: its menu (Copy, Edit). */
  onUserMessage?: (item: MessageTimelineItem) => void;
  /** When the running turn started (the provider's own clock): "Working · 1:05". */
  workingSince?: () => string | undefined;
  /** A link pressed in a reply: a web page, or a file the Session wrote. None: the chat's own (`openChatLink` on Brain's files). */
  onLink?: (url: string) => void;
  /** A file a tool call names, pressed. None: the chat's own (`openChatPath` on Brain's files). */
  onFile?: (path: string) => void;
  /** An image a reply shows by its path on the computer, as a data URL. */
  loadImage?: (path: string) => Promise<string | null>;
  /** Brain's thread: where its Work cards lead. */
  work?: WorkCardHandlers;
  /** A picture pressed (its `pictureKey`): the chat's viewer opens on it. Set by the chat itself. */
  openPicture?: (key: string) => void;
};

/** "Working" with the turn's elapsed time, ticking each second while it shows. */
function WorkingRow(props: { since: () => string | undefined }) {
  const now = signal(Date.now());
  const timer = setInterval(() => now.set(Date.now()), 1000);
  cleanup(() => clearInterval(timer));
  const label = () => providerActivityElapsedLabels({ startedAt: props.since(), nowMs: now(), active: true }).visual;
  return <Text testID="working" class="px-4 text-sub text-muted-foreground">{label() ? `Working · ${label()}` : "Working…"}</Text>;
}

function UserMessage(props: { item: () => MessageTimelineItem; handlers: MessageHandlers }) {
  const it = props.item;
  const failed = () => it().pending && it().pendingLifecycle === "failed";
  const waiting = () => it().pending && !failed();
  // Sent pictures show as pictures above the bubble (SPEC §1.7); other files stay rows in it.
  const pictures = () => sentPictures(it().attachments);
  const files = () => it().attachments.filter((file) => !pictures().includes(file));
  return (
    <View class="max-w-[84%] items-end gap-1 self-end">
      {pictures().length ? (
        <View testID="sent-pictures" class={`flex-row flex-wrap justify-end gap-2 ${waiting() ? "opacity-55" : ""}`}>
          {pictures().map((file) => (
            <ChatImage frame="sent" src={file.path} alt={file.name} load={props.handlers.loadImage} onOpen={() => props.handlers.openPicture?.(pictureKey(it().id, file.path))} />
          ))}
        </View>
      ) : null}
      {it().body || files().length ? (
        <Button testID="user-message" onPress={() => !it().pending && props.handlers.onUserMessage?.(it())}
          class={`rounded-(--radius-bubble) bg-card px-4 py-3 ${failed() ? "border border-foreground" : ""} ${waiting() ? "opacity-55" : ""}`}>
          {it().body ? <Text class="text-message">{it().body}</Text> : null}
          {files().map((file) => (
            <View class="flex-row items-center gap-1.5 pt-1">
              <Icon name="document" size={14} />
              <Text class="text-sub text-muted-foreground line-clamp-1">{file.name}</Text>
            </View>
          ))}
        </Button>
      ) : null}
      {waiting() ? <Icon name="time" size={12} /> : null}
      {failed() ? (
        <View class="flex-row items-center gap-3">
          <Text class="text-sub font-semibold">Not sent</Text>
          <Button testID="retry" onPress={() => it().onRetryPending?.()} class="min-h-[44px] justify-center"><Text class="text-sub font-semibold">Send again</Text></Button>
          <Button testID="discard" onPress={() => it().onDiscardPending?.()} class="min-h-[44px] justify-center"><Text class="text-sub text-muted-foreground">Delete</Text></Button>
        </View>
      ) : null}
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
      {long() ? <Icon name={open() ? "chevron-up" : "chevron-down"} size={18} color="faint" /> : null}
    </Button>
  );
}

/**
 * One recycled cell. Every branch reads `item()` inside JSX, so a reused cell
 * shows the item it was given next; per-row state (open or not) lives in
 * `expansion`, by id.
 */
export function TimelineRow(props: { item: () => TimelineItem | DateDividerItem; expansion: Expansion; handlers: MessageHandlers }) {
  const it = props.item as () => TimelineItem;
  // The reply's width, which its pictures take.
  const textWidth = signal(0);
  const as = <T extends TimelineItem["type"]>(type: T) => () => it() as Extract<TimelineItem, { type: T }>;
  return (
    <View class="py-1">
      {(it() as TimelineItem | DateDividerItem).type === "date-divider" ? (
        <View testID="date-divider" class="py-2">
          {/* The whole row's width, centred, one line: sized to its own word, a release build's measure wrapped "Toda / y". */}
          <Caption plain center text={(it() as unknown as DateDividerItem).label} />
        </View>
      ) : it().type === "message" && (it() as MessageTimelineItem).delegatedTask ? (
        <View class="px-4"><DelegatedBrief item={as("message") as () => MessageTimelineItem} expansion={props.expansion} /></View>
      ) : it().type === "message" && (it() as MessageTimelineItem).role === "user" ? (
        <View class="px-4"><UserMessage item={as("message")} handlers={props.handlers} /></View>
      ) : it().type === "message" ? (
        <View class="px-4">
          {/* Brain's reply is its text alone (soft-final SPEC §11 Message); a long press opens Copy and Select Text. */}
          <View testID="reply" onLayout={({ bounds }) => textWidth.set(bounds.size.width)}
            onLongPress={() => !(it() as MessageTimelineItem).streaming && void showMessageMenu(it() as MessageTimelineItem, { connected: true, running: false })}>
            <TextView markdown={(it() as MessageTimelineItem).body} options={{ streaming: Boolean((it() as MessageTimelineItem).streaming) }} class="text-message"
              renderCodeBlock={renderCode}
              onLinkPress={(event) => props.handlers.onLink?.(event.url)}
              renderImage={(image) => {
                const key = pictureKey(it().id, image.src);
                return <ChatImage src={image.src} alt={image.alt} load={props.handlers.loadImage} width={textWidth} onOpen={() => props.handlers.openPicture?.(key)} />;
              }} />
          </View>
        </View>
      ) : isWorkingRow(it()) ? (
        <WorkingRow since={() => props.handlers.workingSince?.()} />
      ) : it().type === "activity" ? (
        <ActivityRow item={as("activity") as () => ActivityTimelineItem} expansion={props.expansion} onFile={props.handlers.onFile}
          pictures={{ load: props.handlers.loadImage, open: (src) => props.handlers.openPicture?.(pictureKey(it().id, src)) }} />
      ) : it().type === "plan" ? (
        <PlanRow item={as("plan") as () => PlanTimelineItem} />
      ) : it().type === "task-notification" ? (
        <TaskNotificationCard item={as("task-notification") as () => TaskNotificationTimelineItem} expansion={props.expansion} />
      ) : it().type === "computer-message" ? (
        <ComputerMessageCard item={as("computer-message") as () => ComputerMessageTimelineItem} expansion={props.expansion} />
      ) : it().type === "answered" ? (
        <AnsweredRow item={as("answered") as () => AnsweredTimelineItem} />
      ) : it().type === "brain-work-event" ? (
        <WorkCard item={as("brain-work-event") as () => BrainWorkEventTimelineItem} handlers={props.handlers.work} />
      ) : null}
    </View>
  );
}

/** A chat's rows, kept at the newest one while the person reads at the bottom. */
/** Select Text: the message in a sheet, where the system's selection works on it. */
function SelectTextSheet() {
  return (
    <Sheet open={selectingText() !== ""} onOpenChange={({ open }) => !open && selectingText.set("")} closeOnSwipe
      popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View class="gap-3 pb-8">
        <Text class="text-lg font-semibold">Select Text</Text>
        <PlainText text={selectingText()} class="text-base" />
      </View>
    </Sheet>
  );
}

/** The thread's foot as a row of its list. */
type FootRow = { type: "thread-foot"; id: string };
const FOOT: FootRow = { type: "thread-foot", id: "thread-foot" };

/** Where each chat was read to (its first row on screen, by key), by scope, while the app runs: reopening it comes back there. */
const readingPositions = new Map<string, string>();

export function ChatTimeline(props: {
  testID: string;
  items: () => TimelineItem[];
  empty: () => unknown;
  handlers?: MessageHandlers;
  /** The chat's identity for its reading position (a Session's key); none keeps no position. */
  scope?: string;
  /** Counts the person's sends: each brings their newest message to the top, with the reply under it. */
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
  // Read back from the newest row: the jump button brings it into view again.
  const away = signal(false);
  const rows = computed((): (TimelineItem | DateDividerItem | FootRow)[] => {
    const all = withDayDividers(props.items());
    return props.foot?.shown() ? [...all, FOOT] : all;
  });
  // Back at the row the person left it at; at the newest row, nothing to restore. The list opens there
  // itself when the row is already in (`initialScrollIndex`); rows that come later are scrolled to by index.
  // Only the person's own scrolling moves the saved place, not the list settling as it mounts.
  let restore = props.scope ? readingPositions.get(props.scope) : undefined;
  const hadPlace = restore !== undefined;
  const initialIndex = untracked(() => readingPlaceIndex(rows(), restore));
  if (initialIndex !== undefined) restore = undefined;
  let touched = false;
  let shownFirst: string | undefined;
  const mounted = signal(false);
  effect(() => {
    const index = readingPlaceIndex(rows(), restore);
    if (index === undefined || !mounted()) return;
    restore = undefined;
    untracked(() => list?.scrollToIndex({ index, viewPosition: 0, animated: false }));
  });
  // A chat with no place to go back to opens at its newest row. The list does that itself when its rows
  // are in as it mounts; rows that come after (a Session's transcript loads once the chat is open) are
  // brought to the newest here, once, unless the person has already moved: on a slow phone the list
  // stayed on the oldest rows it had laid out (the test for a long chat opened at "Message 0" under load).
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
    if (!list || all[index] !== FOOT || !view) return void list?.scrollToEnd({ animated });
    let card = await view.measure();
    if (!card) {
      list.scrollToIndex({ index, viewPosition: 1, animated: false });
      card = (await untracked(anchor)?.measure()) ?? null;
    }
    const box = await list?.measure();
    if (!list || !card || !box) return void list?.scrollToEnd({ animated });
    const top = Math.max(0, (box.bounds.size.height - card.bounds.size.height) / 2);
    list.scrollToIndex({ index, viewPosition: 0, viewOffset: top - card.bounds.origin.y, animated });
  };
  let opened = false;
  const openRequests = () => Boolean(props.foot?.shown()) && (props.requests?.() ?? 0) > 0;
  effect(() => {
    if (rows().length === 0 || !mounted() || opened) return;
    opened = true;
    if (hadPlace || touched || untracked(openRequests)) return;
    untracked(() => list?.scrollToEnd({ animated: false }));
  });
  // With a request open, the chat opens on the oldest one, centred (SPEC §3.1 cold start): once, when the
  // first card is laid out (the requests can come after the rows), unless the person has a place or has moved.
  let centred = false;
  effect(() => {
    if (!openRequests() || !mounted() || !anchor() || centred) return;
    centred = true;
    if (hadPlace || touched) return;
    untracked(() => void centreRequest(false));
  });
  effect(() => {
    if (!props.reveal || props.reveal() === 0 || !mounted()) return;
    untracked(() => void centreRequest(true));
  });
  effect(() => {
    if (!props.sent || props.sent() === 0) return;
    untracked(() => {
      const all = rows();
      for (let index = all.length - 1; index >= 0; index -= 1) {
        const row = all[index] as TimelineItem;
        if (row.type === "message" && (row as MessageTimelineItem).role === "user") return void list?.scrollToIndex({ index, viewPosition: 0, animated: true });
      }
    });
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
  return (
    <View class="flex-1">
      <SelectTextSheet />
      <PictureViewer pictures={pictures} open={viewing} load={handlers.loadImage} />
      <List keyboardShouldPersistTaps="handled" testID={props.testID} class="flex-1" each={rows()} keyExtractor={(item) => item.id} ref={(node) => {
          list = node;
          mounted.set(true);
        }}
        onScrollDragStart={() => (touched = true)}
        // A chat opens at its newest row (NSH 0.5.0, before the first frame), unless it is read back to a saved row.
        initialScrollAtEnd={initialIndex === undefined && restore === undefined}
        initialScrollIndex={initialIndex}
        onViewableItemsChange={({ viewableItems }) => {
          // A hidden list's rows (a browser resets its scroll) are not where the person was.
          if (untracked(shown)) shownFirst = readingPlace(viewableItems);
        }}
        maintainVisibleContentPosition={{ autoscrollToBottomThreshold: 80 }} keyboardDismissMode="interactive" contentClass="py-3"
        onScroll={({ offset, contentSize, size }) => {
          // A hidden list's scroll (a browser resets it) is not where the person was.
          if (!untracked(shown)) return;
          // The person's own scrolling holds the header cat still.
          if (touched) noteScroll();
          const far = contentSize.height - (offset.y + size.height) > 240;
          away.set(far);
          last = { offset: offset.y, atEnd: !far };
          if (!props.scope || !touched) return;
          if (far && shownFirst !== undefined) readingPositions.set(props.scope, shownFirst);
          else readingPositions.delete(props.scope);
        }}
        empty={props.empty() as never}>
        {(item) => {
          // A recycled cell takes whatever row comes next: the foot's cell can become a row and back,
          // so which of the two it draws follows its item, not the item it was made for.
          const foot = computed(() => (item() as FootRow).type === "thread-foot");
          return (
            <View>
              {foot() ? (props.foot!.render((node) => anchor.set(node)) as never) : <TimelineRow item={item as () => TimelineItem | DateDividerItem} expansion={expansion} handlers={handlers} />}
            </View>
          );
        }}
      </List>
      {away() ? (
        (props.requests?.() ?? 0) > 0 ? (
          <Button testID="jump-requests" accessibilityLabel={`${props.requests!()} open, jump to ${props.requests!() === 1 ? "it" : "them"}`} onPress={() => void centreRequest(true)}
            class="absolute bottom-3 right-4 h-[40px] flex-row items-center gap-1.5 rounded-full bg-card px-3.5 shadow-float active:opacity-70">
            <View class="size-2 rounded-full bg-seal" />
            <Text class="text-sub font-semibold">{String(props.requests!())}</Text>
            <Icon name="arrow-down" size={16} color="foreground" />
          </Button>
        ) : (
          <Button testID="jump-latest" accessibilityLabel="Jump to latest" onPress={() => list?.scrollToEnd({ animated: true })}
            class="absolute bottom-3 right-4 size-11 items-center justify-center rounded-full bg-card shadow-float active:opacity-70">
            <Icon name="arrow-down" size={18} color="foreground" />
          </Button>
        )
      ) : null}
      {notice() ? (
        <View class="absolute bottom-16 left-4 right-4 items-center">
          <Text testID="chat-notice" onPress={() => notice.set("")} class="rounded-full bg-foreground px-4 py-2.5 text-sub font-semibold text-background">{notice()}</Text>
        </View>
      ) : null}
    </View>
  );
}
