import { Clipboard, cleanup, effect, signal, Text, untracked, View } from "nsh";
import { failureText } from "../../../core/more/daemon";
import { serverAddressForDisplay, serverAddressProblem, setServerAutoConnect, updateServer, type StoredServer } from "../../../core/servers";
import {
  changeAgentUse,
  getAgentUse,
  getDaemonUpdate,
  installDaemonUpdate,
  MIN_COMPUTER_VERSION,
  type AgentUseStatus,
  type DaemonUpdateStatus,
} from "../../../core/settings/computer";
import { computerProblems, computerProblemsMayChange, computerTmuxProblem, computerUpdateProblem, type ComputerAction, type ComputerProblem } from "../../../core/settings/computerModel";
import { effectiveMinComputerVersion } from "../../../core/update/version";
import manifest from "../../nsh.json";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, Spinner, SwitchRow } from "../kit/kit";
import type { PageNav } from "../routes";
import { connection, current, resumeConnection, servers, switchTo, transportPath } from "../../session";
import { linkPathText } from "../../../core/link/directAddresses";
import {
  apartNames,
  checks,
  currentConnectionIssue,
  followConnectionIssue,
  forgetConnectionIssue,
  probeComputer,
  reloadServers,
  removeComputer,
  removedNotice,
  statusWord,
} from "./computers";

