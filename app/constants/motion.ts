import type { WithSpringConfig } from "react-native-reanimated";

export const Spring = {
  press: {
    stiffness: 420,
    damping: 30,
    mass: 0.9,
  } as WithSpringConfig,

  card: {
    stiffness: 340,
    damping: 28,
    mass: 1,
  } as WithSpringConfig,

  rise: {
    stiffness: 260,
    damping: 26,
    mass: 1,
  } as WithSpringConfig,
} as const;

export const PRESSED_SCALE = 0.96;

/**
 * Product motion: short, eased out, never bouncy. Springs above are for touch
 * feedback; things that appear, open or turn use these durations (ms).
 */
export const Timing = {
  /** A row or a block of details appearing. */
  quick: 150,
  /** A disclosure chevron turning, or a surface fading in. */
  base: 200,
} as const;
