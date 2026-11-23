import { cleanup, createMotion, Easing, platform, safeArea, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { BackHandler, isAvailable as mobile } from "nsh/mobile";
import { forgetConnectionIssue } from "../more/settings/computers";
import { current, currentTransport, resumeConnection, workersHydrated } from "../session";
import { BusyLabel, delayed, Rows, Spinner } from "../kit/wait";
import { haptic } from "../shell/haptics";
import { decideApproval, answerPrompt } from "../../core/conversation/turn";
import { isPromptGoneError, PROMPT_GONE_NOTICE } from "../../core/timeline/claudeChoiceModel";
import { filterTray, TODAY_SHOWN, type TrayFilter, type TrayNoticeAction, type TrayRequest, type TrayRow as Row, type TrayTarget, type TrayTile } from "../../core/tray/model";
import { Icon } from "../kit/Icon";
import { PillButton } from "../kit/kit";
import { homeShown, openCalendarItem, openPage, openSession, openTrigger, openWorkDetail, trayOpen } from "../shell/nav";
import { createMenuAnchor, showMenu } from "../kit/Menu";
import { countsForm, countText, hasDot } from "../../core/tray/counts";
import { requestPaneKey, rowKey } from "../../core/tray/pane";
import { servicesOpen } from "../sessions/ServicesSheet";
import { showDeviceRequest } from "../enrollment/state";
import { MacPane, RowEnd, RowHair, RowText, RowTile } from "./rowParts";
import { isMacApp } from "../mac/local";
import { progressMarkLabel } from "../../core/work/judgedProgress";
import { setTraySort, tray, traySort } from "./state";

/** Where the tray's rows open their pages: pushed over home (the phone's overlay) or in place of the page above Brain (the left pane). */
export type TrayOpeners = {
  session: (workerId: string) => void;
  work: (workId: string) => void;
  item: (itemId: string) => void;
  trigger: (triggerId: string) => void;
  calendar: () => void;
  allSessions: () => void;
  computer: () => void;
};

/** The phone's tray pushes (SPEC §3.2: Back returns to the open tray). */
const PUSH: TrayOpeners = {
  session: (workerId) => openSession(workerId),
  work: (workId) => openWorkDetail(workId),
  item: (itemId) => openCalendarItem(itemId),
  trigger: (triggerId) => openTrigger(triggerId),
  calendar: () => openPage({ name: "calendar" }),
  allSessions: () => openPage({ name: "allSessions" }),
  computer: () => openPage({ name: "computer" }),
};

export type TrayProps = {
  onPairAgain: () => void;
  onNewSession: () => void;
  /** Scrolls Brain's thread to a request's card (the tray has closed). */
  onShowCard: (requestKey: string) => void;
  /** Brain can't start: switch Brain to the agent the computer suggests. */
  onUseExecutor?: (executorId: string) => void;
  /** Brain can't start: the "Brain host" sheet. */
  onSwitchExecutor?: () => void;
  /** Where its top hangs under a capsule header (SPEC §11); home's header's bottom otherwise. */
  top?: () => number;
};

/** Home's header row (SPEC §11 Home header): 52 pt under the top safe area. */
export const HOME_HEADER_HEIGHT = 52;

/** The top safe area home's header sits under: a phone's status bar; none in a browser or a desktop window. */
export function homeHeaderInset(): number {
  return mobile ? safeArea().top : 0;
}

/** Android's Back while the tray lives; a browser has its own back (NSH's web runtime has no `BackHandler.on`). */
function onAndroidBack(listener: () => boolean): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}

/** A row in Mewla for Mac's pane (RESULT-DESIGN-PAIRS #6): 40 tall, radius 9, on the material; hover and selected the well. */
/** Running rows Mewla for Mac's pane shows before "N more" (w-home). */
const MAC_RUNNING_SHOWN = 5;

const MAC_ROW = "min-h-[40px] gap-[9px] overflow-hidden rounded-[9px] px-2 py-1";

