/**
 * The capsule header (the owner's pick, 11-13: family 5 of site/v/headers, refined in site/v/capsule): no
 * bar, one rounded capsule floating over the page's content, which collapses while the person reads.
 *
 * Every number the capsule is drawn with is here, so the designer's final values are a change of this
 * table only. Sizes in points.
 */
export const CAPSULE = {
  /** Below the status bar (the safe area's top). */
  top: 4,
  /** From the screen's sides. */
  side: 8,
  /** The full capsule's height and corner radius. */
  height: 52,
  radius: 26,
  /** Inside it, at both ends. */
  padding: 4,
  /** Its controls (Back, the view switch, ⋯) and the cat: 44 targets. */
  control: 44,
  /** A Session's mark. */
  mark: 28,
  /** The collapsed form: Back (where there is one) and the mark, a pill this tall. */
  compactHeight: 44,
  compactRadius: 22,
  /** Room between the capsule and the content's first row at rest. */
  below: 4,
  /** The canvas fade the content passes under: solid this far below the status bar, clear this far below it. */
  fadeSolid: 8,
  fadeClear: 76,
  /** Its soft shadow (with a 0.5 pt hairline in the border token). */
  shadow: "0px 8px 24px -10px rgba(0, 0, 0, 0.28)",
  /** The canvas fade's strips (NSH draws no gradient). */
  fadeStrips: 12,
  /** The title: one line, or a long one in two smaller lines; a second line where a page has one (Brain's terminal). */
  title: { size: 16, line: 21 },
  titleLong: { size: 15, line: 19 },
  subtitle: { size: 13, line: 17 },
  /** A title longer than this many characters takes two smaller lines (the mock's rule). */
  longTitle: 16,
  /** The state dot on the mark (needs you, running, offline): its size, its ring in the capsule's colour, its corner. */
  dot: { size: 10, ring: 2, right: 6, bottom: 8 },
  /** The movement: the full capsule lifts and shrinks a little as it goes; the pill grows in. */
  motion: { lift: -6, fullScale: 0.97, compactScale: 0.9 },
  /** How far the person scrolls before it collapses, from where their drag began. */
  collapseAfter: 24,
  /** Within this of the newest end, coming to rest expands it (the thread's own AT_END). */
  atEnd: 120,
  /** The one movement between the two states; reduced motion makes it a cut. */
  duration: 220,
} as const;

/** How far the content starts below the screen's top, so nothing hides under the capsule at rest. */
export function capsuleInset(safeTop: number): number {
  return safeTop + CAPSULE.top + CAPSULE.height + CAPSULE.below;
}

/** A Session title in two smaller lines rather than one ("Daily Hacker News Briefing · Nov 13"). */
export function capsuleTitleIsLong(title: string): boolean {
  return title.length > CAPSULE.longTitle;
}

/**
 * Whether the capsule is collapsed, from the person's own scrolling only. It collapses once they have
 * scrolled `collapseAfter` from where their drag began (either way); it expands when the scroll they
 * started comes to rest at the newest end, when they tap it, and when something needs them. A scroll the
 * person did not start (the list following a new message, a jump) never collapses it: the thread already
 * moves only by the person, and the header follows that rule.
 */
export type CapsuleState = {
  collapsed: boolean;
  /** The person's drag, and the glide it started, is under way. */
  reading: boolean;
  /** Where their drag began. */
  from: number;
};

export const CAPSULE_OPEN: CapsuleState = { collapsed: false, reading: false, from: 0 };

export function capsuleDragStart(state: CapsuleState, offset: number): CapsuleState {
  return { ...state, reading: true, from: offset };
}

export function capsuleScroll(state: CapsuleState, offset: number): CapsuleState {
  if (!state.reading || state.collapsed || Math.abs(offset - state.from) < CAPSULE.collapseAfter) return state;
  return { ...state, collapsed: true };
}

/**
 * The scroll came to rest; `atEnd`: at the newest end. Resting there expands it however the list got there
 * (their glide, the jump control, their send); resting elsewhere keeps it as it is.
 */
export function capsuleRest(state: CapsuleState, atEnd: boolean): CapsuleState {
  const next = { collapsed: state.collapsed && !atEnd, reading: false, from: 0 };
  return next.collapsed === state.collapsed && !state.reading ? state : next;
}

/** A tap on the collapsed capsule, or something that needs the person (a new request, offline). */
export function capsuleExpand(state: CapsuleState): CapsuleState {
  return state.collapsed ? { ...state, collapsed: false } : state;
}
