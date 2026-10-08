import type React from "react";
import type {
  SessionFileMediaKind,
  SessionFileMediaStatus,
} from "../../services/sessionFileMedia";

/**
 * The one contract both players meet: a DOM media element on Web and the same
 * element inside a WebView on Android and iOS. Mewla draws every control.
 */
export interface SessionFileMediaSurfaceHandle {
  play(): void;
  pause(): void;
  seek(seconds: number): void;
  setMuted(muted: boolean): void;
  /** Swaps in a freshly signed stream and resumes where playback was. */
  load(uri: string, time: number, play: boolean): void;
  toggleFullscreen(): void;
}

export interface SessionFileMediaSurfaceProps {
  kind: SessionFileMediaKind;
  uri: string;
  label: string;
  background: string;
  /** Drawn over the picture, e.g. the paused play mark. */
  overlay?: React.ReactNode;
  /** The control bar; stays with the picture in full screen where the platform allows. */
  controls: React.ReactNode;
  onPressStage?(): void;
  onStatus(status: SessionFileMediaStatus): void;
  onError(code: number): void;
  onFullscreenChange?(fullscreen: boolean): void;
}
