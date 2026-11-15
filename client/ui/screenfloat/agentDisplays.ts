import { signal } from "nsh";
import type { AgentDisplay } from "../../core/screen/screen";

/**
 * The current computer's agent displays (Linux), as the floating screen last read them: empty
 * elsewhere and until the first read. A Worker's display lasts as long as its Session, so a page it
 * left for the person (a sign-in) is here after its turn ends; its Work card offers Screen from it.
 */
export const agentDisplays = signal<AgentDisplay[]>([]);

/** The Worker's display while it has something on it (a browser window or an app), or undefined. */
export function displayOfWorker(list: readonly AgentDisplay[], worker: string | undefined): AgentDisplay | undefined {
  if (!worker) return undefined;
  return list.find((display) => display.id === worker && ((display.browser ?? 0) > 0 || display.apps > 0));
}
