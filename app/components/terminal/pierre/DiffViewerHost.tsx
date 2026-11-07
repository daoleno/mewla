import { Asset } from "expo-asset";
import React, { forwardRef, useCallback, useImperativeHandle, useMemo, useRef } from "react";
import { Platform, StyleSheet } from "react-native";
import { WebView } from "react-native-webview";
import { terminalFontCache } from "../terminalSurfaceBootstrap";
import { terminalWebViewBaseUrl } from "../terminalWebViewSource";
import { PIERRE_DIFFS_SOURCE } from "./pierreDiffsSource";
import { buildPierreViewerHtml } from "./pierreViewerHtml";
import type { DiffHostMessage, DiffViewerHostHandle, DiffViewerHostProps } from "./diffViewerProtocol";

const DEFAULT_BASE_URL = "https://diffs.mewla.invalid/";

/** Messages that define the document's state; the rest (find, seek, files, top) are one-shot. */
function slotOf(message: DiffHostMessage): string | null {
  switch (message.type) {
    case "style":
    case "layout":
      return message.type;
    case "diff":
    case "file":
    case "clear":
      return "content";
    default:
      return null;
  }
}

/** Native host: the reader runs in a WebView that cannot navigate or fetch. */
export const DiffViewerHost = forwardRef<DiffViewerHostHandle, DiffViewerHostProps>(function DiffViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const web = useRef<WebView>(null);
  const ready = useRef(false);
  // The last message of each kind that defines what the document shows. A
  // reload (theme flip aside, the OS can kill the WebView's process) comes up
  // empty, so these are replayed whenever the document says it is ready.
  const slots = useRef(new Map<string, DiffHostMessage>());
  const initialBackground = useRef(backgroundColor);
  const font = useMemo(
    () => terminalFontCache.resolve(
      () => Asset.fromModule(require("../../../assets/fonts/MapleMono-CN-Regular.ttf")),
      (message, error) => console.warn("[Diff font] " + message, error),
    ),
    [],
  );
  const source = useMemo(() => ({
    html: buildPierreViewerHtml({ source: PIERRE_DIFFS_SOURCE, fontUri: font.uri, background: initialBackground.current }),
    baseUrl: font.uri ? terminalWebViewBaseUrl(font.uri, Platform.OS) : DEFAULT_BASE_URL,
  }), [font.uri]);
  const inject = useCallback((batch: DiffHostMessage[]) => {
    if (!batch.length) return;
    web.current?.injectJavaScript(`window.pierreDiffHost&&window.pierreDiffHost(${JSON.stringify(batch)});true;`);
  }, []);
  const replay = useCallback(() => {
    inject(["style", "layout", "content"].flatMap((slot) => slots.current.get(slot) ?? []));
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
