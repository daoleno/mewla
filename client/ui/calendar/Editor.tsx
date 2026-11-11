import { host, platform, Scroll, signal, Text, View, type Signal } from "nsh";
import { Button, Input } from "nsh/base";
import { GuardedDialog as Dialog } from "../shell/AppSheet";
import { draftTimeField, type CalendarDraft } from "../../core/calendar/calendar";
import { kindLabel, type CalendarItem, type CalendarKind, type CalendarRecurrence } from "../../core/calendar/model";
import type { LocalResolution } from "../../core/calendar/zones";
import { brain } from "../session";
import { clock, ensureZones } from "./clock";
import { Choices } from "./parts";
import { calendar } from "./state";

/**
 * New and edited Calendar items, in a sheet: Cancel · New item · Save. Dates and times
 * come from the system's pickers and mean the wall clock in the item's zone; what Save
 * will store is spelled out under them, and a time the clock skips or shows twice is
 * resolved here, not by the computer.
 */
const open = signal(false);
const editing = signal<CalendarItem | null>(null);
const title = signal("");
const kind = signal<CalendarKind>("reminder");
const zone = signal("UTC");
const recurrence = signal<CalendarRecurrence>("none");
const notes = signal("");
const summary = signal("");
const instruction = signal("");
const startDate = signal("");
const startTime = signal("");
const endDate = signal("");
const endTime = signal("");
/** Which of a twice-shown time the person chose (0 the first, 1 the second). */
const occurrence = signal(0);
const failure = signal("");
const saving = signal(false);

/** Another computer: what was being edited was the old one's. */
export function closeEditor(): void {
  open.set(false);
}

export function openEditor(item: CalendarItem | null): void {
  const c = clock();
  const device = c.zones.device;
  editing.set(item);
  failure.set("");
  occurrence.set(0);
  title.set(item?.title ?? "");
  kind.set(item?.kind ?? "reminder");
  zone.set(item?.timezone || device);
  recurrence.set(item?.recurrence || "none");
  notes.set(item?.notes ?? "");
  summary.set(item?.summary ?? "");
  instruction.set(item?.action_instruction ?? "");
  const start = item ? Date.parse(item.start_at || item.notify_at || item.due_at || item.next_at) : Math.ceil((c.now + 3_600_000) / 900_000) * 900_000;
  const end = item?.end_at ? Date.parse(item.end_at) : start + 3_600_000;
  void ensureZones(item?.timezone ? [item.timezone] : []).then(() => {
    // A new item takes the phone's zone, which may only now be known (until then the table says UTC).
    const z = item?.timezone || clock().zones.device;
    if (!item?.timezone && zone() === device) zone.set(z);
    const s = clock().zones.localFields(start, z);
    const e = clock().zones.localFields(end, z);
    startDate.set(s.date);
    startTime.set(s.time);
    endDate.set(e.date);
    endTime.set(e.time);
  });
  open.set(true);
}

function resolve(date: string, time: string): LocalResolution {
  return clock().zones.resolveLocal(date, time, zone());
}

function instantOf(resolution: LocalResolution): number | null {
  if (resolution.status === "resolved") return resolution.instant;
  if (resolution.status === "ambiguous") return resolution.instants[occurrence()];
  return null;
}

/** The instant with its zone's offset then: the computer refuses any other. */
const ISO = (at: number) => clock().zones.iso(at, zone().trim());

async function save(): Promise<void> {
  const model = calendar();
  if (!model || saving()) return;
  failure.set("");
  if (!title().trim()) return failure.set("Title is required.");
  await ensureZones([zone().trim()]);
  const start = resolve(startDate(), startTime());
  const at = instantOf(start);
  if (at === null) return failure.set("message" in start ? start.message : "Choose a time.");
  const draft: CalendarDraft = { title: title().trim(), kind: kind(), timezone: zone().trim(), recurrence: recurrence(), notes: notes().trim() || undefined };
  draft[draftTimeField(kind())] = ISO(at);
  if (kind() === "event") {
    const end = instantOf(resolve(endDate(), endTime()));
    if (end === null || end <= at) return failure.set("Event end must be after its start.");
    draft.end_at = ISO(end);
  }
  if (kind() === "scheduled_action") {
    const thread = editing()?.source_thread_id || brain()?.chat_thread_id;
    if (!thread) return failure.set("Scheduled Work requires an active Brain conversation on this server.");
    if (!instruction().trim()) return failure.set("Tell Mewla what to do.");
    draft.source_thread_id = thread;
    draft.action_instruction = instruction().trim();
    draft.summary = summary().trim() || undefined;
  }
  saving.set(true);
  try {
    const item = editing();
    await (item ? model.update(item, draft) : model.create(draft));
    open.set(false);
  } catch (error) {
    failure.set(error instanceof Error ? error.message : "Failed to save calendar item.");
  } finally {
    saving.set(false);
  }
}

