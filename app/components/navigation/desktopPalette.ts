import type { DesktopSidebarKey } from "./desktopWeb";
import { goShortcutLabel } from "./desktopShortcuts";

/** One row of the ⌘K palette. Pure data; the shell runs `action`. */
export type PaletteAction =
  | { kind: "go"; key: DesktopSidebarKey }
  | { kind: "session"; workerId: string }
  | { kind: "work"; workId: string; sessionId?: string }
  | { kind: "new-session" }
  | { kind: "new-brain-chat" }
  | { kind: "shortcuts" };

export interface PaletteItem {
  id: string;
  section: "Go to" | "Sessions" | "Work" | "Actions";
  label: string;
  detail?: string;
  shortcut?: string;
  action: PaletteAction;
}

const PLACES: ReadonlyArray<{ key: DesktopSidebarKey; label: string }> = [
  { key: "brain", label: "Brain" },
  { key: "sessions", label: "Sessions" },
  { key: "calendar", label: "Calendar" },
  { key: "plugins", label: "Plugins" },
  { key: "skills", label: "Skills" },
  { key: "stats", label: "Stats" },
  { key: "resources", label: "Resources" },
  { key: "settings", label: "Settings" },
];

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

/** Actions first, then places, Work and Sessions of the current server. */
export function buildPaletteItems(input: {
  sessions: readonly PaletteSession[];
  work: readonly PaletteWork[];
}): PaletteItem[] {
  const items: PaletteItem[] = [
    { id: "action:new-session", section: "Actions", label: "New session", shortcut: "N", action: { kind: "new-session" } },
    { id: "action:new-brain-chat", section: "Actions", label: "New Brain chat", action: { kind: "new-brain-chat" } },
    { id: "action:shortcuts", section: "Actions", label: "Keyboard shortcuts", shortcut: "?", action: { kind: "shortcuts" } },
  ];
  for (const place of PLACES) {
    items.push({
      id: `go:${place.key}`,
      section: "Go to",
      label: place.label,
      shortcut: goShortcutLabel(place.key),
      action: { kind: "go", key: place.key },
    });
  }
  for (const work of input.work) {
    items.push({
      id: `work:${work.workId}`,
      section: "Work",
      label: work.title,
      detail: work.statusLabel,
      action: { kind: "work", workId: work.workId, sessionId: work.sessionId },
    });
  }
  for (const session of input.sessions) {
    items.push({
      id: `session:${session.id}`,
      section: "Sessions",
      label: session.name,
      detail: session.detail,
      action: { kind: "session", workerId: session.id },
    });
  }
  return items;
}

function normalize(value: string): string {
  return value.toLocaleLowerCase().normalize("NFKC");
}

/**
 * Each query word must appear in the label or detail. Label prefix matches
 * rank first, then label matches, then detail-only matches; ties keep order.
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

/** Arrow keys wrap around the list. */
export function movePaletteSelection(current: number, delta: number, count: number): number {
  if (count <= 0) return 0;
  return (((current + delta) % count) + count) % count;
}
