export type PrimaryDrawerIcon =
  | "calendar"
  | "plugins"
  | "skills"
  | "stats"
  | "browser"
  | "resources"
  | "settings";

export type PrimaryDrawerPathname =
  | "/calendar"
  | "/plugins"
  | "/skills"
  | "/stats"
  | "/browser"
  | "/resources"
  | "/settings";

export interface PrimaryDrawerDestination {
  key: string;
  label: string;
  pathname: PrimaryDrawerPathname;
  icon: PrimaryDrawerIcon;
}

/**
 * Drawer destinations, grouped by owner. The first group acts on the current
 * server; the second is the app itself. Each destination appears exactly
 * once, and every row pushes a screen. Calendar also stays in Brain's menu
 * and its notifications. Browser is temporarily hidden from the drawer; its
 * route and server resources remain.
 */
export const PRIMARY_DRAWER_GROUPS: readonly (readonly PrimaryDrawerDestination[])[] = [
  [
    { key: "calendar", label: "Calendar", pathname: "/calendar", icon: "calendar" },
    { key: "plugins", label: "Plugins", pathname: "/plugins", icon: "plugins" },
    { key: "skills", label: "Skills", pathname: "/skills", icon: "skills" },
    { key: "stats", label: "Stats", pathname: "/stats", icon: "stats" },
    {
      key: "resources",
      label: "Resources",
      pathname: "/resources",
      icon: "resources",
    },
  ],
  [
    {
      key: "settings",
      label: "Settings",
      pathname: "/settings",
      icon: "settings",
    },
  ],
];

/** One quiet caption per group, in PRIMARY_DRAWER_GROUPS order. */
export const PRIMARY_DRAWER_GROUP_CAPTIONS: readonly string[] = [
  "On this computer",
  "App",
];
