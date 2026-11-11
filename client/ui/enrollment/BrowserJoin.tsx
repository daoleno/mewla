import { cleanup, host, signal, Storage, Text, View } from "nsh";
import { Button, NavPage } from "nsh/base";
import { browserEnrollmentServer, readBrowserEnrollmentStatus, requestBrowserEnrollment, type BrowserEnrollmentPrompt } from "../../core/enrollment";
import { addPairedServer } from "../session";

type JoinState = "starting" | "pending" | "denied" | "expired" | "error";

/** The request survives a reload of the page for as long as it is valid (Expo: sessionStorage). */
const PROMPT_KEY = "mewla:enrollment:v1";

async function savedPrompt(): Promise<BrowserEnrollmentPrompt | null> {
  try {
    const cached = JSON.parse((await Storage.getItem(PROMPT_KEY)) ?? "null") as BrowserEnrollmentPrompt | null;
    return cached?.requestId && cached.secret && Date.parse(cached.expiresAt) > Date.now() ? cached : null;
  } catch {
    return null;
  }
}

/**
 * A browser that isn't paired asks the computer serving it to be let in: it shows a number to
 * approve on the computer (`mewla devices approve NNN`) or on a paired phone, and joins once
 * approved. "Use a pairing link" goes to the pairing page instead.
 */
export function BrowserJoin(props: { onPairingLink: () => void }) {
  const state = signal<JoinState>("starting");
  const prompt = signal<BrowserEnrollmentPrompt | null>(null);
  const problem = signal("");
  let run = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  cleanup(() => {
    run++;
    clearTimeout(timer);
  });

  const start = async () => {
    const mine = ++run;
    clearTimeout(timer);
    state.set("starting");
    prompt.set(null);
    problem.set("");
    let origin = "";
    try {
      origin = String(await host.call("app.origin"));
      const request = (await savedPrompt()) ?? (await requestBrowserEnrollment(origin));
      await Storage.setItem(PROMPT_KEY, JSON.stringify(request)).catch(() => {});
      if (mine !== run) return;
      prompt.set(request);
      state.set("pending");
      void poll(mine, origin, request);
    } catch (error) {
      if (mine !== run) return;
      state.set("error");
      problem.set(error instanceof Error ? error.message : "Could not request approval.");
    }
  };

  const poll = async (mine: number, origin: string, request: BrowserEnrollmentPrompt) => {
    try {
      const status = await readBrowserEnrollmentStatus(origin, request);
      if (mine !== run) return;
      if (status.status === "approved") {
        const input = browserEnrollmentServer(origin, status);
        if (!input) throw new Error("The approval response is incomplete. Please retry.");
        await Storage.removeItem(PROMPT_KEY).catch(() => {});
        await addPairedServer(input);
        return;
      }
      problem.set("");
      state.set(status.status);
      if (status.status === "pending") timer = setTimeout(() => void poll(mine, origin, request), 1500);
    } catch {
      if (mine !== run) return;
      if (Date.now() >= Date.parse(request.expiresAt)) return state.set("expired");
      problem.set("Connection interrupted. Retrying…");
      timer = setTimeout(() => void poll(mine, origin, request), 3000);
    }
  };

  const again = async () => {
    await Storage.removeItem(PROMPT_KEY).catch(() => {});
    void start();
  };
  void start();

  const waiting = () => state() === "pending" || state() === "starting";
  const title = () => (waiting() ? "Waiting for approval" : state() === "denied" ? "Request denied" : state() === "expired" ? "Request expired" : "Could not connect");
  const detail = () => waiting()
    ? prompt() ? `On your computer, run mewla devices approve ${prompt()!.verificationNumber} — or approve on a paired phone.` : "Approve this number on your computer or a paired phone."
    : state() === "denied" ? "This browser wasn’t approved." : state() === "expired" ? "This number expired." : problem();

  return (
    <NavPage title="Connect to Mewla">
      <View testID="browser-join" class="flex-1 items-center justify-center bg-background p-6">
        <View class="w-full max-w-[440px] gap-6">
          <Text class="text-center text-sm text-muted-foreground">Connect to Mewla</Text>
          <Text class="text-center text-3xl font-semibold">{title()}</Text>
          <Text testID="join-detail" class="text-center text-base text-muted-foreground">{detail()}</Text>
          {waiting() && prompt() ? (
            <Text testID="join-number" accessibilityLabel={`Verification number ${prompt()!.verificationNumber}`} class="text-center font-mono text-6xl tracking-[12px]">{prompt()!.verificationNumber}</Text>
          ) : null}
          {waiting() ? (
            problem() ? <Text class="text-center text-sm text-muted-foreground">{problem()}</Text> : null
          ) : (
            <Button testID="join-again" onPress={() => void again()} class="items-center rounded-full bg-primary py-3 active:opacity-80">
              <Text class="text-base font-semibold text-primary-foreground">{state() === "expired" ? "New number" : "Try again"}</Text>
            </Button>
          )}
          <Text class="text-center text-sm text-muted-foreground">First device? Run <Text class="font-mono">mewla pair</Text> on your computer and open its link.</Text>
          <Button testID="join-pairing-link" onPress={props.onPairingLink} class="items-center py-2">
            <Text class="text-base text-primary">Use a pairing link</Text>
          </Button>
        </View>
      </View>
    </NavPage>
  );
}

