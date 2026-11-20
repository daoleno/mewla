import { cleanup, Clipboard, effect, Image, Share, signal, Text, untracked, View } from "nsh";
import { Button, Dialog } from "nsh/base";
import { Icon } from "../kit/Icon";
import { Segmented } from "../kit/Segmented";
import mark from "./assets/mark.png";
import { daemonPairing, showMainWindow, type DaemonPairing } from "./local";

/** Pair a phone (DESIGN-MAC.md §2.4, `w-pair`): a 440 dialog over the window, radius 22, padding 20. */
export const pairPhoneOpen = signal(false);
const SIDE = 200;
const CARD = 220;

type Way = "anywhere" | "wifi";
type Code = NonNullable<DaemonPairing["phone_qr"]>;

/** The QR as the daemon encoded it (level Q): dark modules on white in light and dark alike, the mark in its centre. */
function QrCode(props: { qr: Code }) {
  const module = SIDE / props.qr.size;
  const markSide = Math.round(SIDE * 0.2);
  return (
    <View testID="pair-phone-qr" class="items-center justify-center rounded-[20px]" style={{ width: CARD, height: CARD, backgroundColor: "#ffffff" }}>
      <View style={{ width: SIDE, height: SIDE }}>
        {props.qr.rows.map((row) => (
          <View style={{ flexDirection: "row", height: module }}>
            {[...row].map((dark) => <View style={{ width: module, height: module, backgroundColor: dark === "1" ? "#000000" : "#ffffff" }} />)}
          </View>
        ))}
      </View>
      <View class="absolute items-center justify-center rounded-full" style={{ width: markSide + 6, height: markSide + 6, backgroundColor: "#ffffff" }}>
        <Image src={mark} style={{ width: markSide, height: markSide }} />
      </View>
    </View>
  );
}

/** "9:41 left" of a code that ends at `expires`. */
function left(expires: string, now: number): string {
  const seconds = Math.max(0, Math.round((Date.parse(expires) - now) / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")} left`;
}

function PairPhone() {
  const pairing = signal<DaemonPairing | null>(null);
  const problem = signal("");
  const way = signal<Way>("anywhere");
  const now = signal(Date.now());
  const copied = signal(false);
  const load = () => {
    problem.set("");
    void daemonPairing()
      .then((next) => pairing.set(next))
      .catch((error) => problem.set(error instanceof Error ? error.message : String(error)));
  };
  load();
  // A code works once until it runs out: a new one then.
  effect(() => {
    const expires = pairing()?.expires_at;
    if (!expires) return;
    const timer = setTimeout(load, Math.max(5_000, Date.parse(expires) - Date.now()));
    cleanup(() => clearTimeout(timer));
  });
  const tick = setInterval(() => now.set(Date.now()), 1_000);
  cleanup(() => clearInterval(tick));

  const link = () => (way() === "wifi" ? pairing()?.wifi_link : pairing()?.phone_link) ?? "";
  const code = () => (way() === "wifi" ? pairing()?.wifi_qr : pairing()?.phone_qr) ?? null;
  const ways = () => [
    { key: "anywhere" as const, label: "Anywhere" },
    ...(pairing()?.wifi_link ? [{ key: "wifi" as const, label: "This Wi‑Fi" }] : []),
  ];
  const copy = () => {
    const value = untracked(link);
    if (!value) return;
    void Clipboard.writeText(value).then(() => {
      copied.set(true);
      setTimeout(() => copied.set(false), 1_600);
    });
  };

  const send = () => {
    const value = untracked(link);
    if (value) void Share.show({ title: "Pair with Mewla", url: value }).catch((error) => console.warn("send to phone:", error));
  };

  return (
    <View testID="pair-phone" class="gap-4 bg-sheet-surface text-foreground" style={{ padding: 20 }}>
      <View class="flex-row items-center">
        <Text role="header" class="flex-1 font-bold" style={{ fontSize: 17 }}>Pair a phone</Text>
        <Button testID="pair-phone-close" accessibilityLabel="Close" onPress={() => pairPhoneOpen.set(false)} class="size-7 items-center justify-center rounded-full hover:bg-accent">
          <Icon name="close" size={16} tone="soft" />
        </Button>
      </View>
      <View class="items-center gap-3">
        {/* Made again for each code and way (a component runs once). */}
        <If when={code()} else={<View class="items-center justify-center" style={{ height: CARD }}><Text class="text-sub text-muted-foreground">{problem() || (pairing() ? "Phones can't reach this Mac yet." : "…")}</Text></View>}>
          <Each of={code() ? [`${pairing()!.expires_at}:${way()}`] : []} key={(key) => key}>{() => <QrCode qr={untracked(code)!} />}</Each>
        </If>
        <Text testID="pair-phone-left" class="text-[13px] text-muted-foreground">{pairing() ? `Scan with the Mewla app · ${left(pairing()!.expires_at, now())}` : " "}</Text>
      </View>
      <If when={ways().length > 1}>
        <Segmented testID="pair-phone-way" options={ways()} value={way()} onChange={(next) => way.set(next)} accessibilityLabel="How the phone reaches this Mac" />
      </If>
      <View class="flex-row gap-2">
        <Button testID="pair-phone-copy" onPress={copy} disabled={!link()} class="h-[30px] flex-1 flex-row items-center justify-center gap-1.5 rounded-[9px] bg-accent hover:opacity-80 active:opacity-70 disabled:opacity-40">
          <Icon name="copy" size={14} />
          <Text class="text-[13px] font-semibold text-foreground">{copied() ? "Copied" : "Copy link"}</Text>
        </Button>
        <Button testID="pair-phone-send" onPress={send} disabled={!link()} class="h-[30px] flex-1 flex-row items-center justify-center gap-1.5 rounded-[9px] bg-accent hover:opacity-80 active:opacity-70 disabled:opacity-40">
          <Icon name="share" size={14} />
          <Text class="text-[13px] font-semibold text-foreground">Send to phone</Text>
        </Button>
      </View>
    </View>
  );
}

/** The dialog, over the window (mounted by the desktop overlays on the Mac). */
export function PairPhoneDialog() {
  return (
    <Dialog testID="pair-phone-dialog" open={pairPhoneOpen()} animation="fade" onOpenChange={({ open }) => pairPhoneOpen.set(open)}
      class="w-[440px] max-w-[92%] self-center my-auto overflow-hidden rounded-[22px] shadow-float" backdropClass="bg-scrim">
      {/* Made each time it opens: a fresh code. */}
      <If when={pairPhoneOpen()}><PairPhone /></If>
    </Dialog>
  );
}

/** File › Pair a Phone…, the cat's menu, This Mac: the window comes forward with the dialog over it. */
export function openPairPhone(): void {
  showMainWindow();
  pairPhoneOpen.set(true);
}
