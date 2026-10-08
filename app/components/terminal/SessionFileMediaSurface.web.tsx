import React, {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
} from "react";
import { Pressable, StyleSheet, View } from "react-native";
import type { SessionFileMediaStatus } from "../../services/sessionFileMedia";
import { sessionFileMediaFirstFrameUri } from "./sessionFileMediaWebViewHtml";
import type {
  SessionFileMediaSurfaceHandle,
  SessionFileMediaSurfaceProps,
} from "./sessionFileMediaSurfaceTypes";

type WebkitVideo = HTMLVideoElement & { webkitEnterFullscreen?: () => void };

const STATUS_EVENTS = [
  "loadedmetadata",
  "loadeddata",
  "durationchange",
  "play",
  "pause",
  "playing",
  "waiting",
  "seeking",
  "seeked",
  "ended",
  "volumechange",
  "canplay",
  "emptied",
] as const;

function readStatus(media: HTMLMediaElement): SessionFileMediaStatus {
  return {
    currentTime: media.currentTime || 0,
    duration: Number.isFinite(media.duration) ? media.duration : 0,
    paused: media.paused,
    muted: media.muted,
    ended: media.ended,
    waiting:
      media.readyState < 1 ||
      media.seeking ||
      (!media.paused && media.readyState < 3),
    loaded: media.readyState >= 1,
  };
}

export const SessionFileMediaSurface = forwardRef<
  SessionFileMediaSurfaceHandle,
  SessionFileMediaSurfaceProps
>(function SessionFileMediaSurface(
  {
    kind,
    uri,
    label,
    background,
    overlay,
    controls,
    onPressStage,
    onStatus,
    onError,
    onFullscreenChange,
  },
  ref,
) {
  const mediaRef = useRef<HTMLMediaElement | null>(null);
  const frameRef = useRef<View | null>(null);
  const callbacks = useRef({ onStatus, onError, onFullscreenChange });
  callbacks.current = { onStatus, onError, onFullscreenChange };

  useEffect(() => {
    const media = mediaRef.current;
    if (!media) return;
    let last = 0;
    const report = () => callbacks.current.onStatus(readStatus(media));
    const reportTime = () => {
      const now = Date.now();
      if (now - last < 200) return;
      last = now;
      report();
    };
    const reportError = () =>
      callbacks.current.onError(media.error ? media.error.code : 0);
    STATUS_EVENTS.forEach((name) => media.addEventListener(name, report));
    media.addEventListener("timeupdate", reportTime);
    media.addEventListener("error", reportError);
    return () => {
      STATUS_EVENTS.forEach((name) => media.removeEventListener(name, report));
      media.removeEventListener("timeupdate", reportTime);
      media.removeEventListener("error", reportError);
    };
  }, []);

  // Closing the preview cancels in-flight Range requests.
  useEffect(() => {
    const media = mediaRef.current;
    return () => {
      if (!media) return;
      media.pause();
      media.removeAttribute("src");
      media.load();
    };
  }, []);

  useEffect(() => {
    const changed = () => {
      const frame = frameRef.current as unknown as Element | null;
      callbacks.current.onFullscreenChange?.(
        Boolean(frame && document.fullscreenElement === frame),
      );
    };
    document.addEventListener("fullscreenchange", changed);
    return () => document.removeEventListener("fullscreenchange", changed);
  }, []);

  useImperativeHandle(
    ref,
    () => ({
      play() {
        void mediaRef.current?.play().catch(() => {
          const media = mediaRef.current;
          if (media) callbacks.current.onStatus(readStatus(media));
        });
      },
      pause() {
        mediaRef.current?.pause();
      },
      seek(seconds) {
        const media = mediaRef.current;
        if (media && Number.isFinite(seconds)) {
          media.currentTime = Math.max(0, seconds);
        }
      },
      setMuted(muted) {
        if (mediaRef.current) mediaRef.current.muted = muted;
      },
      load(nextUri, time, play) {
        const media = mediaRef.current;
        if (!media) return;
        const resume = () => {
          media.removeEventListener("loadedmetadata", resume);
          if (time > 0) media.currentTime = time;
          if (play) void media.play().catch(() => {});
        };
        media.addEventListener("loadedmetadata", resume);
        media.src = nextUri;
        media.load();
      },
      toggleFullscreen() {
        const frame = frameRef.current as unknown as HTMLElement | null;
        if (document.fullscreenElement) {
          void document.exitFullscreen().catch(() => {});
          return;
        }
        if (frame?.requestFullscreen) {
          void frame.requestFullscreen().catch(() => {});
          return;
        }
        // iPhone Safari only lets the video element itself go full screen.
        (mediaRef.current as WebkitVideo | null)?.webkitEnterFullscreen?.();
      },
    }),
    [],
  );

  const setMedia = useCallback((node: HTMLMediaElement | null) => {
    mediaRef.current = node;
  }, []);
  const src = sessionFileMediaFirstFrameUri(uri);

  if (kind === "audio") {
    return (
      <View ref={frameRef} style={styles.audioFrame}>
        <audio ref={setMedia} src={src} preload="metadata" aria-label={label} />
        {controls}
      </View>
    );
  }
  return (
    <View ref={frameRef} style={[styles.videoFrame, { backgroundColor: background }]}>
      <Pressable
        accessibilityLabel={label}
        onPress={onPressStage}
        style={styles.stage}
      >
        <video
          ref={setMedia}
          src={src}
          preload="metadata"
          playsInline
          style={videoStyle}
        />
        {overlay}
      </Pressable>
      {controls}
    </View>
  );
});

const videoStyle: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  width: "100%",
  height: "100%",
  objectFit: "contain",
  background: "transparent",
};

const styles = StyleSheet.create({
  videoFrame: { flex: 1, minHeight: 0 },
  audioFrame: { width: "100%", alignItems: "center" },
  stage: { flex: 1, minHeight: 0, overflow: "hidden" },
});
