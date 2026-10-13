import React, { forwardRef, useImperativeHandle, useMemo, useRef } from "react";
import { StyleSheet } from "react-native";
import { WebView } from "react-native-webview";
import { buildScreenViewerHtml } from "./screenViewerHtml";
import type { ScreenViewerHostHandle, ScreenViewerHostProps } from "./screenViewerProtocol";

/** Native host: the viewer runs in a WebView that cannot navigate or fetch. */
export const ScreenViewerHost = forwardRef<ScreenViewerHostHandle, ScreenViewerHostProps>(function ScreenViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const web = useRef<WebView>(null);
  const source = useMemo(() => ({ html: buildScreenViewerHtml(), baseUrl: "about:blank" }), []);
  useImperativeHandle(ref, () => ({
    post(batch) {
      web.current?.injectJavaScript(`window.screenHost&&window.screenHost(${JSON.stringify(batch)});true;`);
    },
  }), []);
  return (
    <WebView ref={web} style={[styles.view, { backgroundColor }]} source={source} originWhitelist={["about:blank"]}
      accessibilityLabel={accessibilityLabel}
      onShouldStartLoadWithRequest={(request) => request.url === "about:blank"}
      javaScriptCanOpenWindowsAutomatically={false} setSupportMultipleWindows={false}
      allowFileAccess={false} domStorageEnabled={false} sharedCookiesEnabled={false}
      thirdPartyCookiesEnabled={false} cacheEnabled={false} incognito scrollEnabled={false}
      bounces={false} overScrollMode="never" setBuiltInZoomControls={false}
      onMessage={(event) => onMessage(event.nativeEvent.data)} />
  );
});

const styles = StyleSheet.create({ view: { flex: 1 } });
