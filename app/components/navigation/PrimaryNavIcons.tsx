import React from "react";
import Svg, { Circle, Path } from "react-native-svg";

interface PrimaryNavIconProps {
  color: string;
  size?: number;
}

const STROKE = 1.75;

function iconSize(size: number | undefined): number {
  return size ?? 22;
}

/** Minimal two-line menu glyph for the primary drawer control. */
export function NavMenuIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M5 9.25h14"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M5 14.75h14"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Quiet outline close mark for the drawer. */
export function NavCloseIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M7 7l10 10"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17 7L7 17"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Restrained outline chevron for drawer rows. */
export function NavChevronIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M9.5 6.5L15.5 12l-6 5.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

/** Outline bar chart for Stats. */
export function NavStatsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M6.5 16.5v-4"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M12 16.5V7.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17.5 16.5v-7"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Outline gauge with a soft pulse line for machine Resources. */
export function NavResourcesIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M4.75 6.75a2 2 0 0 1 2-2h10.5a2 2 0 0 1 2 2v10.5a2 2 0 0 1-2 2H6.75a2 2 0 0 1-2-2z"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinejoin="round"
      />
      <Path
        d="M8 13h1.75l1.5-3.5 2 6 1.5-2.5H16"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

/** Stacked layers for Skills: reusable capabilities an agent can load. */
export function NavSkillsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M12 4.75l7.25 3.75L12 12.25 4.75 8.5z"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinejoin="round"
      />
      <Path
        d="M4.75 12.25L12 16l7.25-3.75"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <Path
        d="M4.75 15.75L12 19.5l7.25-3.75"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

/** Outline plug for Plugins: external services connected to Brain. */
export function NavPluginsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M9.25 4.5v3.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M14.75 4.5v3.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M7 8h10v2.75a5 5 0 0 1-10 0z"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinejoin="round"
      />
      <Path
        d="M12 15.75v3.75"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Outline sliders for Settings — stroke-only, no fill. */
export function NavSettingsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M5 8h2.2"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M11.8 8H19"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M5 16h8.2"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17.8 16H19"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Circle
        cx={9}
        cy={8}
        r={2.1}
        stroke={color}
        strokeWidth={STROKE}
        fill="none"
      />
      <Circle
        cx={15}
        cy={16}
        r={2.1}
        stroke={color}
        strokeWidth={STROKE}
        fill="none"
      />
    </Svg>
  );
}

/** Vertical overflow glyph for primary page actions. */
export function NavOverflowIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Circle cx={12} cy={6.5} r={1.15} fill={color} />
      <Circle cx={12} cy={12} r={1.15} fill={color} />
      <Circle cx={12} cy={17.5} r={1.15} fill={color} />
    </Svg>
  );
}
