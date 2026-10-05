/** Happy-eyeballs selection for a paired daemon's known entry points. */
export interface EndpointCandidate { name: string; url: string; }
export interface EndpointRaceResult { candidate: EndpointCandidate; latencyMs: number; }

export async function raceEndpoints(
  candidates: readonly EndpointCandidate[],
  probe: (url: string, signal: AbortSignal) => Promise<boolean>,
  options: { timeoutMs?: number } = {},
): Promise<EndpointRaceResult> {
  const timeoutMs = options.timeoutMs ?? 4000;
  const started = new Map<string, number>();
  const controllers = candidates.map(() => new AbortController());
  if (!candidates.length) throw new Error("No daemon endpoints are configured.");
  return new Promise<EndpointRaceResult>((resolve, reject) => {
    let remaining = candidates.length;
    let settled = false;
    const timers = controllers.map((controller) => setTimeout(() => controller.abort(), timeoutMs));
    candidates.forEach((candidate, index) => {
      started.set(candidate.url, now());
      void probe(candidate.url, controllers[index]!.signal).then((ok) => {
        if (!ok || settled) return;
        settled = true;
        controllers.forEach((other, otherIndex) => { if (otherIndex !== index) other.abort(); });
        timers.forEach(clearTimeout);
        resolve({ candidate, latencyMs: Math.max(1, Math.round(now() - started.get(candidate.url)!)) });
      }).catch(() => undefined).finally(() => {
        remaining -= 1;
        if (remaining === 0 && !settled) { settled = true; timers.forEach(clearTimeout); reject(new Error("No daemon endpoint is reachable.")); }
      });
    });
  });
}

function now(): number { return typeof performance !== "undefined" && typeof performance.now === "function" ? performance.now() : Date.now(); }
