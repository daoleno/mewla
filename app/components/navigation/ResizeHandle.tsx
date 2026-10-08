import React, { useCallback, useEffect, useRef, useState } from "react";
import { Platform, StyleSheet, View } from "react-native";
import { useAppColors } from "../../constants/tokens";
import { clampPanelWidth } from "./panelWidth";


function readStoredWidth(key: string): number | null {
  try {
    const raw = globalThis.localStorage?.getItem(key);
    const value = raw == null ? NaN : Number(raw);
    return Number.isFinite(value) ? value : null;
  } catch {
    return null;
  }
}

function writeStoredWidth(key: string, value: number) {
  try {
    globalThis.localStorage?.setItem(key, String(value));
  } catch {
    // A private window keeps the width for this page only.
  }
}

/**
 * A desktop web panel width the user drags and the browser remembers. Native
 * never resizes, so it always gets the default.
 */
export function useResizableWidth(storageKey: string, initial: number, min: number, max: number) {
  const [width, setWidth] = useState(() =>
    clampPanelWidth(Platform.OS === "web" ? readStoredWidth(storageKey) ?? initial : initial, min, max),
  );
  const update = useCallback(
    (next: number) => setWidth(clampPanelWidth(next, min, max)),
    [max, min],
  );
  const commit = useCallback(
    (next: number) => writeStoredWidth(storageKey, clampPanelWidth(next, min, max)),
    [max, min, storageKey],
  );
  const reset = useCallback(() => {
    setWidth(clampPanelWidth(initial, min, max));
    writeStoredWidth(storageKey, clampPanelWidth(initial, min, max));
  }, [initial, max, min, storageKey]);
  return { width, update, commit, reset };
}

interface ResizeHandleProps {
  /** Which edge of the panel the handle sits on. */
  edge: "left" | "right";
  width: number;
  label: string;
  onResize(width: number): void;
  onCommit(width: number): void;
  onReset(): void;
}

/**
 * The drag strip on a panel's edge (web only). Drag to resize, double-click
 * to restore the default; arrow keys nudge it when focused.
 */
export function ResizeHandle({ edge, width, label, onResize, onCommit, onReset }: ResizeHandleProps) {
  const colors = useAppColors();
  const ref = useRef<View>(null);
  const [active, setActive] = useState(false);
  const latest = useRef({ width, onResize, onCommit, onReset });
  latest.current = { width, onResize, onCommit, onReset };

  useEffect(() => {
    if (Platform.OS !== "web") return;
    const node = ref.current as unknown as HTMLElement | null;
    if (!node) return;
    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0) return;
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = latest.current.width;
      let next = startWidth;
      setActive(true);
      const previousCursor = document.body.style.cursor;
      const previousSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      const onMove = (move: PointerEvent) => {
        const delta = move.clientX - startX;
        next = startWidth + (edge === "right" ? delta : -delta);
        latest.current.onResize(next);
      };
      const onUp = () => {
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousSelect;
        setActive(false);
        latest.current.onCommit(next);
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
    };
    const onDoubleClick = () => latest.current.onReset();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const step = (event.key === "ArrowRight" ? 16 : -16) * (edge === "right" ? 1 : -1);
      const next = latest.current.width + step;
      latest.current.onResize(next);
      latest.current.onCommit(next);
    };
    node.addEventListener("pointerdown", onPointerDown);
    node.addEventListener("dblclick", onDoubleClick);
    node.addEventListener("keydown", onKeyDown);
    node.setAttribute("title", `${label} (drag to resize, double-click to reset)`);
    return () => {
      node.removeEventListener("pointerdown", onPointerDown);
      node.removeEventListener("dblclick", onDoubleClick);
      node.removeEventListener("keydown", onKeyDown);
    };
  }, [edge, label]);

  if (Platform.OS !== "web") return null;
  return (
    <View
      ref={ref}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={width}
      tabIndex={0}
      style={[
        styles.handle,
        edge === "right" ? styles.right : styles.left,
        { cursor: "col-resize" } as object,
      ]}
    >
      <View
        style={[
          styles.line,
          { backgroundColor: active ? colors.focusRing : "transparent" },
        ]}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  handle: {
    position: "absolute",
    top: 0,
    bottom: 0,
    width: 9,
    zIndex: 20,
    alignItems: "center",
  },
  right: { right: -5 },
  left: { left: -5 },
  line: {
    width: 2,
    height: "100%",
  },
});
