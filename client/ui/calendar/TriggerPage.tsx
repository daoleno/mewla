import { Scroll, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import type { NavStackState } from "nsh/base";
import {
  attentionWords, everyWords, firingState, gapLine, triggerAttention, triggerLine, triggerNeedsPerson,
  type Trigger, type TriggerFiring,
} from "../../core/triggers/model";
import { Icon } from "../kit/Icon";
import { LeadHero, RowTile, tile } from "../kit/RowTile";
import { showAlert } from "../shell/alert";
import type { Route } from "../shell/routes";
import { clock } from "./clock";
import { NavPage } from "../shell/NavPage";
import { Row, SectionTitle } from "./parts";
import { triggers, triggersModel } from "./triggerState";

/** A Trigger's glyph on the neutral tile (SPEC §10.4): it watches. */
const TRIGGER_LEAD = tile("eye");

/** Its mark on a row: the seal while it needs you, else none. */
function NeedsDot(props: { trigger: () => Trigger }) {
  return <If when={triggerNeedsPerson(triggerAttention(props.trigger(), clock().now))}><View testID="trigger-needs" class="size-2 rounded-full bg-seal" /></If>;
}

/** One Trigger in Calendar's Triggers section: what it watches and what it did last. */
export function TriggerRow(props: { trigger: () => Trigger; onPress: () => void }) {
  return (
    <Button testID={`trigger-row-${props.trigger().id}`} onPress={props.onPress}
      accessibilityLabel={`${props.trigger().title}, ${triggerLine(props.trigger(), clock().now)}`}
      class="flex-row items-center gap-3 border-b border-border bg-card px-4 py-3 active:bg-accent">
      <RowTile lead={TRIGGER_LEAD} />
      <View class="flex-1 gap-1">
        <Text class="text-base line-clamp-1">{props.trigger().title}</Text>
        <View class="flex-row items-center gap-1.5">
          <NeedsDot trigger={props.trigger} />
          <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{triggerLine(props.trigger(), clock().now)}</Text>
        </View>
      </View>
    </Button>
  );
}

/** Calendar's Triggers, above Today; nothing at all while the computer has none. */
export function TriggerSection(props: { onOpen: (id: string) => void }) {
  return (
    <If when={triggers().length > 0}>
      <View testID="trigger-section">
        <SectionTitle title="Triggers" />
        <View class="border-t border-border">
          <Each of={triggers()} key={(t: Trigger) => t.id}>{(t: () => Trigger) => <TriggerRow trigger={t} onPress={() => props.onOpen(t().id)} />}</Each>
        </View>
      </View>
    </If>
  );
}

function FiringRow(props: { firing: () => TriggerFiring }) {
  const when = () => clock().format.dateTime(Date.parse(props.firing().at), clock().zones.device);
  const changes = () => (props.firing().outputs > 1 ? `${props.firing().outputs} changes` : "1 change");
  const state = () => firingState(props.firing(), clock().now);
  return (
    <View testID={`firing-${props.firing().seq}`} class="flex-row items-center gap-1.5 border-b border-border bg-card px-4 py-3">
      <If when={state() === "handled"} else={<If when={state() === "not handled"} else={<View class="size-2 rounded-full bg-ring" />}><Icon name="alert-circle" size={16} tone="warning" /></If>}><Icon name="check-circle" size={16} tone="ok" /></If>
      <Text class="flex-1 text-sm text-muted-foreground">{`${when()} · ${changes()} · ${state()}`}</Text>
    </View>
  );
}

export function TriggerPage(props: { id: string; nav: NavStackState<Route> }) {
  const trigger = () => triggers().find((t) => t.id === props.id);
  const busy = signal(false);
  const failure = signal("");
  const firingsNewestFirst = () => [...(trigger()?.firings ?? [])].reverse();

  const act = async (work: () => Promise<unknown>) => {
    busy.set(true);
    failure.set("");
    try {
      await work();
    } catch (error) {
      failure.set(error instanceof Error ? error.message : "That did not go through.");
    } finally {
      busy.set(false);
    }
  };

  const remove = async (target: Trigger) => {
    const answer = await showAlert({
      title: "Delete Trigger?",
      description: `${target.title} stops watching.`,
      actions: [{ id: "keep", label: "Keep", role: "cancel" }, { id: "delete", label: "Delete", role: "destructive" }],
    });
    if (answer.action === "delete") await act(async () => {
      await triggersModel()!.delete(target.id);
      props.nav.pop();
    });
  };

  return (
    <NavPage title={trigger()?.title ?? "Trigger"} testID="trigger-page">
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background">
        <If when={!trigger()} else={<View class="pb-10">
            <View class="gap-1.5 px-4 pb-2 pt-4">
              <LeadHero testID="trigger-lead" lead={TRIGGER_LEAD} />
              <Text testID="trigger-title" class="text-xl font-semibold">{trigger()!.title}</Text>
              <Text testID="trigger-line" class="text-base text-muted-foreground">{triggerLine(trigger()!, clock().now)}</Text>
            </View>
            <If when={attentionWords(trigger()!, clock().now)}><View class="flex-row gap-2 px-4 py-2">
                <Icon name="alert-circle" size={18} tone="warning" />
                <Text testID="trigger-attention" class="flex-1 text-base text-warning">{attentionWords(trigger()!, clock().now)}</Text>
              </View></If>
            <If when={gapLine(trigger()!, clock().now)}><View class="px-4 py-1"><Text testID="trigger-gap" class="text-sm text-muted-foreground">{gapLine(trigger()!, clock().now)}</Text></View></If>

            <SectionTitle title="Checks" />
            <View class="gap-1 border-y border-border bg-card px-4 py-3">
              <Text class="text-sm text-muted-foreground">{`${everyWords(trigger()!)} · stops after ${trigger()!.timeout_seconds} s`}</Text>
              <View selectable><Text testID="trigger-check" class="font-mono text-sm">{trigger()!.check}</Text></View>
              <If when={trigger()!.cwd}><Text class="text-sm text-muted-foreground line-clamp-1">{`in ${trigger()!.cwd}`}</Text></If>
            </View>
            <If when={trigger()!.instruction}><View>
                <SectionTitle title="When it fires" />
                <View selectable class="px-4"><Text class="text-base text-muted-foreground">{trigger()!.instruction!}</Text></View>
              </View></If>

            <If when={firingsNewestFirst().length}><SectionTitle title="Fired" /></If>
            <View class="border-t border-border">
              <Each of={firingsNewestFirst()} key={(f: TriggerFiring) => String(f.seq)}>{(f: () => TriggerFiring) => <FiringRow firing={f} />}</Each>
              <If when={(trigger()!.pending_changes ?? 0) > 0}><View class="px-4 py-3"><Text class="text-sm text-muted-foreground">{`${trigger()!.pending_changes} more ${trigger()!.pending_changes === 1 ? "change waits" : "changes wait"} for Brain to handle the last one.`}</Text></View></If>
            </View>

            <View class="mt-6 border-t border-border">
              <Row testID="trigger-check-now" title="Check now" tone="primary" disabled={busy() || Boolean(trigger()!.paused)} onPress={() => void act(() => triggersModel()!.checkNow(props.id))} />
              <If when={trigger()!.paused} else={<Row testID="trigger-pause" title="Pause" disabled={busy()} onPress={() => void act(() => triggersModel()!.pause(props.id))} />}>
                <Row testID="trigger-resume" title="Resume" disabled={busy()} onPress={() => void act(() => triggersModel()!.resume(props.id))} />
              </If>
              <Row testID="trigger-delete" title="Delete" tone="destructive" disabled={busy()} onPress={() => void remove(trigger()!)} />
            </View>
            <If when={failure()}><View class="px-4 pt-3"><Text testID="trigger-failure" class="text-base text-destructive">{`That did not go through. ${failure()}`}</Text></View></If>
          </View>}><View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">This Trigger is no longer on this computer.</Text></View></If>
      </Scroll>
    </NavPage>
  );
}
