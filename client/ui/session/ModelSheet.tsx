import { effect, Scroll, signal, Text, untracked, View } from "nsh";
import { BusyPlace } from "../kit/wait";
import { Button } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import type { Daemon } from "../../core/more/daemon";
import { classifyMutationPersistence, durabilityWarningMessage } from "../../core/providers/persistence";
import { PROVIDER_ERROR_CODES, ProviderError } from "../../core/providers/errors";
import { ProviderRequestOwner } from "../../core/providers/reconcile";
import type { ProvidersSnapshot, ThreadRuntimeChoice, ThreadRuntimeSelection } from "../../core/providers/types";
import {
  effectRowsForRuntime,
  reasoningEffortLabel,
  refetchFoundBindingNotSwitchable,
  runtimeChoiceForRow,
  runtimeSwitchNote,
  threadRuntimeRows,
  type ProviderPickerModelRow,
} from "../../core/session/sessionModelHelpers";
import { getThreadRuntime, setThreadRuntime, threadRuntimeCatalog } from "../../core/session/sessionRequests";
import { Icon } from "../icons/Icon";

type Problem = { title: string; message: string };

/** How often an open sheet reads a pick held for the end of the agent's turn. */
const PENDING_REREAD_MS = 2_000;

function messageOf(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

/**
 * Model & Effort: the Session's models (from its connection's catalog, or a dsh
 * Session's own), each "Use <model>"; a model with effects opens its Effect list
 * (Back returns). A current model that is no longer offered is listed and
 * flagged. A switch closes the sheet once the computer saved it; a failure says
 * why, and Retry reloads (after an unknown outcome a reload is required before
 * the next switch). A pick the agent takes when its turn ends keeps the sheet
 * open and says so ("Opus 5.5 after this turn"); the sheet reads the runtime
 * again while it waits, so the mark moves once the agent switched, and a pick
 * the agent did not take says why. Shown only for a Session where
 * `canSwitchModel` holds.
 *
 * Mount one per Session (and computer): it keeps what it read between openings.
 * `onRuntime` hears each runtime read or applied, so the composer's model chip
 * can follow it.
 */
export function ModelSheet(props: {
  open: () => boolean;
  onClose: () => void;
  transport: Daemon;
  workerId: string;
  onRuntime?: (selection: ThreadRuntimeSelection) => void;
}) {
  return (
    <AppSheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="rounded-t-2xl bg-background pt-4" sidePadding={16} backdropClass="bg-black/40">
      <ModelPanel shown={props.open} onClose={props.onClose} transport={props.transport} workerId={props.workerId} onRuntime={props.onRuntime} />
    </AppSheet>
  );
}

/**
 * The sheet's content, for a sheet that shows it as one of its pages (Brain's
 * menu › Model): each time `shown` turns true it reads the runtime again.
 */
export function ModelPanel(props: {
  shown: () => boolean;
  onClose: () => void;
  transport: Daemon;
  workerId: string;
  onRuntime?: (selection: ThreadRuntimeSelection) => void;
}) {
  const owner = new ProviderRequestOwner();
  const loading = signal(false);
  const activating = signal(false);
  const problem = signal<Problem | null>(null);
  const selection = signal<ThreadRuntimeSelection | null>(null);
  const catalog = signal<ProvidersSnapshot | null>(null);
  /** The row whose Effect list is open, by key. */
  const effectKey = signal("");

  const rows = () => threadRuntimeRows({ snapshot: catalog(), selection: selection(), activating: activating() });
  const note = () => runtimeSwitchNote(selection());
  const effectTarget = () => (effectKey() ? rows().find((row) => row.key === effectKey()) ?? null : null);

  const load = async () => {
    owner.rebind(props.transport.server.id, props.workerId);
    const admission = owner.admitSessionLoad();
    if (!admission.ok) return problem.set({ title: "Could not load models", message: admission.reason });
    const token = admission.token;
    loading.set(true);
    problem.set(null);
    try {
      const next = await getThreadRuntime(props.transport, props.workerId);
      if (!owner.acceptSession(token)) return;
      selection.set(next);
      props.onRuntime?.(next);
      if (refetchFoundBindingNotSwitchable({ activationCapable: true, hotSwitchable: next.hot_switchable === true })) {
        catalog.set(null);
        return props.onClose();
      }
      const nextCatalog = await threadRuntimeCatalog(props.transport, next);
      if (!owner.isCurrent(token)) return;
      catalog.set(nextCatalog);
    } catch (error) {
      if (owner.isCurrent(token)) problem.set({ title: "Could not load models", message: messageOf(error, "Failed to load thread runtime.") });
    } finally {
      if (owner.isCurrent(token)) loading.set(false);
    }
  };

  const activate = async (runtime: ThreadRuntimeChoice) => {
    if (!selection() || !catalog()) return;
    owner.rebind(props.transport.server.id, props.workerId);
    if (owner.runtimeSwitchRequiresRefresh()) return problem.set({ title: "Could not switch model", message: "Refresh required before switching runtime." });
    const admission = owner.admitRuntimeSwitch();
    if (!admission.ok) return problem.set({ title: "Could not switch model", message: admission.reason });
    const token = admission.token;
    activating.set(true);
    problem.set(null);
    try {
      const result = await setThreadRuntime(props.transport, props.workerId, runtime);
      if (!owner.isCurrent(token)) return;
      const outcome = classifyMutationPersistence(result.persistence);
      if (outcome === "applied_durable" || outcome === "applied_uncertain") {
        owner.settleRuntimeSwitch(token, { refreshRequired: outcome === "applied_uncertain" });
        selection.set(result.runtime);
        props.onRuntime?.(result.runtime);
        // Held for the end of the agent's turn: the sheet stays and says so.
        if (result.runtime.pending_model_id) return effectKey.set("");
        if (outcome === "applied_durable") return props.onClose();
        return problem.set({ title: "Switched", message: durabilityWarningMessage(result.persistence) });
      }
      owner.settleRuntimeSwitch(token, { refreshRequired: true });
      problem.set({
        title: "Could not switch model",
        message: outcome === "ambiguous" ? "Runtime switch was not acknowledged. Refresh and try again." : "Runtime switch was not applied.",
      });
    } catch (error) {
      if (!owner.isCurrent(token)) return;
      const typed = error instanceof ProviderError ? error : new ProviderError("unknown", messageOf(error, "Runtime switch failed"), "unknown", true);
      // Only an answer that never came, or one that did not say what happened,
      // leaves the outcome unknown; a refusal is definite and the next pick
      // is taken as it is (11-24: one refusal locked every later tap behind
      // "Refresh required before switching runtime").
      const unknownOutcome = typed.kind === "timeout" || typed.kind === "offline" || typed.code === PROVIDER_ERROR_CODES.invalidReply;
      owner.settleRuntimeSwitch(token, { refreshRequired: unknownOutcome });
      problem.set({ title: "Could not switch model", message: typed.message });
    } finally {
      if (owner.isCurrent(token)) activating.set(false);
    }
  };

  const choose = (row: ProviderPickerModelRow) => {
    if (row.disabled) return;
    if (row.effects.length > 0) return effectKey.set(row.key);
    const runtime = runtimeChoiceForRow(row);
    if (runtime) void activate(runtime);
  };

  // Each opening reads the runtime again; closing leaves the Effect list.
  effect(() => {
    if (props.shown()) untracked(() => void load());
    else effectKey.set("");
  });
  // While a pick waits for the end of the agent's turn, the open sheet reads
  // the runtime again (the runtime only: no catalog, no new request turn), so
  // it shows the switch once the agent made it, or why it did not (11-25: the
  // hourglass stayed after Claude had switched).
  effect(() => {
    if (!props.shown() || !selection()?.pending_model_id) return;
    let stopped = false;
    const timer = setInterval(() => {
      if (untracked(activating)) return;
      void getThreadRuntime(props.transport, props.workerId)
        .then((next) => {
          if (stopped || untracked(activating)) return;
          selection.set(next);
          props.onRuntime?.(next);
        })
        .catch(() => {});
    }, PENDING_REREAD_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  });

  return (
    <View testID="model-sheet" class="gap-3 pb-8">
      <View class="flex-row items-center gap-2">
        <If when={effectTarget()}><Button testID="model-back" accessibilityLabel="Back to Model and Effort" onPress={() => effectKey.set("")} class="rounded-lg p-1 active:bg-accent">
            <Icon name="chevron-left" size={22} color="foreground" />
          </Button></If>
        <Text class="flex-1 text-lg font-semibold">{effectTarget() ? "Effort" : "Model & Effort"}</Text>
        <Button testID="model-close" accessibilityLabel="Close runtime selection" onPress={props.onClose} class="rounded-full p-1.5 active:bg-accent">
          <Icon name="close" size={20} color="foreground" />
        </Button>
      </View>

      <If when={loading()} else={<Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 420 }}>
          <View class="gap-2">
            <If when={effectTarget()} else={<If when={rows().length > 0}><View class="overflow-hidden rounded-xl border border-border bg-card">
                {rows().map((row) => (
                  <Button testID={`model-row-${row.modelId}`} accessibilityLabel={`Use ${row.modelId} model`} disabled={row.disabled} onPress={() => choose(row)}
                    class={row.disabled ? "flex-row items-center gap-2 border-b border-border px-3 py-3 opacity-50" : "flex-row items-center gap-2 border-b border-border px-3 py-3 active:bg-accent"}>
                    <View class="flex-1 gap-0.5">
                      <Text class="text-base line-clamp-1">{row.label}</Text>
                      <If when={row.unavailableCurrent}><Text testID="model-unavailable-current" class="text-xs text-muted-foreground">Current runtime is no longer selectable.</Text></If>
                    </View>
                    <If when={row.pending}><View testID={`model-pending-${row.modelId}`}><Icon name="hourglass" size={16} color="muted-foreground" /></View></If>
                    <If when={row.current && !row.pending}><View testID={`model-current-${row.modelId}`}><Icon name="check" size={18} color="primary" /></View></If>
                    <If when={row.effects.length > 0}><Icon name="chevron-right" size={18} /></If>
                  </Button>
                ))}
              </View></If>}><View class="gap-2">
                <Text class="text-xs text-muted-foreground">{effectTarget()!.label}</Text>
                <View class="overflow-hidden rounded-xl border border-border bg-card">
                  {effectRowsForRuntime(effectTarget()!).map((entry) => (
                    <Button testID={`effect-row-${entry.key}`} accessibilityLabel={`Use ${reasoningEffortLabel(entry.effect)} effect`} disabled={activating()}
                      onPress={() => {
                        const target = effectTarget();
                        const runtime = target ? runtimeChoiceForRow(target, entry.effect) : null;
                        if (runtime) void activate(runtime);
                      }}
                      class="flex-row items-center gap-2 border-b border-border px-3 py-3 active:bg-accent">
                      <Text class="flex-1 text-base">{reasoningEffortLabel(entry.effect)}</Text>
                      <If when={entry.pending}><Icon name="hourglass" size={16} color="muted-foreground" /></If>
                      <If when={entry.selected && !entry.pending}><Icon name="check" size={18} color="primary" /></If>
                    </Button>
                  ))}
                </View>
              </View></If>

            <If when={activating()}><Text class="py-1 text-sm text-muted-foreground">Switching…</Text></If>
            <If when={!activating() && note()}><Text testID="model-switch-note" class={note()!.kind === "failed" ? "py-1 text-sm text-destructive" : "py-1 text-sm text-muted-foreground"}>{note()!.text}</Text></If>
            <If when={problem()}><View testID="model-problem" class="gap-2 rounded-xl border border-border bg-card px-3 py-3">
                <Text class="text-sm font-semibold text-destructive">{problem()!.title}</Text>
                <Text class="text-sm text-muted-foreground">{problem()!.message}</Text>
                <Button testID="model-retry" onPress={() => void load()} class="self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
                  <Text class="text-sm font-semibold text-primary">Retry</Text>
                </Button>
              </View></If>
            <If when={!activating() && !problem() && rows().length === 0}><Text class="text-sm text-muted-foreground">No configured runtime is available. Sync models in Settings.</Text></If>
          </View>
        </Scroll>}><BusyPlace label="Reading models" testID="models-loading" rows={4} failMs={31_000 /* the runtime, then the catalog: 15 s each */} /></If>
    </View>
  );
}
