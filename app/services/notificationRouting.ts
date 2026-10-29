export type NotificationDestination =
  | { kind: "terminal"; workerId: string; serverId: string }
  | { kind: "inbox" }
  | { kind: "calendar"; calendarId?: string; serverId?: string }
  | {
      kind: "brain";
      brainThreadId?: string;
      brainMessageId?: string;
      serverId?: string;
    };

/** The notification's routing data, from a received notification. */
type ReceivedNotification = { request?: { content?: { data?: unknown } } };

/** A calendar alert (local or pushed): the in-app card is what shows it while the app is open. */
export function isCalendarAlert(data: unknown): boolean {
  if (!data || typeof data !== "object") return false;
  const fields = data as Record<string, unknown>;
  return fields.screen === "calendar" && typeof fields.alert_key === "string";
}

export async function foregroundNotificationPresentation(notification?: ReceivedNotification) {
  if (isCalendarAlert(notification?.request?.content?.data)) {
    return {
      shouldPlaySound: false,
      shouldSetBadge: false,
      shouldShowBanner: false,
      shouldShowList: false,
    };
  }
  return {
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  };
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

export function resolveNotificationDestination(
  data: unknown,
): NotificationDestination | null {
  if (!data || typeof data !== "object") {
    return null;
  }
  const fields = data as Record<string, unknown>;
  const workerId = optionalString(fields.worker_id);
  const serverId = optionalString(fields.server_id);
  if (workerId && serverId) {
    return { kind: "terminal", workerId, serverId };
  }

  switch (fields.screen) {
    case "inbox":
      return { kind: "inbox" };
    case "calendar":
      return {
        kind: "calendar",
        calendarId: optionalString(fields.calendar_id),
        serverId,
      };
    case "brain":
      return {
        kind: "brain",
        brainThreadId: optionalString(fields.brain_thread_id),
        brainMessageId: optionalString(fields.brain_message_id),
        serverId,
      };
    default:
      return null;
  }
}
