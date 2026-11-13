import { colorScheme, LayoutAnimation, reduceMotion, safeArea, signal, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { CAPSULE, CAPSULE_OPEN, capsuleCollapse, capsuleDragStart, capsuleExpand, capsuleRest, capsuleScroll, type CapsuleState } from "../../core/shell/capsule";

/** The person's scrolling, as a list reports it to the capsule (`ChatTimeline`'s `capsule.reading`). */
export type CapsuleReading = {
  dragStart: () => void;
  /** `fromEnd`: how far the list is from its newest end. */
  scroll: (offset: number, fromEnd: number) => void;
  rest: (offset: number, fromEnd: number) => void;
};

/**
 * The one transition between the capsule's two layouts (SPEC §11): its width, height and corner over
 * `dur.move`; the parts that leave fade over `dur.fast` while the width closes, and the returning ones fade
 * in as it opens. NSH runs it natively for the commit that flips the state. With reduced motion, a cut.
 */
export function animateSwitch() {
  if (reduceMotion()) return;
  LayoutAnimation.configureNext({
    duration: CAPSULE.move,
    update: { type: "easeInEaseOut" },
    delete: { type: "linear", property: "opacity", duration: CAPSULE.fast },
    create: { type: "linear", property: "opacity", duration: CAPSULE.fast, delay: CAPSULE.move - CAPSULE.fast },
  });
}

/**
 * A page's capsule state (core/shell/capsule.ts). Only the capsule reads `collapsed`, and it changes only
 * when the state flips, so the content under it (a thread) never draws again.
 */
export function createCapsule(options: { collapsed?: boolean } = {}) {
  let state: CapsuleState = options.collapsed ? capsuleCollapse(CAPSULE_OPEN) : CAPSULE_OPEN;
  const collapsed = signal(state.collapsed);
  const apply = (next: CapsuleState) => {
    state = next;
    if (next.collapsed === untracked(collapsed)) return;
    animateSwitch();
    collapsed.set(next.collapsed);
  };
  const reading: CapsuleReading = {
    dragStart: () => apply(capsuleDragStart(state)),
    scroll: (offset, fromEnd) => apply(capsuleScroll(state, offset, fromEnd)),
    rest: (offset, fromEnd) => apply(capsuleRest(state, offset, fromEnd)),
  };
  return { collapsed: () => collapsed(), reading, expand: () => apply(capsuleExpand(state)), collapse: () => apply(capsuleCollapse(state)) };
}

/**
 * The page under a capsule: the app's canvas, edge to edge. Without a system header nothing else covers the
 * top of the screen, and the screen view behind a page is painted in the system's theme, not the app's (dark
 * under a light Mewla on a phone set to dark): an uncovered strip, a page's top padding, showed as a dark band
 * behind the capsule (owner 11-13 14:45).
 */
export function CapsulePage(props: { children: JSX.Element }) {
  return <View testID="capsule-page" class="flex-1 bg-background">{props.children}</View>;
}

/**
 * The capsule header (SPEC §11): one opaque shape on the sheet surface with a 1 pt hairline (and in light a
 * 1 pt contact shadow), 6 below the status bar and 8 from the sides (the page's: the shell keeps pages clear of
 * the side safe areas), over the page's content. Expanded it is
 * the screen's width less the sides; collapsed it is as wide as what it keeps, at the same top-left. Its
 * radius is half its height in both. Touches outside it reach the content.
 */
export function Capsule(props: {
  collapsed: () => boolean;
  /** Expanded with a two-line name: 56 high, not 48. */
  twoLines?: () => boolean;
  /** Collapsed to one control (a pushed page's Back): a 44 circle, no inner padding. */
  alone?: () => boolean;
  /** A pushed page's: always the compact shape (44 high, as wide as what it holds), with or without its title. */
  compact?: boolean;
  children: JSX.Element;
  /** A note under the capsule (a Session's durability warning). */
  below?: JSX.Element;
  /** The whole header's (Brain's is home's header, `home-header`). */
  testID?: string;
}) {
  const top = () => safeArea().top;
  const compact = () => props.compact || props.collapsed();
  const height = () => (compact() ? CAPSULE.compactHeight : props.twoLines?.() ? CAPSULE.heightTwoLines : CAPSULE.height);
  return (
    <View testID={props.testID ?? "capsule-layer"} class="absolute left-0 right-0 top-0" style={{ pointerEvents: "box-none" }}>
      <StatusBarStrip top={top} />
      <View style={{ paddingTop: top() + CAPSULE.top, paddingLeft: CAPSULE.side, paddingRight: CAPSULE.side, gap: 4, pointerEvents: "box-none" }}>
        {/* Its testID says which layout shows (a test reads no sizes). */}
        <View testID={props.collapsed() ? "capsule-collapsed" : "capsule"} class="flex-row items-center rounded-full border border-border bg-sheet-surface"
          style={{
            // Alone, it is the 44 circle itself (the hairline inside it, not 2 more around the control).
            height: height(), alignSelf: compact() ? "flex-start" : "stretch", maxWidth: "100%", paddingHorizontal: props.collapsed() && props.alone?.() ? 0 : CAPSULE.padding,
            width: props.collapsed() && props.alone?.() ? CAPSULE.compactHeight : undefined, justifyContent: props.collapsed() && props.alone?.() ? "center" : undefined,
            boxShadow: colorScheme() === "dark" ? undefined : CAPSULE.lightShadow,
          }}>
          {props.children}
        </View>
        {props.below ?? null}
      </View>
    </View>
  );
}

/**
 * Above the capsule the status bar keeps a strip: canvas down to 6 below it, then a short fade (in strips:
 * NSH draws no gradient), so the time and icons stay legible and no half line shows above the capsule.
 */
function StatusBarStrip(props: { top: () => number }) {
  const step = CAPSULE.fade / CAPSULE.fadeStrips;
  return (
    <View class="absolute left-0 right-0 top-0" style={{ pointerEvents: "none" }}>
      <View class="bg-background" style={{ height: props.top() + CAPSULE.top }} />
      {Array.from({ length: CAPSULE.fadeStrips }, (_, i) => <View class="bg-background" style={{ height: step, opacity: 1 - (i + 1) / (CAPSULE.fadeStrips + 1) }} />)}
    </View>
  );
}
