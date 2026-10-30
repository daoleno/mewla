import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(
  join(import.meta.dir, "../app/settings.tsx"),
  "utf8",
);

describe("Settings copy density", () => {
  test("does not restate nearby controls, selection state, or screen identity", () => {
    for (const redundantCopy of [
      "Pair your first server below",
      "Pair via link or QR code",
      "System follows this device. Light and Dark stay fixed until changed.",
      "Mobile-native agent control plane",
      '"Current daemon"',
      "Scan the QR, or choose an image from this device.",
      "Advanced / Self-managed: run mewla pair",
      "Bot chats are Telegram cloud chats. The token remains on this daemon.",
      "Create a bot with BotFather, then enter its token once.",
      "Full-origin endpoint from LAN, Tailscale, Cloudflare",
    ]) {
      expect(source).not.toContain(redundantCopy);
    }
  });

  test("retains requirements, consequences, security boundaries, and accessibility semantics", () => {
    for (const necessaryCopy of [
      "Camera permission required",
      "Allow camera access to scan a mewla pairing QR code.",
      "On the computer, run mewla pair. Scan its QR code or paste the link it prints.",
      "Telegram cloud messages are not deleted.",
    ]) {
      expect(source).toContain(necessaryCopy);
    }
    // A computer's address is edited on its own page, never in the list.
    const computerPage = readFileSync(
      join(import.meta.dir, "../components/settings/ComputerPanel.tsx"),
      "utf8",
    );
    expect(computerPage).toContain('accessibilityLabel="Address"');
    // Appearance is one row per theme; each row owns its radio state.
    const appearance = readFileSync(
      join(import.meta.dir, "../components/settings/AppearanceSection.tsx"),
      "utf8",
    );
    expect(appearance).toContain('accessory="check"');
    expect(appearance).toContain("selected={choice.value === current}");
    // The segmented control keeps its own radio contract for the screens that use it.
    const segmented = readFileSync(
      join(import.meta.dir, "../components/ui/SegmentedControl.tsx"),
      "utf8",
    );
    expect(segmented).toContain('accessibilityRole="radiogroup"');
    expect(segmented).toContain('accessibilityRole="radio"');
    expect(segmented).toContain("accessibilityState={{ checked: selected, selected }}");
  });
});
