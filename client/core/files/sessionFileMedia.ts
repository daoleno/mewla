export type SessionFileMediaKind = "video" | "audio";

/** What a media element reports; the same shape on Web and inside the native WebView. */
export interface SessionFileMediaStatus {
  currentTime: number;
  duration: number;
  paused: boolean;
  muted: boolean;
  ended: boolean;
  waiting: boolean;
  /** The element has decoded data for the current source. */
  loaded: boolean;
}

export const initialSessionFileMediaStatus: SessionFileMediaStatus = {
  currentTime: 0,
  duration: 0,
  paused: true,
  muted: false,
  ended: false,
  waiting: true,
  loaded: false,
};

/**
 * A stream URL carries a short-lived read capability, and every seek is a new
 * Range request. When the element errors after it had played data, the
 * capability (or Link tunnel port) has most likely lapsed, so the player signs
 * a fresh source and resumes. An error before any data means the file itself
 * can't be played, which a new signature won't fix.
 */
export const SESSION_FILE_MEDIA_RECOVERY_LIMIT = 3;

export function sessionFileMediaRecovery(input: {
  loadedSinceSource: boolean;
  attempts: number;
}): "reauthorize" | "fail" {
  if (!input.loadedSinceSource) return "fail";
  return input.attempts < SESSION_FILE_MEDIA_RECOVERY_LIMIT
    ? "reauthorize"
    : "fail";
}

export interface SessionFileMediaSource {
  uri: string;
}

export interface SessionFileMediaRecoveryHost<S extends SessionFileMediaSource> {
  kind: SessionFileMediaKind;
  /** Signs a fresh read capability for the same file and generation. */
  resolve(): Promise<S>;
  /** Plays `source` from `resumeAt` seconds; `play` when it was playing. */
  load(source: S, resumeAt: number, play: boolean): void;
  /** Playback can't go on: what the person reads, and whether the file changed. */
  fail(message: string, stale: boolean): void;
  isStale(error: unknown): boolean;
}

/**
 * The player's side of a stream whose capability lapses (2 minutes): it hears
 * the player's load, progress and error, and on an error after data played it
 * signs a fresh source and resumes where it was, at most
 * `SESSION_FILE_MEDIA_RECOVERY_LIMIT` times in a row (playback moving again
 * resets the count). An error before any data is a file this player can't play.
 */
export function createSessionFileMediaRecovery<S extends SessionFileMediaSource>(host: SessionFileMediaRecoveryHost<S>) {
  let loadedSinceSource = false;
  let attempts = 0;
  let recovering = false;
  let disposed = false;
  let currentTime = 0;
  let resumedAt = 0;
  let playing = false;
  return {
    loaded(): void {
      loadedSinceSource = true;
    },
    progress(time: number): void {
      if (time > currentTime) playing = true;
      currentTime = time;
      // Playback moving again after a recovery means the stream is healthy.
      if (time > resumedAt + 1) attempts = 0;
    },
    ended(): void {
      playing = false;
    },
    paused(): void {
      playing = false;
    },
    error(): void {
      if (recovering || disposed) return;
      if (sessionFileMediaRecovery({ loadedSinceSource, attempts }) === "fail") {
        host.fail(
          loadedSinceSource
            ? "Playback stopped and couldn't resume. Refresh and try again."
            : `${host.kind === "video" ? "This video" : "This audio"} can't be played here. Download it to play it in another app.`,
          false,
        );
        return;
      }
      attempts += 1;
      recovering = true;
      const resumeAt = currentTime;
      const play = playing;
      host
        .resolve()
        .then((next) => {
          if (disposed) return;
          loadedSinceSource = false;
          resumedAt = resumeAt;
          host.load(next, resumeAt, play);
        })
        .catch((error: unknown) => {
          if (disposed) return;
          host.fail(error instanceof Error && error.message ? error.message : "Could not authorize the stream again.", host.isStale(error));
        })
        .finally(() => {
          recovering = false;
        });
    },
    dispose(): void {
      disposed = true;
    },
  };
}

/** 0:07, 1:04, 12:30, 1:02:03; hours appear only when the media is that long. */
export function formatSessionFileMediaTime(
  seconds: number,
  duration = seconds,
): string {
  // A seek lands on the frame just before the asked-for second (29.98 for
  // 0:30); read that as the second it was aimed at.
  const total =
    Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds + 0.05) : 0;
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = String(total % 60).padStart(2, "0");
  if (hours > 0 || (Number.isFinite(duration) && duration >= 3600)) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${secs}`;
  }
  return `${minutes}:${secs}`;
}

/** The time under a point on the scrubber track, clamped to the media. */
export function sessionFileMediaScrubTime(
  x: number,
  width: number,
  duration: number,
): number {
  if (!(width > 0) || !(duration > 0) || !Number.isFinite(x)) return 0;
  return Math.min(duration, Math.max(0, (x / width) * duration));
}

export function sessionFileMediaProgress(
  currentTime: number,
  duration: number,
): number {
  if (!(duration > 0) || !Number.isFinite(currentTime)) return 0;
  return Math.min(1, Math.max(0, currentTime / duration));
}

export type SessionFileMediaEvent =
  | { type: "status"; status: SessionFileMediaStatus }
  | { type: "error"; code: number };

/** Parses a message posted by the native WebView player; anything else is dropped. */
export function parseSessionFileMediaEvent(
  raw: string,
): SessionFileMediaEvent | null {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!value || typeof value !== "object") return null;
  const source = value as Record<string, unknown>;
  if (source.type === "error") {
    return {
      type: "error",
      code: typeof source.code === "number" ? source.code : 0,
    };
  }
  if (source.type !== "status") return null;
  return {
    type: "status",
    status: {
      currentTime: finiteSeconds(source.currentTime),
      duration: finiteSeconds(source.duration),
      paused: source.paused !== false,
      muted: source.muted === true,
      ended: source.ended === true,
      waiting: source.waiting === true,
      loaded: source.loaded === true,
    },
  };
}

function finiteSeconds(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? value
    : 0;
}

const VIDEO_EXTENSION_RE = /\.(?:m4v|mov|mp4|ogv|webm)$/i;
const AUDIO_EXTENSION_RE = /\.(?:flac|m4a|mp3|oga|ogg|opus|wav)$/i;

/**
 * Where a surface must choose a player before asking the daemon (the Brain
 * workspace reads files whole), the extension decides; the daemon still
 * checks the container before it streams anything.
 */
export function sessionFileMediaKindForPath(
  path: string,
): SessionFileMediaKind | null {
  if (VIDEO_EXTENSION_RE.test(path)) return "video";
  if (AUDIO_EXTENSION_RE.test(path)) return "audio";
  return null;
}
