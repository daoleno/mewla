import { AnimatedValue, batch, effect, reduceMotion, signal, untracked, View } from "nsh";
import type { AnimatedNode, JSX } from "nsh";
import { isAvailable as mobile } from "nsh/mobile";
import { settlePage, TAB_PAGER } from "../../core/shell/tabPager";

/**
 * A page's tabs that follow the finger (core/shell/tabPager.ts): one state shared by its `Segmented` (pass it as
 * `pager`, the chosen segment then moves with the row) and its `TabPages` (the row of pages). A tap on a segment
 * moves the pages to it; a sideways swipe on the pages moves them, and the segment, under the finger.
 *
 * Swiping is the phone's and the tablet's: on the desktops and the web a drag selects text, so there the pages
 * are not moved by a pointer and a tab shows its page alone.
 */
export type TabPager<K extends string> = {
  keys: readonly K[];
  /** The chosen tab: the page the row rests on, or settles to. */
  value: () => K;
  /** A tap on a segment. */
  choose: (key: K) => void;
  /** Where the row is, as a page index (1.5: halfway from the second page to the third), on the UI thread; null until the pages are measured. */
  progress: () => AnimatedNode | null;
  /** Whether a tab's page is made: once chosen, or once a swipe may reach it (a page that loads on first view starts then). */
  made: (key: K) => boolean;
  /** @internal The row's state, for `TabPages`. */
  row: PagerRow;
};

type PagerRow = {
  width: () => number;
  measure: (width: number) => void;
  rest: () => number;
  x: AnimatedValue;
  /** Whether a finger or a settle is moving the row. */
  moving: () => boolean;
  made: () => ReadonlySet<number>;
  panStart: () => void;
  panEnd: (offset: number, velocity: number) => void;
};

/** Swiping is for touch: phones and tablets. */
export const swipeable = mobile;

export function createTabPager<K extends string>(options: { keys: readonly K[]; value: () => K; onChange: (key: K) => void }): TabPager<K> {
  const keys = options.keys;
  const indexOf = (key: K) => Math.max(0, keys.indexOf(key));
  const first = untracked(() => indexOf(options.value()));
  const width = signal(0);
  // Where the row rests is layout: it is laid out with that page in view, and `x` is only how far a finger or
  // a settle has moved it from there. When a settle lands, its page becomes the layout and `x` returns to 0 in
  // the same step, so a host that draws no transforms (the test engine) shows the chosen page too.
  const rest = signal(first);
  const x = new AnimatedValue(0);
  const moving = signal(false);
  const made = signal<ReadonlySet<number>>(new Set([first]));
  // The page it is going to (or rests on), and which move is the latest: a settle a finger or a newer move
  // interrupted never lands.
  let target = first;
  let run = 0;

  const make = (...pages: number[]) => {
    const now = untracked(made);
    const missing = pages.filter((p) => p >= 0 && p < keys.length && !now.has(p));
    if (missing.length) made.set(new Set([...now, ...missing]));
  };

  const land = (page: number) => {
    batch(() => {
      rest.set(page);
      x.set(0);
      moving.set(false);
    });
  };

  const goTo = (page: number, velocity = 0) => {
    target = page;
    make(page);
    const mine = ++run;
    const w = untracked(width);
    if (!w || !swipeable || untracked(reduceMotion)) return land(page);
    moving.set(true);
    void x.spring((untracked(rest) - page) * w, { velocity, ...TAB_PAGER.spring }).then((done) => {
      if (done.finished && mine === run) land(page);
    });
  };

  // A tap on a segment (or any other change of the tab) moves the pages to it.
  effect(() => {
    const page = indexOf(options.value());
    untracked(() => {
      if (page !== target) goTo(page);
    });
  });

  const row: PagerRow = {
    width,
    measure: (w) => {
      if (w === untracked(width)) return;
      // A new width (a rotation): it rests on its page at once.
      run++;
      void x.stop();
      batch(() => {
        width.set(w);
        rest.set(target);
        x.set(0);
        moving.set(false);
      });
    },
    rest,
    x,
    moving,
    made,
    panStart: () => {
      run++;
      void x.stop();
      moving.set(true);
      // The pages beside are made when a swipe may reach them, not before: a page opens as quickly as one tab.
      make(target - 1, target + 1);
    },
    panEnd: (offset, velocity) => {
      const next = settlePage({ rest: untracked(rest), from: target, count: keys.length, width: untracked(width), offset, velocity });
      const changed = next !== indexOf(untracked(options.value));
      goTo(next, velocity);
      if (changed) options.onChange(keys[next]!);
    },
  };

  return {
    keys,
    value: options.value,
    choose: (key) => {
      if (key !== untracked(options.value)) options.onChange(key);
    },
    progress: () => {
      const w = width();
      if (!w || !swipeable) return null;
      return x.mul(-1 / w).add(rest());
    },
    made: (key) => made().has(keys.indexOf(key)),
    row,
  };
}

