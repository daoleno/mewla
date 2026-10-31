/**
 * The frames on the phone's WebSocket to its computer (/phone/device). Mirrors
 * daemon/phone/protocol.go: change them together.
 */
export const PHONE_PROTOCOL_VERSION = 1;

export type PhoneState = "ready" | "setup" | "person_in_control" | "stopped";

export interface PhoneHello {
  type: "hello";
  version: number;
  model: string;
  sdk: number;
  app_version: string;
  state: PhoneState;
}

export interface PhoneStateFrame {
  type: "state";
  state: PhoneState;
  w?: number;
  h?: number;
  /** Why the helper is not up, when the state is setup. */
  problem?: string;
  /** The agent display's density as asked and as the system has it, next to the phone's own screen. */
  display?: string;
}

export type PhoneEventKind = "stop" | "take_over" | "give_back" | "handed_off";

export interface PhoneEventFrame {
  type: "event";
  kind: PhoneEventKind;
  id?: string;
}

/** What the daemon asks of the phone; the native side runs it and answers with a result frame. */
export interface PhoneRequestFrame {
  type: "request";
  id: string;
  op: string;
  worker?: string;
  worker_name?: string;
  args?: unknown;
  image?: boolean;
}

export type PhoneFrameFromDaemon = PhoneRequestFrame;

export function parseDaemonFrame(raw: string): PhoneFrameFromDaemon | null {
  try {
    const frame = JSON.parse(raw) as { type?: unknown };
    if (frame && typeof frame === "object" && typeof frame.type === "string") return frame as PhoneFrameFromDaemon;
  } catch {
    // Not a frame: ignore it.
  }
  return null;
}
