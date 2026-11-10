import { cleanup, signal, Text, untracked, View } from "nsh";
import { Button, Input, Sheet, TextView } from "nsh/base";
import { dshInteraction, watchDSHInteractions, type DSHAnswer, type DSHInteraction, type DSHInteractionSnapshot, type DSHSessionRun } from "../../core/session/dshInteractions";
import type { Transport } from "../../core/transport";
import { Icon } from "../icons/Icon";

type Values = Record<string, { selected: string[]; custom?: string }>;

/**
 * A DSH Session's requests (a permission, or questions): a banner above the
 * composer with Review, which opens the request to answer. Follows the
 * Session's run while it is on screen; a new run starts clean.
 */
export function DSHPanel(props: { transport: Transport; run: DSHSessionRun }) {
  const snapshot = signal<DSHInteractionSnapshot | null>(null);
  const error = signal("");
  const open = signal(false);
  const busy = signal(false);
  const values = signal<Values>({});
  const stop = watchDSHInteractions(props.transport, props.run, (next, message) => {
    snapshot.set(next);
    error.set(message ?? "");
  });
  cleanup(stop);
  const first = (): DSHInteraction | undefined => snapshot()?.items[0];

  const respond = async (answer: Omit<DSHAnswer, "epoch" | "rpcId">) => {
    const current = untracked(snapshot);
    const item = current?.items[0];
    if (!current?.connected || !item || untracked(busy)) return;
    busy.set(true);
    error.set("");
    try {
      await dshInteraction(props.transport, props.run, { ...answer, rpcId: item.rpcId, epoch: current.epoch });
      open.set(false);
      values.set({});
      const after = untracked(snapshot);
      if (after?.epoch === current.epoch) snapshot.set({ ...after, items: after.items.filter((i) => i.rpcId !== item.rpcId) });
    } catch (failure) {
      error.set(failure instanceof Error ? failure.message : "Answer failed. Refresh before retrying.");
    } finally {
      busy.set(false);
    }
  };

  const toggle = (questionId: string, label: string, multi: boolean) => {
    const all = untracked(values);
    const value = all[questionId] ?? { selected: [] };
    const on = value.selected.includes(label);
    const selected = multi ? (on ? value.selected.filter((l) => l !== label) : [...value.selected, label]) : on ? [] : [label];
    values.set({ ...all, [questionId]: { selected, custom: multi ? value.custom : undefined } });
  };
  const answerable = () => {
    const questions = first()?.payload.questions ?? [];
    return questions.length > 0 && questions.every((q) => values()[q.id]?.selected.length || values()[q.id]?.custom?.trim());
  };

  return (
    <View>
      {first() ? (
        <View testID="dsh-banner" class="mb-2 flex-row items-center gap-2 rounded-xl border border-border bg-card px-3 py-2.5">
          <Icon name="lock" size={16} color="seal" />
          <Text class="flex-1 text-sm font-semibold">{first()!.payload.type === "approval/requested" ? "Permission requested" : "Answer requested"}</Text>
          <Button testID="dsh-review" onPress={() => open.set(true)}><Text class="text-sm font-semibold text-primary">Review</Text></Button>
        </View>
      ) : null}
      {error() ? <Text class="pb-1.5 text-sm text-destructive">{error()}</Text> : null}
      <Sheet open={open() && Boolean(first())} onOpenChange={({ open: next }) => open.set(next)} closeOnSwipe
        popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
        <View class="gap-3 pb-8">
          {first()?.payload.type === "approval/requested" ? (
            <View class="gap-2">
              <Text class="text-lg font-semibold">{first()!.payload.toolName || "Permission requested"}</Text>
              {first()!.payload.reason ? <TextView markdown={first()!.payload.reason!} selectable class="text-base" /> : null}
              {first()!.payload.callId ? <Text class="text-xs text-muted-foreground">{first()!.payload.callId!}</Text> : null}
              <View class="flex-row gap-3 pt-2">
                <Button testID="dsh-reject" onPress={() => void respond({ outcome: "rejected" })} disabled={busy()} class="flex-1 items-center rounded-full border border-border py-2.5 active:bg-accent">
                  <Text class="text-base font-semibold">Reject</Text>
                </Button>
                <Button testID="dsh-allow" onPress={() => void respond({ outcome: "allowed-once" })} disabled={busy()} class="flex-1 items-center rounded-full bg-primary py-2.5 active:opacity-80">
                  <Text class="text-base font-semibold text-primary-foreground">Allow once</Text>
                </Button>
              </View>
            </View>
          ) : first() ? (
            <View class="gap-3">
              {(first()!.payload.questions ?? []).map((question) => (
                <View class="gap-2">
                  <Text class="text-lg font-semibold">{question.question}</Text>
                  {question.detail ? <Text class="text-sm text-muted-foreground">{question.detail}</Text> : null}
                  {(question.options ?? []).map((option) => (
                    <Button onPress={() => toggle(question.id, option.label, Boolean(question.multiSelect))} disabled={busy()}
                      class="flex-row items-center gap-2 rounded-xl border border-border px-3 py-2.5 active:bg-accent">
                      <View class="flex-1">
                        <Text class="text-base">{option.label}</Text>
                        {option.description ? <Text class="text-xs text-muted-foreground">{option.description}</Text> : null}
                      </View>
                      {values()[question.id]?.selected.includes(option.label) ? <Icon name="check" size={18} color="primary" /> : null}
                    </Button>
                  ))}
                  <Input multiline value={values()[question.id]?.custom ?? ""} placeholder="Your answer"
                    onChange={({ value }) => {
                      const all = untracked(values);
                      values.set({ ...all, [question.id]: { selected: question.multiSelect || !value ? all[question.id]?.selected ?? [] : [], custom: value } });
                    }}
                    class="rounded-xl border border-border bg-card px-3 py-2 text-base" />
                </View>
              ))}
              <View class="flex-row gap-3 pt-2">
                <Button testID="dsh-cancel" onPress={() => void respond({ cancel: true })} disabled={busy()} class="flex-1 items-center rounded-full border border-border py-2.5 active:bg-accent">
                  <Text class="text-base font-semibold">Cancel request</Text>
                </Button>
                <Button testID="dsh-send" disabled={busy() || !answerable()} class="flex-1 items-center rounded-full bg-primary py-2.5 active:opacity-80"
                  onPress={() => {
                    const questions = untracked(first)?.payload.questions ?? [];
                    const all = untracked(values);
                    void respond({ answer: { answers: questions.map((q) => ({ id: q.id, selected: all[q.id]?.selected ?? [], ...(all[q.id]?.custom?.trim() ? { custom: all[q.id]!.custom!.trim() } : {}) })) } });
                  }}>
                  <Text class="text-base font-semibold text-primary-foreground">Send answer</Text>
                </Button>
              </View>
            </View>
          ) : null}
        </View>
      </Sheet>
    </View>
  );
}
