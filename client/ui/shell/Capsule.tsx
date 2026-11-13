import { reduceMotion, safeArea, signal, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { CAPSULE, CAPSULE_OPEN, capsuleDragStart, capsuleExpand, capsuleRest, capsuleScroll, type CapsuleState } from "../../core/shell/capsule";

/** The person's scrolling, as a list reports it to the capsule (`ChatTimeline`'s `reading`). */
export type CapsuleReading = {
  dragStart: (offset: number) => void;
  scroll: (offset: number) => void;
  rest: (atEnd: boolean) => void;
};

/**
 * A page's capsule state: collapsed or not (core/shell/capsule.ts). Only the capsule reads `collapsed`,
 * and it changes only when the state flips, so the content under it (a thread) never draws again.
 */
export function createCapsule() {
  let state: CapsuleState = CAPSULE_OPEN;
  const collapsed = signal(false);
  const apply = (next: CapsuleState) => {
    state = next;
    if (next.collapsed !== untracked(collapsed)) collapsed.set(next.collapsed);
  };
  const reading: CapsuleReading = {
    dragStart: (offset) => apply(capsuleDragStart(state, offset)),
    scroll: (offset) => apply(capsuleScroll(state, offset)),
    rest: (atEnd) => apply(capsuleRest(state, atEnd)),
  };
  return { collapsed: () => collapsed(), reading, expand: () => apply(capsuleExpand(state)) };
}

const EDGE = "border-[0.5px] border-border bg-card";

/**
 * The capsule header (core/shell/capsule.ts): over the page's content, which passes under a canvas fade
 * below the status bar. Two forms, crossfading in one movement: the full capsule, and collapsed, a small
 * pill (`compact`: Back and the mark) that expands on a tap. Touches outside the capsule reach the content.
 */
export function Capsule(props: {
  collapsed: () => boolean;
  onExpand: () => void;
  /** What the collapsed pill says to a screen reader ("mewla, show the header"). */
  compactLabel: () => string;
  full: JSX.Element;
  compact: JSX.Element;
  /** A note under the capsule (a Session's durability warning). */
  below?: JSX.Element;
}) {
  const top = () => safeArea().top;
  // Clear of a cutout at the side too (in landscape the camera is there).
  const left = () => CAPSULE.side + safeArea().left;
  const right = () => CAPSULE.side + safeArea().right;
  const motion = () => ({ transitionProperty: "opacity, transform", transitionDuration: reduceMotion() ? 0 : CAPSULE.duration });
  const { lift, fullScale, compactScale } = CAPSULE.motion;
  return (
    <View testID="capsule-layer" class="absolute left-0 right-0 top-0" style={{ pointerEvents: "box-none" }}>
      <CapsuleFade top={top} />
      <View style={{ paddingTop: top() + CAPSULE.top, paddingLeft: left(), paddingRight: right(), gap: CAPSULE.below, pointerEvents: "box-none" }}>
        {/* Each form's testID says whether it is the one showing (a test cannot read opacity); a screen reader
            reads only that one. */}
        <View testID={props.collapsed() ? "capsule-collapsed" : "capsule"} class={`flex-row items-center ${EDGE}`}
          accessibilityElementsHidden={props.collapsed()} importantForAccessibility={props.collapsed() ? "no-hide-descendants" : "auto"}
          style={{
            height: CAPSULE.height, borderRadius: CAPSULE.radius, paddingHorizontal: CAPSULE.padding, boxShadow: CAPSULE.shadow,
            opacity: props.collapsed() ? 0 : 1, transform: [{ translateY: props.collapsed() ? lift : 0 }, { scale: props.collapsed() ? fullScale : 1 }],
            pointerEvents: props.collapsed() ? "none" : "auto", ...motion(),
          }}>
          {props.full}
        </View>
        {props.below ?? null}
      </View>
      <View testID={props.collapsed() ? "capsule-compact" : "capsule-compact-hidden"} role="button" accessibilityLabel={props.compactLabel()} onPress={props.onExpand} class={`absolute flex-row items-center ${EDGE}`}
        accessibilityElementsHidden={!props.collapsed()} importantForAccessibility={props.collapsed() ? "auto" : "no-hide-descendants"}
        style={{
          top: top() + CAPSULE.top + (CAPSULE.height - CAPSULE.compactHeight) / 2, left: left(), height: CAPSULE.compactHeight, borderRadius: CAPSULE.compactRadius,
          paddingRight: CAPSULE.padding * 2, boxShadow: CAPSULE.shadow,
          opacity: props.collapsed() ? 1 : 0, transform: [{ scale: props.collapsed() ? 1 : compactScale }],
          pointerEvents: props.collapsed() ? "auto" : "none", ...motion(),
        }}>
        {props.compact}
      </View>
    </View>
  );
}

/**
 * The canvas the content passes under: solid over the status bar and a little below it, then clearing
 * (in strips: NSH draws no gradient). It takes no touches.
 */
function CapsuleFade(props: { top: () => number }) {
  const strips = CAPSULE.fadeStrips;
  const step = (CAPSULE.fadeClear - CAPSULE.fadeSolid) / strips;
  return (
    <View class="absolute left-0 right-0 top-0" style={{ pointerEvents: "none" }}>
      <View class="bg-background" style={{ height: props.top() + CAPSULE.fadeSolid }} />
      {Array.from({ length: strips }, (_, i) => <View class="bg-background" style={{ height: step, opacity: 1 - (i + 1) / (strips + 1) }} />)}
    </View>
  );
}
