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

/** The line the computer logs: kind, version, the error and the first frame that names it. */
export function crashSummary(report: CrashReport): string {
  const [header, ...rest] = report.text.split(/\n\s*\n/);
  const version = /^Mewla [^\n]*?\)/.exec(header ?? "")?.[0]?.replace(/^Mewla /, "") ?? "";
  const body = rest.join("\n\n").split("\n").map((line) => line.trim()).filter(Boolean);
  const message = body.find((line) => !/^Uncaught exception on thread/.test(line)) ?? body[0] ?? "";
  const frame = body.find((line) => line !== message && /^(at |#\d|\d+\s+\S+\s+0x|[\w$.]+@|.*\.(kt|java|swift|tsx?):\d)/.test(line)) ?? "";
  const summary = [report.kind, version, message, frame ? `@ ${frame}` : ""].filter(Boolean).join(" ");
  return summary.length > SUMMARY_LIMIT ? `${summary.slice(0, SUMMARY_LIMIT - 1)}…` : summary;
}

/** The frame the daemon's `client_diagnostic` handler logs. */
export function crashDiagnostic(report: CrashReport): { type: "client_diagnostic"; name: string; request_id: string; body: string } {
  return { type: "client_diagnostic", name: "app_crash", request_id: report.id.slice(0, 80), body: crashSummary(report) };
}
