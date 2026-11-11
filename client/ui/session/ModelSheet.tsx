import { effect, Scroll, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import type { Daemon } from "../../core/more/daemon";
import { classifyMutationPersistence, durabilityWarningMessage } from "../../core/providers/persistence";
import { ProviderError, providerMutationRequiresRefresh } from "../../core/providers/errors";
import { ProviderRequestOwner } from "../../core/providers/reconcile";
import type { ProvidersSnapshot, ThreadRuntimeChoice, ThreadRuntimeSelection } from "../../core/providers/types";
import {
  effectRowsForRuntime,
  reasoningEffortLabel,
  refetchFoundBindingNotSwitchable,
  runtimeChoiceForRow,
  threadRuntimeRows,
  type ProviderPickerModelRow,
} from "../../core/session/sessionModelHelpers";
import { getThreadRuntime, setThreadRuntime, threadRuntimeCatalog } from "../../core/session/sessionRequests";
import { Icon } from "../icons/Icon";

type Problem = { title: string; message: string };

function messageOf(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

/**
 * Model & Effect: the Session's models (from its connection's catalog, or a dsh
 * Session's own), each "Use <model>"; a model with effects opens its Effect list
 * (Back returns). A current model that is no longer offered is listed and
 * flagged. A switch closes the sheet once the computer saved it; a failure says
 * why, and Retry reloads (after an unknown outcome a reload is required before
 * the next switch). Shown only for a Session where `canSwitchModel` holds.
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
  const owner = new ProviderRequestOwner();
  const loading = signal(false);
  const activating = signal(false);
  const problem = signal<Problem | null>(null);
  const selection = signal<ThreadRuntimeSelection | null>(null);
  const catalog = signal<ProvidersSnapshot | null>(null);
  /** The row whose Effect list is open, by key. */
  const effectKey = signal("");

  const rows = () => threadRuntimeRows({ snapshot: catalog(), selection: selection(), activating: activating() });
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
      owner.settleRuntimeSwitch(token, { refreshRequired: typed.kind !== "busy" && (providerMutationRequiresRefresh(typed) || typed.kind === "conflict") });
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
    if (props.open()) untracked(() => void load());
    else effectKey.set("");
  });

  return (
    <AppSheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View testID="model-sheet" class="gap-3 pb-8">
        <View class="flex-row items-center gap-2">
          {effectTarget() ? (
            <Button testID="model-back" accessibilityLabel="Back to Model and Effect" onPress={() => effectKey.set("")} class="rounded-lg p-1 active:bg-accent">
              <Icon name="chevron-left" size={22} color="foreground" />
            </Button>
          ) : null}
          <Text class="flex-1 text-lg font-semibold">{effectTarget() ? "Effect" : "Model & Effect"}</Text>
          <Button testID="model-close" accessibilityLabel="Close runtime selection" onPress={props.onClose} class="rounded-full p-1.5 active:bg-accent">
            <Icon name="close" size={20} color="foreground" />
          </Button>
        </View>

        {loading() ? (
          <Text class="py-8 text-center text-sm text-muted-foreground">Loading models…</Text>
        ) : (
          <Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 420 }}>
            <View class="gap-2">
              {effectTarget() ? (
                <View class="gap-2">
                  <Text class="text-xs text-muted-foreground">{effectTarget()!.modelId}</Text>
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
                        {entry.selected ? <Icon name="check" size={18} color="primary" /> : null}
                      </Button>
                    ))}
                  </View>
                </View>
              ) : rows().length > 0 ? (
                <View class="overflow-hidden rounded-xl border border-border bg-card">
                  {rows().map((row) => (
                    <Button testID={`model-row-${row.modelId}`} accessibilityLabel={`Use ${row.modelId} model`} disabled={row.disabled} onPress={() => choose(row)}
                      class={row.disabled ? "flex-row items-center gap-2 border-b border-border px-3 py-3 opacity-50" : "flex-row items-center gap-2 border-b border-border px-3 py-3 active:bg-accent"}>
                      <View class="flex-1 gap-0.5">
                        <Text class="text-base line-clamp-1">{row.label}</Text>
                        {row.unavailableCurrent ? <Text testID="model-unavailable-current" class="text-xs text-muted-foreground">Current runtime is no longer selectable.</Text> : null}
                      </View>
                      {row.current ? <Icon name="check" size={18} color="primary" /> : null}
                      {row.effects.length > 0 ? <Icon name="chevron-right" size={18} /> : null}
                    </Button>
                  ))}
                </View>
              ) : null}

              {activating() ? <Text class="py-1 text-sm text-muted-foreground">Switching…</Text> : null}
              {problem() ? (
                <View testID="model-problem" class="gap-2 rounded-xl border border-border bg-card px-3 py-3">
                  <Text class="text-sm font-semibold text-destructive">{problem()!.title}</Text>
                  <Text class="text-sm text-muted-foreground">{problem()!.message}</Text>
                  <Button testID="model-retry" onPress={() => void load()} class="self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
                    <Text class="text-sm font-semibold text-primary">Retry</Text>
                  </Button>
                </View>
              ) : null}
              {!activating() && !problem() && rows().length === 0 ? (
                <Text class="text-sm text-muted-foreground">No configured runtime is available. Sync models in Settings.</Text>
              ) : null}
            </View>
          </Scroll>
        )}
      </View>
    </AppSheet>
  );
}
