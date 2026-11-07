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

/** Messages that define the document's state; `reveal` is one-shot. */
function slotOf(message: TreeHostMessage): string | null {
  return message.type === "reveal" ? null : message.type;
}

/** Native host: the reader runs in a WebView that cannot navigate or fetch. */
export const TreeViewerHost = forwardRef<TreeViewerHostHandle, TreeViewerHostProps>(function TreeViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const web = useRef<WebView>(null);
  const ready = useRef(false);
  // The last message of each kind that defines what the document shows. A
  // reload (theme flip aside, the OS can kill the WebView's process) comes up
  // empty, so these are replayed whenever the document says it is ready.
  const slots = useRef(new Map<string, TreeHostMessage>());
  const initialBackground = useRef(backgroundColor);
  const font = useMemo(
    () => terminalFontCache.resolve(
      () => Asset.fromModule(require("../../../assets/fonts/MapleMono-CN-Regular.ttf")),
      (message, error) => console.warn("[Tree font] " + message, error),
    ),
    [],
  );
  const source = useMemo(() => ({
    html: buildPierreViewerHtml({ source: PIERRE_TREES_SOURCE, fontUri: font.uri, background: initialBackground.current }),
    baseUrl: font.uri ? terminalWebViewBaseUrl(font.uri, Platform.OS) : DEFAULT_BASE_URL,
  }), [font.uri]);
  const inject = useCallback((batch: TreeHostMessage[]) => {
    if (!batch.length) return;
    web.current?.injectJavaScript(`window.pierreTreeHost&&window.pierreTreeHost(${JSON.stringify(batch)});true;`);
  }, []);
  const replay = useCallback(() => {
    inject(["style", "paths", "select"].flatMap((slot) => slots.current.get(slot) ?? []));
  }, [inject]);
  useImperativeHandle(ref, () => ({
    post(batch) {
      for (const message of batch) {
        const slot = slotOf(message);
        if (slot) slots.current.set(slot, message);
      }
      // Before the document is ready the slots are all there is to send.
      if (ready.current) inject(batch);
    },
  }), [inject]);
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
          replay();
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
