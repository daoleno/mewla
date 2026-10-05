import { describe, expect, test } from "bun:test";
import { raceEndpoints } from "./endpointRacing";

describe("raceEndpoints", () => {
  test("returns the first healthy endpoint and cancels slower probes", async () => {
    const aborted: string[] = [];
    const result = await raceEndpoints([
      { name: "slow", url: "https://slow.example" },
      { name: "fast", url: "https://fast.example" },
    ], async (url, signal) => {
      await new Promise((resolve) => setTimeout(resolve, url.includes("slow") ? 30 : 2));
      if (signal.aborted) { aborted.push(url); return false; }
      return url.includes("fast");
    });
    expect(result.candidate.name).toBe("fast");
    expect(result.latencyMs).toBeGreaterThan(0);
  });
});
