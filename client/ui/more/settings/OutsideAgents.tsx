import { cleanup, effect, signal, Text, untracked, View, lifecycle } from "nsh";
import { formatFullDateTime } from "../../../core/more/dates";
import { failureText } from "../../../core/more/daemon";
import { decideOutsideAgent, listOutsideAgents, type OutsideAgents as Agents } from "../../../core/settings/outsideAgents";
import { daemon } from "../daemon";
import { confirmDestructive, EmptyState, Page, PillButton, Row, Section } from "../../kit/kit";

/** Outside agents: approve the ones that ask to hand work to Brain, revoke the ones you no longer trust. */
export function OutsideAgents() {
  const agents = signal<Agents | null>(null);
  const error = signal("");
  const busy = signal("");
  let timer: ReturnType<typeof setInterval> | null = null;
  let forServer = "";

  const read = () => {
    const server = daemon()?.server;
    if (!server) return;
    listOutsideAgents(server)
      .then((next) => server.id === daemon()?.server.id && (agents.set(next), error.set("")))
      .catch((e: unknown) => server.id === daemon()?.server.id && error.set(failureText(e, "Outside agents did not load")));
  };

  // Every three seconds while the page is open: a waiting request expires quickly.
  effect(() => {
    const server = daemon()?.server;
    if (timer) clearInterval(timer);
    timer = null;
    const active = lifecycle() === "active";
    untracked(() => {
      if ((server?.id ?? "") !== forServer) {
        forServer = server?.id ?? "";
        agents.set(null);
      }
    });
    if (!server || !active) return;
    untracked(read);
    timer = setInterval(read, 3_000);
  });
  cleanup(() => timer && clearInterval(timer));

  async function decide(action: "approve" | "deny" | "revoke", id: string): Promise<void> {
    const server = daemon()?.server;
    if (!server || busy()) return;
    busy.set(`${action}:${id}`);
    try {
      agents.set(await decideOutsideAgent(server, action, id));
      error.set("");
    } catch (e) {
      error.set(failureText(e, "That didn't finish"));
    } finally {
      busy.set("");
    }
  }

  return (
    <Page title="Outside agents" testID="outside-agents">
      {!daemon() ? (
        <EmptyState title="No current server" />
      ) : (
        <View>
          {error() ? <Text testID="agents-error" role="alert" android={{ accessibilityLiveRegion: "polite" }} class="mt-4 px-1 text-sm text-destructive">{error()}</Text> : null}
          {agents()?.pending.length ? (
            <Section title="Waiting">
              {agents()!.pending.map((a) => (
                <View class="gap-2 px-4 py-3">
                  <Text class="text-base">{a.name}</Text>
                  <Text class="text-sm text-muted-foreground">{`Wants to hand work to Brain. Approve only if its terminal shows ${a.number}.`}</Text>
                  <View class="flex-row gap-2">
                    <PillButton testID={`deny-${a.id}`} title="Deny" busy={busy() === `deny:${a.id}`} disabled={Boolean(busy())} onPress={() => void decide("deny", a.id)} />
                    <PillButton testID={`approve-${a.id}`} filled title="Approve" busy={busy() === `approve:${a.id}`} disabled={Boolean(busy())} onPress={() => void decide("approve", a.id)} />
                  </View>
                </View>
              ))}
            </Section>
          ) : null}
          {agents()?.agents.length ? (
            <Section title="Approved">
              {agents()!.agents.map((a) => (
                <Row
                  testID={`agent-${a.id}`}
                  title={a.name}
                  subtitle={a.last_used_at ? `Used ${formatFullDateTime(a.last_used_at)}` : "Not used yet"}
                  trailing={
                    <PillButton
                      destructive
                      title="Revoke"
                      busy={busy() === `revoke:${a.id}`}
                      disabled={Boolean(busy())}
                      onPress={() => void confirmDestructive(`Revoke ${a.name}?`, "It can no longer hand work to Brain. Connect it again to allow it.", "Revoke").then((yes) => { if (yes) void decide("revoke", a.id); })}
                    />
                  }
                />
              ))}
            </Section>
          ) : null}
          {agents() && !agents()!.pending.length && !agents()!.agents.length ? (
            <EmptyState testID="agents-empty" icon="brain" title="No outside agents" detail="On the computer where Claude Code or Codex runs: mewla agents mcp connect" />
          ) : null}
          {!agents() && !error() ? <EmptyState busy /> : null}
        </View>
      )}
    </Page>
  );
}
