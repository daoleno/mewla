import type { StoredServer } from "../storedServerContract";
import { connectionProblem, nextSlowStreak, slowLasts, type StreamReadout } from "./screenPageModel";
import type { ScreenPath } from "./screenStream";

/** How long the header says "Slow connection", once per connection. */
export const SLOW_NOTE_MS = 8_000;

/**
 * Whether slowness has lasted, sample by sample (one per round trip), and whether to
 * say so now: once per connection, so one spike never lights the indicator and a slow
 * link is mentioned only once. `reset` starts a new connection.
 */
export class SlowWatch {
  private streak = 0;
  private said = false;

  reset(): void {
    this.streak = 0;
    this.said = false;
  }

  /** A sample with no round trip measured clears the streak. */
  sample(readout: StreamReadout | null): { slow: boolean; say: boolean } {
    if (!readout || readout.rttMs === null) {
      this.streak = 0;
      return { slow: false, say: false };
    }
    this.streak = nextSlowStreak(this.streak, connectionProblem(readout) !== null);
    const slow = slowLasts(this.streak);
    const say = slow && !this.said;
    if (say) this.said = true;
    return { slow, say };
  }
}

/**
 * Why a relay stream is not direct: "announced" when the computer published LAN or
 * Tailscale addresses (this phone, on the relay, could not use them), "none" when it
 * published none, null when the stream is not on the relay.
 */
export function directHint(server: Pick<StoredServer, "transportCandidates">, path: ScreenPath | null | undefined): "announced" | "none" | null {
  if (path !== "relay") return null;
  return (server.transportCandidates ?? []).some((candidate) => candidate.kind === "direct") ? "announced" : "none";
}
