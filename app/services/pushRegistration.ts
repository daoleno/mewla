import { Platform } from "react-native";
import { getMewlaLiveModule } from "../modules/mewla-live/src";
import { strongCalendarAlerts } from "./calendarAlertSettings";

let pushToken: string | undefined;

export function rememberPushToken(token: string) {
  pushToken = token;
}

export function hasPushToken() {
  return pushToken !== undefined;
}

/**
 * What the app tells a daemon about this phone so pushes fit it: the token, the
 * time zone and language Watch follows, whether it takes live state, and how
 * calendar alerts should arrive (Android posts them itself; iOS shows them).
 */
export function registerPushMessage(serverId: string, token = pushToken) {
  if (!token) return null;
  // An Android build without the native alert code cannot post a data-only
  // alert, so it says nothing and gets the ordinary visible push.
  const platform =
    Platform.OS === "android" && !getMewlaLiveModule()?.calendarAlertsSchedule
      ? ""
      : Platform.OS;
  return {
    type: "register_push",
    push_token: token,
    server_ref: serverId,
    time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    locale: Intl.DateTimeFormat().resolvedOptions().locale,
    platform,
    strong_alerts: strongCalendarAlerts(),
    // The Android live surface is a data push only this build can show.
    ...(getMewlaLiveModule() ? { live_state: true } : {}),
  };
}
