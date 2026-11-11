import { describe, expect, test } from "bun:test";
import { directHint, SlowWatch } from "./connectionWatch";
import type { StreamReadout } from "./screenPageModel";

const readout = (rttMs: number | null): StreamReadout => ({ rttMs, inputMs: null, fps: 30, kbps: 800 });

describe("SlowWatch", () => {
  test("three slow samples in a row light it, and it is said once", () => {
    const watch = new SlowWatch();
    expect(watch.sample(readout(300))).toEqual({ slow: false, say: false });
    expect(watch.sample(readout(300))).toEqual({ slow: false, say: false });
    expect(watch.sample(readout(300))).toEqual({ slow: true, say: true });
    expect(watch.sample(readout(300))).toEqual({ slow: true, say: false });
  });

  test("a good sample ends the streak; a new connection may say it again", () => {
    const watch = new SlowWatch();
    watch.sample(readout(300));
    watch.sample(readout(300));
    expect(watch.sample(readout(40)).slow).toBe(false);
    expect(watch.sample(readout(300)).slow).toBe(false);
    watch.sample(readout(300));
    expect(watch.sample(readout(300))).toEqual({ slow: true, say: true });
    watch.reset();
    watch.sample(readout(300));
    watch.sample(readout(300));
    expect(watch.sample(readout(300)).say).toBe(true);
  });

  test("no round trip yet clears it", () => {
    const watch = new SlowWatch();
    watch.sample(readout(300));
    watch.sample(readout(300));
    watch.sample(readout(null));
    expect(watch.sample(readout(300)).slow).toBe(false);
  });
});

describe("directHint", () => {
  test("only a relay stream has one", () => {
    expect(directHint({}, "lan")).toBeNull();
    expect(directHint({ transportCandidates: [{ name: "lan", kind: "direct", url: "http://10.0.0.2:9877" }] }, "relay")).toBe("announced");
    expect(directHint({ transportCandidates: [] }, "relay")).toBe("none");
  });
});
