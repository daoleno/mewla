import { cleanup, effect, host, nativeComponent, signal, Text, untracked, View, lifecycle } from "nsh";
import { Button } from "nsh/base";
import { cameraRuns, cameraSearching, finderState, readCameraPermission, type CameraPermission } from "../../core/onboarding";
import { Spinner } from "../kit/kit";
import { pairFromText, pairingBusy } from "../pairing";

/** The host's camera reader (CameraX + ML Kit on Android, AVFoundation on iOS). */
const CodeScanner = nativeComponent<{ active: boolean }, { scan: { value: string }; unavailable: { reason: string; message?: string } }>("mewla.qrScanner", {
  props: ["active"],
  events: ["scan", "unavailable"],
});

const LIGHT = "#F4F0EA";
const DARK = "#0A0908";

/** Whether the camera is really looking for a code, for the line under the finder. */
export const scannerSearching = signal(false);

/**
 * The viewfinder. The first time it shows, the system asks for the camera by itself (a finder
 * that waits for a tap reads as a black screen); the camera runs only while its page is on screen
 * (`onScreen`: a tab's page stays mounted under another tab), the app in front and nothing pairing. Refused, failed or missing, it says so and points to Settings;
 * the link can always be pasted instead.
 */
export function OnboardingScanner(props: { compact?: boolean; onScreen?: () => boolean }) {
  const permission = signal<CameraPermission | null>(null);
  const asking = signal(false);
  const failed = signal(false);
  const state = () => finderState(permission(), failed(), asking());
  const onScreen = () => props.onScreen?.() ?? true;
  const running = () => cameraRuns(state(), lifecycle() === "active", onScreen());

  const ask = async () => {
    if (!host.available()) return permission.set("denied");
    asking.set(true);
    const answer = readCameraPermission(await host.call("scan.request").catch(() => null));
    asking.set(false);
    permission.set(answer ?? "denied");
  };
  const check = async (askIfNew: boolean) => {
    if (!host.available()) return permission.set("denied");
    const answer = readCameraPermission(await host.call("scan.permission").catch(() => null)) ?? "denied";
    permission.set(answer);
    if (answer === "undetermined" && askIfNew) await ask();
  };
  void check(true);
  // Back from Settings (or the system prompt): ask the system again what it says now.
  effect(() => {
    if (lifecycle() !== "active") return;
    untracked(() => {
      if (permission() !== null && !asking()) void check(false);
    });
  });
  effect(() => scannerSearching.set(cameraSearching(state(), lifecycle() === "active" && onScreen(), pairingBusy())));
  cleanup(() => scannerSearching.set(false));

  const openSettings = () => void host.call("app.openSettings").catch(() => {});
  return (
    <View testID="finder" class="w-full items-center justify-center self-center overflow-hidden rounded-3xl border border-border"
      style={{ backgroundColor: DARK, aspectRatio: 1, maxHeight: 320, maxWidth: props.compact ? 200 : 320 }}>
      {state() === "camera" ? (running() ? (
        <CodeScanner testID="qr-camera" active={!pairingBusy()} class="absolute inset-0"
          onScan={({ value }) => void pairFromText(value, "camera")}
          onUnavailable={({ reason }) => {
            if (reason === "permission") void check(false);
            else failed.set(true);
          }} />
      ) : null) : state() === "loading" ? (
        <Spinner />
      ) : (
        <View class="items-center gap-3.5 px-7">
          <Text class="text-center text-sm" style={{ color: LIGHT }}>
            {state() === "request" ? "Mewla reads the code with the camera."
              : state() === "denied" ? "The camera is off for Mewla. Turn it on in Settings, or paste the link instead."
              : "The camera could not start. Check that Mewla may use it in Settings, or paste the link instead."}
          </Text>
          <FinderButton testID={state() === "request" ? "allow-camera" : "open-settings"} label={state() === "request" ? "Allow camera" : "Open Settings"}
            onPress={() => (state() === "request" ? void ask() : openSettings())} />
        </View>
      )}
      <Corner style={{ top: 18, left: 18, borderTopWidth: 3, borderLeftWidth: 3, borderTopLeftRadius: 10 }} />
      <Corner style={{ top: 18, right: 18, borderTopWidth: 3, borderRightWidth: 3, borderTopRightRadius: 10 }} />
      <Corner style={{ bottom: 18, left: 18, borderBottomWidth: 3, borderLeftWidth: 3, borderBottomLeftRadius: 10 }} />
      <Corner style={{ bottom: 18, right: 18, borderBottomWidth: 3, borderRightWidth: 3, borderBottomRightRadius: 10 }} />
    </View>
  );
}

function Corner(props: { style: Record<string, number> }) {
  return <View style={{ position: "absolute", pointerEvents: "none", width: 26, height: 26, borderColor: LIGHT, opacity: 0.9, ...props.style }} />;
}

/** The finder is a dark window in both themes, so its button is light on dark in both. */
function FinderButton(props: { testID: string; label: string; onPress(): void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} accessibilityLabel={props.label}
      class="min-h-11 justify-center rounded-full px-[18px] py-2.5 active:opacity-80" style={{ backgroundColor: LIGHT }}>
      <Text class="text-sm font-semibold" style={{ color: DARK }}>{props.label}</Text>
    </Button>
  );
}
