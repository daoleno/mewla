import { cleanup, platform, safeArea, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { BackHandler } from "nsh/mobile";
import { forgetConnectionIssue } from "../more/settings/computers";
import { currentTransport, resumeConnection } from "../session";
import { decideApproval, answerPrompt } from "../../core/conversation/turn";
import { isPromptGoneError, PROMPT_GONE_NOTICE } from "../../core/timeline/claudeChoiceModel";
import { DONE_SHOWN, RUNNING_SHOWN, TODAY_SHOWN, type TrayNoticeAction, type TrayRequest, type TrayRow as Row, type TrayTile } from "../../core/tray/model";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../kit/Icon";
import { PillButton } from "../kit/kit";
import { ProgressBar } from "../kit/ProgressBar";
import { homeShown, openCalendarItem, openPage, openSession, openWorkDetail, trayOpen } from "../shell/nav";
import { createMenuAnchor, showMenu } from "../kit/Menu";
import { rowKey } from "../../core/tray/pane";
import { servicesOpen } from "../sessions/ServicesSheet";
import { showDeviceRequest } from "../enrollment/state";
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
};

/** Android's Back while the tray lives; a browser has its own back (NSH's web runtime has no `BackHandler.on`). */
function onAndroidBack(listener: () => boolean): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}

function Tile(props: { tile: TrayTile }) {
  return (
    <View class="size-[28px] items-center justify-center overflow-hidden">
      {props.tile.kind === "agent" ? <AgentIcon kind={props.tile.agent} flavor={props.tile.flavor} size={20} />
        : props.tile.kind === "brain" ? <View class="size-[28px] items-center justify-center rounded-lg bg-foreground"><Icon name="brain" size={18} tone="canvas" /></View>
        : props.tile.kind === "device" ? <Icon name="shield-check" size={22} tone="soft" />
        : <Icon name="calendar" size={22} tone="soft" />}
    </View>
  );
}

/** A section label (caption 600, soft) with an optional link at the right ("Calendar ›", "All ›"). */
function Label(props: { title: string; link?: string; onLink?: () => void; testID?: string }) {
  return (
    <View class="mb-2 mt-6 min-h-[18px] flex-row items-center justify-between px-4">
      <Text role="header" class="text-caption font-semibold text-muted-foreground">{props.title}</Text>
      {props.link ? (
        <Button testID={props.testID} onPress={props.onLink} class="min-h-[44px] -my-3 flex-row items-center justify-center pl-4 active:opacity-70">
          <Text class="text-caption font-semibold text-muted-foreground">{props.link}</Text>
          <Icon name="chevron-right" size={14} tone="soft" />
        </Button>
      ) : null}
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

/** The hair under a row, from 58 pt in; the group hides the last one. */
function Hair() {
  return <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 58 }} />;
}

/** A tray row (SPEC §11 Tray row): 68 pt, a 28 pt tile, a title and a line, the end. */
function TrayRow(props: { row: Row; onPress: () => void; selected?: boolean }) {
  const row = () => props.row;
  return (
    <Button testID="tray-row" accessibilityLabel={`${row().title}, ${row().line}`} selected={props.selected} onPress={props.onPress}
      class={`min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring ${row().past ? "opacity-55" : ""}`}>
      {props.selected ? <SelectedWell /> : null}
      <Tile tile={row().tile} />
      <View class="min-w-0 flex-1">
        <Text class={`text-body line-clamp-1 ${row().done || row().quiet ? "" : "font-semibold"} ${row().quiet ? "text-muted-foreground" : ""}`}>{row().title}</Text>
        {row().line ? <Text class="text-sub text-muted-foreground line-clamp-1">{row().line}</Text> : null}
        {row().progress !== undefined ? <ProgressBar filled={row().progress!} testID="tray-progress" /> : null}
      </View>
      {row().unread ? <View testID="tray-unread" class="size-2 rounded-full bg-foreground" />
        : row().done ? <Icon name="check" size={18} tone="soft" />
        : row().end === "live" ? (
          <View class="flex-row items-center gap-1.5"><View class="size-1.5 rounded-full bg-foreground" /><Text class="text-sub text-muted-foreground">live</Text></View>
        ) : row().end ? <Text class="text-sub text-muted-foreground">{row().end}</Text> : null}
      {props.selected ? null : <Hair />}
    </Button>
  );
}

