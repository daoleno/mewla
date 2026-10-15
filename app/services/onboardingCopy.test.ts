import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  INSTALL_COMMAND,
  INSTALL_NOTE,
  OTHER_WAYS_URL,
  START_COMMAND,
  START_NOTE,
  WELCOME_LEDE,
  WELCOME_TITLE,
  previousOnboardingStep,
} from "../components/onboarding/onboardingModel";

const read = (path: string) => readFileSync(join(import.meta.dir, path), "utf8");
const source = read("../app/onboarding.tsx");
const presentation = read("../components/onboarding/OnboardingPresentation.tsx");
const settings = read("../app/settings.tsx");
const importer = read("importConnection.ts");
const site = read("../../site/index.html");
const siteScript = read("../../site/main.js");
const text = (html: string) => html.replace(/<br>/g, " ").replace(/<[^>]+>/g, "").replace(/\s+/g, " ").trim();

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
  test("drops the old setup steps and never invents a pairing origin", () => {
    expect(presentation).not.toMatch(/mewla doctor|mewla --lan|Tailscale|One person/);
    expect(presentation).not.toMatch(/192\.168|0\.0\.0\.0|mewla pair http/);
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
});

describe("pairing from onboarding", () => {
  test("shares the Settings enrollment pipeline and its single-import latch", () => {
    expect(source).toContain("importConnection(value");
    expect(source).toContain("refreshServers(importedServer.id)");
    expect(source).toContain("tryClaimPairScan(scanClaim.current)");
    expect(source).toContain("releasePairScan(scanClaim.current)");
    expect(source).toContain("error instanceof PairingCancelledError");
    expect(source).not.toContain("markOnboarded");
    expect(settings).toContain('pathname: "/onboarding", params: { paired: "1" }');
    expect(importer.indexOf("await markOnboarded()")).toBeGreaterThan(importer.indexOf("await saveServer("));
    expect(importer.indexOf("await saveServer(")).toBeGreaterThan(importer.indexOf("await enrollWithDaemon("));
  });
  test("a paired computer reports connection progress and current-server recovery", () => {
    expect(source).toContain("isCurrentServer(server.id)");
    expect(presentation).toContain('label="Retry connection"');
    expect(presentation).toContain('label="Open Brain"');
  });
});
