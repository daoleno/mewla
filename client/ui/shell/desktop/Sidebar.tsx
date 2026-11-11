import { keymap, platform, Text, View } from "nsh";
import { Button, Tooltip } from "nsh/base";
import { sidebarRows, type DesktopPlaceKey, type WebPlace } from "../../../core/web/places";
import { desktopShortcutTooltip } from "../../../core/web/shortcuts";
import { friendsEnabled } from "../../more/friends/state";
import { Icon, type IconName } from "../../more/kit/Icon";
import { ConnectionLine } from "../../screens/More";
import { openPlace, selectedPlace } from "../Shell";
import { needsYou } from "../attention";
import { paletteOpen } from "./Overlays";

const ICONS: Record<DesktopPlaceKey, IconName> = {
  brain: "brain",
  sessions: "sessions",
  calendar: "calendar",
  extensions: "plugins",
  screen: "desktop",
  stats: "stats",
  resources: "resources",
  friends: "people",
  settings: "settings",
};

function Hint(props: { text: string }) {
  return <Tooltip class="rounded-md bg-popover px-2 py-1 border border-border"><Text class="text-xs text-popover-foreground">{props.text}</Text></Tooltip>;
}

/**
 * The desktop's one way around: every place, the one you are in selected (a
 * Session selects Sessions), the palette's key, and the computer's connection.
 * Friends stays off the web, as it was.
 */
export function Sidebar() {
  const rows = () => sidebarRows({ friends: platform.os !== "web" && friendsEnabled() });
  return (
    <View testID="sidebar" accessibilityLabel="Navigation" class="flex-1 bg-secondary px-2 pt-3">
      <Button testID="sidebar-search" accessibilityLabel="Command palette" onPress={() => paletteOpen.set(true)}
        class="mb-3 flex-row items-center gap-2 rounded-lg border border-border bg-background px-3 py-2 hover:bg-accent">
        <Icon name="search" size={16} />
        <Text class="flex-1 text-sm text-muted-foreground">Search or run…</Text>
        <Text class="text-xs text-muted-foreground">{keymap.label("mod+k")}</Text>
      </Button>
      <View class="gap-0.5">
        {rows().map((row) => {
          const tip = desktopShortcutTooltip(row.label, row.key);
          return (
            <Button testID={`sidebar-${row.key}`} accessibilityLabel={row.label} selected={selectedPlace() === row.key}
              tooltip={tip ? <Hint text={tip} /> : undefined}
              onPress={() => openPlace({ key: row.key } as WebPlace)}
              class={selectedPlace() === row.key ? "flex-row items-center gap-3 rounded-lg bg-accent px-3 py-2" : "flex-row items-center gap-3 rounded-lg px-3 py-2 hover:bg-accent"}>
              <Icon name={ICONS[row.key]} size={18} tone={selectedPlace() === row.key ? "ink" : "soft"} />
              <Text class={selectedPlace() === row.key ? "flex-1 text-sm font-semibold" : "flex-1 text-sm text-muted-foreground"}>{row.label}</Text>
              {needsYou(row.key) ? <View testID={`sidebar-${row.key}-dot`} accessibilityLabel="Needs you" class="h-2 w-2 rounded-full bg-seal" /> : null}
            </Button>
          );
        })}
      </View>
      <View class="flex-1" />
      <ConnectionLine />
    </View>
  );
}
