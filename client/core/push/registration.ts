import type { KeyValueStore } from "../platform";

/** The Expo project the daemon's pushes go through (`app.config.js`'s official project). */
export const EXPO_PROJECT_ID = "1b76d386-969b-4841-ae68-f32895ad2301";
const EXPO_TOKEN_URL = "https://exp.host/--/api/v2/push/getExpoPushToken";
/** Mewla asks for notifications once, ever (the Expo app's key, so an upgraded phone is not asked again). */
export const PERMISSION_ASKED_KEY = "mewla.push.permission_asked.v1";
const TOKEN_KEY = "mewla:v1:push_token";

/** What the host knows of this install's push identity (`push.deviceToken`). */
export interface DeviceToken {
  type: "fcm" | "apns";
  token: string;
  /** Expo's installation id, kept where expo-notifications kept it: the Expo token stays the same. */
  deviceId: string;
  appId: string;
  development: boolean;
}

export interface PushHost {
  permission(): Promise<{ granted: boolean }>;
  requestPermission(): Promise<boolean>;
  deviceToken(): Promise<DeviceToken>;
}

type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

/** True once notifications are allowed; the system prompt shows at most once in the app's life. */
export async function ensurePermission(host: PushHost, storage: KeyValueStore): Promise<boolean> {
  if ((await host.permission()).granted) return true;
  let asked = true;
  try {
    asked = (await storage.getItem(PERMISSION_ASKED_KEY)) === "1";
  } catch {
    // Unreadable storage must not turn into a prompt on every connection.
  }
  if (asked) return false;
  await storage.setItem(PERMISSION_ASKED_KEY, "1").catch(() => {});
  return host.requestPermission();
}

/**
 * The Expo push token for this install, as expo-notifications got it: the device's
 * FCM/APNs token exchanged at Expo's service. The daemon sends through Expo, so it never
 * learns the difference. The exchange is kept per device token; a rotated device token
 * is exchanged again.
 */
export async function expoPushToken(host: PushHost, storage: KeyValueStore, fetch: Fetch, projectId = EXPO_PROJECT_ID): Promise<string> {
  const device = await host.deviceToken();
  const remembered = await storage.getItem(TOKEN_KEY).catch(() => null);
  if (remembered) {
    try {
      const saved = JSON.parse(remembered) as { device?: string; expo?: string };
      if (saved.device === device.token && saved.expo) return saved.expo;
    } catch {
      // Exchange again.
    }
  }
  const response = await fetch(EXPO_TOKEN_URL, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      type: device.type,
      deviceId: device.deviceId.toLowerCase(),
      development: device.development,
      appId: device.appId,
      deviceToken: device.token,
      projectId,
    }),
  });
  if (!response.ok) throw new Error(`Expo token exchange failed (${response.status})`);
  const body = (await response.json()) as { data?: { expoPushToken?: unknown } };
  const expo = body?.data?.expoPushToken;
  if (typeof expo !== "string" || !expo) throw new Error("Expo token exchange: malformed answer");
  await storage.setItem(TOKEN_KEY, JSON.stringify({ device: device.token, expo })).catch(() => {});
  return expo;
}

export interface RegistrationFacts {
  /** IANA zone of the phone: Watch's quiet hours and the calendar's wall clock follow it. */
  timeZone: string;
  locale: string;
  platform: "android" | "ios";
  /** The phone's own "Strong calendar alerts" setting. */
  strongAlerts: boolean;
  /** Whether this build shows the daemon's live_state (the Android live notification, the iOS Live Activity). */
  liveState: boolean;
}

/** What tells a computer about this phone so its pushes fit it. `server_ref` comes back as `server_id` in each push. */
export function registerPushMessage(serverId: string, token: string, facts: RegistrationFacts) {
  return {
    type: "register_push",
    push_token: token,
    server_ref: serverId,
    time_zone: facts.timeZone,
    locale: facts.locale,
    platform: facts.platform,
    strong_alerts: facts.strongAlerts,
    ...(facts.liveState ? { live_state: true } : {}),
  };
}
