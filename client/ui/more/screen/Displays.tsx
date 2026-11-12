import { cleanup, computed, effect, Scroll, signal, Text, untracked, View } from "nsh";
import type { ScrollRef } from "nsh";
import { Button, Menu, MenuItem } from "nsh/base";
import { displayIssue, type DisplayIssue } from "../../../core/screen/displayIssue";
import { displayRoute } from "../../../core/screen/displayRoute";
import { haltDisplay, listDisplays, resumeDisplay, type AgentDisplay } from "../../../core/screen/screen";
import type { StoredServer } from "../../../core/storedServerContract";
import { workers } from "../../session";
import { EmptyState, Notice, Spinner } from "../../kit/kit";
import { Icon } from "../../kit/Icon";
import { inFront } from "../../kit/inFront";
import type { PageNav } from "../routes";
import { canShowFullSize, modifiersFor, needsSoftKeyboard, RemoteSession, RemoteView } from "./RemoteView";

/** How often the list reads the computer again, so an agent that starts or ends shows up. */
const REFRESH_MS = 4_000;
const ITEM = "rounded-lg px-4 py-3 active:bg-accent disabled:opacity-50";

/**
 * Screen on a Linux computer: every agent's own display, each with its live
 * view, Hold and Stop. `focus` (the floating screen's Expand) opens on that
 * agent's card. Leaving the page, a switch of computer or the app going to the
 * background closes every view, which also lets a held agent go on; coming
 * back to the app reconnects them.
 */
export function AgentDisplays(props: { server: StoredServer; nav: PageNav; focus?: string }) {
  const displays = signal<AgentDisplay[] | null>(null);
  const reason = signal("");
  const issue = signal<DisplayIssue | null>(null);
  const checking = signal(false);
  const sessions = new Map<string, RemoteSession>();
  const tops = new Map<string, number>();
  let scroller: ScrollRef | null = null;
  let focused = false;
  let gone = false;

  const fail = (failure: unknown) => !gone && issue.set(displayIssue(failure, props.server.name || "this computer"));
  const read = async () => {
    try {
      const list = await listDisplays(props.server);
      if (gone) return;
      displays.set(list.displays);
      reason.set(list.reason ?? "");
      issue.set(null);
    } catch (failure) {
      if (gone) return;
      // A first read that fails still ends the wait: what is known is nothing.
      if (untracked(displays) === null) displays.set([]);
      fail(failure);
    }
  };
  void read();
  effect(() => {
    if (!inFront()) return;
    const timer = setInterval(() => void read(), REFRESH_MS);
    return () => clearInterval(timer);
  });

  // A phone that leaves the app gives its viewers up (a hold ends with its socket); coming back reconnects them.
  let foreground = untracked(inFront);
  effect(() => {
    const active = inFront();
    untracked(() => {
      if (active === foreground) return;
      foreground = active;
      for (const session of sessions.values()) {
        if (active) session.connect();
        else session.lose(null);
      }
    });
  });
  cleanup(() => {
    gone = true;
    for (const session of sessions.values()) session.disconnect();
    sessions.clear();
  });

  const sessionFor = (worker: string) => {
    let session = sessions.get(worker);
    if (!session) {
      session = new RemoteSession(props.server, { route: displayRoute(worker), align: "top" });
      sessions.set(worker, session);
      session.connect();
    }
    return session;
  };
  const focusCard = () => {
    const id = props.focus;
    const top = id ? tops.get(id) : undefined;
    if (!id || top === undefined || focused || !scroller) return;
    focused = true;
    scroller.scrollTo({ y: Math.max(0, top - 12), animated: false });
  };
  const run = async (action: () => Promise<unknown>) => {
    try {
      await action();
      await read();
    } catch (failure) {
      fail(failure);
    }
  };
  const check = async () => {
    if (untracked(checking)) return;
    checking.set(true);
    await read();
    checking.set(false);
  };

  const ids = computed(() => (displays() === null ? "loading" : `ids:${displays()!.map((d) => d.id).join(",")}:${displays()!.length ? "" : reason() ? "setup" : "empty"}`));
  const notice = () => {
    const current = issue();
    return current ? <Notice testID="displays-issue" tone={current.tone === "danger" ? "failed" : "warning"} title={current.title} detail={current.detail} /> : null;
  };
  return (
    <View class="flex-1">
      {((key) =>
        untracked(() => {
          const list = displays();
          if (key === "loading" || list === null) return <View class="flex-1 px-4">{notice()}<EmptyState busy /></View>;
          // Cards whose agent is gone close their views.
          const shown = list.map((d) => d.id);
          for (const [worker, session] of sessions) {
            if (!shown.includes(worker)) {
              session.disconnect();
              sessions.delete(worker);
              tops.delete(worker);
            }
          }
          if (!list.length) {
            return (
              <View class="flex-1 px-4">
                {notice()}
                {reason() ? (
                  <View class="items-center gap-2 px-6 py-16">
                    <Icon name="construct" size={28} />
                    <Text class="mt-1 text-center text-lg font-semibold">Browser needs setup on this server</Text>
                    <Text class="text-center text-sm text-muted-foreground">{reason()}</Text>
                    <Button testID="displays-check" disabled={checking()} onPress={() => void check()} class="mt-2 flex-row items-center gap-2 rounded-full bg-secondary px-4 py-2 active:opacity-70 disabled:opacity-50">
                      {checking() ? <Spinner size={14} /> : null}
                      <Text class="text-sm font-medium">Check again</Text>
                    </Button>
                  </View>
                ) : (
                  <EmptyState icon="desktop" title="Nothing on screen" detail="An agent's screen shows up here while it works." />
                )}
              </View>
            );
          }
          return (
            <Scroll keyboardShouldPersistTaps="handled" ref={(node: ScrollRef) => { scroller = node; focusCard(); }} class="flex-1" contentClass="px-4 pb-10 pt-2">
              <View class="w-full max-w-[720px] gap-3 self-center">
                {notice()}
                {list.map((display) => (
                  <View onLayout={({ bounds }) => { tops.set(display.id, bounds.origin.y); focusCard(); }}>
                    <DisplayCard
                      display={() => displays()?.find((d) => d.id === display.id) ?? display}
                      name={() => workers().find((w) => w.id === display.id)?.name || ""}
                      session={sessionFor(display.id)}
                      onStop={() => run(() => haltDisplay(props.server, display.id))}
                      onResume={() => run(() => resumeDisplay(props.server, display.id))}
                    />
                  </View>
                ))}
              </View>
            </Scroll>
          );
        }))(ids())}
    </View>
  );
}

