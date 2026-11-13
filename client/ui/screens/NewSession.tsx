import { computed, effect, keyboardFrame, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Delayed, Rows } from "../kit/wait";
import { Button } from "nsh/base";
import { Sheet, SheetRoom } from "../kit/Sheet";
import { agentGridColumns, composing, scrollBesidePinned, taskCaptionShows } from "../../core/shell/sheet";
import { Field, PillButton, Row, Section } from "../kit/kit";
import { LAUNCH_PRESETS } from "../../core/sessions/agentCommands";
import { createSession } from "../../core/sessions/create";
import { resolveLaunchSelection } from "../../core/session/providerLaunch";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../kit/Icon";
import { canCreateSession, connection, current, currentTransport, currentWorkerSessions, workers } from "../session";
import { startedWithFirstMessage } from "./firstMessage";
import { sessionRowModel } from "../../core/sessions/listRow";
import { projectName } from "../../core/work/surface";
import type { Worker } from "../../core/sessions/workers";

/** Open from the Sessions list (its + and its empty state). */
export const newSessionOpen = signal(false);
/** Set by a Session's "New Terminal": the new window joins that Session's tmux session, and its folder by default. */
export const newSessionTarget = signal("");

type Entry = { name: string; path: string };

/** "/a/b" → "/a"; the root stays. */
export function parentDirectoryPath(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed || trimmed === "/") return "/";
  return trimmed.replace(/\/[^/]+$/, "") || "/";
}

/**
 * The folder picker inside the sheet: the computer lists a folder's folders
 * (`list_dir`); a newer listing makes an older reply moot.
 */
function DirectoryPicker(props: { start: string; onPick: (path: string) => void; onCancel: () => void }) {
  const path = signal(props.start);
  const entries = signal<Entry[]>([]);
  const loading = signal(false);
  const problem = signal("");
  let epoch = 0;
  const load = async (cwd: string) => {
    const transport = currentTransport();
    if (!transport) return;
    const mine = ++epoch;
    loading.set(true);
    problem.set("");
    try {
      const reply = await transport.request("list_dir", { cwd }, { reply: "dir_list", timeoutMs: 10_000 });
      if (mine !== epoch) return;
      path.set(String(reply.path ?? cwd));
      entries.set(Array.isArray(reply.entries) ? (reply.entries as Entry[]).filter((e) => e && typeof e.path === "string") : []);
    } catch (error) {
      if (mine === epoch) problem.set(error instanceof Error ? error.message : "Could not list this folder.");
    } finally {
      if (mine === epoch) loading.set(false);
    }
  };
  void load(props.start);
  // 440, or what the sheet leaves on a short window (a phone held sideways).
  const room = SheetRoom();
  return (
    <View class="gap-3 px-4" style={{ height: Math.min(440, room()) }}>
      <View class="min-h-[44px] flex-row items-center gap-2">
        <Button testID="dir-up" accessibilityLabel="Up a folder" onPress={() => void load(parentDirectoryPath(path()))} class="size-11 items-center justify-center rounded-full active:bg-accent"><Icon name="arrow-up" size={20} tone="ink" /></Button>
        <Text class="min-w-0 flex-1 font-mono text-code line-clamp-1">{path() || "Home"}</Text>
      </View>
      <If when={problem()}><Text class="text-sub text-muted-foreground">{problem()}</Text></If>
      <View class="flex-1 overflow-hidden rounded-(--radius-group) bg-sheet-surface">
        {/* SPEC §16.4: placeholder rows from 150 ms, no words; "No folders here" only after a read. */}
        <If when={loading() && entries().length === 0}><Delayed><View class="px-4"><Rows count={4} lead={false} /></View></Delayed></If>
        <If when={!loading() && entries().length === 0 && !problem()}><View class="px-4 py-4"><Text class="text-sub text-muted-foreground">No folders here</Text></View></If>
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1">
          {entries().map((entry) => (
            <Button testID="dir-entry" onPress={() => void load(entry.path)} class="min-h-[52px] flex-row items-center gap-3 px-4 active:bg-accent">
              <Icon name="folder" size={20} tone="soft" />
              <Text class="min-w-0 flex-1 text-body line-clamp-1">{entry.name}</Text>
              <Icon name="chevron-right" size={18} tone="faint" />
            </Button>
          ))}
        </Scroll>
      </View>
      <View class="flex-row gap-2">
        <View class="flex-1"><PillButton title="Cancel" small onPress={props.onCancel} /></View>
        <View class="flex-[2]"><PillButton testID="dir-select" title="Use this folder" small filled disabled={!path()} onPress={() => props.onPick(path())} /></View>
      </View>
    </View>
  );
}

