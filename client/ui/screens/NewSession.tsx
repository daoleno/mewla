import { effect, Scroll, signal, Text, untracked, View } from "nsh";
import { Button, Input } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import { LAUNCH_PRESETS } from "../../core/sessions/agentCommands";
import { createSession } from "../../core/sessions/create";
import { resolveLaunchSelection } from "../../core/session/providerLaunch";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../icons/Icon";
import { canCreateSession, connection, currentTransport, currentWorkerSessions, workers } from "../session";

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
  return (
    <View class="gap-2" style={{ height: 420 }}>
      <View class="flex-row items-center gap-2">
        <Button testID="dir-up" onPress={() => void load(parentDirectoryPath(path()))} class="rounded-lg px-2 py-1.5 active:bg-accent"><Icon name="arrow-up" size={18} color="foreground" /></Button>
        <Text class="flex-1 font-mono text-sm line-clamp-1">{path() || "Home"}</Text>
      </View>
      {problem() ? <Text class="text-sm text-destructive">{problem()}</Text> : null}
      <View class="flex-1 rounded-xl border border-border">
        {loading() && entries().length === 0 ? <Text class="p-3 text-sm text-muted-foreground">Loading…</Text> : null}
        {!loading() && entries().length === 0 && !problem() ? <Text class="p-3 text-sm text-muted-foreground">No folders here.</Text> : null}
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1">
          {entries().map((entry) => (
            <Button testID="dir-entry" onPress={() => void load(entry.path)} class="flex-row items-center gap-2 px-3 py-2 active:bg-accent">
              <Icon name="folder" size={16} />
              <Text class="flex-1 text-sm line-clamp-1">{entry.name}</Text>
            </Button>
          ))}
        </Scroll>
      </View>
      <View class="flex-row justify-end gap-4">
        <Button onPress={props.onCancel}><Text class="text-base text-muted-foreground">Cancel</Text></Button>
        <Button testID="dir-select" onPress={() => props.onPick(path())} disabled={!path()}><Text class="text-base font-semibold text-primary">Select This Folder</Text></Button>
      </View>
    </View>
  );
}

/**
 * New session: a tap on an agent starts it in the folder shown (the first
 * Session's by default); Advanced sets the command and the window title.
 * Opens the new Session once the computer has made it.
 */
