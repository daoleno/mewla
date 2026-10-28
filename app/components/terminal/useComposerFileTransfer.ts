import { useEffect, useState, type RefObject } from "react";
import { Platform } from "react-native";
import { clipboardFiles, dragHasFiles } from "./composerFiles";

interface ComposerFileTransferInput {
  /** The chat pane: the drop target, and where a paste bubbles up to. */
  rootRef: RefObject<unknown>;
  enabled: boolean;
  onFiles(files: File[]): void;
}

/**
 * Web only: paste an image or file into the composer, or drop files on the
 * chat. `dragging` is true while files are over the pane. A drop that misses
 * the pane is swallowed so the browser never navigates away to the file.
 */
export function useComposerFileTransfer({ rootRef, enabled, onFiles }: ComposerFileTransferInput): { dragging: boolean } {
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (Platform.OS !== "web" || typeof window === "undefined") return;
    const root = rootRef.current as HTMLElement | null;
    if (!root || typeof root.addEventListener !== "function") return;
    let depth = 0;
    const end = () => {
      depth = 0;
      setDragging(false);
    };
    const onDragEnter = (event: DragEvent) => {
      if (!dragHasFiles(event.dataTransfer)) return;
      event.preventDefault();
      if (enabled) {
        depth += 1;
        setDragging(true);
      }
    };
    const onDragOver = (event: DragEvent) => {
      if (!dragHasFiles(event.dataTransfer)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = enabled ? "copy" : "none";
    };
    const onDragLeave = (event: DragEvent) => {
      if (!dragHasFiles(event.dataTransfer)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDragging(false);
    };
    const onDrop = (event: DragEvent) => {
      if (!dragHasFiles(event.dataTransfer)) return;
      event.preventDefault();
      end();
      if (enabled) onFiles(Array.from(event.dataTransfer?.files ?? []));
    };
    const onPaste = (event: ClipboardEvent) => {
      if (!enabled) return;
      const files = clipboardFiles(event.clipboardData);
      if (!files.length) return;
      event.preventDefault();
      onFiles(files);
    };
    // A file dropped beside the pane would otherwise open in the tab.
    const swallowStrayDrop = (event: DragEvent) => {
      if (!dragHasFiles(event.dataTransfer) || root.contains(event.target as Node | null)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = "none";
    };
    root.addEventListener("dragenter", onDragEnter);
    root.addEventListener("dragover", onDragOver);
    root.addEventListener("dragleave", onDragLeave);
    root.addEventListener("drop", onDrop);
    root.addEventListener("paste", onPaste);
    window.addEventListener("dragover", swallowStrayDrop);
    window.addEventListener("drop", swallowStrayDrop);
    return () => {
      root.removeEventListener("dragenter", onDragEnter);
      root.removeEventListener("dragover", onDragOver);
      root.removeEventListener("dragleave", onDragLeave);
      root.removeEventListener("drop", onDrop);
      root.removeEventListener("paste", onPaste);
      window.removeEventListener("dragover", swallowStrayDrop);
      window.removeEventListener("drop", swallowStrayDrop);
      end();
    };
  }, [enabled, onFiles, rootRef]);

  return { dragging };
}
