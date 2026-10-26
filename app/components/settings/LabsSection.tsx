import React from "react";
import { Platform } from "react-native";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { useFriendsLabs } from "../../store/friendsLabs";

/** Things still being tried. Today one: Friends, off until the owner turns it on. */
export function LabsSection() {
  const { phase, handle, linked, error, available, setEnabled, useThisComputer } = useFriendsLabs();
  // Friends is a phone feature; the web build has no secure key store.
  if (Platform.OS === "web") return null;
  const busy = phase === "working";
  const subtitle = error
    ? error
    : busy
      ? "Signing in…"
      : phase === "on"
        ? handle
          ? `@${handle}`
          : "On"
        : available
          ? "Your Brain shares what it finishes"
          : "Connect a computer first";
  return (
    <ListSection>
      <ListRow
        title="Friends"
        subtitle={subtitle}
        icon="person-circle"
        disabled={!available && phase === "off"}
        accessibilityRole="switch"
        trailing={
          <InkSwitch
            value={phase === "on" || busy}
            disabled={busy || (!available && phase === "off")}
            accessibilityLabel="Friends"
            onValueChange={(next) => void setEnabled(next)}
          />
        }
      />
      {phase === "on" && linked === false ? (
        <ListRow
          title="Use this computer’s Brain"
          subtitle="Another computer is your agent now"
          icon="desktop"
          accessory="chevron"
          disabled={busy}
          onPress={() => void useThisComputer()}
        />
      ) : null}
    </ListSection>
  );
}