/** The body's own foot (`pb-2`). */
const BODY_FOOT = 8;

/**
 * The scroll's height in the room the sheet leaves (core/shell/sheet.ts `scrollBesidePinned`): 520 at most, what is
 * left beside the fields and Start pinned under it (`pinned`, as laid out). Made under the sheet, so it reads its room.
 */
function Fitted(props: { pinned: () => number; children: (scroll: () => number) => JSX.Element }) {
  const room = SheetRoom();
  const scroll = computed(() => scrollBesidePinned(room() - BODY_FOOT, props.pinned()));
  return <>{props.children(scroll)}</>;
}

/** How long a blurred field keeps composing, so a tap on the inline Start that blurs it first still lands. */
const BLUR_SETTLE_MS = 300;

/** The agent grid's columns, as wide as it is laid out (core/shell/sheet.ts `agentGridColumns`). */
const gridWidth = signal(0);

/** A folder the sheet offers: a recent project, or the one picked or inherited. */
export type FolderChoice = { path: string; name: string };

/** How many recent projects the sheet lists before "Other folder…". */
const RECENT_FOLDERS = 5;

/** The newest Sessions' folders, newest first, each once (SPEC §11 New session sheet: recent projects by name). */
export function recentFolders(list: readonly Worker[]): FolderChoice[] {
  const seen = new Set<string>();
  const out: FolderChoice[] = [];
  for (const worker of [...list].sort((a, b) => (b.started_at ?? 0) - (a.started_at ?? 0))) {
    const path = worker.cwd?.trim();
    if (!path || seen.has(path)) continue;
    seen.add(path);
    out.push({ path, name: projectName(path) || path });
    if (out.length === RECENT_FOLDERS) break;
  }
  return out;
}

/** The agent of the newest Session, as a preset; Claude when there is none to go by. */
export function lastUsedPreset(list: readonly Worker[]): string {
  const newest = [...list].sort((a, b) => (b.started_at ?? 0) - (a.started_at ?? 0))[0];
  const kind = newest ? sessionRowModel(newest).kind : undefined;
  return LAUNCH_PRESETS.find((preset) => preset.kind === kind)?.key ?? "claude";
}

/**
 * New session (SPEC §11 New session sheet): the agent (the last used one chosen), the folder (recent projects,
 * the last used one chosen; "Other folder…" browses), then Start. A tile only chooses; Start starts, and the new
 * Session opens once the computer has made it. Custom command replaces the grid with the command and a window title.
 */
