/** Send the first update without waiting for a second display clock. During
 * sustained output, retain dirty state in Ghostty and send at most once per
 * interval; the WebView remains the owner of presentation vsync. */
export function createTerminalRenderScheduler(
  flush: () => void,
  clock = {
    now: () => performance.now(),
    setTimeout: (callback: () => void, delay: number) => setTimeout(callback, delay),
    clearTimeout: (timer: ReturnType<typeof setTimeout>) => clearTimeout(timer),
  },
) {
  let lastFlush = -Infinity;
  let timer: ReturnType<typeof setTimeout> | null = null;
  const run = () => {
    timer = null;
    lastFlush = clock.now();
    flush();
  };
  return {
    schedule() {
      if (timer !== null) return;
      const remaining = 16 - (clock.now() - lastFlush);
      if (remaining <= 0) run();
      else timer = clock.setTimeout(run, remaining);
    },
    cancel() {
      if (timer !== null) clock.clearTimeout(timer);
      timer = null;
      lastFlush = -Infinity;
    },
  };
}
