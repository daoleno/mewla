import type { Daemon } from "../more/daemon";
import { codexSubscriptionVisibleForProviderState } from "./codexSubscriptionStats";
import { normalizeStatsPayload, type StatsPayload, type StatsView } from "./statsPayload";

/** A computer that has not counted anything yet answers with no ranges; ask again a little later, three times. */
export const EMPTY_STATS_RETRIES = 3;
export const EMPTY_STATS_RETRY_MS = 700;

type Wait = (ms: number) => Promise<void>;
const wait: Wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * The computer's stats. The Codex subscription card shows only while Codex runs on
 * its own ChatGPT sign-in: a Provider routing Codex hides it (and the list of Providers
 * failing never hides the stats). A failure rejects; it is not retried behind the person's back.
 */
export async function loadStats(daemon: Daemon, options: { wait?: Wait; stillWanted?: () => boolean } = {}): Promise<StatsView | null> {
  const pause = options.wait ?? wait;
  for (let attempt = 0; ; attempt++) {
    const [providers, payload] = await Promise.all([
      daemon.request<{ defaults?: Record<string, { connection_id?: string } | undefined> }>("list_providers", {}, { reply: "providers" }).catch(() => null),
      daemon.request<StatsPayload>("get_stats", {}, { reply: "stats_data", errors: [], timeoutMs: 15_000 }).catch((error: unknown) => {
        throw new Error(error instanceof Error && /timed out/.test(error.message) ? "Stats request timed out." : error instanceof Error ? error.message : String(error));
      }),
    ]);
    const shown = codexSubscriptionVisibleForProviderState(payload.codexSubscription, providers) ? payload : { ...payload, codexSubscription: undefined };
    const view = normalizeStatsPayload(shown);
    if (Object.keys(view?.ranges ?? {}).length > 0 || attempt >= EMPTY_STATS_RETRIES) return view;
    await pause(EMPTY_STATS_RETRY_MS * (attempt + 1));
    if (options.stillWanted && !options.stillWanted()) return view;
  }
}