/** The system's date or time picker on the host; typed fields where there is none (the web). */
async function pick(which: "date" | "time", date: Signal<string>, time: Signal<string>): Promise<void> {
  const [y, m, d] = (date() ?? "").split("-").map(Number);
  const [hh, mm] = (time() ?? "").split(":").map(Number);
  if (which === "date") {
    const picked = await host.call<{ year: number; month: number; day: number } | null>("calendar.pickDate", { year: y, month: m, day: d }, { timeout: 0 }).catch(() => null);
    if (picked) date.set(`${String(picked.year).padStart(4, "0")}-${String(picked.month).padStart(2, "0")}-${String(picked.day).padStart(2, "0")}`);
  } else {
    const picked = await host.call<{ hour: number; minute: number } | null>("calendar.pickTime", { hour: hh, minute: mm }, { timeout: 0 }).catch(() => null);
    if (picked) time.set(`${String(picked.hour).padStart(2, "0")}:${String(picked.minute).padStart(2, "0")}`);
  }
}

const native = host.available() && platform.os !== "web";

function shownDate(date: string): string {
  const at = Date.parse(`${date}T12:00:00Z`);
  return Number.isFinite(at) ? clock().format.day(at, "UTC") : date;
}

function shownTime(time: string): string {
  const [h, m] = time.split(":").map(Number);
  if (!Number.isFinite(h) || !Number.isFinite(m)) return time;
  return clock().format.time(Date.UTC(2000, 0, 1, h, m), "UTC");
}

function When(props: { label: string; id: string; date: Signal<string>; time: Signal<string> }) {
  return (
    <View class="flex-row items-center justify-between border-b border-border bg-card px-4 py-2.5">
      <Text class="text-base">{props.label}</Text>
      {native ? (
        <View class="flex-row gap-2">
          <Button testID={`${props.id}-date`} onPress={() => void pick("date", props.date, props.time)} class="rounded-lg bg-secondary px-3 py-1.5"><Text class="text-base">{shownDate(props.date())}</Text></Button>
          <Button testID={`${props.id}-time`} onPress={() => void pick("time", props.date, props.time)} class="rounded-lg bg-secondary px-3 py-1.5"><Text class="text-base">{shownTime(props.time())}</Text></Button>
        </View>
      ) : (
        <View class="flex-row gap-2">
          <Input testID={`${props.id}-date`} value={props.date()} onChange={({ value }) => props.date.set(value)} placeholder="YYYY-MM-DD" class="w-32 rounded-lg border border-border px-2 py-1.5 text-base" />
          <Input testID={`${props.id}-time`} value={props.time()} onChange={({ value }) => props.time.set(value)} placeholder="HH:MM" class="w-20 rounded-lg border border-border px-2 py-1.5 text-base" />
        </View>
      )}
    </View>
  );
}

function Field(props: { label: string; id: string; value: () => string; onChange: (value: string) => void; placeholder?: string; multiline?: boolean; maxLength?: number }) {
  return (
    <View class="gap-1 border-b border-border bg-card px-4 py-2.5">
      <Text class="text-sm text-muted-foreground">{props.label}</Text>
      <Input testID={props.id} value={props.value()} onChange={({ value }) => props.onChange(value)} placeholder={props.placeholder} multiline={props.multiline} maxLength={props.maxLength}
        class={props.multiline ? "min-h-16 text-base" : "text-base"} />
    </View>
  );
}