/** A Needs you row: what is asked and who asks, ending in a red tag: Approve answers, Answer goes to the card. */
function RequestRow(props: { request: TrayRequest; busy: boolean; onApprove: () => void; onOpen: () => void }) {
  const r = () => props.request;
  return (
    <Button testID="tray-request" accessibilityLabel={`${r().title}, ${r().source}`} onPress={props.onOpen} class="min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
      <Tile tile={r().agent ? { kind: "agent", agent: r().agent!, flavor: r().flavor ?? "shell" } : r().kind === "alert" ? { kind: "calendar" } : r().kind === "device" ? { kind: "device" } : { kind: "brain" }} />
      <View class="min-w-0 flex-1">
        <Text class="text-body font-semibold line-clamp-1">{r().title}</Text>
        <Text class="text-sub text-muted-foreground line-clamp-1">{r().source}</Text>
      </View>
      <Button testID={r().oneTap ? "tray-approve" : "tray-answer"} disabled={props.busy} onPress={() => (r().oneTap ? props.onApprove() : props.onOpen())}
        class="h-[30px] items-center justify-center rounded-full bg-seal px-3 active:opacity-70 disabled:opacity-40">
        <Text class="text-caption font-semibold text-seal-foreground">{r().oneTap ? "Approve" : "Answer"}</Text>
      </Button>
      <Hair />
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
          {notice().detail ? <Text class="mt-0.5 text-sub text-muted-foreground">{notice().detail}</Text> : null}
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
 * The tray itself (SPEC §1.2, §3.2): its title, ⋯ and +, the notice, Needs you, Running, Today,
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
    } catch (error) {
      said.set(isPromptGoneError(error) ? PROMPT_GONE_NOTICE : "Couldn't send. Try again.");
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
  const selected = (key: string) => props.selected?.() === key;
  const running = () => (runningOpen() ? m().running : m().running.slice(0, RUNNING_SHOWN));
  const row = (item: Row) => <TrayRow row={item} selected={selected(rowKey(item))} onPress={() => openRow(item)} />;
  return (
    <>
      <View class={`flex-row items-center px-4 ${props.pane ? "min-h-[52px] gap-0.5 pr-2" : "gap-2 pb-1 pt-4"}`}>
        <Text testID="tray-title" role="header" class={`flex-1 line-clamp-1 ${props.pane ? "text-[17px] font-semibold" : "text-title font-bold tracking-title"}`}>{props.title()}</Text>
        <Button testID="tray-new" accessibilityLabel="New session" onPress={props.onNewSession} class={`${props.pane ? "size-10" : "size-11"} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring`}><Icon name="add" size={24} tone="ink" /></Button>
        <Button ref={menuAnchor.ref} testID="tray-menu" accessibilityLabel="Tray menu" onPress={() => void menu()} class={`${props.pane ? "size-10" : "size-11"} items-center justify-center rounded-full hover:bg-accent active:opacity-70 focus-visible:ring-2 focus-visible:ring-ring`}><Icon name="more-horizontal" size={24} tone="ink" /></Button>
      </View>
      <Scroll testID="tray-scroll" class={props.pane ? "flex-1" : "shrink"} contentClass="px-4" style={{ paddingBottom: safeArea().bottom + 16 }} keyboardShouldPersistTaps="handled">
        {m().notice ? <Notice onAction={noticeAction} /> : null}
        {said() ? <Text testID="tray-said" class="mt-2 px-4 text-sub text-muted-foreground" onPress={() => said.set("")}>{said()}</Text> : null}
        <View class={m().dimmed ? "opacity-45" : ""}>
          {m().needs.length ? (
            <View testID="tray-needs">
              <Label title="Needs you" />
              <Group>{m().needs.map((request) => <RequestRow request={request} busy={busy() === request.key} onApprove={() => void approve(request)} onOpen={() => openRequest(request)} />)}</Group>
            </View>
          ) : null}
          {m().running.length ? (
            <View testID="tray-running">
              <Label title="Running" />
              <Group>
                {running().map(row)}
                {m().running.length > RUNNING_SHOWN && !runningOpen() ? (
                  <Button testID="tray-running-more" onPress={() => runningOpen.set(true)} class="min-h-[52px] justify-center px-4 hover:bg-accent active:bg-accent">
                    <Text class="text-sub text-muted-foreground">{`${m().running.length - RUNNING_SHOWN} more`}</Text>
                  </Button>
                ) : null}
              </Group>
            </View>
          ) : null}
          {m().today.length ? (
            <View testID="tray-today">
              <Label title="Today" link="Calendar" testID="tray-calendar" onLink={props.open.calendar} />
              <Group>{m().today.slice(0, TODAY_SHOWN).map(row)}</Group>
            </View>
          ) : null}
          {m().idle.length ? (
            <View testID="tray-idle">
              <Label title="Idle" />
              <Group>{m().idle.map(row)}</Group>
            </View>
          ) : null}
          {m().done.length ? (
            <View testID="tray-done">
              <Label title="Done" link="All" testID="tray-all" onLink={props.open.allSessions} />
              <Group>{m().done.slice(0, DONE_SHOWN).map(row)}</Group>
            </View>
          ) : null}
          <Button testID="tray-all-sessions" selected={selected("all")} onPress={props.open.allSessions}
            class="mt-6 min-h-[52px] flex-row items-center justify-between overflow-hidden rounded-(--radius-group) bg-sheet-surface px-4 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
            {selected("all") ? <SelectedWell /> : null}
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
  return <>{trayOpen() ? (
    <View testID="tray-overlay" class="absolute inset-0">
      <View testID="tray-scrim" class="absolute inset-0 bg-scrim" onPress={close} accessibilityLabel="Close the tray" role="button" />
      <View testID="tray" class="absolute bottom-0 left-0 right-0 rounded-t-(--radius-sheet) bg-sheet"
        style={{ maxHeight: windowSize().height * 0.88 }}>
        <TrayBody {...props} open={PUSH} onLeave={close} title={() => tray().title} />
      </View>
    </View>
  ) : null}</>;
}
