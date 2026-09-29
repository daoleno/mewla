import React from "react";
import {
  Platform,
  StyleSheet,
  View,
  type StyleProp,
  type ViewProps,
  type ViewStyle,
} from "react-native";
import { GlassView, isLiquidGlassAvailable } from "expo-glass-effect";
import { ContinuousCorners, shadow, useAppTheme } from "../../constants/tokens";
import type { MaterialPalette } from "../../theme";
import { relativeLuminance } from "../../theme/colorUtils";
import { outlinedSurface } from "./outlinedSurface";
import { useReduceTransparency } from "./useReduceTransparency";

export type GlassMaterial = keyof Pick<
  MaterialPalette,
  "chrome" | "regular" | "thick" | "thin"
>;

export interface GlassSurfaceProps extends ViewProps {
  material?: GlassMaterial;
  radius?: number;
  elevation?: "none" | "card" | "raised" | "float";
  /** Accent-tinted fill for selected or primary floating controls. */
  tinted?: boolean;
  /** iOS Liquid Glass reacts to touch. */
  interactive?: boolean;
  /**
   * Fallback fill and hairline for surfaces that sit on a non-app canvas
   * (terminal-theme chat chrome) so the material follows that canvas.
   */
  fill?: string;
  stroke?: string;
  style?: StyleProp<ViewStyle>;
}

const LIQUID_GLASS = Platform.OS === "ios" && isLiquidGlassAvailable();

/**
 * The single material owner. iOS 26 renders system Liquid Glass; elsewhere a
 * translucent fill and one continuous outline stand in for it. The outline is
 * the only edge: a partial lit band would brighten the straight run and fall
 * back at the corners. Reduce Transparency switches to an opaque fill, and so
 * does a floating Android surface, whose elevation shadow smears through a
 * translucent one.
 */
export function GlassSurface({
  material = "regular",
  radius = 0,
  elevation = "none",
  tinted = false,
  interactive = false,
  fill: fillOverride,
  stroke: strokeOverride,
  style,
  children,
  ...viewProps
}: GlassSurfaceProps) {
  const { theme, colors } = useAppTheme();
  const reduceTransparency = useReduceTransparency();
  const materials = theme.materials;
  const lift =
    elevation === "none" ? null : shadow(elevation, colors.shadowColor);
  const shape: ViewStyle = { borderRadius: radius, ...ContinuousCorners };

  if (LIQUID_GLASS && !reduceTransparency) {
    return (
      <GlassView
        {...viewProps}
        glassEffectStyle={material === "thin" ? "clear" : "regular"}
        colorScheme={fillOverride ? schemeForFill(fillOverride, theme.colorScheme) : theme.colorScheme}
        tintColor={tinted ? colors.accent : undefined}
        isInteractive={interactive}
        style={[shape, lift, style]}
      >
        {children}
      </GlassView>
    );
  }

  const opaque = reduceTransparency || (Platform.OS === "android" && lift?.elevation != null);
  const fill = opaque
    ? material === "thin"
      ? colors.bgElevated
      : colors.modalSurface
    : fillOverride ?? materials[material];

  return (
    <View
      {...viewProps}
      style={[
        outlinedSurface(radius, strokeOverride ?? materials.stroke),
        { backgroundColor: fill },
        lift,
        style,
      ]}
    >
      {tinted ? (
        <View
          pointerEvents="none"
          style={[StyleSheet.absoluteFill, shape, { backgroundColor: materials.tint }]}
        />
      ) : null}
      {children}
    </View>
  );
}

/**
 * A caller-supplied fill (terminal-theme chat chrome) decides whether system
 * glass renders light or dark, so it matches that canvas, not the app theme.
 */
function schemeForFill(fill: string, fallback: "light" | "dark"): "light" | "dark" {
  if (!/^#[0-9a-f]{6}$/i.test(fill)) return fallback;
  return relativeLuminance(fill) > 0.4 ? "light" : "dark";
}