/** What Save will store, or why it cannot; a time shown twice asks which. */
function Resolved() {
  const resolution = () => {
    zone();
    return clock().zones.knows(zone().trim()) ? resolve(startDate(), startTime()) : ({ status: "invalid", message: "Enter a valid IANA timezone, such as Asia/Shanghai." } as LocalResolution);
  };
  const instant = () => instantOf(resolution());
  const endInstant = () => (clock().zones.knows(zone().trim()) ? instantOf(resolve(endDate(), endTime())) : null);
  return (
    <View class="gap-2 px-4 py-3">
      {resolution().status === "ambiguous" ? (
        <View class="gap-2">
          <Text class="text-sm text-muted-foreground">{(resolution() as { message: string }).message}</Text>
          <Choices testID="occurrence" value={String(occurrence()) as "0" | "1"} onChange={(v) => occurrence.set(Number(v))}
            options={(resolution() as { instants: [number, number] }).instants.map((at, i) => ({ value: String(i) as "0" | "1", label: `${i === 0 ? "First" : "Second"} · ${clock().format.time(at, zone().trim(), true)}` }))} />
        </View>
      ) : null}
      <Text class="text-sm font-semibold text-muted-foreground">Resolved time</Text>
      <Text testID="resolved-time" class={instant() === null ? "text-sm text-destructive" : "text-sm text-muted-foreground"}>
        {instant() === null ? (resolution() as { message?: string }).message ?? "" : clock().format.full(instant()!, zone().trim())}
      </Text>
      {kind() === "event" && instant() !== null && endInstant() !== null ? (
        <Text testID="resolved-end" class="text-sm text-muted-foreground">{`Ends ${clock().format.full(endInstant()!, zone().trim())}`}</Text>
      ) : null}
    </View>
  );
}

const KINDS: CalendarKind[] = ["event", "reminder", "deadline", "scheduled_action"];

export function CalendarEditor() {
  const whenLabel = () => (kind() === "event" ? "Starts" : kind() === "reminder" ? "Notify at" : "Due at");
  return (
    <Dialog testID="calendar-editor" open={open()} presentation="pageSheet" onOpenChange={({ open: next }) => open.set(next)} class="flex-1 bg-background">
      <View class="flex-row items-center justify-between border-b border-border px-4 py-3">
        <Button testID="editor-cancel" onPress={() => open.set(false)}><Text class="text-base text-primary">Cancel</Text></Button>
        <Text class="text-base font-semibold">{editing() ? "Edit item" : "New item"}</Text>
        <Button testID="editor-save" disabled={saving()} onPress={() => void save()}><Text class="text-base font-semibold text-primary">{saving() ? "Saving…" : "Save"}</Text></Button>
      </View>
      <Scroll class="flex-1">
        <View class="pb-10">
          {failure() ? <Text testID="editor-failure" class="px-4 pt-3 text-base text-destructive">{failure()}</Text> : null}
          <View class="mt-3 border-t border-border">
            <Field label="Title" id="editor-title" value={title} onChange={(v) => title.set(v)} placeholder="What is the commitment?" />
          </View>
          {editing()?.owner ? null : (
            <View class="px-4 pb-1 pt-4"><Choices testID="editor-kind" value={kind()} onChange={(v) => kind.set(v)} options={KINDS.map((k) => ({ value: k, label: kindLabel[k] }))} /></View>
          )}
          <View class="mt-3 border-t border-border">
            <When label={whenLabel()} id="editor-start" date={startDate} time={startTime} />
            {kind() === "event" ? <When label="Ends" id="editor-end" date={endDate} time={endTime} /> : null}
            <Field label="Time zone" id="editor-zone" value={zone} onChange={(v) => { zone.set(v); void ensureZones([v.trim()]); }} placeholder="Asia/Shanghai" />
          </View>
          <Resolved />
          <View class="px-4 pb-1 pt-2">
            <Text class="pb-2 text-sm text-muted-foreground">Repeats</Text>
            <Choices testID="editor-repeat" value={recurrence()} onChange={(v) => recurrence.set(v)}
              options={[{ value: "none", label: "None" }, { value: "daily", label: "Daily" }, { value: "weekly", label: "Weekly" }, { value: "weekdays", label: "Weekdays" }]} />
          </View>
          <View class="mt-4 border-t border-border">
            <Field label="Notes" id="editor-notes" value={notes} onChange={(v) => notes.set(v)} multiline placeholder="Optional context" />
            {kind() === "scheduled_action" ? <Field label="Summary" id="editor-summary" value={summary} onChange={(v) => summary.set(v)} maxLength={120} placeholder="What it does, in one line" /> : null}
            {kind() === "scheduled_action" ? <Field label="Action instruction" id="editor-instruction" value={instruction} onChange={(v) => instruction.set(v)} multiline placeholder="What should Mewla do when the time comes?" /> : null}
          </View>
        </View>
      </Scroll>
    </Dialog>
  );
}