/** A section label (caption 600, soft) with an optional link at the right ("Calendar ›", "All ›"). */
function Label(props: { title: string; link?: string; onLink?: () => void; testID?: string }) {
  const mac = MacPane();
  return (
    // Mewla for Mac (win.css `.lp .label`): 11.5/600, 10 above and 4 below, 8 in.
    <View class={`${mac ? "mb-1 mt-[10px] min-h-[15px] px-2" : "mb-2 mt-6 min-h-[18px] px-4"} flex-row items-center justify-between`}>
      <Text role="header" class={`${mac ? "text-[11.5px] leading-[15px]" : "text-caption"} font-semibold text-muted-foreground`}>{props.title}</Text>
      <If when={props.link}>{(link) => <Button testID={props.testID} onPress={props.onLink} class={`min-h-[44px] ${mac ? "-my-[14.5px]" : "-my-3"} flex-row items-center justify-center pl-4 active:opacity-70`}>
          <Text class={`${mac ? "text-[11.5px] leading-[15px]" : "text-caption"} font-semibold text-muted-foreground`}>{link()}</Text>
          <Icon name="chevron-right" size={14} tone="soft" />
        </Button>}</If>
    </View>
  );
}

function Group(props: { children: JSX.Element; testID?: string }) {
  // On the Mac's pane the rows sit on the material, no card, 40 apart (win.css `.lp .rows`).
  return <View testID={props.testID} class={MacPane() ? "" : "overflow-hidden rounded-(--radius-group) bg-sheet-surface"}>{props.children}</View>;
}

/** The pressed well of the row whose page is in the right pane (the wide layout's left pane). */
export function SelectedWell() {
  return <View testID="selected-well" class="absolute inset-0 bg-accent" />;
}

/**
 * A tray row (SPEC §11 Tray row): 68 pt, a 28 pt tile, a title and a line, the end. A Work its one Worker runs has
 * two targets: the row opens the Work, the Worker's logo (with its ring) opens its Session (owner 11-19 16:10).
 */
function TrayRow(props: { row: () => Row; onPress: () => void; onTile: (target: TrayTarget) => void; selected?: boolean; last?: boolean }) {
  const row = props.row;
  const mac = MacPane();
  return (
    <Button testID="tray-row" accessibilityLabel={[row().title, row().line, row().end, row().mark ? progressMarkLabel(row().mark!) : ""].filter(Boolean).join(", ")} selected={props.selected} onPress={props.onPress}
      class={`${mac ? MAC_ROW : "min-h-[68px] gap-3.5 px-4 py-3"} flex-row items-center hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${row().past ? "opacity-55" : ""}`}>
      <If when={props.selected}><SelectedWell /></If>
      {/* Mewla for Mac's Today (w-home, mac.js `todayRow`): the time where the tile goes, then the title; a running one ends with the ring. */}
      <If when={mac && row().tile.kind === "calendar"} else={<>
        <If when={row().tileTarget} else={<RowTile tile={() => row().tile} mark={() => row().mark} />}>
          {(target) => <Button testID="tray-row-session" accessibilityLabel="Open its Session" onPress={() => props.onTile(target())} class="-m-2 rounded-full p-2 active:opacity-60">
            <RowTile tile={() => row().tile} mark={() => row().mark} />
          </Button>}
        </If>
        <RowText row={row} />
        <RowEnd row={row} />
      </>}>
        <Text testID="tray-today-time" class="text-[11.5px] leading-[15px] text-muted-foreground line-clamp-1" style={{ width: /[AP]M/i.test(row().time ?? row().end) ? 58 : 40, fontVariant: ["tabular-nums"] }}>{row().time ?? row().end}</Text>
        <Text class="min-w-0 flex-1 text-[13px] font-semibold leading-[17px] line-clamp-1">{row().title}</Text>
        <If when={row().live}><Spinner size={14} label="Running" /></If>
      </If>
      <If when={!(mac || props.selected || props.last)}><RowHair /></If>
    </Button>
  );
}

/**
 * A Needs you row, ending in a red tag (Approve answers, Answer goes to the card). Its title is the Session's or
 * Work's name, as in every section, and its line the ask (SPEC §11 Tray row); a Calendar alert or a new device keeps
 * what it is over where it comes from.
 */