export function NewSession(props: { onCreated: (workerId: string, fromSession: boolean, hint: TerminalRouteSessionHint) => void }) {
  const cwd = signal("");
  const command = signal("");
  const name = signal("");
  const advanced = signal(false);
  const picking = signal(false);
  const busy = signal(false);
  const problem = signal("");

  const reset = () => {
    // From a Session, its folder (the Expo app's New Terminal did so; parity #40); an empty one means that pane's current folder.
    const target = untracked(newSessionTarget);
    const list = untracked(workers);
    cwd.set((target ? list.find((w) => w.id === target)?.cwd : list.find((w) => w.cwd)?.cwd) ?? "");
    command.set("");
    name.set("");
    advanced.set(false);
    picking.set(false);
    problem.set("");
  };
  // Each opening starts fresh.
  effect(() => {
    if (newSessionOpen()) untracked(reset);
    else newSessionTarget.set("");
  });

  const launch = async (launchCommand: string, launchName: string) => {
    const transport = currentTransport();
    const store = currentWorkerSessions();
    if (!transport || !store || busy()) return;
    if (!canCreateSession()) return problem.set("Refresh required: the last new session's outcome is not known yet. Wait for the list to update.");
    busy.set(true);
    problem.set("");
    const startedAt = Date.now();
    const targetId = untracked(newSessionTarget);
    // A New Terminal carries its agent's Provider connection, as the Expo app's did (parity #39).
    const selection = targetId ? await resolveLaunchSelection(transport, launchCommand) : null;
    if (untracked(currentTransport) !== transport) return busy.set(false);
    const result = await createSession(transport, {
      cwd: cwd().trim(), command: launchCommand.trim(), name: launchName.trim(), targetId: targetId || undefined,
      connectionId: selection?.connectionId, modelId: selection?.modelId,
    });
    busy.set(false);
    if (result.workerSession) store.added(result.workerSession);
    if (result.kind === "ambiguous") {
      store.blockCreatesUntilList();
      return problem.set(result.message);
    }
    if (result.kind === "failed") return problem.set(result.message);
    newSessionOpen.set(false);
    props.onCreated(result.workerId, Boolean(targetId), { cwd: cwd().trim() || undefined, command: launchCommand.trim() || undefined, name: launchName.trim() || undefined, startedAt, focusComposer: true, ...(result.durabilityWarning ? { durabilityWarning: result.durabilityWarning } : {}) });
  };

  return (
    <AppSheet testID="new-session-sheet" open={newSessionOpen()} onOpenChange={({ open }) => newSessionOpen.set(open)} closeOnSwipe popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View class="gap-3 pb-8">
        <View class="flex-row items-center">
          <Text class="flex-1 text-lg font-semibold">{newSessionTarget() ? "New terminal" : "New session"}</Text>
          {busy() ? <Text class="text-sm text-muted-foreground">Starting…</Text> : null}
        </View>
        {picking() ? (
          <DirectoryPicker start={cwd()} onCancel={() => picking.set(false)} onPick={(path) => {
            cwd.set(path);
            picking.set(false);
          }} />
        ) : (
          <View class="gap-3">
            <View class="gap-1">
              <Text class="text-xs font-semibold uppercase text-muted-foreground">Working directory</Text>
              <View class="flex-row items-center gap-2">
                <Input testID="new-cwd" value={cwd()} placeholder={newSessionTarget() ? "This Session's folder" : "Home"} onChange={({ value }) => cwd.set(value)}
                  class="flex-1 rounded-xl border border-border bg-card px-3 py-2 font-mono text-sm" />
                <Button testID="new-pick-dir" onPress={() => picking.set(true)} disabled={connection() !== "connected"} class="rounded-xl border border-border px-3 py-2 active:bg-accent">
                  <Icon name="folder-open" size={18} color="foreground" />
                </Button>
              </View>
            </View>
            {!advanced() ? (
              <View class="flex-row flex-wrap gap-2">
                {LAUNCH_PRESETS.map((preset) => (
                  <Button testID={`preset-${preset.key}`} onPress={() => void launch(preset.command, "")} disabled={busy() || connection() !== "connected"}
                    class="w-[23%] items-center gap-1 rounded-xl border border-border py-2.5 active:bg-accent">
                    <AgentIcon kind={preset.kind} size={22} />
                    <Text class="text-xs">{preset.label}</Text>
                  </Button>
                ))}
              </View>
            ) : (
              <View class="gap-2">
                <Text class="text-xs font-semibold uppercase text-muted-foreground">Command</Text>
                <Input testID="new-command" value={command()} placeholder="Login shell" onChange={({ value }) => command.set(value)}
                  class="rounded-xl border border-border bg-card px-3 py-2 font-mono text-sm" />
                <View class="flex-row flex-wrap gap-1.5">
                  {LAUNCH_PRESETS.filter((p) => p.command).map((preset) => (
                    <Button onPress={() => command.set(preset.command)} class="rounded-full border border-border px-2.5 py-1 active:bg-accent"><Text class="text-xs">{preset.label}</Text></Button>
                  ))}
                </View>
                <Text class="text-xs font-semibold uppercase text-muted-foreground">Window title</Text>
                <Input testID="new-name" value={name()} placeholder="Optional" onChange={({ value }) => name.set(value)}
                  class="rounded-xl border border-border bg-card px-3 py-2 text-sm" />
                <Button testID="new-launch" onPress={() => void launch(command(), name())} disabled={busy() || connection() !== "connected"} class="items-center rounded-full bg-primary py-2.5 active:opacity-80">
                  <Text class="text-base font-semibold text-primary-foreground">Launch</Text>
                </Button>
              </View>
            )}
            {problem() ? <Text testID="new-problem" class="text-sm text-destructive">{problem()}</Text> : null}
            <Button testID="new-advanced" onPress={() => advanced.set(!advanced())} class="self-start py-1">
              <Text class="text-sm text-primary">{advanced() ? "Back to agents" : "Advanced"}</Text>
            </Button>
          </View>
        )}
      </View>
    </AppSheet>
  );
}
