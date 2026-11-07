import React, { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { BrainWorkSheet } from "./BrainWorkPanel";

export interface BrainWorkSheetHandle {
  open(): void;
  close(): void;
}

type SheetProps = Omit<React.ComponentProps<typeof BrainWorkSheet>, "visible" | "onClose">;

/**
 * The phone's Work sheet with its own open state. The tail row's tap opens it
 * through the handle, so only this sheet renders: opening never re-renders the
 * Brain screen (the chat, composer and menus), which is what the tap waits for.
 * `suppressed` (the wide Work column is showing) closes it and keeps it shut.
 */
export const BrainWorkSheetHost = forwardRef<BrainWorkSheetHandle, SheetProps & { suppressed?: boolean }>(
  function BrainWorkSheetHost({ suppressed = false, ...sheet }, ref) {
    const [visible, setVisible] = useState(false);
    // Beside the Work column there is no sheet to open, and none left waiting.
    const suppressedRef = useRef(suppressed);
    suppressedRef.current = suppressed;
    const open = useCallback(() => {
      if (!suppressedRef.current) setVisible(true);
    }, []);
    const close = useCallback(() => setVisible(false), []);
    useImperativeHandle(ref, () => ({ open, close }), [open, close]);
    useEffect(() => {
      if (suppressed) setVisible(false);
    }, [suppressed]);
    return <BrainWorkSheet {...sheet} visible={visible && !suppressed} onClose={close} />;
  },
);
