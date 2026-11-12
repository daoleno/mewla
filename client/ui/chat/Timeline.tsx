import { cleanup, Clipboard, computed, effect, List, platform, signal, Text, untracked, View, type JSX, type ListRef, type ViewRef } from "nsh";
import { noteScroll } from "../pets/hold";
import { providerActivityElapsedLabels } from "../../core/timeline/providerActivityElapsed";
import { Button, TextView } from "nsh/base";
import { GuardedSheet as Sheet } from "../shell/AppSheet";
import { isWorkingRow } from "../../core/conversation/timeline";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import type { BrainWorkEventTimelineItem, DateDividerItem, MessageTimelineItem, PlanTimelineItem, TimelineItem } from "../../core/timeline/types";
import { compactAge, latestTimestamp, withDayDividers } from "../../core/timeline/dayDividers";
import { readingPlace, readingPlaceIndex } from "../../core/timeline/readingPlace";
import { Icon } from "../icons/Icon";
import { ChatImage } from "./ChatImage";
import { PlainText } from "./PlainText";
import { selectingText } from "./messageEdit";
import { ActivityRow, ComputerMessageCard, createExpansion, PlanRow, TaskNotificationCard, WorkCard, type Expansion, type WorkCardHandlers } from "./TimelineRows";

export type MessageHandlers = {
  /** A tap on one of the person's own messages: its menu (Copy, Edit). */
  onUserMessage?: (item: MessageTimelineItem) => void;
  /** A turn's final reply gets a Copy button under it. */
  isFinalReply?: (id: string) => boolean;
  /** When the running turn started (the provider's own clock): "Working · 1:05". */
  workingSince?: () => string | undefined;
  /** A link pressed in a reply: a web page, or a file the Session wrote. */
  onLink?: (url: string) => void;
  /** A file a tool call names, pressed. */
  onFile?: (path: string) => void;
  /** An image a reply shows by its path on the computer, as a data URL. */
  loadImage?: (path: string) => Promise<string | null>;
  /** Brain's thread: where its Work cards lead. */
  work?: WorkCardHandlers;
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
  return (
    <View class="max-w-[84%] items-end gap-1 self-end">
      <Button testID="user-message" onPress={() => !it().pending && props.handlers.onUserMessage?.(it())}
        class={`rounded-(--radius-bubble) bg-card px-4 py-3 ${failed() ? "border border-foreground" : ""} ${waiting() ? "opacity-55" : ""}`}>
        {it().body ? <Text class="text-message">{it().body}</Text> : null}
        {it().attachments.map((file) => (
          <View class="flex-row items-center gap-1.5 pt-1">
            <Icon name="document" size={14} />
            <Text class="text-sub text-muted-foreground line-clamp-1">{file.name}</Text>
          </View>
        ))}
      </Button>
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
        <View testID="date-divider" class="items-center py-2">
          <Text class="text-caption text-muted-foreground">{(it() as unknown as DateDividerItem).label}</Text>
        </View>
      ) : it().type === "message" && (it() as MessageTimelineItem).role === "user" ? (
        <View class="px-4"><UserMessage item={as("message")} handlers={props.handlers} /></View>
      ) : it().type === "message" ? (
        <View class="px-4">
          <View onLayout={({ bounds }) => textWidth.set(bounds.size.width)}>
            <TextView markdown={(it() as MessageTimelineItem).body} options={{ streaming: Boolean((it() as MessageTimelineItem).streaming) }} selectable class="text-message"
              onLinkPress={(event) => props.handlers.onLink?.(event.url)}
              renderImage={(image) => <ChatImage src={image.src} alt={image.alt} load={props.handlers.loadImage} width={textWidth} />} />
          </View>
          {!(it() as MessageTimelineItem).streaming && props.handlers.isFinalReply?.(it().id) ? (
            <Button testID="copy-reply" onPress={() => void Clipboard.writeText((it() as MessageTimelineItem).body.replace(/\s+$/u, "")).catch(() => {})}
              class="mt-1 size-11 items-center justify-center self-start rounded-full active:bg-accent">
              <Icon name="copy" size={18} />
            </Button>
          ) : null}
        </View>
      ) : isWorkingRow(it()) ? (
        <WorkingRow since={() => props.handlers.workingSince?.()} />
      ) : it().type === "activity" ? (
        <ActivityRow item={as("activity") as () => ActivityTimelineItem} expansion={props.expansion} onFile={props.handlers.onFile} />
      ) : it().type === "plan" ? (
        <PlanRow item={as("plan") as () => PlanTimelineItem} />
      ) : it().type === "task-notification" ? (
        <TaskNotificationCard item={as("task-notification") as () => TaskNotificationTimelineItem} expansion={props.expansion} />
      ) : it().type === "computer-message" ? (
        <ComputerMessageCard item={as("computer-message") as () => ComputerMessageTimelineItem} expansion={props.expansion} />
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
  const handlers = props.handlers ?? {};
  const expansion = createExpansion();
  let list: ListRef | undefined;
  // Read back from the newest row: the jump button brings it into view again, saying how old it is.
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
  const now = signal(Date.now());
  effect(() => {
    if (!away()) return;
    now.set(Date.now());
    const timer = setInterval(() => now.set(Date.now()), 1000);
    cleanup(() => clearInterval(timer));
  });
  const age = () => compactAge(latestTimestamp(props.items()), now());
  return (
    <View class="flex-1">
      <SelectTextSheet />
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
        {(item) => ((item() as FootRow).type === "thread-foot" ? (props.foot!.render((node) => anchor.set(node)) as never) : <TimelineRow item={item as () => TimelineItem | DateDividerItem} expansion={expansion} handlers={handlers} />)}
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
          <Button testID="jump-latest" accessibilityLabel={age() ? `Jump to latest, ${age()} ago` : "Jump to latest"} onPress={() => list?.scrollToEnd({ animated: true })}
            class="absolute bottom-3 right-4 h-[40px] flex-row items-center gap-1 rounded-full bg-card px-3 shadow-float active:opacity-70">
            <Icon name="arrow-down" size={16} color="foreground" />
            {age() ? <Text class="text-sub text-muted-foreground">{age()}</Text> : null}
          </Button>
        )
      ) : null}
    </View>
  );
}
