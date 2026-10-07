import React, { useEffect, useState } from "react";
import { AppState, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import Animated, {
  cancelAnimation,
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withDelay,
  withRepeat,
  withTiming,
  type SharedValue,
} from "react-native-reanimated";
import Svg, { Circle, Ellipse, G, Line, Path, Rect } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";
import type { BrainCatState } from "./brainCatState";
import {
  CAT_IN_SEAL,
  CURL,
  CURL_PEEK,
  RIG,
  SEAL,
  SEAL_AT,
  SEAL_PAPER,
  SEAL_RED,
  SEAL_RED_INK,
  pose,
  sealCarving,
  standingCatFrame,
  type Pose,
  type RigLine,
  type StandingCatFrame,
} from "./sealCatGeometry";

/** The standing cat's crop of the landing rig: feet on the bottom edge. */
const STAND_CROP = { x: -58, y: -80, width: 110, height: 84 };
const STAND_ASPECT = STAND_CROP.height / STAND_CROP.width;
const SEAL_STATES = new Set<BrainCatState>(["homeless", "offline", "waking", "idle"]);

interface SealCatProps {
  state: BrainCatState;
  /** Width in points; seal states are square, standing states ~0.76 tall. */
  size: number;
  /** Off when the screen is hidden; reduced motion and background force it off. */
  animate?: boolean;
  style?: StyleProp<ViewStyle>;
}

/**
 * Brain, drawn as the landing's seal cat. Asleep it is carved into the
 * vermilion seal; awake it is a small red cat on its feet. One cat per
 * screen, and every pose maps to a real Brain state (brainCatState.ts).
 */
export function SealCat({ state, size, animate = true, style }: SealCatProps) {
  const moving = useCatMotion(animate);
  const box = SEAL_STATES.has(state)
    ? { width: size, height: size }
    : { width: size, height: size * STAND_ASPECT };
  return (
    <View
      style={[box, styles.inert, style]}
      accessible={false}
      importantForAccessibility="no-hide-descendants"
    >
      {SEAL_STATES.has(state) ? (
        <SleepingSeal state={state} size={size} moving={moving} />
      ) : (
        <StandingCat state={state} width={size} moving={moving} />
      )}
    </View>
  );
}

/** Motion runs only while wanted, allowed by the OS, and the app is in front. */
function useCatMotion(animate: boolean): boolean {
  const reduced = useReducedMotion();
  const [active, setActive] = useState(AppState.currentState !== "background");
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) =>
      setActive(next === "active"),
    );
    return () => subscription.remove();
  }, []);
  return animate && !reduced && active;
}

// ---- asleep in the seal ------------------------------------------------------

