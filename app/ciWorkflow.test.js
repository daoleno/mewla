const { describe, expect, it } = require("bun:test");
const fs = require("fs");
const path = require("path");

const root = path.join(__dirname, "..");
const workflow = fs.readFileSync(
  path.join(root, ".github", "workflows", "ci.yml"),
  "utf8",
);
const rootPackage = require(path.join(root, "package.json"));

describe("ordinary CI contract", () => {
  it("keeps the offline installer suite in a bounded Linux job", () => {
    expect(rootPackage.scripts["installer:test"]).toBe(
      "./scripts/tests/install_test.sh",
    );
    expect(workflow).toMatch(
      /installer:\s*[\s\S]*?runs-on: ubuntu-latest[\s\S]*?timeout-minutes: 5[\s\S]*?run: bun run installer:test/,
    );
  });

  it("exports only the web bundle on an ordinary push and gates native exports", () => {
    const appJob = workflow.slice(
      workflow.indexOf("  app:"),
      workflow.indexOf("  ios-native:"),
    );
    expect(appJob).toContain("bunx expo export --platform web");
    expect(appJob).toMatch(
      /name: Android export\s*\n\s*if: github\.event_name == 'workflow_dispatch' \|\| github\.ref_type == 'tag'/,
    );
    expect(appJob).toMatch(
      /name: iOS export\s*\n\s*if: github\.event_name == 'workflow_dispatch' \|\| github\.ref_type == 'tag'/,
    );
  });

  it("runs native jobs only on tag pushes or manual dispatch", () => {
    expect(workflow).toMatch(/push:\s*\n\s*branches: \[main\]\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*"\s*\n\s*- "v\*\.\*\.\*-beta\.\*"/);
    expect(workflow).toMatch(/pull_request:\s*\n\s*workflow_dispatch:/);
    for (const [job, name] of [
      ["ios-native", "iOS native"],
      ["android-native", "Android native"],
    ]) {
      expect(workflow).toMatch(
        new RegExp(
          `${job}:\\s*\\n\\s*name: ${name}[^\\n]*\\n\\s*if: github\\.event_name == 'workflow_dispatch' \\|\\| github\\.ref_type == 'tag'`,
        ),
      );
    }
  });

  it("runs daemon tests and vet on every push and gates release targets", () => {
    const daemonJob = workflow.slice(
      workflow.indexOf("  daemon:"),
      workflow.indexOf("  app:"),
    );
    expect(daemonJob).toMatch(
      /run: go test \.\/\.\.\.[\s\S]*run: go vet \.\/\.\.\.[\s\S]*run: python3 scripts\/test-plugin-publisher-flags\.py/,
    );
    expect(daemonJob).toMatch(
      /name: Build release targets\s*\n\s*if: github\.event_name == 'workflow_dispatch' \|\| github\.ref_type == 'tag'/,
    );
    expect(daemonJob).toMatch(
      /name: Install web UI build dependencies\s*\n\s*if: github\.event_name == 'workflow_dispatch' \|\| github\.ref_type == 'tag'/,
    );
  });

  it("keeps bounded Android native evidence beside the iOS native job", () => {
    expect(workflow).toMatch(
      /ios-native:\s*[\s\S]*?name: iOS native \(unsigned Simulator\)/,
    );
    expect(workflow).toMatch(
      /android-native:\s*[\s\S]*?name: Android native \(bounded link\)/,
    );
    expect(workflow).toContain("zen-android-ghostty-output-v2-arm64-");
    expect(workflow).toContain(
      "app/modules/zen-terminal-vt/android/src/main/cpp/ghostty",
    );
    expect(workflow).toContain(
      "./scripts/build-libghostty.sh --abis arm64-v8a",
    );
    const installZigStep = workflow.indexOf(
      "- name: Install Zig when Android Ghostty cache misses",
    );
    const buildGhosttyStep = workflow.indexOf(
      "- name: Build arm64 Ghostty when cache misses",
    );
    expect(installZigStep).toBeGreaterThan(-1);
    expect(buildGhosttyStep).toBeGreaterThan(installZigStep);
    expect(workflow).toContain("./scripts/verify-zen-terminal-abi-gradle.sh");
    expect(workflow).toContain(":zen-terminal-vt:assembleDebug");
    expect(workflow).toContain(":app:assembleDebug");
    expect(workflow).not.toMatch(
      /android-native:[\s\S]*?android-release-apk\.sh/,
    );
  });

  it("asserts barcode companion pods after iOS pod install", () => {
    expect(workflow).toContain(
      "./scripts/assert-ios-camera-barcode-pod.sh Podfile.lock",
    );
  });
});
