import { effect, Scroll, signal, Text, View } from "nsh";
import { AlertDialog, Button, Input, TextView } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import { calendarOriginOfWork, originLabel } from "../../core/calendar/model";
import { readableBody, WorkItemConflict, workItemStatus, workItemStatusLabel, type WorkItem } from "../../core/work/workItems";
import { clock } from "../calendar/clock";
import { calendarItems } from "../calendar/state";
import { workFiles, workFilesModel } from "./state";

const AUTOSAVE_MS = 600;

/**
 * A Markdown Work file (the Work a Calendar run wrote): read it, edit it (saved as you
 * stop typing), mark it done or reopen it, delete it. An edit is saved over the version
 * it started from; if the computer changed the file meanwhile, it says so and keeps your
 * text until you load theirs.
 */
export function WorkFilePage(props: { id: string; onClose: () => void; openCalendarItem: (itemId: string, runId?: string) => void }) {
  const item = () => workFiles().find((w) => w.id === props.id);
  const editingBody = signal(false);
  const text = signal("");
  /** The version the text was loaded from. */
  const base = signal<WorkItem | null>(null);
  const state = signal<"saved" | "unsaved" | "saving">("saved");
  const newer = signal(false);
  const failure = signal("");
  let timer: ReturnType<typeof setTimeout> | null = null;

  effect(() => {
    const current = item();
    if (!current) return;
    const loaded = base();
    if (!loaded) {
      base.set(current);
      text.set(current.body);
    } else if (current.mtime !== loaded.mtime && state() === "saved") {
      base.set(current);
      text.set(current.body);
    } else if (current.mtime !== loaded.mtime) newer.set(true);
  });

  const save = async () => {
    const model = workFilesModel();
    const from = base();
    // A newer version on the computer waits for the person: no save over it.
    if (newer()) return;
    if (!model || !from || text() === from.body) return state.set("saved");
    state.set("saving");
    try {
      const saved = await model.save(from, { body: text() });
      base.set(saved);
      state.set(text() === saved.body ? "saved" : "unsaved");
      failure.set("");
    } catch (error) {
      state.set("unsaved");
      if (error instanceof WorkItemConflict) newer.set(true);
      else failure.set(`Save failed. ${error instanceof Error && error.message ? error.message : "Could not save work item."}`);
    }
  };
  const edited = (value: string) => {
    text.set(value);
    state.set("unsaved");
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => void save(), AUTOSAVE_MS);
  };
  const loadRemote = () => {
    const current = item();
    if (!current) return;
    base.set(current);
    text.set(current.body);
    state.set("saved");
    newer.set(false);
  };
  const toggleDone = async (current: WorkItem) => {
    try {
      const saved = await workFilesModel()!.setDone(current, workItemStatus(current) !== "done");
      base.set(saved);
    } catch (error) {
      failure.set(`Save failed. ${error instanceof Error && error.message ? error.message : "Could not save work item."}`);
    }
  };
  const remove = async (current: WorkItem) => {
    const answer = await AlertDialog.show({
      title: "Delete work item?", description: "This removes the Markdown work file from the server.",
      actions: [{ id: "keep", label: "Cancel", role: "cancel" }, { id: "delete", label: "Delete", role: "destructive" }],
    });
    if (answer.action !== "delete") return;
    try {
      await workFilesModel()!.remove(current);
      props.onClose();
    } catch (error) {
      failure.set(`Delete failed. ${error instanceof Error && error.message ? error.message : "Could not delete work item."}`);
    }
  };
  const origin = () => (item() ? calendarOriginOfWork(item()!) : null);
  const age = (iso: string) => {
    const ms = clock().now - Date.parse(iso);
    if (!Number.isFinite(ms) || ms < 60_000) return "now";
    const m = Math.floor(ms / 60_000);
    if (m < 60) return `${m}m`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h`;
    const d = Math.floor(h / 24);
    return d < 30 ? `${d}d` : `${Math.floor(d / 30)}mo`;
  };

  return (
    <NavPage title={item()?.project ?? "Work"} testID="work-file-page"
      headerRight={item() ? <Button testID="work-file-edit" onPress={() => { if (editingBody()) void save(); editingBody.set(!editingBody()); }}><Text class="text-base text-primary">{editingBody() ? "Preview" : "Edit"}</Text></Button> : undefined}>
      {!item() ? (
        <View class="flex-1 items-center gap-2 bg-background px-6 py-16">
          <Text class="text-lg font-semibold">Work item not found</Text>
          <Text class="text-center text-base text-muted-foreground">It may have been deleted or belongs to another server.</Text>
        </View>
      ) : (
        <View class="flex-1 bg-background">
          {newer() ? (
            <View testID="work-file-newer" class="gap-2 border-b border-border bg-secondary px-4 py-3">
              <Text class="text-base font-semibold">Newer version on the server</Text>
              {state() !== "saved" ? <Text class="text-sm text-muted-foreground">Loading it discards your unsaved edits.</Text> : null}
              <Button testID="work-file-load" onPress={loadRemote} class="self-start"><Text class="text-base text-primary">Load remote changes</Text></Button>
            </View>
          ) : null}
          <Scroll keyboardShouldPersistTaps="handled" class="flex-1">
            <View class="gap-2 px-4 pb-8 pt-4">
              <Text class="text-sm text-muted-foreground">{`${workItemStatusLabel[workItemStatus(item()!)]} · ${age(item()!.mtime)}`}</Text>
              <Text class="text-xl font-semibold">{item()!.title || "Untitled work"}</Text>
              {origin() ? (
                <Button testID="work-file-calendar-link" onPress={() => props.openCalendarItem(origin()!.itemId, origin()!.runId)} class="self-start">
                  <Text class="text-base text-primary">{originLabel(origin()!, calendarItems(), clock(), { title: item()!.title, at: item()!.frontmatter.created }) ?? "Calendar"}</Text>
                </Button>
              ) : null}
              {editingBody()
                ? <Input testID="work-file-body" multiline value={text()} onChange={({ value }) => edited(value)} onBlur={() => void save()} class="min-h-64 rounded-xl border border-border bg-card p-3 text-base" />
                : <TextView testID="work-file-preview" markdown={readableBody(text())} />}
            </View>
          </Scroll>
          <View class="flex-row items-center justify-between border-t border-border px-4 py-3">
            <Text testID="work-file-state" class="text-sm text-muted-foreground">{failure() || (state() === "saving" ? "Saving" : state() === "unsaved" ? "Unsaved" : "Saved")}</Text>
            <View class="flex-row gap-4">
              <Button testID="work-file-delete" onPress={() => void remove(item()!)}><Text class="text-base text-destructive">Delete</Text></Button>
              <Button testID="work-file-done" onPress={() => void toggleDone(item()!)}><Text class="text-base font-semibold text-primary">{workItemStatus(item()!) === "done" ? "Reopen" : "Mark done"}</Text></Button>
            </View>
          </View>
        </View>
      )}
    </NavPage>
  );
}