function SleepingSeal({
  state,
  size,
  moving,
}: {
  state: BrainCatState;
  size: number;
  moving: boolean;
}) {
  const { colors } = useAppTheme();
  const { bold, detail } = sealCarving(size);
  const offline = state === "offline";
  const block = offline ? colors.borderStrong : SEAL_RED;
  const breathing = moving && (state === "idle" || state === "waking");
  const breath = useLoop(breathing, 3600);
  const breathStyle = useAnimatedStyle(() => {
    const wave = Math.sin(breath.value * Math.PI * 2);
    return {
      transform: [
        { scaleX: 1 + 0.005 * (wave + 1) },
        { scaleY: 1 + 0.0125 * (wave + 1) },
      ],
    };
  });

  return (
    <View style={StyleSheet.absoluteFill}>
      <Svg viewBox="0 0 100 100" width={size} height={size} style={StyleSheet.absoluteFill}>
        <Path d={SEAL.block} fill={block} />
        {detail ? (
          <>
            <Circle {...SEAL.moon} fill={SEAL_PAPER} />
            <Circle {...SEAL.moonBite} fill={block} />
          </>
        ) : null}
        {/* Chips knocked off the edge show the page, as on the landing. */}
        {SEAL.chips.map((d) => (
          <Path key={d} d={d} fill={colors.bgPrimary} />
        ))}
        {state === "homeless" ? (
          // The empty bed the cat leaves behind: no computer, no cat yet.
          <G transform={CAT_IN_SEAL} opacity={0.22}>
            <CurledCat fill={SEAL_PAPER} line={block} detail={detail} bold={bold} />
          </G>
        ) : null}
      </Svg>
      {state !== "homeless" ? (
        <Animated.View
          style={[
            StyleSheet.absoluteFill,
            { transformOrigin: `${SEAL_AT[0]}% ${SEAL_AT[1]}%` },
            breathStyle,
          ]}
        >
          <Svg viewBox="0 0 100 100" width={size} height={size}>
            <G transform={CAT_IN_SEAL}>
              <CurledCat fill={SEAL_PAPER} line={block} detail={detail} bold={bold} />
              {state === "waking" ? (
                <G>
                  <Ellipse cx={CURL_PEEK.cx} cy={CURL_PEEK.cy} rx={CURL_PEEK.rx} ry={CURL_PEEK.ry} fill={SEAL_PAPER} />
                  <Ellipse cx={CURL_PEEK.cx} cy={CURL_PEEK.cy} rx={CURL_PEEK.pupilRx} ry={CURL_PEEK.pupilRy} fill={block} />
                </G>
              ) : null}
            </G>
          </Svg>
        </Animated.View>
      ) : null}
      {state === "idle" ? <Snores size={size} moving={moving} /> : null}
    </View>
  );
}

function CurledCat({
  fill,
  line,
  detail,
  bold,
}: {
  fill: string;
  line: string;
  detail: boolean;
  bold: number;
}) {
  const [cx, cy, rx, ry, rot] = CURL.head;
  const stroke = (d: string, width: number) => (
    <Path
      key={d}
      d={d}
      fill="none"
      stroke={line}
      strokeWidth={width}
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  );
  return (
    <G>
      <Path d={CURL.tail} fill="none" stroke={fill} strokeWidth={9} strokeLinecap="round" />
      <Path d={CURL.body} fill={fill} />
      <Path d={CURL.earL} fill={fill} />
      <Path d={CURL.earR} fill={fill} />
      <Ellipse cx={cx} cy={cy} rx={rx} ry={ry} transform={`rotate(${rot} ${cx} ${cy})`} fill={fill} />
      <Path d={CURL.paw} fill={fill} />
      {CURL.lines.map((d) => stroke(d, 1.55 * bold))}
      <Path d={CURL.nose} fill={line} stroke={line} strokeWidth={0.8} strokeLinejoin="round" />
      {detail
        ? (bold > 1 ? CURL.fine.slice(2, 4) : CURL.fine).map((d) => stroke(d, 0.9 * bold))
        : null}
      {detail ? CURL.stripes.map((d) => stroke(d, 2 * Math.sqrt(bold))) : null}
    </G>
  );
}

/** Three z's drifting up from the sleeper, as on the landing's seal. */
function Snores({ size, moving }: { size: number; moving: boolean }) {
  return (
    <>
      {[0, 1, 2].map((index) => (
        <Snore key={index} index={index} size={size} moving={moving} />
      ))}
    </>
  );
}

function Snore({ index, size, moving }: { index: number; size: number; moving: boolean }) {
  const progress = useSharedValue(0);
  useEffect(() => {
    if (!moving) {
      cancelAnimation(progress);
      progress.value = 0.35 + index * 0.2;
      return;
    }
    progress.value = 0;
    progress.value = withDelay(
      index * 1200,
      withRepeat(withTiming(1, { duration: 3600, easing: Easing.linear }), -1, false),
    );
    return () => cancelAnimation(progress);
  }, [index, moving, progress]);
  const style = useAnimatedStyle(() => {
    const t = progress.value;
    return {
      opacity: t < 0.2 ? (t / 0.2) * 0.9 : 0.9 * (1 - (t - 0.2) / 0.8),
      transform: [
        { translateX: t * size * 0.09 },
        { translateY: -t * size * 0.2 },
        { scale: 0.6 + t * 0.55 },
      ],
    };
  });
  const fontSize = Math.max(7, size * 0.075);
  return (
    <Animated.Text
      style={[
        styles.snore,
        {
          left: size * (0.086 + index * 0.027),
          top: size * 0.3 - fontSize,
          fontSize,
          lineHeight: fontSize * 1.1,
        },
        style,
      ]}
    >
      z
    </Animated.Text>
  );
}

