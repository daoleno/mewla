import { Asset } from "expo-asset";
import React, { forwardRef, useCallback, useImperativeHandle, useMemo, useRef } from "react";
import { Platform, StyleSheet } from "react-native";
import { WebView } from "react-native-webview";
import { terminalFontCache } from "../terminalSurfaceBootstrap";
import { terminalWebViewBaseUrl } from "../terminalWebViewSource";
import { PIERRE_TREES_SOURCE } from "./pierreTreesSource";
import { buildPierreViewerHtml } from "./pierreViewerHtml";
import type { TreeHostMessage, TreeViewerHostHandle, TreeViewerHostProps } from "./treeViewerProtocol";

const DEFAULT_BASE_URL = "https://trees.mewla.invalid/";

/** Native host: the reader runs in a WebView that cannot navigate or fetch. */
export const TreeViewerHost = forwardRef<TreeViewerHostHandle, TreeViewerHostProps>(function TreeViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const web = useRef<WebView>(null);
  const ready = useRef(false);
  const queue = useRef<TreeHostMessage[]>([]);
  const font = useMemo(
    () => terminalFontCache.resolve(
      () => Asset.fromModule(require("../../../assets/fonts/MapleMono-CN-Regular.ttf")),
      (message, error) => console.warn("[Tree font] " + message, error),
    ),
    [],
  );
  const source = useMemo(() => ({
    html: buildPierreViewerHtml({ source: PIERRE_TREES_SOURCE, fontUri: font.uri, background: backgroundColor }),
    baseUrl: font.uri ? terminalWebViewBaseUrl(font.uri, Platform.OS) : DEFAULT_BASE_URL,
  }), [backgroundColor, font.uri]);
  const flush = useCallback(() => {
    if (!ready.current || !queue.current.length) return;
    const batch = queue.current;
    queue.current = [];
    web.current?.injectJavaScript(`window.pierreTreeHost&&window.pierreTreeHost(${JSON.stringify(batch)});true;`);
  }, []);
  useImperativeHandle(ref, () => ({
    post(batch) {
      queue.current.push(...batch);
      flush();
    },
  }), [flush]);
  const baseUrl = source.baseUrl;
  return (
    <WebView
      ref={web}
      style={[styles.view, { backgroundColor }]}
      source={source}
      originWhitelist={["*"]}
      accessibilityLabel={accessibilityLabel}
      onShouldStartLoadWithRequest={(request) => request.url === "about:blank" || request.url === baseUrl}
      onMessage={(event) => {
        const raw = event.nativeEvent.data;
        if (raw.startsWith('{"type":"ready"')) {
          ready.current = true;
          flush();
        }
        onMessage(raw);
      }}
      onContentProcessDidTerminate={() => {
        // WKWebView lost its process: the document reloads and says ready again.
        ready.current = false;
        web.current?.reload();
      }}
      onRenderProcessGone={() => {
        ready.current = false;
        web.current?.reload();
      }}
      javaScriptEnabled
      javaScriptCanOpenWindowsAutomatically={false}
      setSupportMultipleWindows={false}
      allowFileAccess
      domStorageEnabled={false}
      sharedCookiesEnabled={false}
      thirdPartyCookiesEnabled={false}
      cacheEnabled={false}
      incognito
      textInteractionEnabled
      allowsLinkPreview={false}
      scrollEnabled={false}
      nestedScrollEnabled
      bounces={false}
      overScrollMode="never"
      setBuiltInZoomControls={false}
      automaticallyAdjustContentInsets={false}
      contentInsetAdjustmentBehavior="never"
      {...(Platform.OS === "ios" ? { opaque: true } : null)}
    />
  );
});

const styles = StyleSheet.create({ view: { flex: 1 } });
