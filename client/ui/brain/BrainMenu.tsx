import { effect, signal, untracked, View } from "nsh";
import { AlertDialog, Button } from "nsh/base";
import { Sheet } from "../kit/Sheet";
import { Row } from "../kit/kit";
import { Icon, type IconName } from "../kit/Icon";
import { AgentIcon } from "../icons/AgentIcon";
import { ModelPanel } from "../session/ModelSheet";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import { newBrainChat } from "../../core/brain";
import { brainMenuRows, type BrainMenuRowId } from "../../core/brainMenu";
import { reasoningEffortLabel } from "../../core/session/sessionModelHelpers";
import { canSwitchModel, getThreadRuntime } from "../../core/session/sessionRequests";
import type { ThreadRuntimeSelection } from "../../core/providers/types";
import { BrainHostPanel } from "./BrainHostSheet";
import { currentBrain, type BrainSource } from "./source";

/** What a refused New chat says (soft-final/SPEC.md §1.6). */
export const NEW_CHAT_FAILED = "Couldn't start a new chat";

const ROW_ICONS: Record<BrainMenuRowId, IconName> = {
  terminal: "terminal",
  workspace: "folder",
  executor: "swap-horizontal",
  model: "chip",
  "new-chat": "chat",
};

type Page = "menu" | "host" | "model";

/** "Opus · High": the model Brain runs and its effort. */
function modelValue(runtime: ThreadRuntimeSelection | null): string {
  if (!runtime?.model_id) return "";
  const effort = runtime.reasoning_effort || runtime.reasoning_effort_default;
  return effort ? `${runtime.model_id} · ${reasoningEffortLabel(effort)}` : runtime.model_id;
}

/**
 * Brain's menu (soft-final/SPEC.md §1.6), opened by ⋯ in the home header: one sheet
 * with Open terminal, Browse workspace, Switch executor (trailing the agent's logo),
 * Model (trailing "model · effort") and New chat, always in that order. A row the
 * computer cannot do now stays in place at 40 % and does nothing. Switch executor
 * and Model replace the menu with their page ("Brain host", Model & Effect) in the
 * same sheet; a switch that lands closes it. New chat closes it and asks "Start a
 * new chat?"; a refusal tells `onNotice` "Couldn't start a new chat".
 *
 * It reads the current computer's Brain (`source`, `currentBrain` by default); where
 * Open terminal and Browse workspace go is the host's.
 */
export function BrainMenu(props: {
  open: () => boolean;
  onClose: () => void;
  /** Brain's own Session (`host_worker.id`), on its terminal. */
  onOpenTerminal?: (hostWorkerId: string) => void;
  onBrowseWorkspace?: () => void;
  /**
   * Starts the new chat, and rejects when it could not (the menu then says so); without
   * it the menu asks the computer itself (`brain_chat_new`) and shows the current thread.
   */
  onNewChat?: () => Promise<unknown> | void;
  /** A short message for a toast ("Couldn't start a new chat"): without it a refusal is silent. */
  onNotice?: (text: string) => void;
  source?: BrainSource;
}) {
  const source = props.source ?? currentBrain;
  const page = signal<Page>("menu");
  const runtime = signal<ThreadRuntimeSelection | null>(null);
  const hostId = () => source.snapshot()?.host_worker?.id ?? "";
  const modelSwitchable = () => canSwitchModel(source.snapshot()?.host_worker?.capabilities as Parameters<typeof canSwitchModel>[0]);
  const rows = () => brainMenuRows({ connected: source.connected(), snapshot: source.snapshot(), threadLoaded: source.threadLoaded(), modelSwitchable: modelSwitchable() });

  const close = () => {
    source.hostSwitch.clearError();
    props.onClose();
  };
  // Each opening starts at the menu; the model read for one host is not another's.
  effect(() => {
    if (!props.open()) untracked(() => page.set("menu"));
  });
  effect(() => {
    hostId();
    untracked(() => runtime.set(null));
  });
  // The Model row's value, read when the menu opens on a host whose route can switch.
  effect(() => {
    if (!props.open() || !hostId() || !source.connected() || !modelSwitchable()) return;
    const transport = untracked(source.transport);
    const workerId = untracked(hostId);
    if (!transport) return;
    void getThreadRuntime(transport, workerId).then((selection) => untracked(hostId) === workerId && runtime.set(selection)).catch(() => {});
  });
  // A switch picked on the "Brain host" page that lands closes the sheet.
  let seen = untracked(source.hostSwitch.switched);
  effect(() => {
    const count = source.hostSwitch.switched();
    if (count === seen) return;
    seen = count;
    untracked(() => {
      if (props.open() && page() === "host") close();
    });
  });

  const newChat = async () => {
    close();
    const answer = await AlertDialog.show({
      title: "Start a new chat?",
      actions: [
        { id: "cancel", label: "Cancel", role: "cancel" },
        { id: "new", label: "New chat" },
      ],
    });
    if (answer.action !== "new") return;
    try {
      if (props.onNewChat) return await props.onNewChat();
      const transport = source.transport();
      if (!transport) throw new Error("Your computer is offline.");
      await newBrainChat(transport);
      source.showCurrentThread?.();
    } catch {
      props.onNotice?.(NEW_CHAT_FAILED);
    }
  };

  const choose = (id: BrainMenuRowId) => {
    if (!rows().find((row) => row.id === id)?.enabled) return;
    if (id === "terminal") {
      const worker = hostId();
      close();
      props.onOpenTerminal?.(worker);
    } else if (id === "workspace") {
      close();
      props.onBrowseWorkspace?.();
    } else if (id === "executor") {
      source.hostSwitch.clearError();
      page.set("host");
    } else if (id === "model") page.set("model");
    else void newChat();
  };

  const trailing = (id: BrainMenuRowId) => {
    const provider = source.snapshot()?.host_executor?.provider;
    return id === "executor" && provider ? <AgentIcon kind={agentKindFromProvider(provider)} size={22} /> : undefined;
  };

  return (
    <Sheet testID="brain-menu-sheet" open={props.open} onClose={close}
      title={() => (page() === "host" ? "Brain host" : page() === "model" ? "" : "Brain")}
      leading={page() === "host" ? (
        <Button testID="brain-menu-back" accessibilityLabel="Back to Brain's menu" onPress={() => page.set("menu")} class="-ml-2 rounded-full p-2 active:opacity-70">
          <Icon name="chevron-left" size={22} tone="ink" />
        </Button>
      ) : undefined}>
      {page() === "host" ? (
        <BrainHostPanel source={source} onClose={() => page.set("menu")} />
      ) : page() === "model" && source.transport() && hostId() ? (
        <View class="px-4">
          <ModelPanel shown={() => props.open() && page() === "model"} onClose={() => page.set("menu")} transport={source.transport()!} workerId={hostId()}
            onRuntime={(selection) => runtime.set(selection)} />
        </View>
      ) : (
        <View class="px-4">
          <View class="overflow-hidden rounded-(--radius-group) bg-sheet-surface">
            <View class="-mb-px">
              {rows().map((row) => (
                <Row testID={`brain-menu-${row.id}`} title={row.label} icon={ROW_ICONS[row.id]} disabled={!row.enabled} onPress={() => choose(row.id)}
                  value={row.id === "model" ? modelValue(runtime()) || undefined : undefined} trailing={trailing(row.id)} />
              ))}
            </View>
          </View>
        </View>
      )}
    </Sheet>
  );
}