/**
 * The row of a pager's pages. `fill`: the pages fill the place (each with its own vertical scroll: a feed, a
 * list); otherwise the row sits in the page's scroll and is as high as the page in view (while a swipe or a
 * settle moves it, as high as the highest page it shows). A page is made the first time it is chosen or a
 * swipe may reach it, and kept after: its scroll and what it loaded stay. The pages out of view are hidden from
 * screen readers.
 */
export function TabPages<K extends string>(props: { pager: TabPager<K>; fill?: boolean; testID?: string; pages: Record<K, () => JSX.Element> }) {
  const pager = props.pager;
  const row = pager.row;
  const count = pager.keys.length;
  const fill = untracked(() => Boolean(props.fill));
  const heights = signal<number[]>(pager.keys.map(() => 0));
  const shown = () => Math.max(0, pager.keys.indexOf(pager.value()));
  const height = () => {
    if (fill) return undefined;
    const all = heights();
    const h = row.moving() ? Math.max(...all.filter((_, i) => row.made().has(i))) : all[row.rest()] ?? 0;
    return h > 0 ? h : undefined;
  };
  // Before the first measure (and off touch screens) the row is the page in view alone, as wide as the place.
  const laidOut = () => swipeable && row.width() > 0;
  const pageStyle = (i: number) => (laidOut() ? { width: row.width() } : i === row.rest() ? { width: "100%" as const } : { display: "none" as const });
  return (
    <View testID={props.testID} class={`${fill ? "flex-1" : ""} overflow-hidden`} style={{ height: height() }} onLayout={(e) => row.measure(e.bounds.size.width)}>
      <View class={`flex-row ${fill ? "flex-1" : "items-start"}`}
        style={laidOut() ? { width: row.width() * count, marginLeft: -row.rest() * row.width(), transform: [{ translateX: row.x }] } : undefined}
        pan={swipeable && count > 1 ? { x: row.x } : undefined}
        panActivation={{ activeOffsetX: TAB_PAGER.activeOffsetX, failOffsetY: TAB_PAGER.failOffsetY }}
        onPanStart={() => row.panStart()}
        onPanEnd={(e) => row.panEnd(e.value?.x ?? e.translation.x, e.velocity.x)}>
        {pager.keys.map((key, i) => (
          <View testID={props.testID ? `${props.testID}-${key}` : undefined} class={fill ? "h-full overflow-hidden" : ""} style={pageStyle(i)}
            accessibilityElementsHidden={i !== shown()} importantForAccessibility={i === shown() ? "auto" : "no-hide-descendants"}
            onLayout={fill ? undefined : (e) => {
              const h = e.bounds.size.height;
              const now = untracked(heights);
              if (now[i] !== h) heights.set(now.map((v, j) => (j === i ? h : v)));
            }}>
            <If when={row.made().has(i)}>{props.pages[key]()}</If>
          </View>
        ))}
      </View>
    </View>
  );
}
