/**
 * Where a tapped push opens the app, as the `mewla://` link the app's other entry points
 * use (a live notification, a calendar alert, a widget): one way in for all of them.
 * `server` is the computer the push came from (the id this phone registered with it).
 */
export function pushLink(data: unknown): string | null {
  if (!data || typeof data !== "object") return null;
  const fields = data as Record<string, unknown>;
  const text = (name: string) => (typeof fields[name] === "string" && fields[name] !== "" ? (fields[name] as string) : undefined);
  const server = text("server_id");
  const query = (params: Record<string, string | undefined>) => {
    const parts = Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => `${k}=${encodeURIComponent(v as string)}`);
    return parts.length ? `?${parts.join("&")}` : "";
  };
  const worker = text("worker_id");
  if (worker && server) return `mewla://sessions/${encodeURIComponent(worker)}${query({ serverId: server })}`;
  switch (fields.screen) {
    case "inbox":
      return "mewla://sessions";
    case "calendar":
      return `mewla://calendar${query({ id: text("calendar_id"), serverId: server })}`;
    case "brain":
      return `mewla://brain${query({ thread: text("brain_thread_id"), message: text("brain_message_id"), serverId: server })}`;
    default:
      return null;
  }
}

/** A calendar alert (pushed or the phone's own): while the app is open, the in-app card shows it instead. */
export function isCalendarAlert(data: unknown): boolean {
  if (!data || typeof data !== "object") return false;
  const fields = data as Record<string, unknown>;
  return fields.screen === "calendar" && typeof fields.alert_key === "string";
}

export interface ParsedLink {
  /** `brain`, `sessions`, `calendar`, `live`, `more`, … */
  section: string;
  /** The path after the section, decoded: `["work", "w1"]` for `mewla://brain/work/w1`. */
  path: string[];
  params: Record<string, string>;
}

/** `mewla://calendar?id=c1&serverId=s1` → `{section: "calendar", path: [], params: {id, serverId}}`; null for anything else. */
export function parseLink(url: string): ParsedLink | null {
  const match = /^mewla:\/\/([^/?#]+)([^?#]*)(?:\?([^#]*))?/i.exec(url.trim());
  if (!match) return null;
  const params: Record<string, string> = {};
  for (const part of (match[3] ?? "").split("&")) {
    if (!part) continue;
    const [key, value = ""] = part.split("=");
    try {
      params[decodeURIComponent(key)] = decodeURIComponent(value.replace(/\+/g, " "));
    } catch {
      return null;
    }
  }
  const path = match[2].split("/").filter(Boolean).map((p) => {
    try {
      return decodeURIComponent(p);
    } catch {
      return p;
    }
  });
  return { section: match[1].toLowerCase(), path, params };
}
