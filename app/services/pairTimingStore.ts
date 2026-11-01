import AsyncStorage from "@react-native-async-storage/async-storage";
import type { PairTimelineReport } from "./pairTiming";

/** The last pairing's timeline, readable after the fact (support, a test rig). */
export const PAIR_TIMING_KEY = "mewla.pairTiming.last";

export function rememberPairTiming(report: PairTimelineReport): void {
  void AsyncStorage.setItem(PAIR_TIMING_KEY, JSON.stringify(report)).catch(() => undefined);
}
