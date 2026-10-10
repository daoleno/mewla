import type { IconName } from "../icons/Icon";
import type { PrimaryRouteName } from "../../services/interactionTrace";

export type PrimaryDrawerPathname =
  | "/calendar"
  | "/plugins"
  | "/stats"
  | "/browser"
  | "/resources"
  | "/settings";

export interface PrimaryDrawerPlace {
  key: string;
  label: string;
  route: PrimaryRouteName;
  icon: IconName;
}

export interface PrimaryDrawerDestination {
  key: string;
  label: string;
  pathname: PrimaryDrawerPathname;
  icon: IconName;
}

/** Brain and Sessions, the two primary pages, lead the menu on every layout. */
export const PRIMARY_DRAWER_PLACES: readonly PrimaryDrawerPlace[] = [
  { key: "brain", label: "Brain", route: "brain", icon: "brain" },
  { key: "sessions", label: "Sessions", route: "list", icon: "sessions" },
];

/**
 * The tools that follow them, in one ordered list without captions. Each
 * destination appears exactly once, and every row pushes a screen. Calendar
 * also stays in Brain's menu and its notifications. Browser is temporarily
 * hidden from the drawer; its route and server resources remain.
 */
export const PRIMARY_DRAWER_DESTINATIONS: readonly PrimaryDrawerDestination[] = [
  { key: "calendar", label: "Calendar", pathname: "/calendar", icon: "calendar" },
  { key: "plugins", label: "Extensions", pathname: "/plugins", icon: "plugins" },
  { key: "stats", label: "Stats", pathname: "/stats", icon: "stats" },
  { key: "resources", label: "Resources", pathname: "/resources", icon: "resources" },
];

/** Settings sits apart at the bottom of the menu, above the server status. */
export const PRIMARY_DRAWER_SETTINGS: PrimaryDrawerDestination = {
  key: "settings",
  label: "Settings",
  pathname: "/settings",
  icon: "settings",
};
