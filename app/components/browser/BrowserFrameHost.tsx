import React, { forwardRef, useImperativeHandle, useRef } from "react";
import { StyleSheet } from "react-native";
import { WebView } from "react-native-webview";
import { browserRendererHTML } from "./renderer";
import type { BrowserFrame } from "../../services/browser";

export interface BrowserFrameHostHandle {
  frame(frame: BrowserFrame): void;
  scroll(on: boolean): void;
}
export interface BrowserFrameHostProps {
  /** `ready`, `ack` and `input` messages from the renderer, as JSON text. */
  onMessage(data: string): void;
  backgroundColor: string;
  accessibilityLabel: string;
}

const SOURCE = { html: browserRendererHTML };

/** Native host: the pixels render in a WebView that cannot navigate or fetch. */
export const BrowserFrameHost = forwardRef<BrowserFrameHostHandle, BrowserFrameHostProps>(function BrowserFrameHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const web = useRef<WebView>(null);
  useImperativeHandle(ref, () => ({
    frame(frame) { web.current?.injectJavaScript(`window.browserFrame(${JSON.stringify(frame)});true;`); },
    scroll(on) { web.current?.injectJavaScript(`window.browserScroll(${on});true;`); },
  }), []);
  return (
    <WebView ref={web} style={[styles.view, { backgroundColor }]} source={SOURCE} originWhitelist={["about:blank"]}
      accessibilityLabel={accessibilityLabel}
      onShouldStartLoadWithRequest={(request) => request.url === "about:blank"}
      javaScriptCanOpenWindowsAutomatically={false} setSupportMultipleWindows={false}
      allowFileAccess={false} domStorageEnabled={false} sharedCookiesEnabled={false}
      thirdPartyCookiesEnabled={false} cacheEnabled={false} incognito scrollEnabled={false}
      onMessage={(event) => onMessage(event.nativeEvent.data)} />
  );
});

const styles = StyleSheet.create({ view: { flex: 1 } });
