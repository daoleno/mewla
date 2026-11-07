import React, { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { View, type ViewInstance } from "react-native";
import { mountTreeViewer } from "./pierreTreesRuntime";
import type { TreeHostMessage, TreeViewerHostHandle, TreeViewerHostProps } from "./treeViewerProtocol";

/**
 * Web host: the same reader mounts straight into the page, because the web
 * UI's CSP allows only same-origin scripts (no srcdoc frames).
 */
export const TreeViewerHost = forwardRef<TreeViewerHostHandle, TreeViewerHostProps>(function TreeViewerHost(
  { onMessage, backgroundColor, accessibilityLabel },
  ref,
) {
  const container = useRef<ViewInstance>(null);
  const viewer = useRef<ReturnType<typeof mountTreeViewer> | null>(null);
  const queue = useRef<TreeHostMessage[]>([]);
  const handler = useRef(onMessage);
  handler.current = onMessage;
  useImperativeHandle(ref, () => ({
    post(batch) {
      if (viewer.current) viewer.current.handle(batch);
      else queue.current.push(...batch);
    },
  }), []);
  useEffect(() => {
    const element = container.current as unknown as HTMLElement | null;
    if (!element) return;
    const mounted = mountTreeViewer(element, (message) => handler.current(JSON.stringify(message)));
    viewer.current = mounted;
    if (queue.current.length) {
      const batch = queue.current;
      queue.current = [];
      mounted.handle(batch);
    }
    handler.current(JSON.stringify({ type: "ready" }));
    return () => { viewer.current = null; mounted.destroy(); };
  }, []);
  return <View ref={container} accessibilityLabel={accessibilityLabel} style={{ flex: 1, position: "relative", overflow: "hidden", backgroundColor }} />;
});