/**
 * One agent's private display. The live view is the input surface: what the person
 * does reaches the display while the agent keeps going, and the agent is told how many
 * clicks and keys and when (never what was typed) and decides for itself. The menu
 * holds the two hard switches: Hold (the agent waits until released or the view closes)
 * and Stop.
 */
function DisplayCard(props: { display: () => AgentDisplay; name: () => string; session: RemoteSession; onStop: () => Promise<void>; onResume: () => Promise<void> }) {
  const typing = signal(false);
  const busy = signal(false);
  const title = () => props.name() || props.display().id;
  const holding = () => props.session.holding();
  const live = () => props.session.phase() === "live";
  // Values, not the display object, which every read of the list replaces.
  const stopped = computed(() => !!props.display().stopped);
  const control = computed(() => props.display().control);
  const elsewhere = () => control() === "person" && !holding();
  const run = async (action: () => Promise<void>) => {
    if (untracked(busy)) return;
    busy.set(true);
    try {
      await action();
    } finally {
      busy.set(false);
    }
  };
  return (
    <View class="overflow-hidden rounded-2xl border border-border bg-card" testID={`display-${props.display().id}`}>
      <View class="flex-row items-center gap-2 py-1.5 pl-4 pr-1">
        <View class={`size-2 rounded-full ${holding() ? "bg-destructive" : stopped() || elsewhere() ? "bg-muted-foreground" : "bg-primary"}`} />
        <View class="min-w-0 flex-1">
          <Text class="text-base font-medium line-clamp-1">{title()}</Text>
          {holding() ? <Text class="text-xs text-muted-foreground line-clamp-1">You hold the agent</Text> : elsewhere() ? <Text class="text-xs text-muted-foreground line-clamp-1">Held on another device</Text> : stopped() ? <Text class="text-xs text-muted-foreground line-clamp-1">Stopped</Text> : null}
        </View>
        {live() && needsSoftKeyboard() ? (
          <Button accessibilityLabel={typing() ? "Hide keyboard" : "Show keyboard"} selected={typing()} onPress={() => typing.set(!typing())} class={`rounded-full p-2 ${typing() ? "bg-secondary" : ""}`}>
            <Icon name="keyboard" size={18} tone={typing() ? "ink" : "soft"} />
          </Button>
        ) : null}
        <Menu trigger={<View accessibilityLabel={`Options for ${title()}`} role="button" class="p-2"><Icon name="more-horizontal" size={18} /></View>} popupClass="rounded-2xl bg-popover p-2" backdropClass="bg-black/30">
          <View class="px-4 pb-1 pt-2"><Text class="text-sm font-medium text-muted-foreground line-clamp-1">{title()}</Text></View>
          {live() ? (
            <MenuItem onSelect={() => props.session.hold(!holding())} class={ITEM}><Text class="text-base">{holding() ? "Let the agent go on" : "Hold the agent"}</Text></MenuItem>
          ) : null}
          {live() ? <MenuItem onSelect={() => props.session.fit()} class={ITEM}><Text class="text-base">Fit</Text></MenuItem> : null}
          {live() && canShowFullSize() ? (
            <MenuItem testID="display-full-size" onSelect={() => void props.session.fullSize(`Display of ${untracked(title)}`)} class={ITEM}><Text class="text-base">Full size</Text></MenuItem>
          ) : null}
          {stopped() ? (
            <MenuItem disabled={busy()} onSelect={() => void run(props.onResume)} class={ITEM}><Text class="text-base">Resume</Text></MenuItem>
          ) : (
            <MenuItem disabled={busy() || elsewhere()} accessibilityLabel={`Stop ${title()}`} onSelect={() => void run(props.onStop)} class={ITEM}><Text class="text-base text-destructive">Stop</Text></MenuItem>
          )}
          {props.session.phase() === "lost" ? <MenuItem onSelect={() => props.session.connect()} class={ITEM}><Text class="text-base">Reconnect</Text></MenuItem> : null}
        </Menu>
      </View>
      {props.session.phase() === "lost" ? (
        <View class="px-3 pb-2">
          <Notice title="Lost connection to the display" detail={props.session.reason() || "It may have been closed."} action="Reconnect" onAction={() => props.session.connect()} />
        </View>
      ) : null}
      {props.session.phase() === "connecting" ? <View class="flex-row items-center gap-2 px-4 pb-2"><Spinner size={14} /><Text class="text-sm text-muted-foreground">Connecting…</Text></View> : null}
      <View style={{ aspectRatio: (props.display().width || 1280) / (props.display().height || 800) }}>
        <RemoteView session={props.session} label={`Display of ${title()}`} modifiers={modifiersFor("linux")} typing={typing()} onTypingChange={(on) => typing.set(on)} />
      </View>
    </View>
  );
}
