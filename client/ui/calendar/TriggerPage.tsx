import { Scroll, signal, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import type { NavStackState } from "nsh/base";
import {
  askDetail, attentionWords, durationWords, everyWords, firingState, gapLine, triggerAttention, triggerGroups, triggerNeedsPerson,
  type Trigger, type TriggerFiring, type TriggerListRow,
} from "../../core/triggers/model";
import { pageColumnWidth } from "../../core/web/places";
import { Icon } from "../kit/Icon";
import { dense, density } from "../kit/density";
import { Section } from "../kit/kit";
import { LeadHero, tile } from "../kit/RowTile";
import { showAlert } from "../shell/alert";
import { NavPage } from "../shell/NavPage";
import type { Route } from "../shell/routes";
import { clock } from "./clock";
import { Row } from "./parts";
import { rowSize, SealDot, TileRow, TopHairline } from "./rows";
import { triggers, triggersModel } from "./triggerState";

/** A Trigger's glyph on the neutral tile (SPEC §10.4): it watches. */
const TRIGGER_LEAD = tile("eye");

/** The page's groups, worked out with this device's clock. */
function groups() {
  const c = clock();
  return triggerGroups(triggers(), c.now, (at) => c.zones.dateKey(at, c.zones.device), (at) => c.format.shortDate(at, c.zones.device));
}

/** The page's column and gutters: the phone's 16, the Mac's w-cal column. */
function Column(props: { children: JSX.Element; testID?: string }) {
  return (
    <Scroll keyboardShouldPersistTaps="handled" testID={props.testID} class="flex-1 bg-background" contentClass={dense({ phone: "px-4 pt-1", window: "px-6 pt-1" })} contentStyle={{ paddingBottom: 40 }}>
      <View class="w-full self-center" style={{ maxWidth: pageColumnWidth("page", density() !== "phone") ?? undefined }}>{props.children}</View>
    </Scroll>
  );
}

/**
 * Calendar's one Triggers row (option 2, owner 2026-11-22): how many watch, and the seal only when one needs the
 * person. It opens the Triggers page. Absent while the computer has none.
 */
export function TriggersRow(props: { onOpen: () => void }) {
  const needs = () => groups().needs.length;
  return (
    <If when={triggers().length > 0}>
      <View testID="triggers-row" class={`overflow-hidden bg-card ${dense({ phone: "mb-3 rounded-(--radius-group)", window: "mb-3 rounded-[14px]" })}`}>
        <TileRow testID="triggers-open" icon="eye" first={() => true} onPress={props.onOpen} seal={() => needs() > 0}
          title={() => "Triggers"}
          accessibilityLabel={() => `Triggers, ${groups().watching} watching${needs() ? `, ${needs()} needs you` : ""}`}
          line={() => `${groups().watching} watching`}
          lineNode={
            <View class="flex-row">
              <Text class={`${rowSize().line} text-muted-foreground`}>{`${groups().watching} watching`}</Text>
              <If when={needs()}><Text class={`${rowSize().line} text-seal-ink`}>{` · ${needs()} needs you`}</Text></If>
            </View>
          } />
      </View>
    </If>
  );
}

function TriggerListItem(props: { row: () => TriggerListRow; first: () => boolean; onOpen: (id: string) => void }) {
  const t = () => props.row().trigger;
  const end = () => (props.row().at ? clock().format.time(props.row().at!, clock().zones.device) : undefined);
  return (
    <TileRow testID={`trigger-row-${t().id}`} icon={t().paused ? "pause" : "eye"} first={props.first} onPress={() => props.onOpen(t().id)}
      seal={() => props.row().needsPerson} title={() => t().title} line={() => props.row().line} end={end}
      lineTone={() => (props.row().needsPerson ? "text-seal-ink" : "text-muted-foreground")}
      accessibilityLabel={() => [t().title, props.row().line, end()].filter(Boolean).join(", ")} />
  );
}

function TriggerGroup(props: { title: string; testID: string; rows: () => TriggerListRow[]; seal?: boolean; onOpen: (id: string) => void }) {
  return (
    <If when={props.rows().length}>
      <Section testID={props.testID} label={
        <View class="mb-2 mt-5 flex-row items-center gap-1.5 px-1">
          <If when={props.seal}><SealDot size={7} /></If>
          <Text role="header" class="text-caption font-semibold text-muted-foreground">{props.title}</Text>
        </View>}>
        <Each of={props.rows()} key={(row: TriggerListRow) => row.trigger.id}>{(row: () => TriggerListRow, index: () => number) => (
          <TriggerListItem row={row} first={() => index() === 0} onOpen={props.onOpen} />
        )}</Each>
      </Section>
    </If>
  );
}

/** The Triggers page: what needs you, what noticed something today, what is quiet. */
export function TriggersPage(props: { nav: NavStackState<Route> }) {
  const open = (id: string) => props.nav.push({ name: "trigger", id });
  return (
    <NavPage title="Triggers" testID="triggers-page">
      <Column testID="triggers-list">
        <If when={triggers().length} else={<View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">Nothing is watching on this computer.</Text></View>}>
          <View testID="triggers-headline" class="mb-1 mt-2 flex-row flex-wrap items-baseline gap-2 px-0.5">
            <Text role="header" class={dense({ phone: "text-[30px] font-bold tracking-display", window: "text-[32px] font-bold tracking-display" })}>{`${groups().watching} watching`}</Text>
            <If when={groups().needs.length}><Text class="text-[15px] text-muted-foreground">{`${groups().needs.length} needs you`}</Text></If>
          </View>
          <TriggerGroup testID="triggers-needs" title="Needs you" seal rows={() => groups().needs} onOpen={open} />
          <TriggerGroup testID="triggers-today" title="Noticed today" rows={() => groups().today} onOpen={open} />
          <TriggerGroup testID="triggers-quiet" title="Quiet" rows={() => groups().quiet} onOpen={open} />
        </If>
      </Column>
    </NavPage>
  );
}

/** "Today 06:40", "Nov 21, 22:10": when a firing was, on this device's clock. */
function firingWhen(at: number): string {
  const c = clock();
  const zone = c.zones.device;
  const time = c.format.time(at, zone);
  return c.zones.dateKey(at, zone) === c.zones.dateKey(c.now, zone) ? `Today ${time}` : `${c.format.shortDate(at, zone)}, ${time}`;
}

const FIRING_WORDS: Record<string, string> = { handled: "Brain handled it", "not handled": "Brain did not handle it", "with Brain": "With Brain", "waiting to reach Brain": "Waiting to reach Brain" };

function FiringRow(props: { firing: () => TriggerFiring; first: () => boolean }) {
  const size = rowSize();
  const state = () => firingState(props.firing(), clock().now);
  const changes = () => (props.firing().outputs > 1 ? `${props.firing().outputs} changes` : "1 change");
  const line = () => `${changes()} · ${FIRING_WORDS[state()] ?? state()}`;
  return (
    <View testID={`firing-${props.firing().seq}`} accessible accessibilityLabel={`${firingWhen(Date.parse(props.firing().at))}, ${line()}`} class={`${size.min} flex-row items-center ${size.gap} ${size.pad}`}>
      <TopHairline first={props.first} left={43} />
      <View class="w-[18px] items-center">
        <If when={state() === "handled"} else={<If when={state() === "not handled"} else={<View class="size-2 rounded-full bg-ring" />}><Icon name="alert-circle" size={18} tone="warning" /></If>}><Icon name="check-circle" size={18} tone="ok" /></If>
      </View>
      <View class="min-w-0 flex-1">
        <Text class={`${size.title}`}>{firingWhen(Date.parse(props.firing().at))}</Text>
        <Text class={`${size.line} ${state() === "not handled" ? "text-warning" : "text-muted-foreground"}`}>{line()}</Text>
      </View>
    </View>
  );
}

function Pill(props: { testID: string; label: string; primary?: boolean; disabled?: boolean; onPress: () => void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled}
      class={`h-9 items-center justify-center rounded-full px-4 active:opacity-70 disabled:opacity-40 ${props.primary ? "bg-primary" : "bg-accent"}`}>
      <Text class={`text-sm font-semibold ${props.primary ? "text-primary-foreground" : ""}`}>{props.label}</Text>
    </Button>
  );
}

/** Pause, resume, check now and delete, with what went wrong. */
function useTriggerActions(id: string, nav: NavStackState<Route>) {
  const busy = signal(false);
  const failure = signal("");
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
      nav.pop();
    });
  };
  return {
    busy, failure, remove,
    checkNow: () => void act(() => triggersModel()!.checkNow(id)),
    pause: () => void act(() => triggersModel()!.pause(id)),
    resume: () => void act(() => triggersModel()!.resume(id)),
  };
}

