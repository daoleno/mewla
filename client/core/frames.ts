import { platform } from "./platform";

/**
 * Compressed messages from the computer. NSH's WebSocket cannot negotiate permessage-deflate, so a
 * phone took every message at full size: a Brain thread's first snapshot is some 750 KB of JSON,
 * about 12 s through a tunnel that carries 60 KB/s. Where the platform inflates natively the
 * connection offers this subprotocol beside its auth one; a daemon that knows it then sends each
 * JSON message of 1 KB or more as one binary message of raw DEFLATE (about a third of the size),
 * and smaller ones as text. A daemon from before ignores the offer and sends text, as a browser
 * (which has permessage-deflate) is sent text. The daemon side is `daemon/server/ws_outbox.go`.
 */
export const FRAME_DEFLATE_SUBPROTOCOL = "mewla.frames.deflate-raw";

/**
 * No Session's terminal tail (`last_output_lines`): the app never shows it, and a running Session's tail
 * was most of each `worker_session_updated` (8-16 KB of terminal text every 10 s, per Session, on a link
 * that carries 60 KB/s). A daemon that knows this subprotocol sends the Session list and changes without
 * it; one from before ignores it. The daemon side is `daemon/server/feed_tailless.go`.
 */
export const TAILLESS_SUBPROTOCOL = "mewla.sessions.tailless";

/** The subprotocols to offer after the auth one. */
export function frameSubprotocols(): string[] {
  return platform().inflateRaw ? [FRAME_DEFLATE_SUBPROTOCOL, TAILLESS_SUBPROTOCOL] : [TAILLESS_SUBPROTOCOL];
}

const decoder = new TextDecoder();

/**
 * One message's text: a string as it came, a binary message inflated. Null for what is neither (a
 * binary message where nothing inflates, which a daemon sends only after the offer).
 */
export function frameText(data: unknown): string | null {
  if (typeof data === "string") return data;
  const inflate = platform().inflateRaw;
  if (!inflate) return null;
  const bytes = data instanceof Uint8Array ? data : data instanceof ArrayBuffer ? new Uint8Array(data) : ArrayBuffer.isView(data) ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength) : null;
  if (!bytes) return null;
  return decoder.decode(inflate(bytes));
}
