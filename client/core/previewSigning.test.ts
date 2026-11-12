import { describe, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// R5 (signer): every dispatched Preview was signed with its CI runner's own debug key, a new one per
// run, so no Preview could install over the last (eada627c 541e1eac…, 06e6b4d2 3568cc77…, 1ce8abb7
// 685f9d92…). A Preview now carries the release key, and the gate installs it over the previous one.
const root = join(import.meta.dir, "../..");
const workflow = readFileSync(join(root, ".github/workflows/client.yml"), "utf8");

describe("every Preview is signed with one key", () => {
  test("the key is the release certificate the release checks pin", () => {
    const out = spawnSync("bash", [join(root, "client/scripts/preview-signer.sh"), "--expected"], { encoding: "utf8" });
    expect(out.status).toBe(0);
    const pinned = readFileSync(join(root, "scripts/verify-apk-release.sh"), "utf8").match(/^EXPECTED_CERT="([0-9A-F:]+)"$/m)?.[1];
    expect(out.stdout.trim()).toBe(pinned!.replaceAll(":", "").toLowerCase());
  });

  test("a dispatched Preview is built with the release keystore and checked before it is kept", () => {
    const build = workflow.slice(workflow.indexOf("- name: Build the preview APK"), workflow.indexOf("- name: Shred the keystore"));
    expect(build).toContain("secrets.ZEN_ANDROID_KEYSTORE_PASSWORD");
    expect(build).toContain("mewla-release.p12");
    const check = workflow.indexOf("client/scripts/preview-signer.sh client/android/app/build/outputs/apk/preview/release/");
    const upload = workflow.indexOf("name: mewla-preview-android\n");
    expect(check).toBeGreaterThan(0);
    expect(check).toBeLessThan(upload);
  });

  test("the gate installs this Preview over the last dispatched one", () => {
    expect(workflow).toContain("needs: android");
    expect(workflow).toContain("actions: read");
    expect(workflow).toMatch(/script: \|\n\s+client\/scripts\/upgrade-gate\.sh\n\s+client\/scripts\/preview-upgrade\.sh\n/);
    const gate = readFileSync(join(root, "client/scripts/preview-upgrade.sh"), "utf8");
    expect(gate).toContain('adb install -r "$PREVIEW_APK"');
  });
});
