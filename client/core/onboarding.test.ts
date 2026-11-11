import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  INSTALL_COMMAND,
  INSTALL_NOTE,
  OTHER_WAYS_URL,
  PairLatch,
  START_COMMAND,
  START_NOTE,
  WELCOME_LEDE,
  WELCOME_TITLE,
  cameraSearching,
  catBoxForStep,
  catClipFor,
  connectedCopy,
  cornerCatBox,
  finderState,
  heroCatBox,
  heroZoneWidth,
  isCompactFrame,
  previousOnboardingStep,
  readCameraPermission,
  sealShown,
} from "./onboarding";
import { NOT_A_CODE, alreadyPairedWith, pairWith, pairingProblem, pairingScopeCopy, readPairingLink } from "./pairFlow";
import { installMemoryPlatform } from "./testing/memoryPlatform";

const site = readFileSync(join(import.meta.dir, "../../site/index.html"), "utf8");
const siteScript = readFileSync(join(import.meta.dir, "../../site/main.js"), "utf8");
const text = (html: string) => html.replace(/<br>/g, " ").replace(/<[^>]+>/g, "").replace(/\s+/g, " ").trim();
const directLink = (url = "http://10.0.0.5:9876", key = "b".repeat(64)) =>
  `mewla://settings?u=${encodeURIComponent(url)}&k=${key}&t=${"c".repeat(64)}&n=Desk`;

describe("onboarding copy follows the homepage", () => {
  test("welcome is the hero", () => {
    expect(text(site.match(/<h1>(.*?)<\/h1>/)![1])).toBe(WELCOME_TITLE);
    expect(text(site.match(/<p class="lede">(.*?)<\/p>/)![1])).toBe(WELCOME_LEDE);
  });
  test("install and start are the homepage's install steps", () => {
    const script = siteScript.match(/installScript: '([^']+)'/)![1];
    const pattern = site.match(/data-link="installScript"><code data-cmd="([^"]+)"/)![1];
    expect(INSTALL_COMMAND).toBe(pattern.replace("{}", script));
    expect(site).toContain(`<p class="small">${INSTALL_NOTE}</p>`);
    expect(site).toContain(`<code>${START_COMMAND}</code>`);
    expect(site).toContain(`<p class="small">${START_NOTE} <a href="docs/connect-and-pair/#choose-an-address">Other ways to connect</a>`);
    expect(OTHER_WAYS_URL).toBe("https://mewla.ai/docs/connect-and-pair/#choose-an-address");
  });
});

describe("onboarding steps", () => {
  test("Back walks the steps, then leaves", () => {
    expect(previousOnboardingStep("install", "welcome")).toBe("welcome");
    expect(previousOnboardingStep("scan", "install")).toBe("install");
    expect(previousOnboardingStep("scan", "welcome")).toBe("welcome");
    expect(previousOnboardingStep("welcome", null)).toBeNull();
    expect(previousOnboardingStep("connected", "scan")).toBeNull();
  });

  test("the cat follows the step and the connection; the seal is in only on Welcome and once Brain is home", () => {
    expect(catClipFor("welcome", false, undefined)).toBe("attention");
    expect(catClipFor("install", false, undefined)).toBe("working");
    expect(catClipFor("scan", false, undefined)).toBe("delegating");
    expect(catClipFor("scan", true, undefined)).toBe("attention");
    expect(catClipFor("connected", false, "connected")).toBe("attention");
    expect(catClipFor("connected", false, "connecting")).toBe("delegating");
    expect(catClipFor("connected", false, "waiting")).toBe("offline");
    expect(sealShown("welcome", undefined)).toBe(true);
    expect(sealShown("connected", "connecting")).toBe(false);
    expect(sealShown("connected", "connected")).toBe(true);
    expect(sealShown("scan", "connected")).toBe(false);
  });

  test("Can't reach says what the computer gave, never the transport's own words", () => {
    for (const word of ["error", "silent", "closed (1006)", "direct-available", ""]) {
      expect(connectedCopy("waiting", "Desk", word)).toEqual({ title: "Can't reach your computer", lede: "Check that mewla is running on Desk." });
    }
    expect(connectedCopy("waiting", "Desk", "This device was removed").lede).toBe("This device was removed");
  });
});

const frame = (width: number, height: number, top: number, bottom: number) => ({ width, height, top, bottom });

