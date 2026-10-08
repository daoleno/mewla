import { describe, expect, test } from "bun:test";
import {
  BRAND_COLORS,
  DARK_CHAT_PALETTE,
  LIGHT_CHAT_PALETTE,
  DARK_NEUTRALS,
  LIGHT_NEUTRALS,
} from "./primitives";

describe("chat outbound send status token", () => {
  test("outboundSentClock is dedicated high-contrast Mewla status on chat.background", () => {
    // Light: quiet ink on paper (outside the ink bubble), not bubble fill / outline.
    expect(LIGHT_CHAT_PALETTE.outboundSentClock).toBe(LIGHT_NEUTRALS.textSecondary);
    expect(LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      LIGHT_CHAT_PALETTE.sentBubble,
    );
    expect(LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      LIGHT_CHAT_PALETTE.background,
    );
    expect(LIGHT_CHAT_PALETTE.background).toBe(LIGHT_NEUTRALS.canvas);

    // Dark: secondary paper on the ink environment — readable outside bubble paint.
    // Same hex as sentTimestamp is intentional: both are high-contrast meta,
    // but outboundSentClock is the status-affordance owner (not bubble chrome).
    expect(DARK_CHAT_PALETTE.outboundSentClock).toBe(DARK_NEUTRALS.textSecondary);
    expect(DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      DARK_CHAT_PALETTE.sentBubble,
    );
    expect(DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      DARK_CHAT_PALETTE.background,
    );
    expect(DARK_CHAT_PALETTE.background).toBe(BRAND_COLORS.environment);
  });
});
