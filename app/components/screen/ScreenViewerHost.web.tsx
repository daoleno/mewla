import React, { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { View, type ViewInstance } from "react-native";
import { mountScreenViewer } from "./screenViewerRuntime";
import type { ScreenViewerHostHandle, ScreenViewerHostProps } from "./screenViewerProtocol";

/**
 * Web host: the same viewer mounts straight into the page, because the web
 * UI's CSP allows only same-origin scripts (no srcdoc frames). It still opens
 * no connection of its own.
 */
export const ScreenViewerHost = forwardRef<ScreenViewerHostHandle, ScreenViewerHostProps>(function ScreenViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const container = useRef<ViewInstance>(null);
  const viewer = useRef<ReturnType<typeof mountScreenViewer> | null>(null);
  const handler = useRef(onMessage);
  handler.current = onMessage;
  useImperativeHandle(ref, () => ({
    post(batch) { viewer.current?.handle(batch); },
  }), []);
  useEffect(() => {
    const element = container.current as unknown as HTMLElement | null;
    if (!element) return;
    const mounted = mountScreenViewer(element, (message) => handler.current(JSON.stringify(message)));
    viewer.current = mounted;
    handler.current(JSON.stringify({ type: "ready" }));
    return () => { viewer.current = null; mounted.destroy(); };
  }, []);
  return <View ref={container} accessibilityLabel={accessibilityLabel} style={{ flex: 1, position: "relative", overflow: "hidden", backgroundColor }} />;
});
