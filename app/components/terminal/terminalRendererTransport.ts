/** The native view tag comes from this renderer's ready event. An epoch guards
 * asynchronous failures; the script guard rejects work queued before reload. */
export function createTerminalRendererTransport(
  dispatch: (viewTag: number, script: string) => Promise<void>,
  failed: (message: string, generation: number) => void,
) {
  let target: { viewTag: number; generation: number } | null = null;
  return {
    bind(viewTag: number, generation: number) {
      if (!Number.isSafeInteger(viewTag) || viewTag <= 0) {
        throw new Error('Terminal renderer ready event has no native view tag');
      }
      target = { viewTag, generation };
    },
    clear() { target = null; },
    send(script: string) {
      const current = target;
      if (!current) return;
      const guarded = `if (window.__zenRendererGeneration === ${current.generation}) { ${script} } true;`;
      void dispatch(current.viewTag, guarded).catch(() => {
        if (target !== current) return;
        target = null;
        failed('Terminal renderer transport failed. Retry the terminal.', current.generation);
      });
    },
  };
}
