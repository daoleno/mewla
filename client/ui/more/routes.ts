import type { DayCell, ModelStat, PricingStatus } from "../../core/stats/statsPayload";

/** The pages behind More, and the pages they push. */
export type MorePage =
  | { name: "settings" }
  | { name: "stats" }
  | { name: "stats-model"; model: ModelStat; pricing?: PricingStatus }
  | { name: "stats-day"; day: DayCell }
  | { name: "resources" }
  | { name: "screen" }
  | { name: "extensions" };

/** How a page opens the next one (the More tab's stack, or a test's). */
export interface PageNav {
  push(route: MorePage): void;
  pop(): void;
}