/**
 * A Trigger's page: what it watches and how often, why it needs you (with what moves it on), what it noticed and
 * whether Brain handled it, what Brain does then, how it checks (one level down), and its controls.
 */
export function TriggerPage(props: { id: string; nav: NavStackState<Route> }) {
  const trigger = () => triggers().find((t) => t.id === props.id);
  const actions = useTriggerActions(props.id, props.nav);
  const firingsNewestFirst = () => [...(trigger()?.firings ?? [])].reverse();
  const attention = () => (trigger() ? triggerAttention(trigger()!, clock().now) : null);
  const needsPerson = () => triggerNeedsPerson(attention());
  const pending = () => trigger()?.pending_changes ?? 0;
  return (
    <NavPage title={trigger()?.title ?? "Trigger"} testID="trigger-page">
      <Column>
        <If when={trigger()} else={<View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">This Trigger is no longer on this computer.</Text></View>}>
          <View class="gap-1 px-0.5 pb-3 pt-2">
            <LeadHero testID="trigger-lead" lead={TRIGGER_LEAD} />
            <Text testID="trigger-title" class="mt-2 text-[24px] font-bold leading-[30px] tracking-display">{trigger()!.title}</Text>
            <Text testID="trigger-line" class="text-[15px] text-muted-foreground">{trigger()!.paused ? "Paused" : `Checks ${everyWords(trigger()!).toLowerCase()}`}</Text>
          </View>
          <If when={needsPerson()}>
            <View testID="trigger-ask" class={`flex-row gap-2.5 bg-card p-[14px] ${dense({ phone: "rounded-(--radius-group)", window: "rounded-[14px]" })}`}>
              <View class="pt-1.5"><SealDot size={9} /></View>
              <View class="min-w-0 flex-1">
                <Text class="text-[15px] font-semibold">{attention() === "runaway" ? "It paused itself" : "Its check keeps failing"}</Text>
                <Text testID="trigger-attention" class="mt-0.5 text-sm leading-[19px] text-muted-foreground">{askDetail(trigger()!, (at) => clock().format.time(at, clock().zones.device))}</Text>
                <View class="mt-3 flex-row gap-2">
                  <If when={attention() === "runaway"} else={<View class="flex-row gap-2">
                    <Pill testID="trigger-ask-check" label="Check now" primary disabled={actions.busy()} onPress={actions.checkNow} />
                    <Pill testID="trigger-ask-pause" label="Pause" disabled={actions.busy()} onPress={actions.pause} />
                  </View>}>
                    <Pill testID="trigger-ask-resume" label="Resume" primary disabled={actions.busy()} onPress={actions.resume} />
                  </If>
                </View>
              </View>
            </View>
          </If>
          {/* A firing Brain has not handled is Brain's to handle (owner 2026-11-19): said here, never with the seal. */}
          <If when={!needsPerson() && attention()}><View class="flex-row gap-2 px-0.5 py-1">
              <Icon name="alert-circle" size={18} tone="warning" />
              <Text testID="trigger-attention" class="flex-1 text-[15px] text-warning">{attentionWords(trigger()!, clock().now)}</Text>
            </View></If>
          <If when={gapLine(trigger()!, clock().now)}><View class="px-0.5 py-1"><Text testID="trigger-gap" class="text-sm text-muted-foreground">{gapLine(trigger()!, clock().now)}</Text></View></If>

          <If when={firingsNewestFirst().length}>
            <Section testID="trigger-fired" title="Noticed">
              <Each of={firingsNewestFirst()} key={(f: TriggerFiring) => String(f.seq)}>{(f: () => TriggerFiring, index: () => number) => <FiringRow firing={f} first={() => index() === 0} />}</Each>
              <If when={pending() > 0}><View class="border-t border-border px-[14px] py-3"><Text class="text-sm text-muted-foreground">{`${pending()} more ${pending() === 1 ? "change waits" : "changes wait"} for Brain to handle the last one.`}</Text></View></If>
            </Section>
          </If>
          <If when={trigger()!.instruction}>
            <Section title="When it notices something">
              <View selectable class="px-[14px] py-3"><Text testID="trigger-instruction" class="text-[15px] leading-[21px] text-muted-foreground">{trigger()!.instruction!}</Text></View>
            </Section>
          </If>
          <Section>
            <TileRow testID="trigger-how" icon="terminal" first={() => true} title={() => "How it checks"} line={() => `${everyWords(trigger()!)} · stops after ${durationWords(trigger()!.timeout_seconds * 1000)}`}
              onPress={() => props.nav.push({ name: "triggerCheck", id: props.id })} />
          </Section>
          <Section>
            <If when={!needsPerson()}><Row testID="trigger-check-now" title="Check now" tone="primary" disabled={actions.busy() || Boolean(trigger()!.paused)} onPress={actions.checkNow} /></If>
            <If when={trigger()!.paused} else={<Row testID="trigger-pause" title="Pause" disabled={actions.busy()} onPress={actions.pause} />}>
              <Row testID="trigger-resume" title="Resume" disabled={actions.busy()} onPress={actions.resume} />
            </If>
            <Row testID="trigger-delete" title="Delete" tone="delete" disabled={actions.busy()} onPress={() => void actions.remove(trigger()!)} />
          </Section>
          <If when={actions.failure()}><View class="px-1 pt-3"><Text testID="trigger-failure" class="text-base text-destructive">{`That did not go through. ${actions.failure()}`}</Text></View></If>
        </If>
      </Column>
    </NavPage>
  );
}

/** How a Trigger checks: the command, where it runs, how often and how long it may take. */
export function TriggerCheckPage(props: { id: string }) {
  const trigger = () => triggers().find((t) => t.id === props.id);
  return (
    <NavPage title="How it checks" testID="trigger-check-page">
      <Column>
        <If when={trigger()} else={<View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">This Trigger is no longer on this computer.</Text></View>}>
          <Section title={`${everyWords(trigger()!)} · stops after ${durationWords(trigger()!.timeout_seconds * 1000)}`}>
            <View selectable class="px-[14px] py-3"><Text testID="trigger-check" class="font-mono text-sm">{trigger()!.check}</Text></View>
          </Section>
          <If when={trigger()!.cwd}>
            <Section title="Runs in">
              <View selectable class="px-[14px] py-3"><Text testID="trigger-cwd" class="font-mono text-sm">{trigger()!.cwd!}</Text></View>
            </Section>
          </If>
        </If>
      </Column>
    </NavPage>
  );
}
