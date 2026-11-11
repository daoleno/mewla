import type { DayCell, ModelStat, PricingStatus } from "../../core/stats/statsPayload";
import type { ExtensionsPage } from "./extensions/routes";
import type { FilesPage } from "./files/routes";
import type { FriendsPage } from "./friends/routes";
import type { GitPage } from "./git/routes";

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
  | { name: "settings-phone" }
  | { name: "settings-virtual-screen" }
  | { name: "settings-pet" }
  | { name: "settings-update" }
  | { name: "settings-licenses" }
  | { name: "settings-license"; id: string }
  | { name: "stats" }
  | { name: "stats-model"; model: ModelStat; pricing?: PricingStatus }
  | { name: "stats-day"; day: DayCell }
  | { name: "resources" }
  /** `worker`: open on that agent's display (the floating screen's Expand). */
  | { name: "screen"; worker?: string }
  | ExtensionsPage
  | FriendsPage
  | GitPage
  | FilesPage;

/** How a page opens the next one (the More tab's stack, or a test's). */
export interface PageNav {
  push(route: MorePage): void;
  pop(): void;
  /** The stack's routes, bottom to top (reactive): a page is on top again when the last one is its route. */
  routes?: () => readonly { name: string }[];
}
