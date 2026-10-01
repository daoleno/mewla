/** RN timers can wait until a display frame. Yield by elapsed work, rather
 * than paying that wait for every small native history page. */
export function createTerminalHistoryDecodeBudget(
  now = () => performance.now(),
  yieldTask = () => new Promise<void>((resolve) => setTimeout(resolve, 0)),
) {
  let started = now();
  return () => {
    if (now() - started < 4) return null;
    return yieldTask().then(() => { started = now(); });
  };
}
