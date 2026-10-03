export type PrimaryDrawerIcon =
  | "plugins"
  | "skills"
  | "stats"
  | "resources"
  | "settings";

export type PrimaryDrawerPathname =
  | "/plugins"
  | "/skills"
  | "/stats"
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
 * once, and every row pushes a screen.
 */
export const PRIMARY_DRAWER_GROUPS: readonly (readonly PrimaryDrawerDestination[])[] = [
  [
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
