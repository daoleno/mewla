import { placeLabel, type PlaceKey } from "./places";
import { goShortcutLabel } from "./shortcuts";

/** One row of the ⌘K palette. Pure data; the shell runs `action`. */
export type PaletteAction =
  | { kind: "go"; key: PlaceKey }
  | { kind: "session"; workerId: string }
  | { kind: "work"; workId: string; sessionId?: string }
  | { kind: "new-session" }
  | { kind: "new-chat" }
  | { kind: "brain-terminal" }
  | { kind: "browse-workspace" }
  | { kind: "switch-executor" }
  | { kind: "shortcuts" };

export type PaletteSection = "Actions" | "Go to" | "Work" | "Sessions";

export interface PaletteItem {
  id: string;
  section: PaletteSection;
  label: string;
  detail?: string;
  /** As people read it (`N`, `G then C`). */
  shortcut?: string;
  action: PaletteAction;
}

/** Go to: the places with a `G` key, in the shortcuts sheet's order. */
const PLACES: readonly PlaceKey[] = ["brain", "sessions", "calendar", "computer", "screen", "extensions", "stats", "resources", "settings"];

export interface PaletteSession {
  id: string;
  name: string;
  detail?: string;
}

export interface PaletteWork {
  workId: string;
  title: string;
  statusLabel: string;
  sessionId?: string;
}

/** Actions first (SPEC §14.3), then places, Work and Sessions of the current computer. */
export function buildPaletteItems(input: { sessions: readonly PaletteSession[]; work: readonly PaletteWork[] }): PaletteItem[] {
  const items: PaletteItem[] = [
    { id: "action:new-session", section: "Actions", label: "New session", shortcut: "N", action: { kind: "new-session" } },
    { id: "action:new-chat", section: "Actions", label: "New chat", action: { kind: "new-chat" } },
    { id: "action:brain-terminal", section: "Actions", label: "Open Brain's terminal", action: { kind: "brain-terminal" } },
    { id: "action:browse-workspace", section: "Actions", label: "Browse workspace", action: { kind: "browse-workspace" } },
    { id: "action:switch-executor", section: "Actions", label: "Switch executor", action: { kind: "switch-executor" } },
    { id: "action:shortcuts", section: "Actions", label: "Keyboard shortcuts", shortcut: "?", action: { kind: "shortcuts" } },
  ];
  for (const key of PLACES) {
    items.push({ id: `go:${key}`, section: "Go to", label: placeLabel(key), shortcut: goShortcutLabel(key), action: { kind: "go", key } });
  }
  for (const work of input.work) {
    items.push({ id: `work:${work.workId}`, section: "Work", label: work.title, detail: work.statusLabel, action: { kind: "work", workId: work.workId, sessionId: work.sessionId } });
  }
  for (const session of input.sessions) {
    items.push({ id: `session:${session.id}`, section: "Sessions", label: session.name, detail: session.detail, action: { kind: "session", workerId: session.id } });
  }
  return items;
}

function normalize(value: string): string {
  return value.toLocaleLowerCase().normalize("NFKC");
}

/**
 * Each query word must appear in the label, detail or section. Label prefix
 * matches rank first, then label matches, then the rest; ties keep order.
 */
export function filterPaletteItems(items: readonly PaletteItem[], query: string): PaletteItem[] {
  const words = normalize(query).split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...items];
  const ranked: Array<{ item: PaletteItem; rank: number; index: number }> = [];
  items.forEach((item, index) => {
    const label = normalize(item.label);
    const haystack = `${label} ${normalize(item.detail ?? "")} ${normalize(item.section)}`;
    if (!words.every((word) => haystack.includes(word))) return;
    const rank = label.startsWith(words[0]) ? 0 : words.every((word) => label.includes(word)) ? 1 : 2;
    ranked.push({ item, rank, index });
  });
  ranked.sort((a, b) => a.rank - b.rank || a.index - b.index);
  return ranked.map((entry) => entry.item);
}
