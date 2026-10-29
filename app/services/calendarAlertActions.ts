import type { CalendarNativeAction } from "../modules/mewla-live/src";
import { remainingSnoozeMinutes } from "./calendarAlerts";
import { dismissCalendarAlert, takeNativeCalendarActions } from "./calendarNotifications";
import { warnOnce } from "./warnOnce";
import { wsClient } from "./websocket";

let pending: CalendarNativeAction[] = [];

/**
 * Tells the daemon what the person did on a calendar alert while no JS ran
 * (Done or Snooze on the lock-screen alert or its notification), so the other
 * phones close the card too. An action for a server that is not connected waits
 * for the next flush.
 */
export async function replayNativeCalendarActions() {
  pending = [...pending, ...takeNativeCalendarActions()];
  const waiting: CalendarNativeAction[] = [];
  for (const action of pending) {
    if (!wsClient.isConnected(action.serverId)) {
      waiting.push(action);
      continue;
    }
    try {
      if (action.type === "done") {
        await wsClient.doneCalendarItem(action.serverId, action.itemId);
      } else {
        // A snooze that already ran out has alerted again on the phone; the
        // daemon's own alert for it is covered by the phone's dedup.
        const minutes = remainingSnoozeMinutes(action);
        if (minutes < 1) continue;
        await wsClient.snoozeCalendarItem(action.serverId, action.itemId, minutes);
      }
      await dismissCalendarAlert(action.itemId);
    } catch (error) {
      // The item may be gone or already answered on another phone.
      warnOnce("calendar.native-actions", error);
    }
  }
  pending = waiting;
}
