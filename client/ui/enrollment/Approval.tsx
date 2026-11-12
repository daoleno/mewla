import { effect, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import { decideEnrollment, verificationChoices } from "../../core/enrollment";
import { current } from "../session";
import { forgetDeviceRequest, putDeviceRequestAside, shownDeviceRequest } from "./state";

/**
 * "Approve new device": a browser (or another device) asked the current computer to be let in.
 * The person picks the number the new device shows among four, then approves; or denies. Closed
 * undecided, the request stays open: a tray row, the count, the cat; the row opens it again.
 */
export function EnrollmentApproval() {
  const request = shownDeviceRequest;
  const choices = signal<string[]>([]);
  const selected = signal("");
  const busy = signal(false);
  const problem = signal("");
  const putAside = () => {
    const shown = untracked(request);
    if (shown && !untracked(busy)) putDeviceRequestAside(shown.id);
  };
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
      forgetDeviceRequest(pending.id);
    } catch (error) {
      problem.set(error instanceof Error ? error.message : "Could not send decision.");
    } finally {
      busy.set(false);
    }
  };

  return (
    <AppSheet open={Boolean(request())} onOpenChange={({ open }) => !open && putAside()} popupClass="rounded-t-2xl bg-background px-5 pt-5" backdropClass="bg-black/40">
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
