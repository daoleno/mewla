import React, { useEffect, useState } from "react";
import { AppState, Platform } from "react-native";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import { getMewlaLiveModule } from "../../modules/mewla-live/src";
import {
  setStrongCalendarAlerts,
  strongCalendarAlerts,
} from "../../services/calendarAlertSettings";

/**
 * The one calendar decision: how hard an alert is to miss. On (the default),
 * Android takes over the lock screen with an alarm-style alert and iOS breaks
 * through Focus; off, alerts are ordinary notifications. The in-app card shows
 * either way.
 */
export function CalendarAlertsSection() {
  const [strong, setStrong] = useState(strongCalendarAlerts);
  const [fullScreenAllowed, setFullScreenAllowed] = useState(true);
  useEffect(() => {
    const read = () => {
      const capabilities = getMewlaLiveModule()?.calendarAlertsCapabilities?.();
      if (capabilities) setFullScreenAllowed(capabilities.fullScreen);
    };
    read();
    // The person may have just allowed it in system settings.
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active") read();
    });
    return () => subscription.remove();
  }, []);
  if (Platform.OS === "web") return null;
  const android = Platform.OS === "android";
  return (
    <ListSection
      footer={
        android
          ? "Takes over a locked screen with Done and Snooze. Off, alerts are ordinary notifications."
          : "Breaks through Focus as a Time Sensitive notification. Off, alerts are ordinary notifications."
      }
    >
      <ListRow
        title="Strong calendar alerts"
        subtitle="Events, reminders and deadlines you can't miss"
        icon="calendar"
        accessibilityRole="switch"
        trailing={
          <InkSwitch
            value={strong}
            accessibilityLabel="Strong calendar alerts"
            onValueChange={(next) => {
              setStrong(next);
              void setStrongCalendarAlerts(next);
            }}
          />
        }
      />
      {android && strong && !fullScreenAllowed ? (
        <ListRow
          title="Allow full-screen alerts"
          subtitle="Android needs your OK to show an alert over the lock screen"
          icon="warning"
          accessory="chevron"
          onPress={() => getMewlaLiveModule()?.calendarAlertsOpenFullScreenSettings?.()}
        />
      ) : null}
    </ListSection>
  );
}
