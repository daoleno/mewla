import { host, nativeComponent, platform, signal, Text, View } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import { pairWith, readPairingLink } from "../../core/pairFlow";
import { addPairedServer } from "../session";

/** The host's camera reader (CameraX + ML Kit on Android, AVFoundation on iOS). */
const CodeScanner = nativeComponent<{ active: boolean }, { scan: { value: string }; unavailable: { reason: string } }>("mewla.qrScanner", {
  props: ["active"],
  events: ["scan", "unavailable"],
});

const busy = signal(false);
const failure = signal("");

/** Pairing from a scanned code, a pasted link or an opened `mewla://` link: one flow. */
export async function pairFromText(text: string): Promise<void> {
  const payload = readPairingLink(text);
  if (!payload) return failure.set("That isn't a Mewla pairing code. Run mewla pair on the computer and scan the code it shows.");
  const device = platform.os === "web" ? "browser" : "phone";
  const { action } = await AlertDialog.show({
    title: `Pair this ${device}?`,
    description: `Pairing gives this ${device} access to sessions, terminal, Brain, Workers and files on that computer.`,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "pair", label: "Pair" },
    ],
  });
  if (action !== "pair") return;
  busy.set(true);
  failure.set("");
  try {
    await addPairedServer(await pairWith(payload));
  } catch (error) {
    failure.set(error instanceof Error ? error.message : String(error));
  } finally {
    busy.set(false);
  }
}

export function Onboarding() {
  const scanning = signal(false);
  const pasted = signal("");
  return (
    <NavPage title="Pair a computer">
      <View class="flex-1 gap-4 bg-background p-5">
        <Text class="text-base text-muted-foreground">On your computer, run mewla pair, then scan the code or paste the link.</Text>
        {scanning() ? (
          <CodeScanner active class="h-72 overflow-hidden rounded-xl" onScan={({ value }) => { scanning.set(false); void pairFromText(value); }}
            onUnavailable={({ reason }) => { scanning.set(false); failure.set(`Camera unavailable: ${reason}`); }} />
        ) : (
          <Button testID="scan" onPress={() => scanning.set(true)} disabled={!host.available() || busy()} class="rounded-xl bg-primary px-4 py-3 active:opacity-80">
            <Text class="text-center text-base font-semibold text-primary-foreground">Scan code</Text>
          </Button>
        )}
        <Input testID="link" value={pasted()} onChange={({ value }) => pasted.set(value)} placeholder="mewla://…" autoCapitalize="none" autoCorrect={false}
          class="rounded-xl border border-border bg-card px-3 py-3 text-base" />
        <Button testID="pair" onPress={() => pairFromText(pasted())} disabled={!pasted().trim() || busy()} class="rounded-xl border border-border px-4 py-3 active:bg-accent">
          <Text class="text-center text-base font-semibold">{busy() ? "Pairing…" : "Pair"}</Text>
        </Button>
        {failure() ? <Text testID="failure" class="text-base text-destructive">{failure()}</Text> : null}
      </View>
    </NavPage>
  );
}
