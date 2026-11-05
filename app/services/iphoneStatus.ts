/**
 * What the current computer says about iPhone use: agents there use the person's iPhone through
 * iPhone Mirroring on that Mac. The switch belongs to the computer, so it works from any phone.
 */
export interface IPhoneDaemonStatus {
  enabled: boolean;
  /** ready, off, unsupported (not a Mac) or blocked (Summary says what the Mac needs). */
  state: "ready" | "off" | "unsupported" | "blocked" | string;
  summary: string;
  /** open_settings when a Mac permission is missing. */
  fix?: string;
  permission?: string;
  /** Whether iPhone Mirroring is open on the Mac. */
  running?: boolean;
  /** The agent using the iPhone now. */
  active?: { worker: string; name?: string; since: string };
  /** A base64 JPEG of the iPhone as the agent last read it, when asked for: Watch. */
  picture?: string;
  picture_at?: string;
}

/** The one word Labs shows for iPhone use. */
export function iphoneStateWord(status: IPhoneDaemonStatus | null): string {
  if (!status || !status.enabled) return "Off";
  if (status.active) return "In use";
  if (status.state === "ready") return "Ready";
  if (status.state === "unsupported") return "Needs a Mac";
  return "Set up";
}

/** Whether the current computer can offer iPhone use at all: a Mewla on a Mac. */
export function iphoneOffered(status: IPhoneDaemonStatus | null): boolean {
  return !!status && status.state !== "unsupported";
}
