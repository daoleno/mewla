import { signal, Text, View } from "nsh";
import { Button, TextView } from "nsh/base";
import type { ActivityTimelineItem } from "../../core/timeline/activityTypes";
import type { BrainWorkEventTimelineItem, PlanTimelineItem } from "../../core/timeline/types";
import type { ComputerMessageTimelineItem } from "../../core/timeline/computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "../../core/timeline/taskNotificationCardModel";
import { Icon, type IconName } from "../icons/Icon";
import { ICON_ASSETS } from "../icons/assets";

/**
 * Which rows are open. A list recycles its cells, so a row's own signal would
 * follow the cell to another item: the open set lives with the chat, by id.
 */
export function createExpansion() {
  const open = signal<ReadonlySet<string>>(new Set());
  return {
    isOpen: (id: string) => open().has(id),
    toggle: (id: string) => {
      const next = new Set(open());
      if (next.has(id)) next.delete(id);
      else next.add(id);
      open.set(next);
    },
  };
}
export type Expansion = ReturnType<typeof createExpansion>;

const iconOf = (name: string | undefined, fallback: IconName): IconName => (name && name in ICON_ASSETS ? (name as IconName) : fallback);
const toneColor = (tone: string | undefined) => (tone === "failed" || tone === "danger" ? "destructive" : tone === "running" || tone === "accent" ? "foreground" : tone === "attention" ? "warning" : "muted-foreground");

const Mono = (props: { text: string }) => (
  <TextView document={{ blocks: [{ type: "codeBlock", code: props.text, language: "" }] }} selectable />
);

/** A tool call, a reasoning or update line, or a folded run of finished calls. */
export function ActivityRow(props: { item: () => ActivityTimelineItem; expansion: Expansion }) {
  const item = props.item;
  const open = () => props.expansion.isOpen(item().id);
  const expandable = () => Boolean(item().commandText || item().queryText || item().body || item().files?.length || item().steps?.length || item().statusLine);
  return (
    <View class="px-4 py-0.5">
      <Button testID="activity" onPress={() => expandable() && props.expansion.toggle(item().id)} class="flex-row items-center gap-2 py-1">
        <Icon name={iconOf(item().icon, "construct")} color={toneColor(item().tone)} size={16} />
        <Text class={`flex-1 text-sm ${item().tone === "running" ? "" : "text-muted-foreground"} ${open() ? "" : "line-clamp-1"}`}>
          {item().title}{item().detail ? ` · ${item().detail}` : ""}
        </Text>
        {expandable() ? <Icon name={open() ? "chevron-up" : "chevron-down"} size={14} /> : null}
      </Button>
      {open() ? (
        <View class="gap-1.5 pb-2 pl-6">
          {item().steps?.length ? item().steps!.map((step) => (
            <View class="flex-row items-center gap-2">
              <Icon name={iconOf(step.icon, "construct")} color={toneColor(step.tone)} size={14} />
              <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{step.title}</Text>
            </View>
          )) : null}
          {item().commandText ? <Mono text={item().commandText!} /> : null}
          {item().queryText ? <TextView document={{ blocks: [{ type: "paragraph", content: [{ type: "text", text: item().queryText! }] }] }} selectable class="text-sm" /> : null}
          {item().statusLine ? <Text class="text-xs text-muted-foreground">{item().statusLine}</Text> : null}
          {item().body ? (item().bodyKind === "terminal" ? <Mono text={item().body!} /> : <TextView markdown={item().body!} selectable class="text-sm" />) : null}
          {item().files?.length ? item().files!.map((file) => <Text class="font-mono text-xs text-muted-foreground">{file}</Text>) : null}
        </View>
      ) : null}
    </View>
  );
}

const STEP_ICON: Record<string, IconName> = { completed: "check-circle", in_progress: "time", pending: "circle" };

export function PlanRow(props: { item: () => PlanTimelineItem }) {
  return (
    <View class="mx-4 my-1 gap-1.5 rounded-xl border border-border px-3 py-2.5">
      <View class="flex-row items-center gap-2">
        <Icon name="list" size={16} />
        <Text class="text-sm font-semibold">Plan</Text>
      </View>
      {props.item().explanation ? <Text class="text-sm text-muted-foreground">{props.item().explanation}</Text> : null}
      {props.item().steps.map((step) => (
        <View class="flex-row items-start gap-2">
          <Icon name={STEP_ICON[step.status] ?? "circle"} color={step.status === "in_progress" ? "foreground" : "muted-foreground"} size={16} />
          <Text class={step.status === "completed" ? "flex-1 text-sm text-muted-foreground line-through" : "flex-1 text-sm"}>{step.step}</Text>
        </View>
      ))}
    </View>
  );
}

