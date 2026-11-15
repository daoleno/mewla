/**
 * What the hosts kept about a crash or an error (`app.crashReports`): the app shows them on the
 * next start, with Copy, and hands a one-line summary to the paired computer as a
 * `client_diagnostic` (the daemon logs 300 characters of it). The whole trace stays on the phone.
 */
export type CrashKind = "java" | "native" | "anr" | "js";

export interface CrashReport {
  /** The host's file name: what `app.clearCrashReports` takes back. */
  id: string;
  kind: CrashKind;
  /** When it happened, ms since the epoch. */
  at: number;
  /** A header (app version and build, device, system) then the trace, as the host wrote it. */
  text: string;
}

const KINDS: readonly CrashKind[] = ["java", "native", "anr", "js"];

/** The host's answer, kept to what is well formed. */
export function readCrashReports(value: unknown): CrashReport[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    const report = item as Partial<CrashReport> | null;
    if (!report || typeof report.id !== "string" || typeof report.text !== "string") return [];
    const kind = KINDS.includes(report.kind as CrashKind) ? (report.kind as CrashKind) : "native";
    const at = typeof report.at === "number" && Number.isFinite(report.at) ? report.at : 0;
    return [{ id: report.id, kind, at, text: report.text }];
  }).sort((a, b) => b.at - a.at);
}

/**
 * A report of a run that ended: the system closed the app (a Java or native crash), or its JS engine was stopped (out
 * of memory). A JS error, NSH's 2 s cut-off ("Error: interrupted") among them, left the app running, and an ANR's app
 * was reopened.
 */
export function endedTheRun(report: CrashReport): boolean {
  if (report.kind === "java" || report.kind === "native") return true;
  return report.kind === "js" && /ran out of memory|was stopped/i.test(report.text.slice(0, 2000));
}

/** Runs that ended this close together are a crash loop: the app cannot go on, and the person is the one to tell us. */
export const CRASH_LOOP = { runs: 3, withinMs: 10 * 60_000 };

/**
 * What the person is shown on the next start: a crash loop's reports, else nothing. Everything the hosts kept goes to
 * the computer either way (`crashDiagnostic`); a report the app survived, or one crash, is ours to read there, not a
 * sheet over the app the person just opened (owner 11-14: eight 2 s cut-offs, each a "Mewla hit an error").
 */
export function crashesToShow(reports: readonly CrashReport[]): CrashReport[] {
  const ended = reports.filter(endedTheRun).sort((a, b) => a.at - b.at);
  for (let index = 0; index + CRASH_LOOP.runs - 1 < ended.length; index += 1) {
    if (ended[index + CRASH_LOOP.runs - 1]!.at - ended[index]!.at <= CRASH_LOOP.withinMs) {
      return ended.slice().sort((a, b) => b.at - a.at);
    }
  }
  return [];
}

/**
 * Whether the hosts keep a report the computer already has: a run that ended stays for as long as it can still be part
 * of a loop. A loop's runs reconnect in between, so one crash forgotten once sent would never be counted with the next.
 */
export function keptForLoop(report: CrashReport, now: number): boolean {
  return endedTheRun(report) && now - report.at <= CRASH_LOOP.withinMs;
}

/** What the sheet is titled: a crash closed the app; a JS error only broke a screen. */
export function crashTitle(reports: readonly CrashReport[]): string {
  return reports.some((report) => report.kind !== "js") ? "Mewla closed unexpectedly" : "Mewla hit an error";
}

export function crashKindLabel(kind: CrashKind): string {
  switch (kind) {
    case "java": return "Crash";
    case "native": return "Native crash";
    case "anr": return "Stopped responding";
    case "js": return "App error";
  }
}

const SUMMARY_LIMIT = 300;

/** The line the computer logs: kind, version, where the app was (front), the error and the first frame that names it. */
export function crashSummary(report: CrashReport): string {
  const [header, ...rest] = report.text.split(/\n\s*\n/);
  const version = /^Mewla [^\n]*?\)/.exec(header ?? "")?.[0]?.replace(/^Mewla /, "") ?? "";
  const body = rest.join("\n\n").split("\n").map((line) => line.trim()).filter(Boolean);
  const message = body.find((line) => !/^Uncaught exception on thread/.test(line)) ?? body[0] ?? "";
  const frame = body.find((line) => line !== message && /^(at |#\d|\d+\s+\S+\s+0x|[\w$.]+@|.*\.(kt|java|swift|tsx?):\d)/.test(line)) ?? "";
  const summary = [report.kind, version, frontOf(header ?? ""), message, frame ? `@ ${frame}` : ""].filter(Boolean).join(" ");
  return summary.length > SUMMARY_LIMIT ? `${summary.slice(0, SUMMARY_LIMIT - 1)}…` : summary;
}

/**
 * Where the app was when it happened, as the host's header says it (CrashLog.kt / CrashLog.swift): "[front 12s]",
 * "[background 37s]", "[background 37s, never front]". NSH counts the time the system keeps the app frozen against
 * the call it froze, so a 2 s cut-off in the background, or in the first seconds back in front, is a freeze's.
 */
function frontOf(header: string): string {
  const line = /^In (front|the background) for (\d+) s(, not in front since it started)?$/m.exec(header);
  if (!line) return "";
  return `[${line[1] === "front" ? "front" : "background"} ${line[2]}s${line[3] ? ", never front" : ""}]`;
}

/** The frame the daemon's `client_diagnostic` handler logs. */
export function crashDiagnostic(report: CrashReport): { type: "client_diagnostic"; name: string; request_id: string; body: string } {
  return { type: "client_diagnostic", name: "app_crash", request_id: report.id.slice(0, 80), body: crashSummary(report) };
}