/** One computer: whether it answers, what agents may use there, its name and address, and removing it. */
export function Computer(props: { id: string; nav: PageNav; onPair: () => void }) {
  const server = () => servers().find((s) => s.id === props.id) ?? null;
  const agentUse = signal<AgentUseStatus | null>(null);
  const update = signal<DaemonUpdateStatus | null>(null);
  const busy = signal("");
  const notes = signal<Record<string, string>>({});
  /** What a step that left the page said (the removal's computers that did not answer, a save that failed). */
  const notice = signal<{ title: string; detail: string } | null>(null);
  const name = signal(server()?.name ?? "");
  const address = signal(server() ? serverAddressForDisplay(server()!.url) : "");

  const isCurrent = () => current()?.id === props.id;
  const reachable = () => (isCurrent() && connection() === "connected") || checks()[props.id]?.state === "online";
  const note = (key: string, text: string) => notes.set({ ...notes(), [key]: text });

  followConnectionIssue();

  // The name a computer reports, adopted here or in Settings, shows in the field.
  let shownName = untracked(() => server()?.name ?? "");
  effect(() => {
    const next = server()?.name ?? "";
    untracked(() => {
      if (next !== shownName && name() === shownName) name.set(next);
      shownName = next;
    });
  });

  let agentTimer: ReturnType<typeof setInterval> | null = null;
  let updateTimer: ReturnType<typeof setInterval> | null = null;
  const stopTimers = () => {
    if (agentTimer) clearInterval(agentTimer);
    if (updateTimer) clearInterval(updateTimer);
    agentTimer = updateTimer = null;
  };
  cleanup(stopTimers);

  // A missed look while the page polls keeps what it last showed; with nothing shown yet, it says why.
  const readAgentUse = (s: StoredServer) =>
    getAgentUse(s)
      .then((v) => {
        agentUse.set(v);
        if (notes()["agent-use-load"]) note("agent-use-load", "");
      })
      .catch((error: unknown) => {
        if (!agentUse()) note("agent-use-load", failureText(error, "That didn't finish"));
      });
  const readUpdate = (s: StoredServer) => getDaemonUpdate(s).then((v) => update.set(v)).catch(() => {});

  // Ask the computer itself, current or not: one not in use is not offline until it fails to answer.
  void untracked(async () => {
    const s = server();
    if (s) await probeComputer(s);
  });

  // Read what the computer reports once it answers; keep reading while it can change.
  effect(() => {
    const s = server();
    stopTimers();
    if (!s || !reachable()) return;
    untracked(() => {
      void readAgentUse(s);
      void readUpdate(s);
    });
  });
  effect(() => {
    const s = server();
    if (agentTimer) clearInterval(agentTimer);
    agentTimer = s && reachable() && computerProblemsMayChange(agentUse()) ? setInterval(() => void readAgentUse(s), 5_000) : null;
  });
  effect(() => {
    const s = server();
    if (updateTimer) clearInterval(updateTimer);
    updateTimer = s && reachable() && update()?.updating ? setInterval(() => void readUpdate(s), 3_000) : null;
  });

  async function act(key: string, work: () => Promise<void>): Promise<void> {
    if (busy()) return;
    busy.set(key);
    note(key, "");
    try {
      await work();
    } catch (error) {
      note(key, failureText(error, "That didn't finish"));
    } finally {
      busy.set("");
    }
  }

  function perform(problem: ComputerProblem, does: ComputerAction): void {
    const s = server();
    if (!s) return;
    if (does.kind === "update") {
      return void act(problem.key, async () => {
        const next = await installDaemonUpdate(s);
        update.set({ ...next, updating: true });
      });
    }
    if (does.kind === "paste_key") {
      return void act(problem.key, async () => {
        const key = (await Clipboard.readText()).trim();
        if (!key) throw new Error("Copy the key the extension shows, then tap Paste key.");
        agentUse.set(await changeAgentUse(s, { key }));
      });
    }
    void act(problem.key, async () => {
      agentUse.set(await changeAgentUse(s, { fix: does.fix }));
      const where = s.name || "the computer";
      if (does.fix === "install_extension") note(problem.key, `Load the Mewla folder in Chrome on ${where}: Developer mode, then Load unpacked.`);
      if (does.fix === "open_settings") note(problem.key, `Turn on Mewla in the settings that opened on ${where}.`);
    });
  }

  /** Try again / Connect: this phone connects to it again, now. */
  function reconnect(): void {
    void setServerAutoConnect(props.id, true);
    forgetConnectionIssue();
    resumeConnection();
  }

  async function saveName(): Promise<void> {
    const s = server();
    const next = name().trim();
    if (!s || !next || next === s.name) return name.set(s?.name ?? "");
    try {
      await updateServer(s.id, { name: next });
      await reloadServers();
    } catch (error) {
      name.set(s.name);
      notice.set({ title: "Check the address", detail: failureText(error, "The computer could not be saved.") });
    }
  }

  async function saveAddress(): Promise<void> {
    const s = server();
    const next = address().trim();
    if (!s || next === serverAddressForDisplay(s.url)) return;
    const issue = serverAddressProblem(next);
    if (issue) {
      notice.set({ title: "Check the address", detail: issue });
      return;
    }
    notice.set(null);
    let saved: StoredServer | null;
    try {
      saved = await updateServer(s.id, { url: next });
    } catch (error) {
      notice.set({ title: "Check the address", detail: failureText(error, "The address could not be saved.") });
      return;
    }
    await reloadServers();
    if (saved) address.set(serverAddressForDisplay(saved.url));
    if (saved && isCurrent()) {
      // The person moved the computer they use: connect to it where it is now.
      await setServerAutoConnect(saved.id, true);
      forgetConnectionIssue();
      switchTo(servers().find((x) => x.id === s.id) ?? saved);
    }
  }

  async function remove(): Promise<void> {
    const s = server();
    if (!s || busy()) return;
    const yes = await confirmDestructive(`Remove ${s.name || "this computer"}?`, "This phone and your other computers forget it. Its Sessions stay there. Pair again to bring it back.", "Remove");
    if (!yes) return;
    busy.set("remove");
    try {
      const missed = await removeComputer(s);
      // The page leaves; what did not answer is said where the person lands.
      if (missed) removedNotice.set(missed);
      props.nav.pop();
    } finally {
      busy.set("");
    }
  }

  const problems = (): ComputerProblem[] => {
    const s = server();
    const own = reachable()
      ? [computerUpdateProblem(update(), effectiveMinComputerVersion(MIN_COMPUTER_VERSION, manifest.version)), computerTmuxProblem(update()), ...computerProblems(agentUse())]
      : [];
    const apart = s ? apartNames(s).map((other) => ({ key: `apart:${other}`, text: `Can't reach ${other}. Give one of them a Tailscale or HTTPS address.` })) : [];
    return [...own.filter((p): p is ComputerProblem => Boolean(p)), ...apart];
  };

  /** The one thing wrong with the connection, and its one way out. */
  const connectionProblem = () => {
    const s = server();
    if (!s) return null;
    const word = statusWord(s);
    if (word === "Pair again") return { testID: "computer-pair-again", title: "This computer no longer knows this phone.", detail: undefined, action: "Pair again", onAction: props.onPair };
    const issue = isCurrent() ? currentConnectionIssue() : null;
    if (issue) return { testID: "computer-issue", title: issue.title, detail: issue.hint || issue.detail, action: "Try again", onAction: reconnect };
    if (word === "Not connected" || word === "Can't connect") return { testID: "computer-connect", title: "Not connected", detail: undefined, action: "Connect", onAction: reconnect };
    return null;
  };

  return (
    <>
      {!server() ? (
        <Page title="Computer"><EmptyState title="This computer is no longer paired" /></Page>
      ) : (
        <Page title={server()!.name || "Computer"} testID="computer">
          {notice() ? <Notice testID="computer-notice" tone="failed" title={notice()!.title} detail={notice()!.detail} /> : null}
          {connectionProblem() ? (
            <Notice
              testID={connectionProblem()!.testID}
              title={connectionProblem()!.title}
              detail={connectionProblem()!.detail}
              action={connectionProblem()!.action}
              onAction={busy() ? undefined : connectionProblem()!.onAction}
            />
          ) : null}

          <Section footer={notes()["agent-use-load"] && !agentUse() ? notes()["agent-use-load"] : undefined}>
            <SwitchRow
              testID="agent-use"
              title="Agents can use this computer"
              checked={agentUse()?.enabled ?? true}
              disabled={!reachable() || !agentUse() || Boolean(busy())}
              busy={busy() === "agent-use"}
              onChange={(enabled) => void act("agent-use", async () => agentUse.set(await changeAgentUse(server()!, { enabled })))}
            />
            {problems().map((problem) => (
              <View>
                <Row
                  title={problem.text}
                  trailing={problem.action && reachable() ? (
                    <PillButton title={problem.action.label} busy={busy() === problem.key} disabled={Boolean(busy())} onPress={() => perform(problem, problem.action!.does)} />
                  ) : undefined}
                />
                {notes()[problem.key] ? <Text class="px-4 pb-3 text-sm text-muted-foreground" android={{ accessibilityLiveRegion: "polite" }}>{notes()[problem.key]}</Text> : null}
              </View>
            ))}
            {notes()["agent-use"] ? <Text class="px-4 pb-3 text-sm text-destructive" android={{ accessibilityLiveRegion: "polite" }}>{notes()["agent-use"]}</Text> : null}
          </Section>

          {reachable() && update()?.version ? (
            <Section>
              <Row title="Mewla" value={`Version ${update()!.version}`} accessibilityLabel={`Mewla on this computer, Version ${update()!.version}`} />
            </Section>
          ) : null}

          <Section title="Name">
            <Field testID="computer-name" accessibilityLabel="Computer name" value={name()} onChange={(v) => name.set(v)} onCommit={() => void saveName()} />
          </Section>
          {server()!.transportKind !== "link" ? (
            <Section title="Address" footer={notes().address || undefined}>
              <Field testID="computer-address" accessibilityLabel="Address" mono keyboardType="url" value={address()} placeholder="https://mewla.example.com" onChange={(v) => address.set(v)} onCommit={() => void saveAddress()} />
            </Section>
          ) : (
            <Section title="Address" footer="Paired through Mewla Relay: reached through it, or straight on your network when the computer can be.">
              <Row testID="computer-link" title="Mewla Relay" value={isCurrent() && connection() === "connected" ? linkPathText(transportPath()) : "Not connected"} />
            </Section>
          )}

          <Section>
            {!isCurrent() ? (
              <Row
                testID="computer-use"
                icon="swap-horizontal"
                title="Use this computer"
                disabled={Boolean(busy())}
                onPress={() => {
                  void setServerAutoConnect(props.id, true);
                  switchTo(server());
                }}
              />
            ) : null}
            <Row
              testID="computer-remove"
              destructive
              icon="trash"
              title="Remove computer"
              disabled={Boolean(busy())}
              trailing={busy() === "remove" ? <Spinner size={16} /> : undefined}
              onPress={() => void remove()}
            />
          </Section>
        </Page>
      )}
    </>
  );
}
