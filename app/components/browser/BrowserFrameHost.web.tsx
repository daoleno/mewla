import React, { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { View, type ViewInstance } from "react-native";
import { mountBrowserFrame } from "./browserFrameRuntime";
import type { BrowserFrameHostHandle, BrowserFrameHostProps } from "./BrowserFrameHost";

/** Web host: the same pixels, mounted straight into the page. */
export const BrowserFrameHost = forwardRef<BrowserFrameHostHandle, BrowserFrameHostProps>(function BrowserFrameHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const container = useRef<ViewInstance>(null);
  const mounted = useRef<ReturnType<typeof mountBrowserFrame> | null>(null);
  const handler = useRef(onMessage);
  handler.current = onMessage;
  useImperativeHandle(ref, () => ({
    frame(frame) { mounted.current?.frame(frame); },
    scroll(on) { mounted.current?.scroll(on); },
  }), []);
  useEffect(() => {
    const element = container.current as unknown as HTMLElement | null;
    if (!element) return;
    const view = mountBrowserFrame(element, (message) => handler.current(JSON.stringify(message)));
    mounted.current = view;
    handler.current(JSON.stringify({ type: "ready" }));
    return () => { mounted.current = null; view.destroy(); };
  }, []);
  return <View ref={container} accessibilityLabel={accessibilityLabel} style={{ flex: 1, position: "relative", overflow: "hidden", backgroundColor }} />;
});
