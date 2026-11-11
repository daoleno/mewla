import { Clipboard, cleanup, effect, signal, Text, untracked, View } from "nsh";
import { failureText } from "../../../core/more/daemon";
import { serverAddressProblem, setServerAutoConnect, updateServer, type StoredServer } from "../../../core/servers";
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
import { checkComputer } from "../../../core/settings/computer";
import { effectiveMinComputerVersion } from "../../../core/update/version";
import manifest from "../../nsh.json";
import { confirmDestructive, EmptyState, Field, Notice, Page, PillButton, Row, Section, SwitchRow } from "../kit/kit";
import type { PageNav } from "../routes";
import { connection, current, resumeConnection, servers, switchTo, transportPath } from "../../session";
import { linkPathText } from "../../../core/link/directAddresses";
import { checks, reloadServers, removeComputer, statusWord } from "./computers";

/** One computer: whether it answers, what agents may use there, its name and address, and removing it. */
export function Computer(props: { id: string; nav: PageNav; onPair: () => void }) {
  const server = () => servers().find((s) => s.id === props.id) ?? null;
  const agentUse = signal<AgentUseStatus | null>(null);
  const update = signal<DaemonUpdateStatus | null>(null);
  const busy = signal("");
  const notes = signal<Record<string, string>>({});
  const name = signal(server()?.name ?? "");
  const address = signal(server()?.url ?? "");

  const isCurrent = () => current()?.id === props.id;
  const reachable = () => (isCurrent() && connection() === "connected") || checks()[props.id]?.state === "online";

  let agentTimer: ReturnType<typeof setInterval> | null = null;
  let updateTimer: ReturnType<typeof setInterval> | null = null;
  const stopTimers = () => {
    if (agentTimer) clearInterval(agentTimer);
    if (updateTimer) clearInterval(updateTimer);
    agentTimer = updateTimer = null;
  };
  cleanup(stopTimers);

  const readAgentUse = (s: StoredServer) => getAgentUse(s).then((v) => agentUse.set(v)).catch(() => {});
  const readUpdate = (s: StoredServer) => getDaemonUpdate(s).then((v) => update.set(v)).catch(() => {});

  void untracked(async () => {
    const s = server();
    if (!s) return;
    const check = await checkComputer(s);
    checks.set({ ...checks(), [s.id]: check });
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
    notes.set({ ...notes(), [key]: "" });
    try {
      await work();
    } catch (error) {
      notes.set({ ...notes(), [key]: failureText(error, "That didn't finish") });
    } finally {
      busy.set("");
    }
  }

  function perform(problem: ComputerProblem, does: ComputerAction): void {
    const s = server();
    if (!s) return;
    if (does.kind === "update") return void act(problem.key, async () => update.set(await installDaemonUpdate(s)));
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
      if (does.fix === "install_extension") notes.set({ ...notes(), [problem.key]: `Load the Mewla folder in Chrome on ${where}: Developer mode, then Load unpacked.` });
      if (does.fix === "open_settings") notes.set({ ...notes(), [problem.key]: `Turn on Mewla in the settings that opened on ${where}.` });
    });
  }

  async function saveName(): Promise<void> {
    const s = server();
    const next = name().trim();
    if (!s || !next || next === s.name) return name.set(s?.name ?? "");
    await updateServer(s.id, { name: next });
    await reloadServers();
  }

  async function saveAddress(): Promise<void> {
    const s = server();
    const next = address().trim();
    if (!s || next === s.url) return;
    const issue = serverAddressProblem(next);
    if (issue) {
      notes.set({ ...notes(), address: `Check the address: ${issue}` });
      return address.set(s.url);
    }
    notes.set({ ...notes(), address: "" });
    await updateServer(s.id, { url: next });
    await reloadServers();
    if (isCurrent()) switchTo(servers().find((x) => x.id === s.id) ?? null);
  }

  async function remove(): Promise<void> {
    const s = server();
    if (!s) return;
    const yes = await confirmDestructive(`Remove ${s.name || "this computer"}?`, "This phone and your other computers forget it. Its Sessions stay there. Pair again to bring it back.", "Remove");
    if (!yes) return;
    await removeComputer(s);
    props.nav.pop();
  }

  const problems = (): ComputerProblem[] =>
    [computerUpdateProblem(update(), effectiveMinComputerVersion(MIN_COMPUTER_VERSION, manifest.version)), computerTmuxProblem(update()), ...computerProblems(agentUse())].filter((p): p is ComputerProblem => Boolean(p));

  return !server() ? (
    <Page title="Computer"><EmptyState title="This computer is no longer paired" /></Page>
  ) : (
    <Page title={server()!.name || "Computer"} testID="computer">
      {statusWord(server()!) === "Pair again" ? (
        <Notice testID="computer-pair-again" title="Pair again" detail="This computer no longer knows this phone." action="Pair again" onAction={props.onPair} />
      ) : isCurrent() && connection() !== "connected" && connection() !== "connecting" ? (
        <Notice testID="computer-connect" title="Not connected" action="Connect" onAction={() => { void setServerAutoConnect(props.id, true); resumeConnection(); }} />
      ) : null}

      <Section>
        <SwitchRow
          testID="agent-use"
          title="Agents can use this computer"
          checked={agentUse()?.enabled ?? true}
          disabled={!reachable() || !agentUse()}
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
            {notes()[problem.key] ? <Text class="px-4 pb-3 text-sm text-muted-foreground">{notes()[problem.key]}</Text> : null}
          </View>
        ))}
        {notes()["agent-use"] ? <Text class="px-4 pb-3 text-sm text-destructive">{notes()["agent-use"]}</Text> : null}
      </Section>

      {reachable() && update()?.version ? (
        <Section>
          <Row title="Mewla" value={`Version ${update()!.version}`} />
        </Section>
      ) : null}

      <Section title="Name">
        <Field testID="computer-name" value={name()} onChange={(v) => name.set(v)} onCommit={() => void saveName()} />
      </Section>
      {server()!.transportKind !== "link" ? (
        <Section title="Address" footer={notes().address || undefined}>
          <Field testID="computer-address" mono keyboardType="url" value={address()} placeholder="https://mewla.example.com" onChange={(v) => address.set(v)} onCommit={() => void saveAddress()} />
        </Section>
      ) : (
        <Section title="Address" footer="Paired through Mewla Link: reached over its relay, or straight on your network when the computer can be.">
          <Row testID="computer-link" title="Mewla Link" value={isCurrent() && connection() === "connected" ? linkPathText(transportPath()) : "Not connected"} />
        </Section>
      )}

      <Section>
        {!isCurrent() ? <Row testID="computer-use" icon="swap-horizontal" title="Use this computer" onPress={() => { void setServerAutoConnect(props.id, true); switchTo(server()); }} /> : null}
        <Row testID="computer-remove" destructive icon="trash" title="Remove computer" onPress={() => void remove()} />
      </Section>
    </Page>
  );
}
