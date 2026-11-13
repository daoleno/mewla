/**
 * The capsule header (the owner's pick, 11-13; soft-final/SPEC.md §11 "Capsule header", run in
 * site/v/capsule): no bar, one floating shape over the page's content, which collapses while the person reads.
 *
 * Every number the capsule is drawn with is here, so a change of the designer's values is a change of this
 * table only. Sizes in points, times in milliseconds.
 */
export const CAPSULE = {
  /** Below the status bar (the safe area's top), and from the screen's sides. */
  top: 6,
  side: 8,
  /** Expanded: this high, or `heightTwoLines` with a two-line name; the screen's width less the sides. */
  height: 48,
  heightTwoLines: 56,
  /** Collapsed: this high, as wide as what it keeps, at the same top-left. Its radius is always half its height. */
  compactHeight: 44,
  /** Inside, at both ends. */
  padding: 2,
  /** Every control inside: a 44 target. */
  control: 44,
  /** A Session's agent mark, with no base of its own, and the red dot at its corner while it needs you. */
  mark: 22,
  dot: { size: 9, ring: 2, right: -2, bottom: -2 },
  /** Between the mark and the name. */
  markGap: 8,
  /** The name: 17/600; long, two balanced lines of 15/19; collapsed, one line of 15/20, ending in "…" at the
   * smaller of `compactNameMax` and `compactNameShare` of the width. */
  name: { size: 17, line: 22 },
  nameLong: { size: 15, line: 19 },
  nameCompact: { size: 15, line: 20 },
  compactNameMax: 200,
  compactNameShare: 0.52,
  /** A name longer than this many characters takes the two lines (the mock's rule). */
  longName: 16,
  /** Brain's: the cat (a 44 target, the still 32), "Brain" 17/600, and with two or more computers the computer's name
   * under it, 13 soft (the capsule is then 56 high). */
  cat: { size: 32, box: 44 },
  brain: { size: 17, line: 22 },
  computer: { size: 13, line: 16 },
  /** In light, a 1 pt contact shadow where white meets white; in dark none. The edge is a 1 pt hairline. */
  lightShadow: "0px 1px 2px rgba(19, 20, 22, 0.06)",
  /** The status bar's strip: canvas down to `top` below the status bar, then a fade this long (in strips). */
  fade: 12,
  fadeStrips: 6,
  /** The terminal is padded for the collapsed capsule, and this below it. */
  terminalGap: 4,
  /** With the keyboard up, this much window or less above it (a phone held sideways) gives the terminal the
   * collapsed capsule's row: an iPhone SE's terminal was 49 pt high, its prompt hidden (11-14). */
  crampedAboveKeyboard: 320,
  /** A pushed page (Settings, the computer page…): Back alone while the page's own title is on screen; the title
   * (17/600) and the page's controls join Back once the content has scrolled past `titleAway`. Never collapses. */
  pageTitle: { size: 17, line: 22 },
  pageTitleMax: 220,
  titleAway: 50,
  /** How far the person moves the content from where it last came to rest before it collapses. */
  collapseAfter: 24,
  /** A scroll that comes to rest this close to the newest end expands it; nearer than this it never collapses. */
  atEnd: 48,
  /** The one transition (`dur.move`, `ease.move`), and the leaving parts' fade (`dur.fast`). */
  move: 300,
  fast: 120,
} as const;

/** Where the content starts below the screen's top: padded once, for the expanded capsule (6 + 56). */
export function capsuleInset(safeTop: number): number {
  return safeTop + CAPSULE.top + CAPSULE.heightTwoLines;
}

/** Where a pushed page's content starts: below its capsule, which is never two lines and never collapses. */
export function capsulePageInset(safeTop: number): number {
  return safeTop + CAPSULE.top + CAPSULE.height;
}

/** Where a Session's terminal starts: below the collapsed capsule, which an expanded one floats over. */
export function capsuleTerminalInset(safeTop: number): number {
  return safeTop + CAPSULE.top + CAPSULE.compactHeight + CAPSULE.terminalGap;
}

/**
 * The keyboard leaves the terminal too little (a phone held sideways): its capsule steps aside until the keyboard
 * goes down, and the terminal takes the capsule's row (its paddings tighten too: core/terminal/keyboardRoom.ts).
 */
export function crampedOverKeyboard(window: number, keyboard: number): boolean {
  return keyboard > 0 && window - keyboard <= CAPSULE.crampedAboveKeyboard;
}

/** A Session name in two balanced lines rather than one ("Daily Hacker News Briefing · Nov 13"). */
export function capsuleNameIsLong(name: string): boolean {
  return name.length > CAPSULE.longName;
}

/** The collapsed name's widest. */
export function capsuleCompactNameWidth(screenWidth: number): number {
  return Math.min(CAPSULE.compactNameMax, Math.round(screenWidth * CAPSULE.compactNameShare));
}

/**
 * Whether the capsule is collapsed, from the person's own scrolling only. It collapses once they have moved
 * the content more than `collapseAfter` from where it last came to rest, either way, away from the newest
 * end. It expands when a scroll comes to rest within `atEnd` of the newest end, when they tap the collapsed
 * name, and when something needs them. Content that arrives by itself, a streaming reply and a scroll the app
 * makes never collapse it: only a scroll that follows the person's touch counts.
 */
export type CapsuleState = {
  collapsed: boolean;
  /** The person's drag, and the glide it started, is under way. */
  reading: boolean;
  /** Where the content last came to rest; `null` before the list has said. */
  anchor: number | null;
};

export const CAPSULE_OPEN: CapsuleState = { collapsed: false, reading: false, anchor: null };

export function capsuleDragStart(state: CapsuleState): CapsuleState {
  return state.reading ? state : { ...state, reading: true };
}

/** The list scrolled to `offset`, `fromEnd` points from its newest end. */
export function capsuleScroll(state: CapsuleState, offset: number, fromEnd: number): CapsuleState {
  // A scroll the app makes moves where the content rests.
  if (!state.reading) return state.anchor === offset ? state : { ...state, anchor: offset };
  const anchor = state.anchor ?? offset;
  if (state.collapsed || fromEnd <= CAPSULE.atEnd || Math.abs(offset - anchor) <= CAPSULE.collapseAfter) return state.anchor === anchor ? state : { ...state, anchor };
  return { ...state, anchor, collapsed: true };
}

/** The scroll came to rest at `offset`, `fromEnd` from the newest end, however it got there. */
export function capsuleRest(state: CapsuleState, offset: number, fromEnd: number): CapsuleState {
  return { collapsed: state.collapsed && fromEnd > CAPSULE.atEnd, reading: false, anchor: offset };
}

/** A tap on the collapsed name, or something that needs the person. */
export function capsuleExpand(state: CapsuleState): CapsuleState {
  return state.collapsed ? { ...state, collapsed: false } : state;
}

/** The Session's terminal: it starts collapsed, and the next touch in it collapses it again. */
export function capsuleCollapse(state: CapsuleState): CapsuleState {
  return state.collapsed ? state : { ...state, collapsed: true };
}