function RequestRow(props: { request: () => TrayRequest; busy: boolean; selected?: boolean; last?: boolean; onApprove: () => void; onOpen: () => void }) {
  const r = props.request;
  const named = () => r().ask !== undefined;
  // A failed Session has nothing to answer: "Open" opens it.
  const tag = () => (r().oneTap ? "Approve" : r().opens ? "Open" : "Answer");
  const mac = MacPane();
  return (
    <Button testID="tray-request" accessibilityLabel={named() ? `${r().source}, ${r().ask}` : `${r().title}, ${r().source}`} selected={props.selected} onPress={props.onOpen} class={`${mac ? MAC_ROW : "min-h-[68px] gap-3.5 px-4 py-3"} flex-row items-center hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
      <If when={props.selected}><SelectedWell /></If>
      <RowTile tile={(): TrayTile => (r().agent ? { kind: "agent", agent: r().agent!, flavor: r().flavor ?? "shell" } : r().kind === "trigger" ? { kind: "trigger" } : r().kind === "alert" ? { kind: "calendar" } : r().kind === "device" ? { kind: "device" } : { kind: "brain" })} />
      <View class="min-w-0 flex-1">
        <Text testID="tray-request-title" class={`${mac ? "text-[13px] leading-[17px]" : "text-body"} font-semibold line-clamp-1`}>{named() ? r().source : r().title}</Text>
        <Text testID="tray-request-line" class={`${mac ? "text-[11.5px] leading-[15px]" : "text-sub"} text-muted-foreground line-clamp-1`}>{named() ? r().ask : r().source}</Text>
      </View>
      <Button testID={r().oneTap ? "tray-approve" : r().opens ? "tray-open" : "tray-answer"} disabled={props.busy} onPress={() => (r().oneTap ? props.onApprove() : props.onOpen())}
        class={`${mac ? "h-[22px] px-[9px]" : "h-[30px] px-3"} items-center justify-center rounded-full bg-seal active:opacity-70 ${props.busy ? "" : "disabled:opacity-40"}`}>
        {/* SPEC §16.2 pattern 5, in a 30 pt pill: the 16 pt ring. The Mac's pane: 22 pt at 11.5 (win.css `.lp .pill`). */}
        <BusyLabel busy={props.busy} small label={tag()} tone="onSeal" testID="tray-request-ring">
          <Text class={`${mac ? "text-[11.5px] leading-[14px]" : "text-caption"} font-semibold text-seal-foreground`}>{tag()}</Text>
        </BusyLabel>
      </Button>
      <If when={!(mac || props.selected || props.last)}><RowHair /></If>
    </Button>
  );
}

/** The tray's first group when something is wrong: what, why, and the fix (ink first). */
function Notice(props: { onAction: (id: TrayNoticeAction) => void }) {
  const notice = () => tray().notice!;
  return (
    <View testID="tray-notice" class="mt-2 min-h-[68px] gap-3 rounded-(--radius-group) bg-sheet-surface px-4 py-3.5">
      <View class="flex-row items-start gap-3.5">
        <Icon name={notice().kind === "offline" ? "cloud-offline" : notice().kind === "pair" ? "link" : "alert"} size={24} tone="soft" />
        <View class="min-w-0 flex-1">
          <Text class="text-body font-semibold">{notice().title}</Text>
          <If when={notice().detail}><Text class="mt-0.5 text-sub text-muted-foreground">{notice().detail}</Text></If>
        </View>
      </View>
      <View class="flex-row flex-wrap gap-2 pl-[38px]">
        {notice().actions.map((action, index) => (
          <PillButton testID={`tray-notice-${action.id}`} title={action.label} kind={index === 0 ? "ink" : "ghost"} small onCanvas={false} onPress={() => props.onAction(action.id)} />
        ))}
      </View>
    </View>
  );
}

export type TrayBodyProps = TrayProps & {
  /** Where rows open their pages. */
  open: TrayOpeners;
  /** The overlay closes when a row leaves for Brain's thread; the pane never opens, so never closes. */
  onLeave?: () => void;
  /** The row whose page is in the right pane (`core/tray/pane` keys); the pane only. */
  selected?: () => string | null;
  /** The title: the state's words on a phone, the counts in the pane. */
  title: () => string;
  /** The pane's smaller title (17/600) and its own scroll padding. */
  pane?: boolean;
};

/**
 * The tray itself (SPEC §1.2, §3.2): its title, the options glyph and +, the notice, Needs you, Running, Today,
 * Idle, Done and All Sessions. The phone's overlay and the wide layout's left pane both show it.
 */
export function TrayBody(props: TrayBodyProps) {
  // The left pane on Mewla for Mac draws at the Mac's sizes; the phone's tray, a foldable's pane and the perch keep theirs.
  const mac = Boolean(props.pane) && isMacApp();
  const doneOpen = signal(false);
  // Mewla for Mac's pane (w-home): Running shows its first five and "N more", which opens the rest in place, so Today
  // stays in view under it. The phone's tray lists them all.
  const runningOpen = signal(false);
  const filter = signal<TrayFilter>({});
  const busy = signal("");
  const said = signal("");
  const menuAnchor = createMenuAnchor();
  const openRow = (row: Row) => openTarget(row.target);
  const openTarget = (target: TrayTarget) => {
    if (target.open === "session") return props.open.session(target.workerId);
    if (target.open === "work") return props.open.work(target.workId);
    return props.open.item(target.itemId);
  };
  const openRequest = (request: TrayRequest) => {
    if (request.where === "brain") {
      props.onLeave?.();
      return props.onShowCard(request.key);
    }
    if (request.where === "session" && request.workerId) return props.open.session(request.workerId);
    if (request.where === "work" && request.workId) return props.open.work(request.workId);
    if (request.where === "trigger" && request.triggerId) return props.open.trigger(request.triggerId);
    if (request.deviceId) {
      props.onLeave?.();
      return showDeviceRequest(request.deviceId);
    }
    if (request.itemId) return props.open.item(request.itemId);
  };
  const approve = async (request: TrayRequest) => {
    const tap = request.oneTap;
    const transport = currentTransport();
    if (!tap || !transport) return;
    busy.set(request.key);
    said.set("");
    try {
      if (tap.kind === "approval") await decideApproval(transport, tap.workerId, tap.approvalId, tap.optionId);
      else await answerPrompt(transport, tap.workerId, tap.promptId, { number: tap.number });
      haptic.impact("light");
    } catch (error) {
      said.set(isPromptGoneError(error) ? PROMPT_GONE_NOTICE : `Didn't reach ${current()?.name || "your computer"}`);
    } finally {
      busy.set("");
    }
  };
  const noticeAction = (id: TrayNoticeAction) => {
    switch (id) {
      case "try_again": forgetConnectionIssue(); return resumeConnection();
      case "details": return props.open.computer();
      case "pair_again": return props.onPairAgain();
      case "use_suggested": {
        const executor = untracked(tray).notice?.actions.find((action) => action.id === "use_suggested")?.executor;
        return executor ? props.onUseExecutor?.(executor) : undefined;
      }
      case "switch_executor": return props.onSwitchExecutor?.();
    }
  };
  const menu = async () => {
    const action = await showMenu({ title: "Tray", anchor: menuAnchor.node(), testID: "tray-menu-popover", items: [{ id: "sort", label: "Sort" }, { id: "services", label: "Services" }] });
    if (action === "services") return servicesOpen.set(true);
    if (action !== "sort") return;
    const picked = await showMenu({
      title: "Sort Running by",
      anchor: menuAnchor.node(),
      items: [
        { id: "opened", label: "Last opened", checked: traySort() === "opened" },
        { id: "activity", label: "Last active", checked: traySort() === "activity" },
      ],
    });
    if (picked === "opened" || picked === "activity") setTraySort(picked);
  };
  const m = () => tray();
  // What the counts and the project chips leave in view (core/tray/model `filterTray`).
  const f = () => filterTray(m(), filter());
  const pick = (next: TrayFilter) => filter.set(next);
  const counted = () => !m().notice && (m().state.kind === "needs" || m().state.kind === "running" || m().fresh.length > 0) && m().needs.length + m().fresh.length + m().runningCount > 0;
  const listed = () => m().needs.length + m().fresh.length + m().running.length + m().today.length + m().idle.length + m().done.length > 0;
  const firstRead = delayed(() => !workersHydrated() && !m().notice && !listed());
  const selected = (key: string) => props.selected?.() === key;
  // The last row of its group has no hair under it. Rows are kept by their key:
  // the model is made again every second (elapsed times tick), and a row made anew re-makes its glyphs and logo,
  // which flashes on the web (a new canvas is blank until drawn).
  const rows = (list: () => readonly Row[], more = () => false) => (
    <Each of={list()} key={rowKey}>
      {(item, index) => <TrayRow row={item} selected={selected(rowKey(item()))} last={!more() && index() === list().length - 1} onPress={() => openRow(item())} onTile={openTarget} />}
    </Each>
  );
  const bar = mac ? "size-7" : props.pane ? "size-10" : "size-11";
  return (
    <MacPane.Provider value={mac}>
      <View class={`flex-row items-center ${mac ? "min-h-[37px] gap-0.5 pb-0 pl-[14px] pr-1.5 pt-1.5" : props.pane ? "min-h-[52px] gap-0.5 px-4 pr-2" : "gap-2 px-4 pb-1 pt-4"}`}>
        <If when={counted()} else={<Text testID="tray-title" role="header" class={`flex-1 line-clamp-1 ${mac ? "text-[13px] font-semibold text-muted-foreground" : props.pane ? "text-[17px] font-semibold" : "text-title font-bold tracking-title"}`}>{props.title()}</Text>}>
          <TrayCounts pane={props.pane} mac={mac} filter={filter} onPick={pick} />
        </If>
        {/* The mock's order: the options glyph (Sort, Services), then + (New Session). */}
        <Button ref={menuAnchor.ref} testID="tray-menu" accessibilityLabel="Options" onPress={() => void menu()} class={`${bar} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}><Icon name="options" size={mac ? 16 : 24} tone="ink" /></Button>
        <Button testID="tray-new" accessibilityLabel="New session" onPress={props.onNewSession} class={`${bar} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}><Icon name="add" size={mac ? 16 : 24} tone="ink" /></Button>
      </View>
      <If when={m().projects.length}><ProjectChips filter={filter} onPick={pick} /></If>
      <Scroll testID="tray-scroll" class="flex-1" contentClass={mac ? "px-2.5" : "px-4"} style={{ paddingBottom: safeArea().bottom + 16 }} keyboardShouldPersistTaps="handled">
        <If when={m().notice}><Notice onAction={noticeAction} /></If>
        {/* The first read (SPEC §16.4 Tray): three placeholder rows from `wait.quiet` until the Workers are in. */}
        <If when={firstRead()}>
          <View testID="tray-placeholder" class="mt-4"><Rows count={3} /></View>
        </If>
        <If when={said()}><View testID="tray-said" onPress={() => said.set("")} class="mt-2 px-4"><Text class="text-sub text-muted-foreground">{said()}</Text></View></If>
        <View class={m().dimmed ? "opacity-45" : ""}>
          {/* A count and a project together can leave nothing: say so rather than show a blank tray. */}
          <If when={(filter().kind || filter().project) && !(f().needs.length + f().fresh.length + f().running.length + f().done.length)}>
            <View testID="tray-filter-empty" class="mt-6 min-h-[52px] justify-center rounded-(--radius-group) bg-sheet-surface px-4">
              <Text class="text-sub text-muted-foreground">Nothing here</Text>
            </View>
          </If>
          <If when={f().needs.length}><View testID="tray-needs">
              <Label title="Needs you" />
              <Group><Each of={f().needs} key={(request) => request.key}>
                {(request, index) => <RequestRow request={request} selected={selected(requestPaneKey(request()) ?? "")} last={index() === f().needs.length - 1} busy={busy() === request().key} onApprove={() => void approve(request())} onOpen={() => openRequest(request())} />}
              </Each></Group>
            </View></If>
          <If when={f().fresh.length}><View testID="tray-new">
              <Label title="New" />
              <Group>{rows(() => f().fresh)}</Group>
            </View></If>
          <If when={f().running.length}><View testID="tray-running">
              <Label title="Running" />
              <Group>{rows(() => (mac && !runningOpen() ? f().running.slice(0, MAC_RUNNING_SHOWN) : f().running), () => mac && f().running.length > MAC_RUNNING_SHOWN && !runningOpen())}</Group>
              <If when={mac && f().running.length > MAC_RUNNING_SHOWN && !runningOpen()}>
                <Button testID="tray-running-more" accessibilityLabel={`${f().running.length - MAC_RUNNING_SHOWN} more running`} onPress={() => runningOpen.set(true)}
                  class="min-h-[24px] justify-center rounded-[9px] pb-1.5 pl-[33px] pt-1 hover:bg-accent active:bg-accent">
                  <Text class="text-[12px] leading-[16px] text-muted-foreground">{`${f().running.length - MAC_RUNNING_SHOWN} more`}</Text>
                </Button>
              </If>
            </View></If>
          <If when={f().today.length}><View testID="tray-today">
              <Label title="Today" link="Calendar" testID="tray-calendar" onLink={props.open.calendar} />
              <Group>{rows(() => f().today.slice(0, TODAY_SHOWN))}</Group>
            </View></If>
          <If when={f().idle.length}><View testID="tray-idle">
              <Label title="Idle" />
              <Group>{rows(() => f().idle)}</Group>
            </View></If>
          <If when={f().done.length}><View testID="tray-done" class="mt-6">
              <Group>
                {/* One quiet row that opens in place: what is done stays findable without filling the list. */}
                <Button testID="tray-done-toggle" accessibilityLabel={`Done, ${f().done.length}`} onPress={() => doneOpen.set(!doneOpen())}
                  class={`${mac ? MAC_ROW : "min-h-[52px] gap-2 px-4"} flex-row items-center hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
                  <Text class={`flex-1 ${mac ? "text-[13px]" : "text-body"}`}>Done</Text>
                  <Text class="text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(f().done.length)}</Text>
                  <Icon name={doneOpen() ? "chevron-up" : "chevron-down"} size={18} tone="faint" />
                  <If when={doneOpen()}><RowHair left={() => 16} /></If>
                </Button>
                <If when={doneOpen()}>{rows(() => f().done)}</If>
              </Group>
            </View></If>
          <Button testID="tray-all-sessions" selected={selected("all")} onPress={props.open.allSessions}
            class={`${mac ? `mt-3 ${MAC_ROW}` : "mt-6 min-h-[52px] overflow-hidden rounded-(--radius-group) bg-sheet-surface px-4"} flex-row items-center justify-between hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
            <If when={selected("all")}><SelectedWell /></If>
            <Text class={mac ? "text-[13px] leading-[17px]" : "text-body"}>All Sessions</Text>
            <Icon name="chevron-right" size={mac ? 14 : 18} tone="faint" />
          </Button>
        </View>
      </Scroll>
    </MacPane.Provider>
  );
}

/**
 * The tray's title as its counts ("2 need you · 3 new · 17 running"): each count is a filter that keeps only its own
 * rows, and a second tap clears it. The same words the state control says, so header and tray still agree. One line,
 * never wrapped: when the words do not fit the room the line has (a narrow column, the phone's 320), needs you and
 * then running give way to their dot and number (core/tray/counts.ts), and each count's label stays whole.
 */
function TrayCounts(props: { pane?: boolean; mac?: boolean; filter: () => TrayFilter; onPick: (next: TrayFilter) => void }) {
  const m = () => tray();
  const room = signal<number | null>(null);
  const parts = () => [
    { kind: "needs" as const, n: m().needs.length, words: m().needs.length === 1 ? "1 needs you" : `${m().needs.length} need you` },
    { kind: "fresh" as const, n: m().fresh.length, words: `${m().fresh.length} new` },
    { kind: "running" as const, n: m().runningCount, words: `${m().runningCount} running` },
  ].filter((part) => part.n > 0);
  const size = props.mac ? 13 : props.pane ? 17 : 20;
  const dot = props.mac ? 7 : 8;
  const form = () => countsForm(parts(), room(), size, dot);
  return (
    <View testID="tray-title" role="header" onLayout={({ bounds }) => room.set(bounds.size.width)} class="-ml-1.5 min-w-0 flex-1 flex-row items-center gap-x-1 overflow-hidden">
      <Each of={parts()} key={(part) => part.kind}>{(part) => {
        const on = () => props.filter().kind === part().kind;
        return (
          <Button testID={`tray-count-${part().kind}`} accessibilityLabel={part().words} selected={on()}
            onPress={() => props.onPick({ ...props.filter(), kind: on() ? undefined : part().kind })}
            class={`shrink-0 flex-row items-center gap-1.5 rounded-full px-1.5 py-0.5 active:opacity-70 ${on() ? "bg-foreground" : ""}`}>
            <If when={hasDot(part().kind, form())}>
              <View style={{ width: dot, height: dot }} class={`rounded-full ${part().kind === "needs" ? "bg-seal" : on() ? "bg-background" : "bg-foreground"}`} />
            </If>
            <Text class={`${props.mac ? "text-[13px] font-semibold" : props.pane ? "text-[17px] font-semibold" : "text-title font-bold tracking-title"} ${on() ? "text-background" : ""}`}>{countText(part(), form())}</Text>
          </Button>
        );
      }}</Each>
    </View>
  );
}

/** The projects the open Work is under, as chips that filter (shown only with two or more): never a level. */
function ProjectChips(props: { filter: () => TrayFilter; onPick: (next: TrayFilter) => void }) {
  const m = () => tray();
  const chip = (name: string | undefined, label: string, count: number) => {
    const on = () => props.filter().project === name;
    return (
      <Button testID={name ? "tray-project" : "tray-project-all"} accessibilityLabel={`${label}, ${count}`} selected={on()}
        onPress={() => props.onPick({ ...props.filter(), project: name })}
        class={`h-8 max-w-[240px] shrink-0 flex-row items-center gap-1.5 rounded-full px-3 active:opacity-70 ${on() ? "bg-foreground" : "bg-sheet-surface"}`}>
        <Text class={`shrink text-sub font-semibold line-clamp-1 ${on() ? "text-background" : ""}`}>{label}</Text>
        <Text class={`shrink-0 text-caption ${on() ? "text-background opacity-70" : "text-muted-foreground"}`}>{String(count)}</Text>
      </Button>
    );
  };
  const all = () => m().projects.reduce((sum, project) => sum + project.count, 0);
  return (
    <Scroll testID="tray-projects" horizontal keyboardShouldPersistTaps="handled" class="max-h-[44px] grow-0" contentClass="flex-row items-center gap-1.5 px-4 pb-1.5">
      {chip(undefined, "All", all())}
      <Each of={m().projects} key={(project) => project.name}>{(project) => chip(project().name, project().name, project().count)}</Each>
    </Scroll>
  );
}

/**
 * Home's tray on a phone and a narrow window (SPEC §1.2, §3.2): home's own overlay, not a sheet:
 * it rises from the bottom over a scrim that dims home, header included, and stays open with its
 * scroll while a page pushed from it is on the stack, so Back from a Session lands on it again.
 * Its top hangs just under home's header and it fills to the screen's bottom whatever it lists
 * (tray-light-390.png), so the index always opens at the same place under the control that opened it.
 * On wide the tray is the left pane instead (ui/shell/LeftPane.tsx).
 */
export function Tray(props: TrayProps) {
  // Back closes the tray only while home is the page shown: a pushed page's Back is its stack's.
  onAndroidBack(() => {
    if (!untracked(trayOpen) || !untracked(homeShown)) return false;
    trayOpen.set(false);
    return true;
  });
  const close = () => trayOpen.set(false);
  // Read in the JSX: a component runs once.
  return <><If when={trayOpen()}><View testID="tray-overlay" class="absolute inset-0">
      <View testID="tray-scrim" class="absolute inset-0 bg-scrim" onPress={close} accessibilityLabel="Close the tray" role="button" />
      <TrayPanel {...props} onClose={close} />
    </View></If></>;
}

/** The panel: made each time the tray opens, so it rises once per opening and not when a pushed page pops back to it. */
function TrayPanel(props: TrayProps & { onClose: () => void }) {
  const top = () => props.top?.() ?? homeHeaderInset() + HOME_HEADER_HEIGHT;
  const rise = createMotion({ enter: { keyframes: { translateY: [untracked(() => windowSize().height - top()), 0] }, duration: 280, easing: Easing.out(Easing.cubic) } });
  return (
    <View testID="tray" class="absolute bottom-0 left-0 right-0 rounded-t-(--radius-sheet) bg-sheet" style={{ top: top(), transform: rise.transform }}>
      <TrayBody {...props} open={PUSH} onLeave={props.onClose} title={() => tray().title} />
    </View>
  );
}
