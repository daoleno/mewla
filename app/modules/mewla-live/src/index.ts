import { requireNativeModule } from "expo-modules-core";

export interface LiveDiagnostics {
  sdk: number;
  notificationsEnabled: boolean;
  /** Android 16 QPR1+: whether the system will promote this app's live notification; null when it cannot say. */
  promotionAllowed: boolean | null;
}

interface MewlaLiveNativeModule {
  /** False on iOS in a build without the Live Activity extension: the surface is absent, not broken. */
  isSupported(): boolean;
  apply(rawLiveState: string): boolean;
  setCurrentServer(serverRef: string): void;
  configureWidget(baseUrl: string, token: string): void;
  /** For a server with no address a widget can fetch (Mewla Link): the widgets show the state the app and pushes apply. */
  configureWidgetPushed(): void;
  clearWidget(): void;
  refreshWidgets(): void;
  /** "none" until provisioned, "ok" while the token works, "revoked" after the daemon refused it. */
  widgetState(): "none" | "ok" | "revoked";
  diagnostics(): LiveDiagnostics;
  /*
   * Calendar alerts (Android). Optional: a binary built before them lacks these, and the app then
   * falls back to the ordinary scheduled notification.
   */
  /** Replaces the armed set with these alerts (JSON array of CalendarAlarm); returns how many are armed. */
  calendarAlertsSchedule?(alarmsJson: string): number;
  /** Cancels an item's armed alarms and dismisses its posted notification. */
  calendarAlertsCancelItem?(itemId: string): void;
  /** The device-local "Strong calendar alerts" setting: alarm-style channel and lock-screen takeover. */
  calendarAlertsSetStrong?(strong: boolean): void;
  /** While the app is in front, alerts mark themselves fired without posting; the in-app card shows instead. */
  calendarAlertsSetForeground?(foreground: boolean): void;
  calendarAlertsCapabilities?(): CalendarAlertCapabilities;
  calendarAlertsOpenFullScreenSettings?(): void;
  /** Done and Snooze taken on the notification or lock-screen alert with no JS running; returns and clears them (JSON). */
  calendarAlertsTakeActions?(): string;
}

export interface CalendarAlarm {
  key: string;
  itemId: string;
  serverId: string;
  title: string;
  body: string;
  atMs: number;
}

export interface CalendarAlertCapabilities {
  exactAlarms: boolean;
  fullScreen: boolean;
  notifications: boolean;
}

/** An action taken natively on a calendar alert; JS replays it against the daemon. */
export interface CalendarNativeAction {
  type: "done" | "snooze";
  itemId: string;
  serverId: string;
  minutes: number;
  atMs: number;
}

let cached: MewlaLiveNativeModule | null | undefined;

/**
 * Android (notification, widgets) and iOS (Live Activity, widgets). Null where the platform or the
 * build has no live surface; callers treat null as "nothing to draw on".
 */
export function getMewlaLiveModule(): MewlaLiveNativeModule | null {
  if (cached === undefined) {
    try {
      const native = requireNativeModule<MewlaLiveNativeModule>("MewlaLive");
      cached = native.isSupported() ? native : null;
    } catch {
      cached = null;
    }
  }
  return cached;
}
