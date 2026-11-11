import { effect, signal, Text, untracked, View } from "nsh";
import { lifecycle } from "../lifecycle";
import { Button } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import { decideEnrollment, decodePendingEnrollment, pendingEnrollments, verificationChoices, type PendingEnrollment } from "../../core/enrollment";
import { current, onTransport } from "../session";
import { forgetOwnEnrollment, ownEnrollmentRequest } from "./ownRequest";

/** The current computer's requests from new devices, newest last; cleared on a switch or a disconnect. */
const requests = signal<PendingEnrollment[]>([]);

function refresh(): void {
  const server = untracked(current);
  if (!server) return;
  void pendingEnrollments(server)
    .then((pending) => untracked(current)?.id === server.id && requests.set(pending))
    .catch(() => {});
}

onTransport((transport) => {
  requests.set([]);
  const add = (request: PendingEnrollment) => requests.set([...untracked(requests).filter((r) => r.id !== request.id), request]);
  const stops = [
    transport.on("enrollment_request", (frame) => {
      const request = decodePendingEnrollment(frame as Record<string, unknown>);
      if (request) add(request);
    }),
    transport.on("enrollment_decision", (frame) => requests.set(untracked(requests).filter((r) => r.id !== frame.request_id))),
    transport.on("connected", refresh),
    transport.on("disconnected", () => requests.set([])),
  ];
  // Expired requests leave on their own.
  const timer = setInterval(() => {
    const live = untracked(requests).filter((r) => Date.parse(r.expiresAt) > Date.now());
    if (live.length !== untracked(requests).length) requests.set(live);
  }, 1000);
  return () => {
    stops.forEach((stop) => stop());
    clearInterval(timer);
    requests.set([]);
  };
});

// This browser's own request, left pending when a pairing link paired it instead: it is
// never shown, and once this browser is paired it is withdrawn, so no device is asked about it.
const withdrawn = new Set<string>();
effect(() => {
  const own = ownEnrollmentRequest();
  const server = current();
  const mine = own ? requests().find((r) => r.id === own) : undefined;
  if (!server || !mine) return;
  untracked(() => {
    withdrawn.add(mine.id);
    requests.set(requests().filter((r) => r.id !== mine.id));
    forgetOwnEnrollment();
    void decideEnrollment(server, mine, mine.verificationNumber, false).catch(() => {});
  });
});

// Coming back to the app reads what arrived while away.
effect(() => {
  if (lifecycle() === "active") untracked(refresh);
});

/**
 * "Approve new device": a browser (or another device) asked the current computer to be let in.
 * The person picks the number the new device shows among four, then approves; or denies. It stays
 * until decided: no swipe or backdrop closes it.
 */
export function EnrollmentApproval() {
  const request = () => requests().find((r) => !withdrawn.has(r.id) && r.id !== ownEnrollmentRequest()) ?? null;
  const choices = signal<string[]>([]);
  const selected = signal("");
  const busy = signal(false);
  const problem = signal("");
  let shownFor = "";
  effect(() => {
    const next = request();
    if ((next?.id ?? "") === shownFor) return;
    shownFor = next?.id ?? "";
    choices.set(next ? verificationChoices(next.verificationNumber) : []);
    selected.set("");
    problem.set("");
  });

  const decide = async (approve: boolean) => {
    const server = untracked(current);
    const pending = untracked(request);
    if (!server || !pending || untracked(busy)) return;
    busy.set(true);
    problem.set("");
    try {
      await decideEnrollment(server, pending, approve ? untracked(selected) : pending.verificationNumber, approve);
      requests.set(untracked(requests).filter((r) => r.id !== pending.id));
    } catch (error) {
      problem.set(error instanceof Error ? error.message : "Could not send decision.");
    } finally {
      busy.set(false);
    }
  };

  return (
    <AppSheet open={Boolean(request())} onOpenChange={() => {}} popupClass="rounded-t-2xl bg-background px-5 pt-5" backdropClass="bg-black/40">
      <View testID="enrollment-approval" class="gap-4 pb-8">
        <Text class="text-xl font-semibold">Approve new device</Text>
        <View class="gap-1">
          <Text class="text-lg font-semibold">{request()?.deviceName ?? ""}</Text>
          <Text class="text-sm text-muted-foreground">{request() ? `${request()!.platform} · ${request()!.origin}` : ""}</Text>
        </View>
        <Text class="text-base">Choose the number shown on the new device. Only approve a request you started.</Text>
        <View class="flex-row flex-wrap gap-2.5">
          {choices().map((number) => (
            <Button testID={`enrollment-choice-${number}`} disabled={busy()} onPress={() => selected.set(number)}
              class={selected() === number ? "rounded-full bg-primary px-5 py-2.5" : "rounded-full border border-border px-5 py-2.5 active:bg-accent"}>
              <Text class={selected() === number ? "font-mono text-lg text-primary-foreground" : "font-mono text-lg"}>{number}</Text>
            </Button>
          ))}
        </View>
        {problem() ? (
          <View testID="enrollment-problem" class="gap-1 rounded-xl border border-border bg-card px-3 py-2.5">
            <Text class="text-sm font-semibold text-destructive">Decision not sent</Text>
            <Text class="text-sm text-muted-foreground">{problem()}</Text>
          </View>
        ) : null}
        <Button testID="enrollment-approve" disabled={!selected() || busy()} onPress={() => void decide(true)}
          class={!selected() || busy() ? "items-center rounded-full bg-primary py-3 opacity-50" : "items-center rounded-full bg-primary py-3 active:opacity-80"}>
          <Text class="text-base font-semibold text-primary-foreground">{busy() ? "Sending…" : "Approve device"}</Text>
        </Button>
        <Button testID="enrollment-deny" disabled={busy()} onPress={() => void decide(false)} class="items-center rounded-full border border-border py-3 active:bg-accent">
          <Text class="text-base font-semibold text-destructive">Deny</Text>
        </Button>
      </View>
    </AppSheet>
  );
}