// ---- awake: the standing cat ------------------------------------------------------

function StandingCat({
  state,
  width,
  moving,
}: {
  state: BrainCatState;
  width: number;
  moving: boolean;
}) {
  const { colors } = useAppTheme();
  const height = width * STAND_ASPECT;
  const frames = standingFrames(state);
  const loop = useLoop(moving && frames.length > 1, frames.length * framePeriod(state));
  return (
    <View style={StyleSheet.absoluteFill}>
      {frames.map((frame, index) => (
        <FrameLayer key={index} index={index} count={frames.length} progress={loop}>
          <StandingCatSvg frame={frame} width={width} height={height} gift={state === "delivered"} />
        </FrameLayer>
      ))}
      {state === "attention" ? (
        <AttentionPing width={width} height={height} color={colors.accent} moving={moving} />
      ) : null}
      {state === "delegating" ? (
        <Dispatch width={width} height={height} color={colors.accent} moving={moving} />
      ) : null}
    </View>
  );
}

const WALK_FRAMES = 8;

/** A handful of precomputed frames per state; the UI thread only flips them. */
function standingFrames(state: BrainCatState): StandingCatFrame[] {
  switch (state) {
    case "working":
      // Walking in place, the cat on its way somewhere for you.
      return Array.from({ length: WALK_FRAMES }, (_, index) => {
        const phase = (index / WALK_FRAMES) * Math.PI * 2;
        return standingCatFrame(pose("stand"), {
          step: Math.sin(phase) * 0.55,
          swish: Math.sin(phase) * 6,
          breath: 1 + 0.015 * Math.cos(phase),
        });
      });
    case "delegating":
      // Sitting, watching the Workers go; the tail sways.
      return swayFrames(pose("sit"), 4, 10);
    case "attention":
      return [standingCatFrame(pose("alert"))];
    case "delivered":
      return swayFrames(pose("loaf"), 4, 8);
    default:
      return [standingCatFrame(pose("sit"))];
  }
}

function swayFrames(P: Pose, count: number, amplitude: number): StandingCatFrame[] {
  return Array.from({ length: count }, (_, index) =>
    standingCatFrame(P, { swish: Math.sin((index / count) * Math.PI * 2) * amplitude }),
  );
}

function framePeriod(state: BrainCatState): number {
  return state === "working" ? 110 : 420;
}

function FrameLayer({
  index,
  count,
  progress,
  children,
}: {
  index: number;
  count: number;
  progress: SharedValue<number>;
  children: React.ReactNode;
}) {
  const style = useAnimatedStyle(() => ({
    opacity: Math.floor(progress.value * count) % count === index ? 1 : 0,
  }));
  return <Animated.View style={[StyleSheet.absoluteFill, style]}>{children}</Animated.View>;
}

