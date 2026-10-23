import type { ComputerCheck } from "../../services/computerTeam";
import type { StoredServer } from "../../services/storedServerContract";
import type { ConnectionAttention } from "./connectionAttention";

/** Quiet when healthy; only a state someone can act on is colored. */
export type ComputerTone = "quiet" | "offline" | "issue";

export interface ComputerSwitcherRow {
  id: string;
  name: string;
  current: boolean;
  /** One word, empty when there is nothing to say yet or all is well. */
  status: string;
  tone: ComputerTone;
}

/** What tapping the menu's computer row does. */
export type ComputerRowTap = "settings" | "switcher";

/**
 * With nothing to pick between, the row goes to Settings → Computers, where
 * the first is paired or the only one managed.
 */
export function computerRowTap(computerCount: number): ComputerRowTap {
  return computerCount > 1 ? "switcher" : "settings";
}

/** The tone of a state word on the menu row, the same states that dot ☰. */
export function attentionTone(badge: ConnectionAttention["badge"]): ComputerTone {
  return badge === "issue" ? "issue" : badge === "offline" ? "offline" : "quiet";
}

/**
 * Every paired computer in Settings' order. The one in use says what the menu
 * row says (so ☰, the row and the list agree); the others say what this phone
 * found when it asked them, and stay blank until they answer.
 */
export function computerSwitcherRows(input: {
  servers: readonly StoredServer[];
  currentServerId: string | null | undefined;
  attention: Pick<ConnectionAttention, "badge" | "detail">;
  checks: Readonly<Record<string, ComputerCheck | undefined>>;
}): ComputerSwitcherRow[] {
  return input.servers.map((server) => {
    if (server.id === input.currentServerId) {
      const tone = attentionTone(input.attention.badge);
      return {
        id: server.id,
        name: server.name,
        current: true,
        status: tone === "quiet" ? "" : input.attention.detail,
        tone,
      };
    }
    switch (input.checks[server.id]?.state) {
      case "online":
        return { id: server.id, name: server.name, current: false, status: "Online", tone: "quiet" };
      case "unpaired":
        return { id: server.id, name: server.name, current: false, status: "Pair again", tone: "issue" };
      case "offline":
        return { id: server.id, name: server.name, current: false, status: "Offline", tone: "offline" };
      default:
        return { id: server.id, name: server.name, current: false, status: "", tone: "quiet" };
    }
  });
}

/** The row's spoken label: name, then state, then whether it is in use. */
export function computerRowLabel(row: Pick<ComputerSwitcherRow, "name" | "status" | "current">): string {
  return [row.name, row.status, row.current ? "in use" : ""].filter(Boolean).join(", ");
}
