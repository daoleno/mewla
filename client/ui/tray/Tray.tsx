import { cleanup, createMotion, Easing, platform, safeArea, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { BackHandler, isAvailable as mobile } from "nsh/mobile";
import { forgetConnectionIssue } from "../more/settings/computers";
import { current, currentTransport, resumeConnection, workersHydrated } from "../session";
import { BusyLabel, delayed, Rows } from "../kit/wait";
import { haptic } from "../shell/haptics";
import { decideApproval, answerPrompt } from "../../core/conversation/turn";
import { isPromptGoneError, PROMPT_GONE_NOTICE } from "../../core/timeline/claudeChoiceModel";
import { DONE_SHOWN, RUNNING_SHOWN, TODAY_SHOWN, type TrayNoticeAction, type TrayRequest, type TrayRow as Row, type TrayTile } from "../../core/tray/model";
import { Icon } from "../kit/Icon";
import { PillButton } from "../kit/kit";
import { homeShown, openCalendarItem, openPage, openSession, openWorkDetail, trayOpen } from "../shell/nav";
import { createMenuAnchor, showMenu } from "../kit/Menu";
import { requestPaneKey, rowKey } from "../../core/tray/pane";
import { servicesOpen } from "../sessions/ServicesSheet";
import { showDeviceRequest } from "../enrollment/state";
import { RowEnd, RowHair, RowText, RowTile } from "./rowParts";
import { progressMarkLabel } from "../../core/work/judgedProgress";
import { setTraySort, tray, traySort } from "./state";

/** Where the tray's rows open their pages: pushed over home (the phone's overlay) or in place of the page above Brain (the left pane). */
export type TrayOpeners = {
  session: (workerId: string) => void;
  work: (workId: string) => void;
  item: (itemId: string) => void;
  calendar: () => void;
  allSessions: () => void;
  computer: () => void;
};

/** The phone's tray pushes (SPEC §3.2: Back returns to the open tray). */
const PUSH: TrayOpeners = {
  session: (workerId) => openSession(workerId),
  work: (workId) => openWorkDetail(workId),
  item: (itemId) => openCalendarItem(itemId),
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

/** A section label (caption 600, soft) with an optional link at the right ("Calendar ›", "All ›"). */
function Label(props: { title: string; link?: string; onLink?: () => void; testID?: string }) {
  return (
    <View class="mb-2 mt-6 min-h-[18px] flex-row items-center justify-between px-4">
      <Text role="header" class="text-caption font-semibold text-muted-foreground">{props.title}</Text>
      <If when={props.link}>{(link) => <Button testID={props.testID} onPress={props.onLink} class="min-h-[44px] -my-3 flex-row items-center justify-center pl-4 active:opacity-70">
          <Text class="text-caption font-semibold text-muted-foreground">{link()}</Text>
          <Icon name="chevron-right" size={14} tone="soft" />
        </Button>}</If>
    </View>
  );
}

function Group(props: { children: JSX.Element; testID?: string }) {
  return <View testID={props.testID} class="overflow-hidden rounded-(--radius-group) bg-sheet-surface">{props.children}</View>;
}

/** The pressed well of the row whose page is in the right pane (the wide layout's left pane). */
export function SelectedWell() {
  return <View testID="selected-well" class="absolute inset-0 bg-accent" />;
}

/** A tray row (SPEC §11 Tray row): 68 pt, a 28 pt tile, a title and a line, the end. */
function TrayRow(props: { row: () => Row; onPress: () => void; selected?: boolean; last?: boolean }) {
  const row = props.row;
  return (
    <Button testID="tray-row" accessibilityLabel={[row().title, row().line, row().end, row().mark ? progressMarkLabel(row().mark!) : ""].filter(Boolean).join(", ")} selected={props.selected} onPress={props.onPress}
      class={`min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${row().past ? "opacity-55" : ""}`}>
      <If when={props.selected}><SelectedWell /></If>
      <RowTile tile={() => row().tile} mark={() => row().mark} />
      <RowText row={row} />
      <RowEnd row={row} />
      <If when={!(props.selected || props.last)}><RowHair /></If>
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
  return (
    <Button testID="tray-request" accessibilityLabel={named() ? `${r().source}, ${r().ask}` : `${r().title}, ${r().source}`} selected={props.selected} onPress={props.onOpen} class="min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
      <If when={props.selected}><SelectedWell /></If>
      <RowTile tile={(): TrayTile => (r().agent ? { kind: "agent", agent: r().agent!, flavor: r().flavor ?? "shell" } : r().kind === "alert" ? { kind: "calendar" } : r().kind === "device" ? { kind: "device" } : { kind: "brain" })} />
      <View class="min-w-0 flex-1">
        <Text testID="tray-request-title" class="text-body font-semibold line-clamp-1">{named() ? r().source : r().title}</Text>
        <Text testID="tray-request-line" class="text-sub text-muted-foreground line-clamp-1">{named() ? r().ask : r().source}</Text>
      </View>
      <Button testID={r().oneTap ? "tray-approve" : "tray-answer"} disabled={props.busy} onPress={() => (r().oneTap ? props.onApprove() : props.onOpen())}
        class={`h-[30px] items-center justify-center rounded-full bg-seal px-3 active:opacity-70 ${props.busy ? "" : "disabled:opacity-40"}`}>
        {/* SPEC §16.2 pattern 5, in a 30 pt pill: the 16 pt ring. */}
        <BusyLabel busy={props.busy} small label={r().oneTap ? "Approve" : "Answer"} tone="onSeal" testID="tray-request-ring">
          <Text class="text-caption font-semibold text-seal-foreground">{r().oneTap ? "Approve" : "Answer"}</Text>
        </BusyLabel>
      </Button>
      <If when={!(props.selected || props.last)}><RowHair /></If>
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
  const runningOpen = signal(false);
  const busy = signal("");
  const said = signal("");
  const menuAnchor = createMenuAnchor();
  const openRow = (row: Row) => {
    const target = row.target;
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
  const listed = () => m().needs.length + m().running.length + m().today.length + m().idle.length + m().done.length > 0;
  const firstRead = delayed(() => !workersHydrated() && !m().notice && !listed());
  const selected = (key: string) => props.selected?.() === key;
  const running = () => (runningOpen() ? m().running : m().running.slice(0, RUNNING_SHOWN));
  // The last row of its group has no hair under it ("N more" follows Running's fifth). Rows are kept by their key:
  // the model is made again every second (elapsed times tick), and a row made anew re-makes its glyphs and logo,
  // which flashes on the web (a new canvas is blank until drawn).
  const rows = (list: () => readonly Row[], more = () => false) => (
    <Each of={list()} key={rowKey}>
      {(item, index) => <TrayRow row={item} selected={selected(rowKey(item()))} last={!more() && index() === list().length - 1} onPress={() => openRow(item())} />}
    </Each>
  );
  return (
    <>
      <View class={`flex-row items-center px-4 ${props.pane ? "min-h-[52px] gap-0.5 pr-2" : "gap-2 pb-1 pt-4"}`}>
        <Text testID="tray-title" role="header" class={`flex-1 line-clamp-1 ${props.pane ? "text-[17px] font-semibold" : "text-title font-bold tracking-title"}`}>{props.title()}</Text>
        {/* The mock's order: the options glyph (Sort, Services), then + (New Session). */}
        <Button ref={menuAnchor.ref} testID="tray-menu" accessibilityLabel="Options" onPress={() => void menu()} class={`${props.pane ? "size-10" : "size-11"} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}><Icon name="options" size={24} tone="ink" /></Button>
        <Button testID="tray-new" accessibilityLabel="New session" onPress={props.onNewSession} class={`${props.pane ? "size-10" : "size-11"} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}><Icon name="add" size={24} tone="ink" /></Button>
      </View>
      <Scroll testID="tray-scroll" class="flex-1" contentClass="px-4" style={{ paddingBottom: safeArea().bottom + 16 }} keyboardShouldPersistTaps="handled">
        <If when={m().notice}><Notice onAction={noticeAction} /></If>
        {/* The first read (SPEC §16.4 Tray): three placeholder rows from `wait.quiet` until the Workers are in. */}
        <If when={firstRead()}>
          <View testID="tray-placeholder" class="mt-4"><Rows count={3} /></View>
        </If>
        <If when={said()}><View testID="tray-said" onPress={() => said.set("")} class="mt-2 px-4"><Text class="text-sub text-muted-foreground">{said()}</Text></View></If>
        <View class={m().dimmed ? "opacity-45" : ""}>
          <If when={m().needs.length}><View testID="tray-needs">
              <Label title="Needs you" />
              <Group><Each of={m().needs} key={(request) => request.key}>
                {(request, index) => <RequestRow request={request} selected={selected(requestPaneKey(request()) ?? "")} last={index() === m().needs.length - 1} busy={busy() === request().key} onApprove={() => void approve(request())} onOpen={() => openRequest(request())} />}
              </Each></Group>
            </View></If>
          <If when={m().running.length}><View testID="tray-running">
              <Label title="Running" />
              <Group>
                {rows(running, () => m().running.length > RUNNING_SHOWN && !runningOpen())}
                <If when={m().running.length > RUNNING_SHOWN && !runningOpen()}><Button testID="tray-running-more" onPress={() => runningOpen.set(true)} class="min-h-[52px] justify-center px-4 hover:bg-accent active:bg-accent">
                    <Text class="text-sub text-muted-foreground">{`${m().running.length - RUNNING_SHOWN} more`}</Text>
                  </Button></If>
              </Group>
            </View></If>
          <If when={m().today.length}><View testID="tray-today">
              <Label title="Today" link="Calendar" testID="tray-calendar" onLink={props.open.calendar} />
              <Group>{rows(() => m().today.slice(0, TODAY_SHOWN))}</Group>
            </View></If>
          <If when={m().idle.length}><View testID="tray-idle">
              <Label title="Idle" />
              <Group>{rows(() => m().idle)}</Group>
            </View></If>
          <If when={m().done.length}><View testID="tray-done">
              <Label title="Done" link="All" testID="tray-all" onLink={props.open.allSessions} />
              <Group>{rows(() => m().done.slice(0, DONE_SHOWN))}</Group>
            </View></If>
          <Button testID="tray-all-sessions" selected={selected("all")} onPress={props.open.allSessions}
            class="mt-6 min-h-[52px] flex-row items-center justify-between overflow-hidden rounded-(--radius-group) bg-sheet-surface px-4 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
            <If when={selected("all")}><SelectedWell /></If>
            <Text class="text-body">All Sessions</Text>
            <Icon name="chevron-right" size={18} tone="faint" />
          </Button>
        </View>
      </Scroll>
    </>
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