function StandingCatSvg({
  frame,
  width,
  height,
  gift,
}: {
  frame: StandingCatFrame;
  width: number;
  height: number;
  gift: boolean;
}) {
  const leg = (line: RigLine | null, color: string, strokeWidth: number, key: string) =>
    line ? (
      <Line key={key} {...line} stroke={color} strokeWidth={strokeWidth} strokeLinecap="round" />
    ) : null;
  const carve = (d: string, strokeWidth: number) => (
    <Path key={d} d={d} fill="none" stroke={SEAL_PAPER} strokeWidth={strokeWidth} strokeLinecap="round" />
  );
  return (
    <Svg
      viewBox={`${STAND_CROP.x} ${STAND_CROP.y} ${STAND_CROP.width} ${STAND_CROP.height}`}
      width={width}
      height={height}
    >
      {leg(frame.legsFar[0], SEAL_RED_INK, 8, "far-front")}
      {leg(frame.legsFar[1], SEAL_RED_INK, 8, "far-rear")}
      <Path d={frame.tail} fill="none" stroke={SEAL_RED} strokeWidth={8} strokeLinecap="round" />
      <Path d={frame.tail} fill="none" stroke={SEAL_PAPER} strokeWidth={8.4} strokeDasharray="1.3 6.2" strokeDashoffset={-9} />
      <G transform={frame.torso}>
        {frame.haunch.rx > 0 ? (
          <Ellipse cx={-9} cy={2} rx={frame.haunch.rx} ry={frame.haunch.ry} fill={SEAL_RED} />
        ) : null}
        <Ellipse cx={0} cy={0} rx={frame.body.rx} ry={frame.body.ry} fill={SEAL_RED} />
        {RIG.marks.map((d) => carve(d, 1.9))}
        {frame.haunchLineOpacity > 0 ? (
          <G opacity={frame.haunchLineOpacity}>{carve(RIG.haunchLine, 1.6)}</G>
        ) : null}
      </G>
      {frame.rearPaws.opacity > 0 ? (
        <G transform={`translate(${frame.rearPaws.x.toFixed(2)} 0)`} opacity={frame.rearPaws.opacity}>
          <Ellipse cx={0} cy={-3} rx={6.4} ry={3.4} fill={SEAL_RED} />
        </G>
      ) : null}
      {leg(frame.legsNear[1], SEAL_RED, 8.5, "near-rear")}
      {leg(frame.legsNear[0], SEAL_RED, 8.5, "near-front")}
      {frame.frontPaws.opacity > 0 ? (
        <G transform={`translate(${frame.frontPaws.x.toFixed(2)} 0)`} opacity={frame.frontPaws.opacity}>
          <Ellipse cx={0} cy={-3} rx={6} ry={3.4} fill={SEAL_RED} />
          {carve(RIG.frontToes, 0.8)}
        </G>
      ) : null}
      {gift ? <Gift /> : null}
      <G transform={frame.head}>
        <Path d={RIG.earL} transform={frame.earL} fill={SEAL_RED} stroke={SEAL_RED} strokeWidth={3} strokeLinejoin="round" />
        <Path d={RIG.earR} transform={frame.earR} fill={SEAL_RED} stroke={SEAL_RED} strokeWidth={3} strokeLinejoin="round" />
        <Ellipse cx={0} cy={0} rx={16.5} ry={14.5} fill={SEAL_RED} />
        <G opacity={frame.earLinesOpacity}>{RIG.earLines.map((d) => carve(d, 1.1))}</G>
        {RIG.whiskers.map((d) => carve(d, 0.9))}
        <Eyes shape={frame.eyes} />
        <Path d={RIG.nose} fill={SEAL_PAPER} stroke={SEAL_PAPER} strokeWidth={0.7} strokeLinejoin="round" />
        {frame.mouthOpen ? (
          <Ellipse cx={4.2} cy={8} rx={2.4} ry={3} fill={SEAL_PAPER} />
        ) : (
          RIG.smile.map((d) => carve(d, 0.9))
        )}
      </G>
    </Svg>
  );
}

function Eyes({ shape }: { shape: StandingCatFrame["eyes"] }) {
  if (shape === "wide") {
    return (
      <G>
        {[-2, 9].map((cx) => (
          <G key={cx}>
            <Ellipse cx={cx} cy={-1.2} rx={2.7} ry={3.1} fill={SEAL_PAPER} />
            <Circle cx={cx + 0.5} cy={-0.8} r={1.2} fill={SEAL_RED_INK} />
          </G>
        ))}
      </G>
    );
  }
  if (shape === "shut" || shape === "happy") {
    const paths = shape === "shut" ? RIG.eyesShut : RIG.eyesHappy;
    return (
      <G>
        {paths.map((d) => (
          <Path key={d} d={d} fill="none" stroke={SEAL_PAPER} strokeWidth={1.8} strokeLinecap="round" />
        ))}
      </G>
    );
  }
  return (
    <G fill={SEAL_PAPER}>
      <Ellipse cx={-2} cy={-1} rx={1.9} ry={2.4} />
      <Ellipse cx={9} cy={-1} rx={1.9} ry={2.4} />
    </G>
  );
}