export function NewSession(props: { onCreated: (workerId: string, fromSession: boolean, hint: TerminalRouteSessionHint) => void }) {
  const cwd = signal("");
  const preset = signal("claude");
  const command = signal("");
  const name = signal("");
  // SPEC §11 New session sheet, Task: "What should it do?", optional; the Session opens with it as its first message.
  const task = signal("");
  const advanced = signal(false);
  const picking = signal(false);
  const busy = signal(false);
  const problem = signal("");
  /** A folder picked with "Other folder…" or inherited from a Session, listed first when it is not a recent one. */
  const extra = signal<FolderChoice | null>(null);

  const reset = () => {
    // From a Session, its folder (the Expo app's New Terminal did so; parity #40); an empty one means that pane's current folder.
    const target = untracked(newSessionTarget);
    const list = untracked(workers);
    const recent = recentFolders(list);
    const inherited = target ? list.find((w) => w.id === target)?.cwd?.trim() ?? "" : recent[0]?.path ?? "";
    cwd.set(inherited);
    extra.set(recent.some((folder) => folder.path === inherited) ? null : { path: inherited, name: inherited ? projectName(inherited) || inherited : target ? "This Session's folder" : "Home" });
    preset.set(lastUsedPreset(list));
    command.set("");
    name.set("");
    task.set("");
    advanced.set(false);
    picking.set(false);
    problem.set("");
  };
  // Each opening starts fresh.
  effect(() => {
    if (newSessionOpen()) untracked(reset);
    else newSessionTarget.set("");
  });

  const folders = (): FolderChoice[] => {
    const recent = recentFolders(workers());
    const more = extra();
    return more && !recent.some((folder) => folder.path === more.path) ? [more, ...recent] : recent;
  };

  // A first message needs an agent: a shell or a custom command takes none (the computer refuses one for a shell).
  const takesTask = () => !advanced() && preset() !== "shell";
  const launch = async (launchCommand: string, launchName: string, firstMessage = "") => {
    const transport = currentTransport();
    const store = currentWorkerSessions();
    if (!transport || !store || busy()) return;
    if (!canCreateSession()) return problem.set("Refresh required: the last new session's outcome is not known yet. Wait for the list to update.");
    busy.set(true);
    problem.set("");
    const startedAt = Date.now();
    const targetId = untracked(newSessionTarget);
    const serverId = untracked(current)?.id;
    // A New Terminal carries its agent's Provider connection, as the Expo app's did (parity #39).
    const selection = targetId ? await resolveLaunchSelection(transport, launchCommand) : null;
    if (untracked(currentTransport) !== transport) return busy.set(false);
    const result = await createSession(transport, {
      cwd: cwd().trim(), command: launchCommand.trim(), name: launchName.trim(), targetId: targetId || undefined,
      connectionId: selection?.connectionId, modelId: selection?.modelId, text: firstMessage,
    });
    busy.set(false);
    if (result.workerSession) store.added(result.workerSession);
    if (result.kind === "ambiguous") {
      store.blockCreatesUntilList();
      return problem.set(result.message);
    }
    if (result.kind === "failed") return problem.set(result.message);
    newSessionOpen.set(false);
    if (serverId) startedWithFirstMessage(serverId, result.workerId, firstMessage.trim(), result.firstMessage === "pending");
    props.onCreated(result.workerId, Boolean(targetId), { cwd: cwd().trim() || undefined, command: launchCommand.trim() || undefined, name: launchName.trim() || undefined, startedAt, focusComposer: true, ...(result.durabilityWarning ? { durabilityWarning: result.durabilityWarning } : {}) });
  };
  const start = () => {
    if (advanced()) return void launch(command(), name());
    const chosen = LAUNCH_PRESETS.find((p) => p.key === preset());
    if (chosen) void launch(chosen.command, "", takesTask() ? task() : "");
  };

  const offline = () => connection() !== "connected";
  const canStart = () => !offline() && (advanced() || Boolean(preset()));
  // The field that has the keyboard: while it is up, the sheet is that field and Start (core/shell/sheet.ts `composing`).
  const focused = signal<"task" | "command" | "name" | null>(null);
  const compose = () => composing(keyboardFrame().height, focused());
  const focus = (field: "task" | "command" | "name") => ({
    onFocus: () => focused.set(field),
    // A tap on the inline Start may blur the field first: clear a little later, so that tap still lands on Start.
    onBlur: () => setTimeout(() => untracked(focused) === field && focused.set(null), BLUR_SETTLE_MS),
  });
  /** Start in the focused field's row while composing: the field and Start show together over the keyboard. */
  const inlineStart = (field: "task" | "command" | "name") => (
    <If when={compose() && focused() === field}>
      <Button testID="new-launch-inline" accessibilityLabel="Start" disabled={!canStart() || busy()} onPress={start}
        class="h-[36px] items-center justify-center rounded-full bg-primary px-4 active:opacity-70 disabled:opacity-40">
        <Text class="text-sub font-semibold text-primary-foreground">Start</Text>
      </Button>
    </If>
  );
  /** Hidden while another field composes; the node stays, so the focused one keeps its focus and its keyboard. */
  const shownWhile = (field: "task" | "command" | "name") => ({ display: compose() && focused() !== field ? ("none" as const) : ("flex" as const) });
  const pinnedHeight = signal(0);
  return (
    <Sheet testID="new-session-sheet" open={newSessionOpen} onClose={() => newSessionOpen.set(false)}
      title={() => (picking() ? "Choose a folder" : newSessionTarget() ? "New terminal" : "New session")}>
      <If when={picking()} else={<Fitted pinned={pinnedHeight}>{(scroll) => <View class="px-4 pb-2">
          {/* Agent and Folder scroll; the fields and Start stay in view under them. While a field has the keyboard
              they step aside, and the sheet is that field with Start in its row. */}
          <View class="flex-col" style={{ display: compose() ? "none" : "flex" }}>
            <Scroll testID="new-scroll" keyboardShouldPersistTaps="handled" style={{ maxHeight: scroll() }}>
              <If when={!advanced()} else={<Section sheet title="Command">
                  <View class="flex-row flex-wrap gap-2 px-4 py-3">
                    {LAUNCH_PRESETS.filter((p) => p.command).map((item) => (
                      <Button onPress={() => command.set(item.command)} class="h-9 justify-center rounded-full bg-background px-3 active:opacity-70"><Text class="text-sub">{item.label}</Text></Button>
                    ))}
                  </View>
                  <Row testID="new-advanced" icon="chevron-left" title="Back to agents" onPress={() => advanced.set(false)} />
                </Section>}><Section sheet title="Agent">
                  {/* SPEC §15 Picker grid: three across on a phone (more on a wide sheet), the art and a 15/600 name; a tap chooses, the ink ring marks it. */}
                  <View class="flex-row flex-wrap px-2 py-2" onLayout={({ bounds }) => gridWidth.set(bounds.size.width - 16)}>
                    {LAUNCH_PRESETS.map((item) => (
                      <View class="p-1" style={{ width: `${100 / agentGridColumns(gridWidth(), LAUNCH_PRESETS.length)}%` }}>
                        <Button testID={`preset-${item.key}`} accessibilityLabel={preset() === item.key ? `${item.label}, chosen` : item.label} onPress={() => preset.set(item.key)} disabled={busy()}
                          class={`min-h-[92px] items-center justify-center gap-2 rounded-(--radius-group) border-2 bg-background px-2 py-3 active:opacity-70 disabled:opacity-40 ${preset() === item.key ? "border-foreground" : "border-transparent"}`}>
                          <AgentIcon kind={item.kind} size={32} />
                          <Text class="text-sub font-semibold text-foreground line-clamp-1">{item.label}</Text>
                        </Button>
                      </View>
                    ))}
                  </View>
                  <Row testID="new-advanced" icon="terminal" title="Custom command" chevron onPress={() => advanced.set(true)} />
                </Section></If>
              <Section sheet title="Folder" testID="new-cwd">
                {folders().map((folder) => (
                  <Button testID="new-folder" accessibilityLabel={`${folder.path || folder.name}${cwd() === folder.path ? ", chosen" : ""}`} onPress={() => cwd.set(folder.path)}
                    class="min-h-[56px] flex-row items-center gap-3 px-4 py-2 active:bg-accent">
                    <Icon name="folder" size={24} tone="soft" />
                    <View class="min-w-0 flex-1">
                      <Text class="text-body line-clamp-1">{folder.name}</Text>
                      <If when={folder.path}>{(pathValue) => <Text testID="new-folder-path" class="text-caption text-muted-foreground line-clamp-1">{pathValue()}</Text>}</If>
                    </View>
                    <If when={cwd() === folder.path}><Icon name="check" size={20} tone="ink" /></If>
                    <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: 52 }} />
                  </Button>
                ))}
                <Row testID="new-pick-dir" icon="folder-open" title="Other folder…" chevron disabled={offline()} onPress={() => picking.set(true)} />
              </Section>
            </Scroll>
          </View>
          {/* The fields in no scroll: NSH keeps a focused field that is in none clear of the keyboard once the sheet
              has risen (a scroll holding it took a keyboard inset before the sheet rose, and ended in blank, 1c). */}
          <View testID="new-pinned" onLayout={({ bounds }) => pinnedHeight.set(bounds.size.height)}>
            <If when={advanced()} else={<If when={takesTask()}><View testID="new-task-group" class="mt-2 rounded-(--radius-group) bg-sheet-surface">
                  <Field testID="new-task" label={taskCaptionShows(windowSize().height, compose()) ? "Task" : undefined} accessibilityLabel="What should it do?" multiline value={task()} placeholder="What should it do?" onChange={(value) => task.set(value)}
                    {...focus("task")} trailing={inlineStart("task")} />
                </View></If>}><View class="mt-2 rounded-(--radius-group) bg-sheet-surface">
                <View class="flex-col" style={shownWhile("command")}>
                  <Field testID="new-command" label="Command" mono value={command()} placeholder="Login shell" onChange={(value) => command.set(value)}
                    {...focus("command")} trailing={inlineStart("command")} />
                </View>
                <View class="flex-col" style={shownWhile("name")}>
                  <Field testID="new-name" label="Window title" value={name()} placeholder="Optional" onChange={(value) => name.set(value)}
                    {...focus("name")} trailing={inlineStart("name")} />
                </View>
              </View></If>
            <If when={problem()}><View class="mt-3 px-4"><Text testID="new-problem" class="text-sub text-muted-foreground">{problem()}</Text></View></If>
            <View class="mt-2 flex-col" style={{ display: compose() ? "none" : "flex" }}>
              <PillButton testID="new-launch" title="Start" filled busy={busy()} disabled={!canStart()} onPress={start} />
            </View>
          </View>
        </View>}</Fitted>}><DirectoryPicker start={cwd()} onCancel={() => picking.set(false)} onPick={(path) => {
          cwd.set(path);
          extra.set({ path, name: projectName(path) || path });
          picking.set(false);
        }} /></If>
    </Sheet>
  );
}
