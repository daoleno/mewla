import type { DayCell, ModelStat, PricingStatus } from "../../core/stats/statsPayload";
import type { ExtensionsPage } from "./extensions/routes";
import type { FriendsPage } from "./friends/routes";

/** The pages behind More, and the pages they push. */
export type MorePage =
  | { name: "settings" }
  | { name: "settings-computer"; id: string }
  | { name: "settings-telegram" }
  | { name: "settings-providers" }
  | { name: "settings-provider-editor"; client: "codex" | "claude"; connectionId?: string; retry?: boolean }
  | { name: "settings-provider-models"; connectionId: string }
  | { name: "settings-agents" }
  | { name: "settings-friends" }
  | { name: "settings-iphone" }
  | { name: "settings-pet" }
  | { name: "settings-licenses" }
  | { name: "settings-license"; id: string }
  | { name: "stats" }
  | { name: "stats-model"; model: ModelStat; pricing?: PricingStatus }
  | { name: "stats-day"; day: DayCell }
  | { name: "resources" }
  | { name: "screen" }
  | ExtensionsPage
  | FriendsPage;

/** How a page opens the next one (the More tab's stack, or a test's). */
export interface PageNav {
  push(route: MorePage): void;
  pop(): void;
}
