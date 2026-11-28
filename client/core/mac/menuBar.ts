import type { PetState } from "../settings/petPreview";

/** Which image the menu bar cat shows (client/ui/mac/assets/menubar, DESIGN-MAC.md §2.1). */
export type MenuBarImage = "quiet" | "offline" | "arc" | "needs";

export interface MenuBarItem {
  image: MenuBarImage;
  /** Requests waiting on the person: the count beside the face with the seal dot; 0 for none. */
  needs: number;
}

/**
 * The menu bar cat from the header cat's state (core/pets/cat.ts), highest first: away (no computer, offline, pair
 * again, Brain can't start) is the face hollow; something needing the person is the face with the seal dot and the
 * count; Brain's own turn turns the arc; else (connecting, Workers running, quiet) the face.
 */
export function menuBarItem(input: { cat: PetState; needs: number }): MenuBarItem {
  if (input.cat === "homeless" || input.cat === "offline") return { image: "offline", needs: 0 };
  if (input.cat === "attention" && input.needs > 0) return { image: "needs", needs: input.needs };
  if (input.cat === "working") return { image: "arc", needs: 0 };
  return { image: "quiet", needs: 0 };
}

/** A reaction the cat plays once (P3): it never loops; only the arc turns, and only while Brain's turn runs. */
export type MenuBarReaction = "arrive" | "nod";

/**
 * The reaction on the way from one state to the next: something starting to need the person lifts the face and
 * stamps the seal dot (arrive); a Work coming back done nods the face once (nod). None on the first state the app
 * sees, so a launch does not react to what was already there.
 */
export function menuBarReaction(
  from: { item: MenuBarItem; cat: PetState } | null,
  to: { item: MenuBarItem; cat: PetState },
): MenuBarReaction | null {
  if (!from) return null;
  if (to.item.image === "needs" && from.item.image !== "needs") return "arrive";
  if (to.cat === "delivered" && from.cat !== "delivered" && to.item.image === "quiet") return "nod";
  return null;
}

/** When the count joins the face during the arrival: after the lift and the stamp. */
export const ARRIVE_COUNT_MS = 240;

/**
 * The perch's frame (DESIGN-MAC.md §2.2): a 380 × 560 panel (min 340 × 420), radius 20, a 50 header, with the person's
 * pet (48) perched at (6, −19) on its top edge. The window is the panel and the pet's overhang above it.
 */
export const PERCH = { width: 380, height: 560, minWidth: 340, minHeight: 420, radius: 20, header: 50, overhang: 19, cat: 48, catX: 6 } as const;

/**
 * Where the perch's window opens: the panel's top 23 pt under the menu bar (the display's visible area starts under
 * it), its left edge 30 pt left of the item's centre (the perched pet's centre under the item's), kept 8 pt inside
 * the screen.
 */
export function perchOrigin(
  item: { x: number; y: number; width: number; height: number },
  area: { x: number; y: number; width: number; height: number },
  width: number = PERCH.width,
): { x: number; y: number } {
  const left = Math.round(item.x + item.width / 2 - 30);
  return {
    x: Math.min(Math.max(left, area.x + 8), Math.max(area.x + 8, area.x + area.width - 8 - width)),
    y: area.y + 23 - PERCH.overhang,
  };
}

/** What Stop Mewla on this Mac… says stops (DESIGN-MAC.md §2.4). */
export function stopWords(running: number): string {
  const reach = "phones can't reach this Mac until Mewla starts again.";
  if (running <= 0) return reach[0]!.toUpperCase() + reach.slice(1);
  return `${running} running ${running === 1 ? "Session stops" : "Sessions stop"} and ${reach}`;
}

/** The peek (DESIGN-MAC.md §2.1): for 6 s the item's title says who asks and what, then it goes back to the count. */
export const PEEK_MS = 6_000;
/** About 300 pt of the menu bar's 13 pt text. */
const PEEK_CHARS = 48;

/**
 * The peek for the first request in `needs` not in `seen` (each request peeks once), or null: "<source> · <what is
 * asked>", cut to fit with an ellipsis.
 */
export function peekOf(seen: ReadonlySet<string>, needs: readonly { key: string; source: string; title: string; ask?: string }[]): { key: string; words: string } | null {
  const request = needs.find((need) => !seen.has(need.key));
  if (!request) return null;
  const words = `${request.source} · ${request.ask || request.title}`;
  return { key: request.key, words: words.length > PEEK_CHARS ? `${words.slice(0, PEEK_CHARS - 1).trimEnd()}…` : words };
}
