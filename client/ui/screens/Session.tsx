import { cleanup, computed, effect, signal, Storage, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Input, NavPage, Sheet, type NavStackState } from "nsh/base";
import { Chat } from "../../core/conversation/chat";
import { chatCacheKey, Draft } from "../../core/conversation/draft";
import { createChatTimeline } from "../../core/conversation/timeline";
import { answerChoice, stopTurn } from "../../core/conversation/turn";
import { resolveBlockedTerminal } from "../../core/session/blockedTerminalSurface";
import { shouldLeaveMissingSession } from "../../core/session/missingSession";
import { resolveInterfaceRenderMode, resolveTerminalRouteWorker, supportsChatInterface, type TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import { getInterfaceRenderModes, getWorkerAliases, setInterfaceRenderMode, setWorkerAlias, type StoredInterfaceRenderModes } from "../../core/sessions/deviceState";
import { sessionRowModel } from "../../core/sessions/listRow";
import { makeSessionKey } from "../../core/sessions/sessionKeys";
import { presentWorker } from "../../core/sessions/workerPresentation";
import { workerStatusLabel } from "../../core/sessions/workerStatusPresentation";
import { CHOICE_GONE_NOTICE, visiblePendingChoice } from "../../core/timeline/claudeChoiceModel";
import { ChoiceCard } from "../chat/ChoiceCard";
import { Composer, createComposerText } from "../chat/Composer";
import { ChatTimeline } from "../chat/Timeline";
import { Icon } from "../icons/Icon";
import { brain, connection, current, currentTransport, outbox, workers, workersHydrated } from "../session";
import type { SessionsRoute } from "../shell/routes";
import { TerminalPane } from "../terminal/TerminalPane";
import { reloadAliases } from "./Sessions";

const MISSING_SESSION_GRACE_MS = 2_500;

/** The person's chat-or-terminal choice per Session, saved on the device (Expo's key). */
const storedModes = signal<StoredInterfaceRenderModes>({});
void getInterfaceRenderModes(Storage).then((modes) => storedModes.set(modes)).catch(() => {});

/**
 * One Session of the current computer: its chat (structured agents) or its
 * terminal, with the terminal kept attached under the chat so switching is
 * instant. The computer is told which Session is on screen.
 */
export function Session(props: { nav: NavStackState<SessionsRoute>; workerId: string; hint?: TerminalRouteSessionHint; active: () => boolean }) {
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
  const mode = computed(() => (!canChat() ? "terminal" : blocked().forceTerminal ? "terminal" : chosenMode()));
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
  const activity = computed(() => thread().conversation?.activity);
  const stoppingFor = signal("");
  const notice = signal("");
  const goneChoices = signal<ReadonlySet<string>>(new Set());
  const pendingChoice = computed(() => visiblePendingChoice(thread().conversation?.events, goneChoices()));
  const pendingChoiceKey = computed(() => pendingChoice()?.call_id ?? "");

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

  const renaming = signal(false);
  const renameValue = signal("");
  const title = computed(() => {
    const w = worker();
    return w ? sessionRowModel(w, { alias: aliasOf(props.workerId) }).title : hint.name || "Session";
  });
  const aliases = signal<Record<string, string>>({});
  const aliasOf = (id: string) => aliases()[id];
  void getWorkerAliases(Storage).then((a) => aliases.set(a)).catch(() => {});

  const saveRename = async () => {
    const name = untracked(renameValue).trim();
    renaming.set(false);
    await setWorkerAlias(Storage, props.workerId, name).catch(() => {});
    aliases.set({ ...untracked(aliases), [props.workerId]: name });
    void reloadAliases();
  };

  const terminate = async () => {
    const { action } = await AlertDialog.show({
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

  const menu = async () => {
    const actions = [
      ...(canChat() ? [{ id: "mode", label: mode() === "chat" ? "Open terminal" : "Open chat" }] : []),
      { id: "rename", label: "Rename" },
      { id: "terminate", label: "Terminate", role: "destructive" as const },
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ];
    const { action } = await AlertDialog.show({ title: untracked(title), presentation: "actionSheet", actions });
    if (action === "mode") chooseMode(untracked(mode) === "chat" ? "terminal" : "chat");
    else if (action === "rename") {
      renameValue.set(aliasOf(props.workerId) ?? "");
      renaming.set(true);
    } else if (action === "terminate") void terminate();
  };

  return (
    <NavPage title={title()}
      headerRight={<Button testID="session-menu" onPress={() => void menu()}><Icon name="more-horizontal" size={22} color="primary" /></Button>}>
      <View class="flex-1 bg-background">
        {worker() ? (
          <View class="flex-row items-center gap-2 border-b border-border px-4 py-1.5">
            <Text testID="session-status" class="text-xs text-muted-foreground">{workerStatusLabel(worker()!.status)}</Text>
            {worker()!.delegated ? <Text class="text-xs text-muted-foreground">· Delegated</Text> : null}
            {connection() !== "connected" ? <Text class="text-xs text-muted-foreground">· Offline</Text> : null}
          </View>
        ) : null}
        <View class="flex-1">
          <View class="absolute inset-0" style={{ display: mode() === "terminal" ? "flex" : "none" }}>
            <TerminalPane transport={transport} targetId={props.workerId} active={() => props.active() && mode() === "terminal"} />
          </View>
          {mode() === "chat" ? (
            <View class="absolute inset-0 bg-background">
              <ChatTimeline testID="session-chat" items={items}
                empty={() => <View class="flex-1 items-center justify-center p-8"><Text class="text-center text-base text-muted-foreground">{thread().loading ? "Loading conversation" : thread().error ?? "Nothing here yet."}</Text></View>} />
              <Composer text={text} placeholder="Message" canSend={() => true} onSend={(value) => chat.send(value)}
                running={() => activity()?.status === "running"} stopping={() => stoppingFor() === activity()?.id} canStop={() => connection() === "connected"}
                onStop={() => void stop()} active={() => props.active() && mode() === "chat"}>
                {pendingChoiceKey() ? (
                  <ChoiceCard choice={untracked(pendingChoice)!.choice} connected={() => connection() === "connected"}
                    submit={(answers) => answerChoice(transport, props.workerId, untracked(pendingChoiceKey), answers)}
                    onGone={() => {
                      goneChoices.set(new Set([...goneChoices(), untracked(pendingChoiceKey)]));
                      notice.set(CHOICE_GONE_NOTICE);
                    }} />
                ) : null}
                {notice() ? <Text testID="session-toast" class="pb-1.5 text-center text-sm text-muted-foreground" onPress={() => notice.set("")}>{notice()}</Text> : null}
              </Composer>
            </View>
          ) : null}
        </View>
      </View>
      <Sheet open={renaming()} onOpenChange={({ open }) => renaming.set(open)} popupClass="rounded-t-2xl bg-background p-4 gap-3">
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
      </Sheet>
    </NavPage>
  );
}
