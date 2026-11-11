/**
 * A failure of the agent displays (Linux Screen) in product words: a title, what to do,
 * and how loud. The raw text stays in `diagnostic`. The rules read the message only, as
 * v0.3.1 did: its display errors never carried a status into them.
 */
export interface DisplayIssue {
  title: string;
  detail: string;
  tone: "warning" | "danger";
  diagnostic: string;
}

export function displayIssue(error: unknown, server: string): DisplayIssue | null {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if ((error instanceof Error && error.name === "AbortError") || /cancelled/i.test(message)) return null;
  const diagnostic = message || "No details";
  const issue = (title: string, detail: string, tone: DisplayIssue["tone"] = "danger"): DisplayIssue => ({ title, detail, tone, diagnostic });
  if (/controlled by someone else/i.test(message)) return issue("Someone else is in control", "Another device or an Agent is using it.", "warning");
  if (/control changed|take control before/i.test(message)) return issue("You're no longer in control", "Another device or an Agent took over.", "warning");
  if (/could not be drained|needs restart/i.test(message)) return issue("This window needs a fresh start", "Stop the agent; its next browser action opens a new window. Sign-ins are kept.");
  if (/browser not found|invalid browser resource/i.test(message)) return issue("This window was closed", "The agent's next browser action opens a new one.", "warning");
  if (/input is busy/i.test(message)) return issue("Still sending your last action", "Wait a moment, then try again.", "warning");
  if (/timed out|disconnected|viewer closed|connection failed/i.test(message)) return issue("Lost connection to the browser", `Still open on ${server}.`, "warning");
  // Offline is a warning, never a failure: nothing broke on the server. A request that
  // ran out of time says "did not answer" here.
  if (error instanceof TypeError || /network request failed|failed to fetch|did not answer/i.test(message)) {
    return issue(`Can't reach ${server}`, "Check the server and this phone's connection.", "warning");
  }
  return issue("That didn't finish", "If it keeps failing, check Connection details in the options menu.");
}
