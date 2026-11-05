import type { AgentUseFix, AgentUseStatus } from "../../services/agentUse";
import { compareVersions } from "../../services/appUpdate";
import type { DaemonUpdateStatus } from "../../services/daemonUpdate";
import type { ComputerCheck } from "../../services/computerTeam";
import type { ConnectionIssueCode } from "../../services/connectionIssue";
import type { ConnectionState } from "../../store/workers";

/** One word for a computer's row: what the user would want to know first. */
export function computerStatusWord(input: {
  current: boolean;
  connection: ConnectionState | undefined;
  issue: ConnectionIssueCode | null | undefined;
  check: ComputerCheck | undefined;
}): string {
  if (input.current) {
    if (input.connection === "connected") return "In use";
    if (input.issue) return input.issue === "device_not_paired" ? "Pair again" : "Can't connect";
    if (input.connection === "connecting") return "Connecting…";
    // Turned off on this phone; the probe answering doesn't make it in use.
    return "Not connected";
  }
  switch (input.check?.state) {
    case "online":
      return "Online";
    case "unpaired":
      return "Pair again";
    case "offline":
      return "Offline";
    default:
      return "";
  }
}

/** What a problem's one button does. */
export type ComputerAction =
  | { kind: "fix"; fix: AgentUseFix; target: "browser" | "computer" }
  | { kind: "paste_key" }
  | { kind: "stop_agent" }
  | { kind: "update" };

export interface ComputerProblem {
  key: string;
  text: string;
  action?: { label: string; does: ComputerAction; destructive?: boolean };
}

/**
 * The things on a computer someone can act on, one line each. States nothing
 * can change from here (a headless server without a desktop, a screen agents
 * can't use on this system, tools still getting ready) show nothing, and so do
 * a missing Chrome or Node.js: they are not a problem for someone who never
 * started browser setup. Browser use stays reachable through one quiet row.
 */
export function computerProblems(status: AgentUseStatus | null): ComputerProblem[] {
  if (!status) return [];
  const problems: ComputerProblem[] = [];
  const active = status.computer?.active;
  const everyone = status.computer?.agents ?? [];
  if (active) {
    problems.push({
      key: "active",
      text: everyone.length > 1
        ? `${everyone.map((agent) => agent.name || "An agent").join(", ")} are using the screen.`
        : `${active.name || "An agent"} is using the screen.`,
      action: { label: "Stop", does: { kind: "stop_agent" }, destructive: true },
    });
  }
  if (!status.enabled) return problems;

  const browser = status.browser;
  switch (browser.state) {
    case "no_extension":
      // Not a problem: nobody has started browser setup. One plain row is the
      // way in; the person who taps it is then walked through the rest.
      problems.push({
        key: "browser",
        text: "Let agents use Chrome",
        action: { label: "Set up", does: { kind: "fix", fix: browser.fix ?? "install_extension", target: "browser" } },
      });
      break;
    case "no_key":
      // Mewla does not read the key out of Chrome: the extension shows it and
      // the person hands it over.
      if (browser.fix === "install_extension") {
        problems.push({
          key: "browser",
          text: "Add the Mewla extension to Chrome. It needs no key.",
          action: { label: "Set up", does: { kind: "fix", fix: browser.fix, target: "browser" } },
        });
      } else if (browser.fix) {
        problems.push({
          key: "browser",
          text: "Open the Playwright extension in Chrome and copy its PLAYWRIGHT_MCP_EXTENSION_TOKEN.",
          action: { label: "Open it", does: { kind: "fix", fix: browser.fix, target: "browser" } },
        });
      }
      problems.push({
        key: "browser-key",
        text: "Or, with the older Playwright extension, run mewla agent-use key on that computer, or paste its key here.",
        action: { label: "Paste key", does: { kind: "paste_key" } },
      });
      break;
    case "failed":
      problems.push({
        key: "browser",
        text: "The browser tools didn't install.",
        action: { label: "Try again", does: { kind: "fix", fix: browser.fix ?? "retry", target: "browser" } },
      });
      break;
    default:
      // ready, off, preparing, no_desktop, no_chrome, no_node: nothing to show.
      break;
  }

  const computer = status.computer;
  if (computer?.state === "permission" && computer.fix) {
    problems.push({
      key: "computer",
      text: `Allow Mewla ${computer.permission ? `${computer.permission} access` : "to use the screen"} on this Mac.`,
      action: { label: "Open Settings", does: { kind: "fix", fix: computer.fix, target: "computer" } },
    });
  }
  return problems;
}

/** Whether anything shown can change while the page is open. */
export function computerProblemsMayChange(status: AgentUseStatus | null): boolean {
  if (!status) return false;
  if (status.computer?.active) return true;
  if (!status.enabled) return false;
  return (
    ["no_extension", "no_key", "preparing", "failed"].includes(status.browser.state) ||
    status.computer?.state === "permission"
  );
}

/**
 * The computer's Mewla needs, or is getting, an update: one line, and the one
 * button when it can update itself. An app and computer that no longer fit
 * together say so with the same button.
 */
export function computerUpdateProblem(update: DaemonUpdateStatus | null, minVersion: string): ComputerProblem | null {
  if (!update) return null;
  const button = update.supported ? { label: "Update", does: { kind: "update" } as ComputerAction } : undefined;
  if (update.updating) return { key: "update", text: "Updating…" };
  if (compareVersions(update.version, minVersion) === -1) {
    return { key: "update", text: "This computer needs an update to work with this app.", action: button };
  }
  if (update.latest && update.supported) {
    return { key: "update", text: `Update available · ${update.latest}`, action: button };
  }
  if (update.error) return { key: "update", text: update.error };
  return null;
}