/** What it brought back: a small parcel by its front paws. */
function Gift() {
  return (
    <G transform="translate(38 -13)">
      <Rect x={0} y={0} width={12} height={12} rx={1.6} fill={SEAL_RED_INK} />
      <Path d="M 6 0 L 6 12 M 0 5 L 12 5" stroke={SEAL_PAPER} strokeWidth={1.6} />
      <Path d="M 6 0 C 3 -4 0 -2 2.5 0 M 6 0 C 9 -4 12 -2 9.5 0" fill="none" stroke={SEAL_RED_INK} strokeWidth={1.6} strokeLinecap="round" />
    </G>
  );
}

/** The landing's ping: a dot that rings out, above the alert cat's ear. */
function AttentionPing({
  width,
  height,
  color,
  moving,
}: {
  width: number;
  height: number;
  color: string;
  moving: boolean;
}) {
  const ring = useLoop(moving, 2400);
  const ringStyle = useAnimatedStyle(() => ({
    opacity: 0.5 * (1 - Math.min(1, ring.value / 0.7)),
    transform: [{ scale: 1 + Math.min(1, ring.value / 0.7) * 1.4 }],
  }));
  const dot = Math.max(6, width * 0.1);
  const position = { left: width * 0.86, top: height * 0.1, width: dot, height: dot, borderRadius: dot / 2 };
  return (
    <>
      <Animated.View style={[styles.dot, position, { backgroundColor: color }, ringStyle]} />
      <View style={[styles.dot, position, { backgroundColor: color }]} />
    </>
  );
}

/** Dispatch dots running off along the ground to the Workers, as on the landing's orbit. */
function Dispatch({
  width,
  height,
  color,
  moving,
}: {
  width: number;
  height: number;
  color: string;
  moving: boolean;
}) {
  const run = useLoop(moving, 1800);
  return (
    <>
      {[0, 1, 2].map((index) => (
        <DispatchDot key={index} index={index} width={width} height={height} color={color} run={run} />
      ))}
    </>
  );
}

function DispatchDot({
  index,
  width,
  height,
  color,
  run,
}: {
  index: number;
  width: number;
  height: number;
  color: string;
  run: SharedValue<number>;
}) {
  const style = useAnimatedStyle(() => {
    const t = (run.value + index / 3) % 1;
    return {
      opacity: t < 0.15 ? t / 0.15 : 1 - (t - 0.15) / 0.85,
      transform: [{ translateX: t * width * 0.2 }],
    };
  });
  const dot = Math.max(3, width * 0.05);
  return (
    <Animated.View
      style={[
        styles.dot,
        {
          left: width * 0.76,
          top: height * 0.68,
          width: dot,
          height: dot,
          borderRadius: dot / 2,
          backgroundColor: color,
        },
        style,
      ]}
    />
  );
}

/** A 0→1 loop on the UI thread; parked at 0 whenever motion is off. */
function useLoop(running: boolean, duration: number): SharedValue<number> {
  const progress = useSharedValue(0);
  useEffect(() => {
    if (!running) {
      cancelAnimation(progress);
      progress.value = 0;
      return;
    }
    progress.value = 0;
    progress.value = withRepeat(
      withTiming(1, { duration, easing: Easing.linear }),
      -1,
      false,
    );
    return () => cancelAnimation(progress);
  }, [duration, progress, running]);
  return progress;
}

const styles = StyleSheet.create({
  inert: {
    pointerEvents: "none",
  },
  snore: {
    position: "absolute",
    color: SEAL_PAPER,
    fontWeight: "700",
  },
  dot: {
    position: "absolute",
  },
});
