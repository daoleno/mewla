import type { AgentUseFix, AgentUseStatus } from "../../services/agentUse";
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
  }
  switch (input.check?.state) {
    case "online":
      return "Online";
    case "unpaired":
      return "Pair again";
    case "offline":
      return "Offline";
    default:
      return input.current ? "Offline" : "";
  }
}

/** What a problem's one button does. */
export type ComputerAction =
  | { kind: "fix"; fix: AgentUseFix; target: "browser" | "computer" }
  | { kind: "paste_key" }
  | { kind: "stop_agent" };

export interface ComputerProblem {
  key: string;
  text: string;
  action?: { label: string; does: ComputerAction; destructive?: boolean };
}

/**
 * The things on a computer someone can act on, one line each. States nothing
 * can change from here (a headless server without a desktop, a screen agents
 * can't use on this system, tools still getting ready) show nothing.
 */
export function computerProblems(status: AgentUseStatus | null): ComputerProblem[] {
  if (!status) return [];
  const problems: ComputerProblem[] = [];
  const active = status.computer?.active;
  if (active) {
    problems.push({
      key: "active",
      text: `${active.name || "An agent"} is using the screen.`,
      action: { label: "Stop", does: { kind: "stop_agent" }, destructive: true },
    });
  }
  if (!status.enabled) return problems;

  const browser = status.browser;
  switch (browser.state) {
    case "no_extension":
      problems.push({
        key: "browser",
        text: "Chrome needs the Playwright extension.",
        action: { label: "Add extension", does: { kind: "fix", fix: browser.fix ?? "install_extension", target: "browser" } },
      });
      break;
    case "no_key":
      problems.push({
        key: "browser",
        text: "Open the Playwright extension once in Chrome.",
        action: browser.fix
          ? { label: "Open it", does: { kind: "fix", fix: browser.fix, target: "browser" } }
          : { label: "Paste key", does: { kind: "paste_key" } },
      });
      break;
    case "no_chrome":
      problems.push({ key: "browser", text: "Install Google Chrome so agents can use a browser." });
      break;
    case "no_node":
      problems.push({ key: "browser", text: "Install a recent Node.js so agents can use a browser." });
      break;
    case "failed":
      problems.push({
        key: "browser",
        text: "The browser tools didn't install.",
        action: { label: "Try again", does: { kind: "fix", fix: browser.fix ?? "retry", target: "browser" } },
      });
      break;
    default:
      // ready, off, preparing, no_desktop: nothing to do from here.
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
