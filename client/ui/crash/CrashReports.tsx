import { Clipboard, host, Scroll, signal, Storage, Text, View } from "nsh";
import { Button, Sheet, TextView } from "nsh/base";
import { crashDiagnostic, crashesToShow, crashKindLabel, crashTitle, keptForLoop, readCrashReports, type CrashReport } from "../../core/crash";
import { current, onTransport } from "../session";
import { sheetSides } from "../shell/AppSheet";

/** The reports the sheet shows (a crash loop's, core/crash.ts `crashesToShow`), until the person dismisses them. */
export const crashReports = signal<CrashReport[]>([]);
/** Every report the hosts kept: each goes to the computer once. */
let kept: CrashReport[] = [];
const SENT_KEY = "mewla:v1:crash_reports_sent";

/**
 * Reads what the hosts kept (once, at start) and sends each report's summary to the computer once. Only a crash loop
 * is the person's to see; the rest the hosts forget once the computer has them.
 */
export async function loadCrashReports(): Promise<void> {
  if (!host.available()) return;
  kept = readCrashReports(await host.call("app.crashReports").catch(() => []));
  crashReports.set(crashesToShow(kept));
  if (kept.length) onTransport(sendWhenConnected);
}

function sendWhenConnected(transport: Parameters<Parameters<typeof onTransport>[0]>[0]) {
  const send = async () => {
    const sent = new Set<string>(JSON.parse((await Storage.getItem(SENT_KEY).catch(() => null)) ?? "[]"));
    const fresh = kept.filter((report) => !sent.has(report.id));
    try {
      for (const report of fresh) {
        transport.send(crashDiagnostic(report));
        sent.add(report.id);
      }
    } catch {
      return; // not connected after all: the next connection tries again
    }
    await Storage.setItem(SENT_KEY, JSON.stringify([...sent].slice(-32))).catch(() => {});
    // The computer has them: the hosts keep only what the sheet shows, until it is dismissed, and a run that ended
    // while it can still be part of a loop.
    const shown = new Set(crashReports().map((report) => report.id));
    const now = Date.now();
    const told = kept.filter((report) => sent.has(report.id) && !shown.has(report.id) && !keptForLoop(report, now)).map((report) => report.id);
    kept = kept.filter((report) => !told.includes(report.id));
    if (told.length) await host.call("app.clearCrashReports", { ids: told }).catch(() => {});
  };
  const off = transport.on("connected", () => void send());
  if (transport.state === "connected") void send();
  return off;
}

async function dismiss(): Promise<void> {
  const ids = crashReports().map((report) => report.id);
  crashReports.set([]);
  await host.call("app.clearCrashReports", { ids }).catch(() => {});
}

function when(at: number): string {
  if (!at) return "";
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/**
 * A crash loop, on the next start: the app cannot go on, and the person must not be left with nothing to hand over.
 * The trace is selectable; Copy takes all of them. Anything else the computer already has.
 */
export function CrashReportSheet() {
  const copied = signal(false);
  const all = () => crashReports().map((report) => report.text).join("\n\n----\n\n");
  return (
    <Sheet open={crashReports().length > 0} onOpenChange={({ open }) => { if (!open) void dismiss(); }}
      closeOnSwipe popupClass="rounded-t-2xl bg-background pt-4 pb-6 gap-3" popupStyle={sheetSides(16)} backdropClass="bg-black/40">
      <View testID="crash-sheet" class="gap-1">
        <Text role="header" class="text-xl font-bold text-foreground">{crashTitle(crashReports())}</Text>
        <Text class="text-sm text-muted-foreground">
          {current() ? `The details below were also sent to ${current()!.name || "your computer"}. ` : ""}Copy them and send them to us if this keeps happening.
        </Text>
      </View>
      <Scroll keyboardShouldPersistTaps="handled" class="max-h-96 rounded-xl border border-border bg-card">
        <View class="gap-3 p-3">
          {crashReports().map((report) => (
            <View class="gap-1">
              <Text class="text-sm font-semibold text-foreground">{crashKindLabel(report.kind)}{report.at ? ` · ${when(report.at)}` : ""}</Text>
              <TextView selectable document={{ blocks: [{ type: "codeBlock", code: report.text }] }} class="text-xs" />
            </View>
          ))}
        </View>
      </Scroll>
      <View class="flex-row gap-2">
        <Button testID="crash-copy" onPress={() => void Clipboard.writeText(all()).then(() => { copied.set(true); setTimeout(() => copied.set(false), 1_600); })}
          class="flex-1 rounded-xl border border-border px-4 py-3 active:bg-accent">
          <Text class="text-center text-base font-semibold text-foreground">{copied() ? "Copied" : "Copy"}</Text>
        </Button>
        <Button testID="crash-dismiss" onPress={() => void dismiss()} class="flex-1 rounded-xl bg-primary px-4 py-3 active:opacity-80">
          <Text class="text-center text-base font-semibold text-primary-foreground">Dismiss</Text>
        </Button>
      </View>
    </Sheet>
  );
}
