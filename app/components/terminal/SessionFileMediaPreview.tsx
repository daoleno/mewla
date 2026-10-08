import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type AccessibilityActionEvent,
  type LayoutChangeEvent,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  formatSessionFileMediaTime,
  initialSessionFileMediaStatus,
  sessionFileMediaProgress,
  sessionFileMediaRecovery,
  sessionFileMediaScrubTime,
  type SessionFileMediaKind,
  type SessionFileMediaStatus,
} from "../../services/sessionFileMedia";
import {
  isStaleSessionFileError,
  type SessionFileBinarySource,
} from "../../services/sessionFilePreview";
import { Icon, type IconName } from "../icons/Icon";
import { SessionFileMediaSurface } from "./SessionFileMediaSurface";
import type { SessionFileMediaSurfaceHandle } from "./sessionFileMediaSurfaceTypes";

const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;
const SEEK_STEP_SECONDS = 5;

export function SessionFileMediaPreview({
  kind,
  name,
  source,
  resolveSource,
  chrome,
  onError,
}: {
  kind: SessionFileMediaKind;
  name: string;
  source: SessionFileBinarySource;
  /** Signs a fresh stream for the same file generation. */
  resolveSource(): Promise<SessionFileBinarySource>;
  chrome: TerminalThemeChrome;
  onError(message: string, stale: boolean): void;
}) {
  const surfaceRef = useRef<SessionFileMediaSurfaceHandle>(null);
  const [status, setStatus] = useState<SessionFileMediaStatus>(
    initialSessionFileMediaStatus,
  );
  const [scrubTime, setScrubTime] = useState<number | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const statusRef = useRef(status);
  const playIntentRef = useRef(false);
  const loadedSinceSourceRef = useRef(false);
  const recoveringRef = useRef(false);
  const attemptsRef = useRef(0);
  const resumedAtRef = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const handleStatus = useCallback((next: SessionFileMediaStatus) => {
    statusRef.current = next;
    if (next.loaded) loadedSinceSourceRef.current = true;
    if (next.ended) playIntentRef.current = false;
    // Playback moving again after a recovery means the stream is healthy.
    if (!next.paused && next.currentTime > resumedAtRef.current + 1) {
      attemptsRef.current = 0;
    }
    setStatus(next);
  }, []);

  const handleError = useCallback(
    (code: number) => {
      if (recoveringRef.current) return;
      const decision = sessionFileMediaRecovery({
        loadedSinceSource: loadedSinceSourceRef.current,
        attempts: attemptsRef.current,
      });
      if (decision === "fail") {
        onError(
          code === MEDIA_ERR_SRC_NOT_SUPPORTED || !loadedSinceSourceRef.current
            ? `${kind === "video" ? "This video" : "This audio"} can't be played here. Download it to play it in another app.`
            : "Playback stopped and couldn't resume. Refresh and try again.",
          false,
        );
        return;
      }
      attemptsRef.current += 1;
      recoveringRef.current = true;
      const resumeAt = statusRef.current.currentTime;
      const play = playIntentRef.current;
      void resolveSource()
        .then((next) => {
          if (!mountedRef.current) return;
          loadedSinceSourceRef.current = false;
          resumedAtRef.current = resumeAt;
          surfaceRef.current?.load(next.uri, resumeAt, play);
        })
        .catch((error: unknown) => {
          if (!mountedRef.current) return;
          onError(
            error instanceof Error
              ? error.message
              : "Could not authorize the stream again.",
            isStaleSessionFileError(error),
          );
        })
        .finally(() => {
          recoveringRef.current = false;
        });
    },
    [kind, onError, resolveSource],
  );

  const togglePlay = useCallback(() => {
    if (statusRef.current.paused) {
      playIntentRef.current = true;
      surfaceRef.current?.play();
    } else {
      playIntentRef.current = false;
      surfaceRef.current?.pause();
    }
  }, []);
  const seek = useCallback((time: number) => {
    statusRef.current = { ...statusRef.current, currentTime: time };
    setStatus((current) => ({ ...current, currentTime: time }));
    surfaceRef.current?.seek(time);
  }, []);
  const toggleMuted = useCallback(() => {
    surfaceRef.current?.setMuted(!statusRef.current.muted);
  }, []);
  const toggleFullscreen = useCallback(() => {
    surfaceRef.current?.toggleFullscreen();
  }, []);

  const shownTime = scrubTime ?? status.currentTime;
  const busy = status.waiting && (playIntentRef.current || !status.loaded);
  const label = `${kind === "video" ? "Video" : "Audio"} ${name}`;
  const timeLabel = `${formatSessionFileMediaTime(shownTime, status.duration)} / ${formatSessionFileMediaTime(status.duration)}`;

  const overlay =
    kind === "video" && (busy || status.paused) ? (
      <View pointerEvents="none" style={styles.overlay}>
        {busy ? (
          <ActivityIndicator color={chrome.text} />
        ) : (
          <View
            style={[
              styles.playMark,
              { backgroundColor: chrome.surface, borderColor: chrome.border },
            ]}
          >
            <Icon name="play" size={26} color={chrome.text} />
          </View>
        )}
      </View>
    ) : null;

  const scrubber = (
    <MediaScrubber
      currentTime={shownTime}
      duration={status.duration}
      label={`${name} position`}
      valueText={timeLabel}
      chrome={chrome}
      style={kind === "video" ? styles.barScrubber : undefined}
      onScrub={setScrubTime}
      onSeek={seek}
    />
  );
  const playButton = (
    <MediaButton
      label={status.paused ? "Play" : "Pause"}
      icon={status.paused ? "play" : "pause"}
      chrome={chrome}
      primary
      size={kind === "audio" ? 44 : 36}
      onPress={togglePlay}
    />
  );
  const muteButton = (
    <MediaButton
      label={status.muted ? "Unmute" : "Mute"}
      icon={status.muted ? "volume-off" : "volume"}
      chrome={chrome}
      selected={status.muted}
      onPress={toggleMuted}
    />
  );

  const controls =
    kind === "audio" ? (
      <View
        style={[
          styles.audioCard,
          { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
        ]}
      >
        {playButton}
        <View style={styles.audioTimeline}>
          {scrubber}
          <View style={styles.audioTimes}>
            <Text style={[styles.time, { color: chrome.textMuted }]}>
              {formatSessionFileMediaTime(shownTime, status.duration)}
            </Text>
            <Text style={[styles.time, { color: chrome.textSubtle }]}>
              {busy ? "Loading" : formatSessionFileMediaTime(status.duration)}
            </Text>
          </View>
        </View>
        {muteButton}
      </View>
    ) : (
      <View
        style={[
          styles.bar,
          { borderTopColor: chrome.border, backgroundColor: chrome.surface },
        ]}
      >
        {playButton}
        <Text style={[styles.time, { color: chrome.textMuted }]}>
          {formatSessionFileMediaTime(shownTime, status.duration)}
        </Text>
        {scrubber}
        <Text style={[styles.time, { color: chrome.textSubtle }]}>
          {formatSessionFileMediaTime(status.duration)}
        </Text>
        {muteButton}
        <MediaButton
          label={fullscreen ? "Exit full screen" : "Full screen"}
          icon="expand"
          chrome={chrome}
          selected={fullscreen}
          onPress={toggleFullscreen}
        />
      </View>
    );

  const surface = (
    <SessionFileMediaSurface
      ref={surfaceRef}
      kind={kind}
      uri={source.uri}
      label={label}
      background={chrome.surfaceMuted}
      overlay={overlay}
      controls={controls}
      onPressStage={togglePlay}
      onStatus={handleStatus}
      onError={handleError}
      onFullscreenChange={setFullscreen}
    />
  );

  if (kind === "audio") {
    return (
      <View style={[styles.audioStage, { backgroundColor: chrome.appBackground }]}>
        {surface}
      </View>
    );
  }
  return surface;
}

function MediaScrubber({
  currentTime,
  duration,
  label,
  valueText,
  chrome,
  style,
  onScrub,
  onSeek,
}: {
  currentTime: number;
  duration: number;
  label: string;
  valueText: string;
  chrome: TerminalThemeChrome;
  style?: StyleProp<ViewStyle>;
  onScrub(time: number | null): void;
  onSeek(time: number): void;
}) {
  const [width, setWidth] = useState(0);
  const widthRef = useRef(0);
  const durationRef = useRef(duration);
  const scrubRef = useRef<number | null>(null);
  durationRef.current = duration;

  const scrubAt = useCallback(
    (x: number) => {
      const time = sessionFileMediaScrubTime(
        x,
        widthRef.current,
        durationRef.current,
      );
      scrubRef.current = time;
      onScrub(time);
    },
    [onScrub],
  );
  const commit = useCallback(() => {
    const time = scrubRef.current;
    scrubRef.current = null;
    onScrub(null);
    if (time !== null && durationRef.current > 0) onSeek(time);
  }, [onScrub, onSeek]);

  const gesture = React.useMemo(
    () =>
      Gesture.Pan()
        .runOnJS(true)
        .minDistance(0)
        .onBegin((event) => scrubAt(event.x))
        .onUpdate((event) => scrubAt(event.x))
        .onFinalize(() => commit()),
    [commit, scrubAt],
  );
  const handleLayout = useCallback((event: LayoutChangeEvent) => {
    widthRef.current = event.nativeEvent.layout.width;
    setWidth(event.nativeEvent.layout.width);
  }, []);
  const handleAction = useCallback(
    (event: AccessibilityActionEvent) => {
      const step =
        event.nativeEvent.actionName === "increment"
          ? SEEK_STEP_SECONDS
          : event.nativeEvent.actionName === "decrement"
            ? -SEEK_STEP_SECONDS
            : 0;
      if (!step || !(duration > 0)) return;
      onSeek(Math.min(duration, Math.max(0, currentTime + step)));
    },
    [currentTime, duration, onSeek],
  );

  const progress = sessionFileMediaProgress(currentTime, duration);
  const thumbLeft = Math.max(0, Math.min(width, progress * width));
  return (
    <GestureDetector gesture={gesture}>
      <View
        accessible
        accessibilityRole="adjustable"
        accessibilityLabel={label}
        accessibilityValue={{
          min: 0,
          max: Math.round(duration),
          now: Math.round(currentTime),
          text: valueText,
        }}
        accessibilityActions={[
          { name: "increment" },
          { name: "decrement" },
        ]}
        onAccessibilityAction={handleAction}
        onLayout={handleLayout}
        style={[styles.scrubber, style]}
      >
        <View style={[styles.track, { backgroundColor: chrome.border }]}>
          <View
            style={[
              styles.trackFill,
              { width: `${progress * 100}%`, backgroundColor: chrome.text },
            ]}
          />
        </View>
        <View
          pointerEvents="none"
          style={[
            styles.thumb,
            { left: thumbLeft - 6, backgroundColor: chrome.text },
          ]}
        />
      </View>
    </GestureDetector>
  );
}

function MediaButton({
  label,
  icon,
  chrome,
  onPress,
  primary = false,
  selected = false,
  size = 34,
}: {
  label: string;
  icon: IconName;
  chrome: TerminalThemeChrome;
  onPress(): void;
  primary?: boolean;
  selected?: boolean;
  size?: number;
}) {
  return (
    <TouchableOpacity
      accessibilityLabel={label}
      accessibilityRole="button"
      accessibilityState={{ selected }}
      activeOpacity={0.75}
      onPress={onPress}
      style={[
        styles.button,
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: primary
            ? chrome.accent
            : selected
              ? chrome.accentSoft
              : "transparent",
        },
      ]}
    >
      <Icon
        name={icon}
        size={primary ? Math.round(size * 0.48) : 20}
        color={primary ? chrome.textOnAccent : selected ? chrome.text : chrome.textMuted}
      />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  overlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    alignItems: "center",
    justifyContent: "center",
  },
  playMark: {
    width: 60,
    height: 60,
    borderRadius: 30,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    // The triangle's weight sits left of its box; nudge it to look centred.
    paddingLeft: 3,
  },
  bar: {
    minHeight: 54,
    paddingHorizontal: 12,
    paddingVertical: 9,
    borderTopWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  time: {
    fontFamily: Typography.terminalFont,
    fontSize: 11,
    lineHeight: 15,
    fontVariant: ["tabular-nums"],
  },
  scrubber: {
    height: 28,
    justifyContent: "center",
  },
  barScrubber: {
    flex: 1,
    minWidth: 48,
  },
  track: {
    height: 4,
    borderRadius: 2,
    overflow: "hidden",
  },
  trackFill: {
    height: 4,
  },
  thumb: {
    position: "absolute",
    top: 8,
    width: 12,
    height: 12,
    borderRadius: 6,
  },
  button: {
    alignItems: "center",
    justifyContent: "center",
  },
  audioStage: {
    flex: 1,
    minHeight: 0,
    paddingHorizontal: 16,
    alignItems: "center",
    justifyContent: "center",
  },
  audioCard: {
    width: "100%",
    maxWidth: 520,
    paddingHorizontal: 14,
    paddingVertical: 12,
    borderRadius: 20,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  audioTimeline: {
    flex: 1,
    minWidth: 0,
  },
  audioTimes: {
    marginTop: -2,
    flexDirection: "row",
    justifyContent: "space-between",
  },
});