describe("onboarding layout", () => {
  test("a phone upright keeps the cat above the text", () => {
    for (const f of [frame(402, 874, 62, 34), frame(375, 667, 20, 0), frame(360, 800, 24, 0), frame(820, 1180, 24, 20)]) {
      expect(isCompactFrame(f)).toBe(false);
      expect(heroZoneWidth(f)).toBe(0);
      const usable = f.height - f.top - f.bottom;
      expect(heroCatBox(f)).toEqual({ cx: f.width / 2, cy: f.top + Math.max(132, Math.round(usable * 0.27)), size: 164 });
      expect(catBoxForStep("welcome", f)).toEqual(heroCatBox(f));
    }
  });

  test("short windows put the cat in a column of its own, inside the usable height", () => {
    for (const f of [frame(594, 466, 0, 34), frame(750, 402, 0, 21), frame(800, 360, 24, 0)]) {
      expect(isCompactFrame(f)).toBe(true);
      const usable = f.height - f.top - f.bottom;
      const zone = heroZoneWidth(f);
      const box = heroCatBox(f);
      expect(zone).toBeGreaterThan(0);
      expect(zone).toBeLessThanOrEqual(260);
      expect(box.cx + box.size / 2).toBeLessThanOrEqual(zone);
      expect(box.cy - box.size / 2).toBeGreaterThanOrEqual(f.top);
      expect(box.cy + box.size / 2).toBeLessThanOrEqual(f.top + usable);
    }
    expect(isCompactFrame(frame(600, 560, 0, 0))).toBe(false);
  });

  test("the corner cat stays inside the frame on install and scan", () => {
    for (const f of [frame(390, 844, 47, 34), frame(750, 402, 0, 21)]) {
      const corner = cornerCatBox(f);
      expect(corner.cx + corner.size / 2).toBeLessThanOrEqual(f.width);
      expect(catBoxForStep("scan", f)).toEqual(corner);
      expect(catBoxForStep("install", f)).toEqual(corner);
    }
  });
});

describe("the viewfinder", () => {
  test("never a dark window that promises a camera", () => {
    expect(finderState(null, false, false)).toBe("loading");
    expect(finderState("undetermined", false, true)).toBe("loading");
    expect(finderState("undetermined", false, false)).toBe("request");
    expect(finderState("denied", false, false)).toBe("denied");
    expect(finderState("granted", true, false)).toBe("failed");
    expect(finderState("granted", false, false)).toBe("camera");
  });

  test("says it is looking only while the camera runs in front and nothing pairs", () => {
    expect(cameraSearching("camera", true, false)).toBe(true);
    expect(cameraSearching("camera", false, false)).toBe(false);
    expect(cameraSearching("camera", true, true)).toBe(false);
    expect(cameraSearching("denied", true, false)).toBe(false);
  });

  test("reads the host's permission answer strictly", () => {
    expect(readCameraPermission("granted")).toBe("granted");
    expect(readCameraPermission({ granted: true })).toBeNull();
  });
});

describe("one pairing at a time", () => {
  test("camera and paste share one latch; a refused code in view is reported once", () => {
    const latch = new PairLatch();
    expect(latch.claim("A", true)).toBe(true);
    expect(latch.claim("B", false)).toBe(false);
    latch.release("A");
    expect(latch.claim("A", true)).toBe(false);
    expect(latch.claim("A", false)).toBe(true);
    latch.release();
    expect(latch.claim("C", true)).toBe(true);
  });
});

describe("pairing", () => {
  test("only a link with the computer's key and a one-time code is a pairing code", () => {
    expect(readPairingLink(`  ${directLink()}\n`)?.url).toBe("ws://10.0.0.5:9876/ws");
    expect(readPairingLink("hello")).toBeNull();
    expect(readPairingLink("mewla://brain")).toBeNull();
    expect(NOT_A_CODE.detail).toContain("mewla pair --details");
  });

  test("asks before granting access, except for a computer this phone already paired", () => {
    const copy = pairingScopeCopy();
    expect(copy.title).toBe("Pair this phone?");
    expect(copy.message).toContain("sessions, terminal, Brain, Workers, and files");
    expect(copy.confirm).toBe("Pair and grant access");
    const target = readPairingLink(directLink())!;
    expect(alreadyPairedWith([{ daemonPublicKey: "B".repeat(64) }], target)).toBe(true);
    expect(alreadyPairedWith([{ daemonPublicKey: "a".repeat(64) }], target)).toBe(false);
    expect(alreadyPairedWith([{ daemonPublicKey: "b".repeat(64), daemonId: "x" }], { ...target, daemonId: "y" })).toBe(false);
  });

  test("an unreachable computer says where and what to check", async () => {
    installMemoryPlatform({ fetch: () => Promise.reject(new Error("Network request failed")) });
    const error = await pairWith(readPairingLink(directLink())!).catch((e) => e);
    const problem = pairingProblem(error);
    expect(problem.title).toBe("Can't reach your computer");
    expect(problem.detail).toContain("10.0.0.5:9876");
    expect(problem.detail).toContain("mewla is running");
  });

  test("a used or expired code says so, with the daemon's reason", async () => {
    installMemoryPlatform({ fetch: async () => new Response("pairing code expired", { status: 401 }) });
    const problem = pairingProblem(await pairWith(readPairingLink(directLink())!).catch((e) => e));
    expect(problem).toEqual({ title: "Code used or expired", detail: "pairing code expired" });
  });

  test("a loopback code names the real problem", async () => {
    installMemoryPlatform({ fetch: () => Promise.reject(new Error("refused")) });
    const problem = pairingProblem(await pairWith(readPairingLink(directLink("http://127.0.0.1:9876"))!).catch((e) => e));
    expect(problem.detail).toContain("the phone itself");
  });
});