function Card(props: { id: string; expansion: Expansion; icon: IconName; tone: string; kind: string; title: string; status?: string; preview: string; body: string; extra?: () => unknown }) {
  const open = () => props.expansion.isOpen(props.id);
  return (
    <Button onPress={() => props.expansion.toggle(props.id)} class="mx-4 my-1 gap-1.5 rounded-xl border border-border bg-card px-3 py-2.5 active:bg-accent">
      <View class="flex-row items-center gap-2">
        <Icon name={props.icon} color={toneColor(props.tone)} size={16} />
        <Text class="flex-1 text-xs font-semibold uppercase text-muted-foreground line-clamp-1">{props.kind}</Text>
        {props.status ? <Text class="text-xs text-muted-foreground">{props.status}</Text> : null}
      </View>
      {props.title ? <Text class="text-base font-semibold">{props.title}</Text> : null}
      {open() ? <TextView markdown={props.body || props.preview} selectable class="text-sm" /> : props.preview ? <Text class="text-sm text-muted-foreground line-clamp-3">{props.preview}</Text> : null}
      {open() && props.extra ? (props.extra() as never) : null}
    </Button>
  );
}

/** A background task, subagent or monitor that reported back. */
export function TaskNotificationCard(props: { item: () => TaskNotificationTimelineItem; expansion: Expansion }) {
  const it = props.item;
  return (
    <Card id={it().id} expansion={props.expansion} icon={iconOf(it().icon, "pulse")} tone={it().tone} kind={it().kindLabel} title={it().title}
      status={it().statusLabel} preview={[it().facts.join(" · "), it().preview].filter(Boolean).join("\n")} body={it().body}
      extra={() => it().details.map((d) => <Text class="text-xs text-muted-foreground">{`${d.label}: ${d.value}`}</Text>)} />
  );
}

/** A task sent to, or answered by, another computer. */
export function ComputerMessageCard(props: { item: () => ComputerMessageTimelineItem; expansion: Expansion }) {
  const it = props.item;
  return (
    <Card id={it().id} expansion={props.expansion} icon="desktop" tone={it().tone} kind={`${it().direction === "to" ? "To" : "From"} ${it().computer} · ${it().kindLabel}`}
      title={it().userSaid ? `“${it().userSaid}”` : ""} status={it().statusLabel} preview={it().preview} body={it().body} />
  );
}

const WORK_KIND: Record<string, string> = {
  "session.done": "Done", "session.failed": "Failed", "session.needs_input": "Needs you", "session.stale": "Stalled",
  "session.uncertain": "Unclear", "session.ownership_lost": "Lost",
};

/** A Work result Brain reports in its thread. */
export function WorkEventCard(props: { item: () => BrainWorkEventTimelineItem }) {
  const event = () => props.item().event;
  return (
    <Button onPress={() => props.item().onPress?.()} class="mx-4 my-1 gap-1 rounded-xl border border-border bg-card px-3 py-2.5 active:bg-accent">
      <View class="flex-row items-center gap-2">
        <Icon name={event().kind === "session.failed" ? "alert-circle" : event().kind === "session.needs_input" ? "hand" : "check-circle"}
          color={event().kind === "session.failed" ? "destructive" : event().kind === "session.needs_input" ? "seal" : "muted-foreground"} size={16} />
        <Text class="flex-1 text-xs font-semibold uppercase text-muted-foreground">{WORK_KIND[event().kind] ?? "Work"}</Text>
        {(props.item().sourceCount ?? 1) > 1 ? <Text class="text-xs text-muted-foreground">{`${props.item().sourceCount} updates`}</Text> : null}
      </View>
      <Text class="text-base font-semibold">{event().work_title}</Text>
      {event().summary ? <Text class="text-sm text-muted-foreground line-clamp-3">{event().summary}</Text> : null}
    </Button>
  );
}
