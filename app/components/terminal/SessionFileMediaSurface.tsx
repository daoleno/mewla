import React, {
  forwardRef,
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { WebView, type WebViewMessageEvent } from "react-native-webview";
import { parseSessionFileMediaEvent } from "../../services/sessionFileMedia";
import {
  buildSessionFileMediaHtml,
  sessionFileMediaOrigin,
} from "./sessionFileMediaWebViewHtml";
import type {
  SessionFileMediaSurfaceHandle,
  SessionFileMediaSurfaceProps,
} from "./sessionFileMediaSurfaceTypes";

/**
 * Android and iOS play the stream in a WebView's media element, so the phone
 * needs no extra native player module. Commands go in as injected calls on
 * `window.__mewlaMedia`; status comes back as messages.
 */
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
  },
  ref,
) {
  const webviewRef = useRef<WebView>(null);
  // The page is built once per opened file; a re-signed stream is swapped in
  // through `load` so playback keeps its place.
  const [initialUri] = useState(uri);
  const source = useMemo(
    () => ({
      html: buildSessionFileMediaHtml({ kind, uri: initialUri, background }),
      baseUrl: sessionFileMediaOrigin(initialUri),
    }),
    [background, initialUri, kind],
  );
  const call = useCallback((script: string) => {
    webviewRef.current?.injectJavaScript(
      `window.__mewlaMedia && window.__mewlaMedia.${script}; true;`,
    );
  }, []);

  useImperativeHandle(
    ref,
    () => ({
      play: () => call("play()"),
      pause: () => call("pause()"),
      seek: (seconds) =>
        call(`seek(${Number.isFinite(seconds) ? Math.max(0, seconds) : 0})`),
      setMuted: (muted) => call(`setMuted(${muted ? "true" : "false"})`),
      load: (nextUri, time, play) =>
        call(
          `load(${JSON.stringify(nextUri)}, ${Number.isFinite(time) ? time : 0}, ${play ? "true" : "false"})`,
        ),
      toggleFullscreen: () => call("fullscreen()"),
    }),
    [call],
  );

  const handleMessage = useCallback(
    (event: WebViewMessageEvent) => {
      const message = parseSessionFileMediaEvent(event.nativeEvent.data);
      if (!message) return;
      if (message.type === "status") onStatus(message.status);
      else onError(message.code);
    },
    [onError, onStatus],
  );

  const player = (
    <WebView
      ref={webviewRef}
      source={source}
      originWhitelist={["*"]}
      onMessage={handleMessage}
      onError={() => onError(0)}
      javaScriptEnabled
      mediaPlaybackRequiresUserAction={false}
      allowsInlineMediaPlayback
      allowsFullscreenVideo
      allowsAirPlayForMediaPlayback
      scrollEnabled={false}
      bounces={false}
      overScrollMode="never"
      setSupportMultipleWindows={false}
      style={[styles.webview, { backgroundColor: background }]}
    />
  );

  if (kind === "audio") {
    return (
      <View style={styles.audioFrame}>
        <View
          pointerEvents="none"
          style={styles.audioEngine}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
        >
          {player}
        </View>
        {controls}
      </View>
    );
  }
  return (
    <View style={[styles.videoFrame, { backgroundColor: background }]}>
      <View style={styles.stage}>
        {player}
        <Pressable
          accessibilityLabel={label}
          onPress={onPressStage}
          style={StyleSheet.absoluteFill}
        >
          {overlay}
        </Pressable>
      </View>
      {controls}
    </View>
  );
});

const styles = StyleSheet.create({
  videoFrame: { flex: 1, minHeight: 0 },
  stage: { flex: 1, minHeight: 0, overflow: "hidden" },
  webview: { flex: 1 },
  audioFrame: { width: "100%", alignItems: "center" },
  // The audio element needs a live page but nothing to show.
  audioEngine: {
    position: "absolute",
    width: 1,
    height: 1,
    opacity: 0,
    overflow: "hidden",
  },
});
