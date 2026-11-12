import { cleanup, computed, effect, openUrl, signal, Storage, Text, untracked, View } from "nsh";
import { listSessionServices } from "../../core/sessions/services";
import { dshWebServiceURL } from "../../core/sessions/servicesPresentation";
import { Button, Input, type NavStackState } from "nsh/base";
import { createMenuAnchor, showMenu, type MenuItem } from "../kit/Menu";
import { AppSheet } from "../shell/AppSheet";
import { NavPage } from "../shell/NavPage";
import { Chat } from "../../core/conversation/chat";
import { chatCacheKey, Draft } from "../../core/conversation/draft";
import { createChatTimeline } from "../../core/conversation/timeline";
import { rewindMessage, stopTurn } from "../../core/conversation/turn";
import { finalReplyIdsOf } from "../../core/timeline/model";
import { createMessageEdit, EditChip, showMessageMenu } from "../chat/messageEdit";
import { resolveBlockedTerminal } from "../../core/session/blockedTerminalSurface";
import { shouldLeaveMissingSession } from "../../core/session/missingSession";
import { resolveInterfaceRenderMode, resolveTerminalRouteWorker, supportsChatInterface, type TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import { getInterfaceRenderModes, getWorkerAliases, setInterfaceRenderMode, setWorkerAlias, type StoredInterfaceRenderModes } from "../../core/sessions/deviceState";
import { sessionRowModel } from "../../core/sessions/listRow";
import { makeSessionKey } from "../../core/sessions/sessionKeys";
import { presentWorker } from "../../core/sessions/workerPresentation";
import { workerStatusLabel } from "../../core/sessions/workerStatusPresentation";
import type { CodexSlashCommand } from "../../core/session/codexSlashCommands";
import type { ComposerAlert, ComposerSendRoute } from "../../core/session/composerSlash";
import { resolveComposerModelControl } from "../../core/session/sessionModelHelpers";
import { canSwitchModel, chatSlashCommands, getThreadRuntime } from "../../core/session/sessionRequests";
import { chatImage } from "../chat/assets";
import type { ThreadRuntimeSelection } from "../../core/providers/types";
import { CodexSkillsSheet } from "../session/CodexSkillsSheet";
import { CodexStatusSheet } from "../session/CodexStatusSheet";
import { ModelSheet } from "../session/ModelSheet";
import { ComposerPlusButton, insertSkillMention, routeComposerPick, routeComposerSend, showComposerAlert, SlashMenu } from "../session/SlashMenu";
import { durableAnswered, RequestStack, threadRequests, threadSealed } from "../chat/RequestCard";
import { buildChatComposerPlaceholder } from "../../core/session/chatComposerPresentation";
import { terminalFallback } from "../../core/session/terminalFallback";
import { conversationUnavailableReason, interfaceTimelinePhase, isConversationSyncingReason } from "../../core/session/timelinePhase";
import { compactPathLabel } from "../../core/timeline/pathDisplay";
import { AgentIcon } from "../icons/AgentIcon";
import { workLinkOf } from "../sessions/workLinks";
import { openCalendarItem } from "../shell/nav";
import { terminalShowing } from "../shell/desktop/keys";
import { lastEditablePrompt } from "../../core/web/composer";
import { slashMenuState } from "../../core/session/composerSlash";
import { DSHPanel } from "../chat/DSHPanel";
import { Composer, createComposerText } from "../chat/Composer";
import { ChatTimeline } from "../chat/Timeline";
import { openChatLink, openChatPath, type ChatFiles } from "../chat/links";
import { Icon } from "../icons/Icon";
import { brain, connection, current, currentTransport, outbox, problem as connectionProblem, resumeConnection, workers, workersHydrated } from "../session";
import type { Route } from "../shell/routes";
import { TerminalPane } from "../terminal/TerminalPane";
import { AttachmentTray, createAttachmentUploads } from "../more/files/Pages";
import { buildGitDiffChipLabel, type GitDiffStatusSnapshot } from "../../core/git/gitDiff";
import { getGitStatus } from "../../core/git/requests";
import { newSessionOpen, newSessionTarget } from "./NewSession";
import { reloadAliases } from "../sessions/AllSessions";
import { showAlert } from "../shell/alert";

const MISSING_SESSION_GRACE_MS = 2_500;


/** The person's chat-or-terminal choice per Session, saved on the device (Expo's key). */
const storedModes = signal<StoredInterfaceRenderModes>({});
void getInterfaceRenderModes(Storage).then((modes) => storedModes.set(modes)).catch(() => {});

/**
 * One Session of the current computer: its chat (structured agents) or its
 * terminal, with the terminal kept attached under the chat so switching is
 * instant. The computer is told which Session is on screen. `brain`: Brain's
 * own Session (Brain's menu › Open terminal), its terminal only, titled
 * "Brain", "{agent} · workspace" (SPEC §1.6, §4).
 */
export function Session(props: { nav: NavStackState<Route>; workerId: string; hint?: TerminalRouteSessionHint; brain?: boolean; active: () => boolean }) {
  const server = untracked(current);
  const transport = untracked(currentTransport);
  if (!server || !transport) return <NavPage title="Session"><View class="flex-1 bg-background" /></NavPage>;
  const sessionKey = makeSessionKey(server.id, props.workerId);
  const hint = props.hint ?? {};

  const worker = computed(() => resolveTerminalRouteWorker({
    storedWorker: workers().find((w) => w.id === props.workerId),
    routeSessionHint: hint,
    sessionKey,
    serverId: server.id,
    workerId: props.workerId,
    brainHostWorker: brain()?.host_worker ?? null,
    brainHostServerId: brain() ? server.id : null,
  }));
  const kind = computed(() => (worker() ? presentWorker(worker()!).kind : "terminal"));
  const canChat = computed(() => supportsChatInterface(kind(), worker()?.capabilities));
  const chosenMode = computed(() => resolveInterfaceRenderMode({ kind: kind(), capabilities: worker()?.capabilities, sessionKey, storedModes: storedModes() }));

  // A Session blocked on something no card can answer shows its terminal, once per block.
  const dismissedBlock = signal<string | null>(null);
  const blocked = computed(() => resolveBlockedTerminal({
    status: worker()?.status,
    prompt: worker()?.prompt,
    approvals: worker()?.approvals,
    live: connection() === "connected" && workersHydrated(),
    identity: `${props.workerId}|${worker()?.process_id ?? ""}|${worker()?.started_at ?? ""}`,
    dismissedKey: dismissedBlock(),
  }));
  const mode = computed(() => (props.brain || !canChat() ? "terminal" : blocked().forceTerminal ? "terminal" : chosenMode()));
  const chooseMode = (next: "chat" | "terminal") => {
    if (next === "chat" && untracked(blocked).blockKey) dismissedBlock.set(untracked(blocked).blockKey);
    storedModes.set({ ...untracked(storedModes), [sessionKey]: next });
    void setInterfaceRenderMode(Storage, sessionKey, next).catch(() => {});
  };

  // The chat: opened once for this Session, closed with the screen.
  const target = untracked(worker) ?? { id: props.workerId, cwd: hint.cwd, command: hint.command, name: hint.name, started_at: hint.startedAt, process_id: undefined };
  const chat = new Chat({
    serverId: server.id,
    transport,
    outbox,
    target: { targetId: props.workerId, workerId: props.workerId, cwd: target.cwd, command: target.command, name: target.name, startedAt: target.started_at, processId: target.process_id },
  });
  const thread = signal(chat.current);
  const stopThread = chat.onChange((state) => thread.set(state));
  const draft = new Draft(Storage, chatCacheKey(server.id, props.workerId));
  const text = createComposerText(() => draft);
  cleanup(() => {
    stopThread();
    chat.close();
    draft.flush();
  });

  const timeline = createChatTimeline("session");
  const items = computed(() => timeline(thread(), { onPendingAction: (id, action) => chat.actOnFailed(id, action) }));
  const durableHere = durableAnswered(items);
  const activity = computed(() => thread().conversation?.activity);
  const editor = createMessageEdit({
    text,
    brain: false,
    messageRewind: () => worker()?.capabilities?.message_rewind === true,
    rewind: (eventId) => rewindMessage(transport, props.workerId, eventId),
    send: (value) => chat.send(value),
  });
  const finalReplies = computed(() => finalReplyIdsOf(thread().conversation?.events ?? []));
  const handlers = {
    onUserMessage: (item: Parameters<typeof showMessageMenu>[0]) =>
      void showMessageMenu(item, { editor, connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" }),
    isFinalReply: (id: string) => finalReplies().has(id),
    workingSince: () => activity()?.started_at,
    onLink: (url: string) => openLink(url),
    onFile: (path: string) => openPath(path),
    loadImage: (path: string) => chatImage(transport, server.id, untracked(worker)?.cwd, path),
  };
  const stoppingFor = signal("");
  /** The person's sends from this screen: each brings the sent message to the top. */
  const sends = signal(0);
  const durability = signal(hint.durabilityWarning ?? "");
  const fallback = computed(() => terminalFallback(connection(), connectionProblem()));
  const notice = signal("");
  // What the Session waits on: its requests, from the same model as the tray (an agent's reported
  // decisions, the prompt the daemon read off its terminal, a question its transcript fills in).
  const requests = threadRequests({ workerId: props.workerId }, () => thread().conversation?.events);
  const sealedHere = threadSealed({ workerId: props.workerId }, () => durableHere());
  const pickedRequest = signal("");
  // A DSH Session asks through its own channel; one panel per run of it.
  const dshRun = computed(() => {
    const w = worker();
    return thread().conversation?.source === "dsh_session_jsonl" && w?.process_id && w.started_at ? `${w.process_id}|${w.started_at}` : "";
  });

  const stop = async () => {
    const running = activity();
    if (!running || running.status !== "running") return;
    stoppingFor.set(running.id);
    try {
      await stopTurn(transport, props.workerId);
    } catch {
      stoppingFor.set("");
      notice.set("Could not stop the Session. Try again.");
    }
  };

  // Presence: the computer knows which Session is on screen.
  const tellActive = (id: string) => {
    try {
      transport.send({ type: "set_active_worker", worker_id: id });
    } catch {
      // Offline: told again on reconnect.
    }
  };
  effect(() => {
    if (connection() === "connected") tellActive(props.active() ? props.workerId : "");
  });
  cleanup(() => tellActive(""));

  // An unknown Session (closed, or a stale link) goes back to the list once the computer has confirmed it.
  let everResolved = Boolean(untracked(worker));
  effect(() => {
    if (worker()) everResolved = true;
    const leave = shouldLeaveMissingSession({ resolved: Boolean(worker()), everResolved, sessionListFresh: connection() === "connected" && workersHydrated(), brainHostKnown: brain() !== null });
    if (!leave) return;
    const timer = setTimeout(() => props.nav.pop(), MISSING_SESSION_GRACE_MS);
    cleanup(() => clearTimeout(timer));
  });

  const aliases = signal<Record<string, string>>({});
  const aliasOf = (id: string) => aliases()[id];
  void getWorkerAliases(Storage).then((a) => aliases.set(a)).catch(() => {});
  const renaming = signal(false);
  const renameValue = signal("");
  // The Work this Session runs, and the Calendar run that started it.
  const link = computed(() => (worker() ? workLinkOf(worker()!) : null));
  const title = computed(() => {
    if (props.brain) return "Brain";
    const w = worker();
    return w ? sessionRowModel(w, { alias: aliasOf(props.workerId), workTitle: link()?.workTitle, calendarLabel: link()?.calendarLabel }).title : hint.name || "Session";
  });
  const openCalendarOrigin = () => {
    const id = untracked(link)?.calendarItemId;
    if (!id) return;
    openCalendarItem(id);
  };

  const saveRename = async () => {
    const name = untracked(renameValue).trim();
    renaming.set(false);
    await setWorkerAlias(Storage, props.workerId, name).catch(() => {});
    aliases.set({ ...untracked(aliases), [props.workerId]: name });
    void reloadAliases();
  };

  const terminate = async () => {
    // As the Expo app did: offline, nothing is asked; the note says why (parity #37).
    if (untracked(connection) !== "connected") {
      notice.set("Your computer is offline.");
      return;
    }
    const { action } = await showAlert({
      title: "Terminate this session?",
      description: "Its terminal and agent stop on your computer.",
      actions: [{ id: "cancel", label: "Cancel", role: "cancel" }, { id: "terminate", label: "Terminate", role: "destructive" }],
    });
    if (action !== "terminate") return;
    try {
      transport.send({ type: "kill_worker", worker_id: props.workerId });
    } catch {
      notice.set("Your computer is offline.");
      return;
    }
    props.nav.pop();
  };

  // The composer's "+" menu, Codex's slash commands, and the Session's model.
  const connected = () => connection() === "connected";
  const pinned = signal(false);
  const modelOpen = signal(false);
  const statusOpen = signal(false);
  const skillsOpen = signal(false);
  const slashCommands = signal<CodexSlashCommand[]>([]);
  const slashUnavailable = signal(false);
  /** A typed `/` opened the command list (not the "+" menu): Return waits for a pick. */
  const slashPicking = () => slashMenuState({ text: text.text(), agentKind: kind(), commands: slashCommands(), connected: connected(), pinned: false }).open;
  /** ↑ in the empty composer edits the last message, as Edit in its menu does. */
  const recall = () => {
    const item = lastEditablePrompt(untracked(items), { connected: untracked(connection) === "connected", running: untracked(activity)?.status === "running" });
    if (item?.type === "message") editor.open(item);
  };
  // The terminal owns every key while it shows: the desktop's shortcuts step aside.
  effect(() => {
    if (!props.active() || mode() !== "terminal") return;
    terminalShowing.set(true);
    return () => terminalShowing.set(false);
  });
  const runtime = signal<ThreadRuntimeSelection | null>(null);
  const codexLive = computed(() => kind() === "codex" && connected());
  effect(() => {
    if (!codexLive()) return;
    untracked(() => void chatSlashCommands(transport)
      .then((commands) => {
        slashCommands.set(commands);
        slashUnavailable.set(false);
      })
      .catch(() => slashUnavailable.set(true)));
  });
  const switchable = computed(() => canSwitchModel(worker()?.capabilities) && connected());
  effect(() => {
    if (!switchable()) return;
    untracked(() => void getThreadRuntime(transport, props.workerId).then((selection) => runtime.set(selection)).catch(() => {}));
  });
  // A DSH Session's web UI, when the computer runs it.
  const dshWeb = signal<string | null>(null);
  const dshLive = computed(() => kind() === "dsh" && connected());
  effect(() => {
    if (!dshLive()) return dshWeb.set(null);
    untracked(() => void listSessionServices(transport).then((snapshot) => dshWeb.set(dshWebServiceURL(snapshot.services))).catch(() => dshWeb.set(null)));
  });
  // Git: the folder's changes on a chip in the header, read every 15 s while the Session is on screen.
  const git = signal<{ status: GitDiffStatusSnapshot | null; error: boolean } | null>(null);
  const gitTarget = computed(() => (props.active() && connected() && worker()?.cwd ? worker()!.cwd! : ""));
  effect(() => {
    const cwd = gitTarget();
    if (!cwd) return;
    let live = true;
    const read = () => void getGitStatus(transport, { targetId: props.workerId, cwd })
      .then((status) => live && git.set({ status, error: false }))
      .catch(() => live && git.set({ status: untracked(git)?.status ?? null, error: true }));
    read();
    const timer = setInterval(read, 15_000);
    cleanup(() => {
      live = false;
      clearInterval(timer);
    });
  });
  const gitChip = computed(() => {
    const state = git();
    if (!state || state.status?.reason === "not_git_repo" || (!state.status?.available && !state.error)) return null;
    return { label: buildGitDiffChipLabel(state.status, false), dirty: Boolean(state.status?.available && !state.status.clean), error: state.error };
  });
  const openGit = () => {
    const cwd = untracked(worker)?.cwd;
    if (cwd) props.nav.push({ name: "git", targetId: props.workerId, cwd, title: untracked(title) });
  };
  // A Git review of this Session closes when the Session changes folder, as the Expo app's sheet did;
  // the chip then reads the new folder (parity #43).
  effect(() => {
    const cwd = worker()?.cwd;
    if (!cwd) return;
    const routes = untracked(props.nav.routes);
    const kept = routes.filter((route) => !(route.name.startsWith("git") && "targetId" in route && "cwd" in route && route.targetId === props.workerId && route.cwd !== cwd));
    if (kept.length !== routes.length) props.nav.setRoutes(kept);
  });
  // A reply's link and a path the terminal showed (ui/chat/links.ts): a file opens in the preview against
  // this run of the Session (without one it says why, parity #38); a page opens in the browser.
  const sessionFiles: ChatFiles = {
    identity: () => {
      const w = untracked(worker);
      return w?.process_id && w.started_at ? { workerId: props.workerId, processId: w.process_id, startedAt: w.started_at } : null;
    },
    notice: (text) => notice.set(text),
  };
  const openLink = (url: string) => openChatLink(url, sessionFiles);
  const openPath = (path: string) => openChatPath(path, sessionFiles);
  // A preview opened for this Session closes when the Session restarts (another process or start time),
  // as the Expo app's sheet did: it was read with the old generation (parity #44). A generation that is
  // only missing for a moment keeps it.
  effect(() => {
    const w = worker();
    if (!w?.process_id || !w.started_at) return;
    const stale = untracked(props.nav.routes).filter((route) => route.name === "files-preview" && route.identity?.workerId === props.workerId
      && (route.identity.processId !== w.process_id || route.identity.startedAt !== w.started_at));
    for (const route of stale) props.nav.remove(route);
  });

  // Files picked for the next message, uploaded to this computer as they are picked.
  const uploads = createAttachmentUploads();
  cleanup(() => uploads.dispose());
  const uploading = () => uploads.items().some((item) => item.status === "uploading" || item.status === "queued");
  const attach = () => {
    pinned.set(false);
    void uploads.pickAndUpload().catch((error) => notice.set(error instanceof Error ? error.message : "Could not attach the file."));
  };

  const modelChip = computed(() => resolveComposerModelControl({ capabilities: worker()?.capabilities, connectionConnected: connected(), selection: runtime(), refreshRequired: false }));

  const answerAlert = async (alert: ComposerAlert) => {
    const action = await showComposerAlert(alert);
    if (action === "open_terminal") chooseMode("terminal");
    else if (action === "send_as_message") {
      const value = untracked(text.text);
      if (!value.trim()) return;
      chat.send(value);
      text.clear();
    }
  };
  /** Does what a routed send or pick asks; true when the composer's text went out. */
  const follow = (route: ComposerSendRoute, fromDraft: boolean): boolean => {
    pinned.set(false);
    switch (route.kind) {
      case "send":
        chat.send(route.text, uploads.ready());
        sends.set(untracked(sends) + 1);
        uploads.clear();
        return fromDraft;
      case "prefill":
        text.set(route.text);
        if (route.alert) void answerAlert(route.alert);
        return false;
      case "alert":
        void answerAlert(route);
        return false;
      case "status":
        text.clear();
        statusOpen.set(true);
        return false;
      case "skills":
        text.clear();
        skillsOpen.set(true);
        return false;
    }
  };
  const sendComposer = (value: string): void | false => {
    if (untracked(editor.edit)) {
      void editor.send(value);
      return false;
    }
    const route = routeComposerSend(value, { agentKind: untracked(kind), slashCommands: untracked(slashCommands), canOpenTerminal: true });
    return follow(route, true) ? undefined : false;
  };
  const pickCommand = (command: CodexSlashCommand) => {
    // A command picked while the draft is that `/word` replaces the draft; otherwise the draft stays.
    const typingCommand = untracked(text.text).trim().startsWith("/");
    if (follow(routeComposerPick(command, { canOpenTerminal: true }), false) && typingCommand) text.clear();
  };

  const placeholder = () => buildChatComposerPlaceholder({
    agentKind: kind(),
    connectionState: connection() === "connected" ? "connected" : connection() === "connecting" ? "connecting" : "offline",
    slashQueryActive: /^\/\S*$/.test(text.text()),
  });
  /** What an empty chat says: loading, could not load, not available here (Open Terminal), or ready in its folder. */
  const ChatEmpty = () => {
    const conversation = () => thread().conversation;
    const syncing = () => Boolean(conversation() && !conversation()!.available) && isConversationSyncingReason(conversation()?.reason);
    const phase = () => interfaceTimelinePhase({
      itemCount: items().length,
      loading: thread().loading,
      error: thread().error,
      suppressed: false,
      unavailable: Boolean(conversation() && !conversation()!.available && !syncing()),
      syncing: syncing() && conversation()?.reason !== "transcript_not_found",
    });
    return (
      <View testID="chat-empty" class="flex-1 items-center justify-center gap-2 p-8">
        {phase() === "loading" ? <Text class="text-sm text-muted-foreground">Loading conversation…</Text>
          : phase() === "error" ? (
            <View class="items-center gap-2">
              <Icon name="alert-circle" size={28} color="destructive" />
              <Text class="text-base font-semibold">Could not load this chat</Text>
              <Text class="text-center text-sm text-muted-foreground">{thread().error ?? ""}</Text>
            </View>
          ) : phase() === "unavailable" ? (
            <View class="items-center gap-2">
              <Icon name="layers" size={28} />
              <Text class="text-base font-semibold">Chat view is not available here</Text>
              <Text class="text-center text-sm text-muted-foreground">{conversationUnavailableReason(conversation()?.reason)}</Text>
              <Button testID="chat-open-terminal" onPress={() => chooseMode("terminal")} class="mt-1 flex-row items-center gap-1.5 rounded-full border border-border px-3 py-1.5 active:bg-accent">
                <Icon name="terminal" size={16} color="foreground" />
                <Text class="text-sm font-semibold">Open Terminal</Text>
              </Button>
            </View>
          ) : (
            <View class="items-center gap-1.5">
              <Text class="text-lg font-semibold">Ready</Text>
              <Text class="text-sm text-muted-foreground">Send a message to start.</Text>
              {worker()?.cwd ? (
                <View class="mt-1 flex-row items-center gap-1.5 rounded-full bg-accent px-2.5 py-1">
                  <Icon name="folder" size={13} />
                  <Text class="text-xs text-muted-foreground line-clamp-1">{compactPathLabel(worker()!.cwd!, { tailSegments: 2, showFullUpTo: 2 })}</Text>
                </View>
              ) : null}
            </View>
          )}
      </View>
    );
  };

  const menuAnchor = createMenuAnchor();
  const menu = async () => {
    const items: MenuItem[] = [
      ...(canChat() ? [{ id: "mode", label: mode() === "chat" ? "Open terminal" : "Open chat" }] : []),
      ...(switchable() ? [{ id: "model", label: "Model & Effect" }] : []),
      ...(dshWeb() ? [{ id: "web", label: "Open Web" }] : []),
      { id: "terminal", label: "New Terminal" },
      { id: "rename", label: "Rename" },
      ...(link() ? [{ id: "work", label: "Open Brain" }] : []),
      { id: "terminate", label: "Terminate", destructive: true },
    ];
    const action = await showMenu({ title: untracked(title), items, anchor: menuAnchor.node(), testID: "session-menu-popover" });
    if (action === "mode") chooseMode(untracked(mode) === "chat" ? "terminal" : "chat");
    else if (action === "model") modelOpen.set(true);
    else if (action === "work") {
      const work = untracked(link)?.work;
      if (work) props.nav.push({ name: "workFile", id: work.id });
    } else if (action === "web") {
      const url = untracked(dshWeb);
      if (url) void openUrl(url).catch(() => notice.set("Could not open DSH Web."));
    }
    else if (action === "terminal") {
      newSessionTarget.set(props.workerId);
      newSessionOpen.set(true);
    } else if (action === "rename") {
      renameValue.set(aliasOf(props.workerId) ?? "");
      renaming.set(true);
    } else if (action === "terminate") void terminate();
  };

  return (
    <NavPage title={title()} column={mode() === "terminal" ? "full" : "reading"}
      headerTitle={
        <View class="max-w-[260px] flex-row items-center gap-2">
          <AgentIcon kind={kind()} size={22} />
          <View class="flex-1">
            <Text class="text-base font-semibold line-clamp-1">{title()}</Text>
            {props.brain ? (
              <Text testID="session-status" class="text-xs text-muted-foreground line-clamp-1">{`${worker() ? presentWorker(worker()!).typeLabel : "Brain"} · workspace`}</Text>
            ) : link()?.calendarLabel ? (
              <Text testID="session-calendar" class="text-xs text-primary line-clamp-1" onPress={openCalendarOrigin}>{link()!.calendarLabel!}</Text>
            ) : worker() ? (
              <Text testID="session-status" class="text-xs text-muted-foreground line-clamp-1">
                {[workerStatusLabel(worker()!.status), worker()!.delegated ? "Delegated" : "", connection() !== "connected" ? "Offline" : ""].filter(Boolean).join(" · ")}
              </Text>
            ) : null}
          </View>
        </View>
      }
      headerRight={
        <View class="flex-row items-center gap-3">
          {gitChip() ? (
            <Button testID="session-git" accessibilityLabel={`Git: ${gitChip()!.label}`} onPress={openGit} class="flex-row items-center gap-1 rounded-full border border-border px-2 py-1 active:bg-accent">
              <Icon name={gitChip()!.error ? "alert-circle" : "git-branch"} size={14} color={gitChip()!.dirty ? "primary" : undefined} />
              <Text class="max-w-[120px] text-xs line-clamp-1">{gitChip()!.label}</Text>
            </Button>
          ) : null}
          {props.brain ? null : <Button ref={menuAnchor.ref} testID="session-menu" accessibilityLabel="Session menu" onPress={() => void menu()} class="size-10 items-center justify-center rounded-full hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"><Icon name="more-horizontal" size={22} color="primary" /></Button>}
        </View>
      }>
      <View class="flex-1 bg-background">
        {durability() ? (
          <View testID="durability-warning" class="mx-3 mt-2 flex-row items-start gap-2 rounded-xl border border-border bg-card px-3 py-2.5">
            <Icon name="alert-circle" size={18} color="warning" />
            <View class="flex-1 gap-0.5">
              <Text class="text-sm font-semibold">Not confirmed durable</Text>
              <Text class="text-sm text-muted-foreground">{durability()}</Text>
            </View>
            <Button testID="durability-dismiss" accessibilityLabel="Dismiss durability warning" onPress={() => durability.set("")} class="active:opacity-60">
              <Text class="text-sm font-semibold text-primary">Dismiss</Text>
            </Button>
          </View>
        ) : null}
        <View class="flex-1">
          <View class="absolute inset-0 flex-col" style={{ display: mode() === "terminal" ? "flex" : "none" }}>
            <TerminalPane transport={transport} targetId={props.workerId} active={() => props.active() && mode() === "terminal"} onOpenPath={(path) => openPath(path)} />
            <View class="absolute left-3 right-3 top-3 gap-2" style={{ pointerEvents: "box-none" }}>
            {fallback() ? (
              <View testID="terminal-offline" class="gap-1.5 rounded-xl border border-border bg-card px-3 py-3">
                <Text class="text-base font-semibold">{fallback()!.title}</Text>
                <Text class="text-sm text-muted-foreground">{fallback()!.detail}</Text>
                <Text class="text-xs text-muted-foreground">{fallback()!.hint}</Text>
                {fallback()!.busy ? null : (
                  <Button testID="terminal-offline-retry" onPress={() => resumeConnection()} class="mt-1 self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
                    <Text class="text-sm font-semibold">Retry</Text>
                  </Button>
                )}
              </View>
            ) : null}
            {/* The chat shows its notes above the composer; the terminal shows them here (parity #37). */}
            {notice() && mode() === "terminal" ? (
              <Text testID="terminal-notice" onPress={() => notice.set("")}
                class="rounded-xl border border-border bg-card px-3 py-2.5 text-sm text-foreground">{notice()}</Text>
            ) : null}
            </View>
          </View>
          {mode() === "chat" ? (
            <View class="absolute inset-0 bg-background">
              <ChatTimeline testID="session-chat" items={items} handlers={handlers} empty={() => <ChatEmpty />} scope={sessionKey} sent={sends} shown={() => props.active() && mode() === "chat"}
                foot={{
                  shown: () => requests().length > 0 || sealedHere().length > 0,
                  render: (anchor) => <RequestStack anchor={anchor} durable={durableHere} thread={{ workerId: props.workerId }} cards={requests} picked={pickedRequest} notice={(text) => notice.set(text)} onOpenTerminal={() => chooseMode("terminal")} />,
                }}
                requests={() => requests().length} />
              <Composer text={text} placeholder={placeholder} autoFocus={hint.focusComposer} canSend={() => !uploading()} attached={() => uploads.ready().length > 0} onSend={sendComposer}
                running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"}
                onStop={() => void stop()} active={() => props.active() && mode() === "chat"} pickerOpen={slashPicking} onRecall={recall}
                onFiles={(files) => {
                  try {
                    uploads.add(files);
                  } catch (error) {
                    notice.set(error instanceof Error ? error.message : "Could not attach the file.");
                  }
                }}
                leading={<ComposerPlusButton pinned={pinned} connected={connected} onPress={() => pinned.set(!untracked(pinned))} />}>
                <SlashMenu text={text.text} commands={slashCommands} commandsUnavailable={slashUnavailable} agentKind={kind} connected={connected} pinned={pinned} onPick={pickCommand}
                  onAttach={attach} uploading={uploading} />
                {uploads.items().length > 0 ? <AttachmentTray uploads={uploads} /> : null}
                {modelChip() ? (
                  <Button testID="model-chip" accessibilityLabel={modelChip()!.accessibilityLabel} onPress={() => modelOpen.set(true)}
                    class="mb-2 flex-row items-center gap-1 self-start rounded-full border border-border px-2.5 py-1 active:bg-accent">
                    <Text class="text-xs">{modelChip()!.label}</Text>
                    <Icon name="chevron-down" size={14} />
                  </Button>
                ) : null}
                {editor.edit() ? <EditChip editor={editor} /> : null}
                {dshRun() ? <DSHPanel transport={transport} run={{ workerId: props.workerId, processId: Number(dshRun().split("|")[0]), startedAt: Number(dshRun().split("|")[1]) }} /> : null}
                {notice() ? <Text testID="session-toast" class="pb-1.5 text-center text-sm text-muted-foreground" onPress={() => notice.set("")}>{notice()}</Text> : null}
              </Composer>
            </View>
          ) : null}
        </View>
      </View>
      <ModelSheet open={modelOpen} onClose={() => modelOpen.set(false)} transport={transport} workerId={props.workerId} onRuntime={(selection) => runtime.set(selection)} />
      <CodexStatusSheet open={statusOpen} onClose={() => statusOpen.set(false)} transport={transport} workerId={props.workerId}
        onOpenTerminal={() => {
          statusOpen.set(false);
          chooseMode("terminal");
        }} />
      <CodexSkillsSheet open={skillsOpen} onClose={() => skillsOpen.set(false)} transport={transport} cwd={worker()?.cwd}
        onPick={(name) => text.set(insertSkillMention(untracked(text.text), name))} />
      <AppSheet open={renaming()} onOpenChange={({ open }) => renaming.set(open)} popupClass="rounded-t-2xl bg-background p-4 gap-3" backdropClass="bg-black/40">
        <View class="gap-3 pb-6">
          <Text class="text-lg font-semibold">Rename</Text>
          <Text class="text-sm text-muted-foreground">A name for this Session on this phone. Empty uses its own name.</Text>
          <Input testID="rename-input" value={renameValue()} placeholder={title()} onChange={({ value }) => renameValue.set(value)}
            class="rounded-xl border border-border bg-card px-3.5 py-2.5 text-base" />
          <View class="flex-row justify-end gap-4">
            <Button onPress={() => renaming.set(false)}><Text class="text-base text-muted-foreground">Cancel</Text></Button>
            <Button testID="rename-save" onPress={() => void saveRename()}><Text class="text-base font-semibold text-primary">Save</Text></Button>
          </View>
        </View>
      </AppSheet>
    </NavPage>
  );
}
