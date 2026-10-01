import React, { useId, useMemo, useState } from "react";
import { StyleSheet, View, type LayoutChangeEvent } from "react-native";
import Svg, { Defs, Line, LinearGradient, Path, Rect, Stop } from "react-native-svg";
import { useAppTheme } from "../../constants/tokens";
import type { ResourcePressureState, SeriesPoint, StateSpan } from "../../services/resourceTelemetry";

function useWidth(): [number, (event: LayoutChangeEvent) => void] {
  const [width, setWidth] = useState(0);
  return [width, (event) => {
    const next = Math.round(event.nativeEvent.layout.width);
    setWidth((previous) => (previous === next ? previous : next));
  }];
}

export function pressureInk(
  state: ResourcePressureState,
  colors: ReturnType<typeof useAppTheme>["colors"],
): string {
  if (state === "critical") return colors.dangerText;
  if (state === "elevated") return colors.warning;
  return colors.accent;
}

interface AreaChartProps {
  points: readonly SeriesPoint[];
  /** Time window shown; points outside are clipped by the frame. */
  start: number;
  end: number;
  color: string;
  height?: number;
  /** Off for a series layered over another chart's grid. */
  grid?: boolean;
  accessibilityLabel: string;
}

const STROKE_WIDTH = 1.75;

/**
 * Calm area sparkline for a 0…1 series. Paths are rebuilt only when the data
 * or width changes, so a new tick costs one path string, not a remount.
 */
export function AreaChart({ points, start, end, color, height = 88, grid = true, accessibilityLabel }: AreaChartProps) {
  const { colors } = useAppTheme();
  const [width, onLayout] = useWidth();
  const gradientId = `area-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const paths = useMemo(() => {
    if (width <= 0 || points.length === 0) return null;
    const span = Math.max(1, end - start);
    const top = STROKE_WIDTH;
    const usable = height - STROKE_WIDTH * 2;
    const coords = points.map((point) => [
      ((point.at - start) / span) * width,
      top + (1 - Math.min(1, Math.max(0, point.value))) * usable,
    ] as const);
    if (coords.length === 1) {
      const [, y] = coords[0];
      coords.unshift([Math.max(0, coords[0][0] - 1), y]);
    }
    const line = coords
      .map(([x, y], index) => `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`)
      .join("");
    const firstX = coords[0][0].toFixed(1);
    const lastX = coords[coords.length - 1][0].toFixed(1);
    return { line, area: `${line}L${lastX},${height}L${firstX},${height}Z` };
  }, [end, height, points, start, width]);

  return (
    <View
      style={{ height }}
      onLayout={onLayout}
      accessible
      accessibilityRole="image"
      accessibilityLabel={accessibilityLabel}
    >
      {width > 0 ? (
        <Svg width={width} height={height}>
          <Defs>
            <LinearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <Stop offset="0" stopColor={color} stopOpacity={0.28} />
              <Stop offset="1" stopColor={color} stopOpacity={0.02} />
            </LinearGradient>
          </Defs>
          {grid ? [0.5, 1].map((ratio) => {
            const y = STROKE_WIDTH + (1 - ratio) * (height - STROKE_WIDTH * 2);
            return (
              <Line
                key={ratio}
                x1={0}
                x2={width}
                y1={y}
                y2={y}
                stroke={colors.borderSubtle}
                strokeWidth={StyleSheet.hairlineWidth}
                strokeDasharray={ratio === 1 ? undefined : "3 4"}
              />
            );
          }) : null}
          {grid ? (
            <Line x1={0} x2={width} y1={height - 0.5} y2={height - 0.5} stroke={colors.borderSubtle} strokeWidth={1} />
          ) : null}
          {paths ? (
            <>
              <Path d={paths.area} fill={`url(#${gradientId})`} />
              <Path d={paths.line} stroke={color} strokeWidth={STROKE_WIDTH} fill="none" strokeLinejoin="round" strokeLinecap="round" />
            </>
          ) : null}
        </Svg>
      ) : null}
    </View>
  );
}

/** Thin timeline of pressure state under a chart; normal stays quiet. */
export function PressureStrip({ spans, start, end }: { spans: readonly StateSpan[]; start: number; end: number }) {
  const { colors } = useAppTheme();
  const [width, onLayout] = useWidth();
  const span = Math.max(1, end - start);
  return (
    <View style={styles.strip} onLayout={onLayout} accessible={false}>
      {width > 0 ? (
        <Svg width={width} height={4}>
          <Rect x={0} y={0} width={width} height={4} rx={2} fill={colors.borderSubtle} />
          {spans
            .filter((item) => item.state !== "normal")
            .map((item) => {
              const x = Math.max(0, ((item.start - start) / span) * width);
              const right = Math.min(width, ((item.end - start) / span) * width);
              return (
                <Rect
                  key={`${item.start}-${item.state}`}
                  x={x}
                  y={0}
                  width={Math.max(3, right - x)}
                  height={4}
                  rx={2}
                  fill={pressureInk(item.state, colors)}
                />
              );
            })}
        </Svg>
      ) : null}
    </View>
  );
}

/** One slim column per core; busy cores warm toward the warning ink. */
export function CoreBars({ values }: { values: readonly number[] }) {
  const { colors } = useAppTheme();
  const height = 36;
  return (
    <View style={[styles.cores, { height }]} accessible={false}>
      {values.map((value, index) => {
        const ratio = Math.min(1, Math.max(0, value / 100));
        return (
          <View key={index} style={[styles.coreTrack, { backgroundColor: colors.borderSubtle }]}>
            <View
              style={[
                styles.coreFill,
                {
                  height: `${Math.max(4, ratio * 100)}%`,
                  backgroundColor: ratio >= 0.9 ? colors.warning : colors.accent,
                  opacity: ratio >= 0.9 ? 1 : 0.45 + ratio * 0.55,
                },
              ]}
            />
          </View>
        );
      })}
    </View>
  );
}

export interface StackSegment {
  key: string;
  ratio: number;
  color: string;
  opacity?: number;
}

/** Rounded horizontal stack (memory used / cache / free, disk used). */
export function StackBar({ segments, height = 10 }: { segments: readonly StackSegment[]; height?: number }) {
  const { colors } = useAppTheme();
  return (
    <View
      style={[styles.stack, { height, borderRadius: height / 2, backgroundColor: colors.borderSubtle }]}
      accessible={false}
    >
      {segments.map((segment) => (
        <View
          key={segment.key}
          style={{
            width: `${Math.min(100, Math.max(0, segment.ratio * 100))}%`,
            backgroundColor: segment.color,
            opacity: segment.opacity ?? 1,
          }}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  strip: { height: 4, marginTop: 8 },
  cores: { flexDirection: "row", alignItems: "flex-end", gap: 3 },
  coreTrack: { flex: 1, height: "100%", borderRadius: 2, overflow: "hidden", justifyContent: "flex-end" },
  coreFill: { width: "100%", borderRadius: 2 },
  stack: { flexDirection: "row", overflow: "hidden" },
});
