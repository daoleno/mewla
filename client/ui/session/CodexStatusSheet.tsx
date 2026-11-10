import { effect, Scroll, signal, Text, untracked, View } from "nsh";
import { Button, Sheet } from "nsh/base";
import type { Daemon } from "../../core/more/daemon";
import { codexStatusSections, type CodexStatus } from "../../core/session/codexStatus";
import { CODEX_STATUS_UNAVAILABLE, codexStatus } from "../../core/session/sessionRequests";
import { DateStyle, formatDate } from "../../core/timeline/dates";
import { Icon } from "../icons/Icon";

type StatusState =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "ready"; status: CodexStatus; fetchedAt: Date }
  | { phase: "failed"; message: string; unavailable: boolean };

/**
 * Codex's /status for the chat, read from the Codex Mewla started: usage
 * meters ("% left · resets"), then the Session, Account and Tokens rows.
 * Refresh reads it again. When Mewla did not start this chat's Codex there is
 * none to read; `onOpenTerminal` offers the terminal, where `/status` works.
 * Each opening reads it again.
 */
export function CodexStatusSheet(props: {
  open: () => boolean;
  onClose: () => void;
  transport: Daemon;
  workerId: string;
  onOpenTerminal?: () => void;
}) {
  const state = signal<StatusState>({ phase: "idle" });
  let generation = 0;

  const load = () => {
    const mine = ++generation;
    state.set({ phase: "loading" });
    codexStatus(props.transport, props.workerId).then(
      (status) => {
        if (generation === mine) state.set({ phase: "ready", status, fetchedAt: new Date() });
      },
      (error: { message?: string; code?: string }) => {
        if (generation === mine) state.set({ phase: "failed", message: error?.message || "Failed to load Codex status.", unavailable: error?.code === CODEX_STATUS_UNAVAILABLE });
      },
    );
  };

  effect(() => {
    if (props.open()) untracked(load);
    else generation++;
  });

  const ready = () => {
    const s = state();
    return s.phase === "ready" ? s : null;
  };
  const failed = () => {
    const s = state();
    return s.phase === "failed" ? s : null;
  };

  return (
    <Sheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View testID="status-sheet" class="gap-3 pb-8">
        <View class="flex-row items-center gap-2">
          <View class="flex-1">
            <Text class="text-lg font-semibold">Status</Text>
            <Text class="text-xs text-muted-foreground">{ready() ? `Updated ${formatDate(ready()!.fetchedAt, DateStyle.time)}` : "Codex"}</Text>
          </View>
          {ready() ? (
            <Button testID="status-refresh" accessibilityLabel="Refresh status" onPress={load} class="rounded-full p-1.5 active:bg-accent">
              <Icon name="refresh" size={20} color="foreground" />
            </Button>
          ) : null}
          <Button testID="status-close" accessibilityLabel="Close status" onPress={props.onClose} class="rounded-full p-1.5 active:bg-accent">
            <Icon name="close" size={20} color="foreground" />
          </Button>
        </View>

        {ready() ? (
          <Scroll style={{ maxHeight: 470 }}>
            <View class="gap-4">
              {codexStatusSections(ready()!.status).length === 0 ? (
                <Text class="py-6 text-center text-sm text-muted-foreground">Codex reported nothing for this chat yet.</Text>
              ) : null}
              {codexStatusSections(ready()!.status).map((section) => (
                <View testID={`status-section-${section.key}`} class="gap-1.5">
                  <Text class="text-xs font-semibold uppercase text-muted-foreground">{section.title}</Text>
                  <View class="overflow-hidden rounded-xl border border-border bg-card">
                    {(section.meters ?? []).map((meter) => (
                      <View testID="status-meter" class="flex-row items-center gap-3 border-b border-border px-3 py-2.5">
                        <View class="flex-1 gap-0.5">
                          <Text class="text-base">{meter.label}</Text>
                          <Text class="text-xs text-muted-foreground">{`${Math.round(100 - meter.usedPercent)}% left${meter.resets ? ` · ${meter.resets}` : ""}`}</Text>
                        </View>
                        <View class="h-1.5 w-[72px] overflow-hidden rounded-full bg-border">
                          <View class={meter.usedPercent >= 90 ? "h-1.5 rounded-full bg-destructive" : "h-1.5 rounded-full bg-primary"}
                            style={{ width: `${Math.max(0, 100 - meter.usedPercent)}%` }} />
                        </View>
                      </View>
                    ))}
                    {section.rows.map((row) => (
                      <View class="gap-0.5 border-b border-border px-3 py-2.5">
                        <Text class="text-sm text-muted-foreground">{row.label}</Text>
                        <Text class="text-base">{row.value}</Text>
                      </View>
                    ))}
                  </View>
                </View>
              ))}
            </View>
          </Scroll>
        ) : failed() ? (
          <View testID="status-failed" class="min-h-[190px] items-center justify-center gap-2 px-4">
            <Text class={failed()!.unavailable ? "text-base font-semibold" : "text-base font-semibold text-destructive"}>
              {failed()!.unavailable ? "No status for this chat" : "Couldn't load status"}
            </Text>
            <Text class="text-center text-sm text-muted-foreground">
              {failed()!.unavailable ? "Mewla reads status from the Codex it started. For this chat, type /status in the terminal." : failed()!.message}
            </Text>
            <View class="flex-row gap-3 pt-1">
              {!failed()!.unavailable ? (
                <Button testID="status-retry" onPress={load} class="flex-row items-center gap-1.5 rounded-full bg-primary px-4 py-2 active:opacity-80">
                  <Icon name="refresh" size={16} color="primary-foreground" />
                  <Text class="text-sm font-semibold text-primary-foreground">Retry</Text>
                </Button>
              ) : null}
              {props.onOpenTerminal ? (
                <Button testID="status-terminal" onPress={() => props.onOpenTerminal?.()} class="flex-row items-center gap-1.5 rounded-full border border-border px-4 py-2 active:bg-accent">
                  <Icon name="terminal" size={16} color="foreground" />
                  <Text class="text-sm font-semibold">Terminal</Text>
                </Button>
              ) : null}
            </View>
          </View>
        ) : (
          <View class="min-h-[190px] items-center justify-center">
            <Text class="text-sm text-muted-foreground">Asking Codex…</Text>
          </View>
        )}
      </View>
    </Sheet>
  );
}
