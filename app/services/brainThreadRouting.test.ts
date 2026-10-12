import { describe, expect, test } from "bun:test";
import {
  isTargetedBrainThreadReadOnly,
  nextBrainChatMount,
} from "./brainThreadRouting";

describe("Brain targeted-thread routing", () => {
  test("a notification-targeted thread is read-only unless it is the live Host thread", () => {
    expect(isTargetedBrainThreadReadOnly("historical", "live")).toBe(true);
    expect(isTargetedBrainThreadReadOnly("historical", undefined)).toBe(true);
    expect(isTargetedBrainThreadReadOnly("live", "live")).toBe(false);
    expect(isTargetedBrainThreadReadOnly(undefined, "live")).toBe(false);
  });
});

describe("Brain chat mount", () => {
  test("the first snapshot naming the thread binds the mounted chat in place", () => {
    const waking = nextBrainChatMount(null, "s1", undefined);
    const bound = nextBrainChatMount(waking, "s1", "brain-thread:t1");
    expect(bound.key).toBe(waking.key);
    expect(bound.threadScopeKey).toBe("brain-thread:t1");
    expect(nextBrainChatMount(bound, "s1", "brain-thread:t1")).toBe(bound);
    // A snapshot that briefly lacks the thread keeps the mounted chat.
    expect(nextBrainChatMount(bound, "s1", undefined)).toBe(bound);
  });

  test("a different thread or server remounts the chat", () => {
    const bound = nextBrainChatMount(null, "s1", "brain-thread:t1");
    expect(nextBrainChatMount(bound, "s1", "brain-thread:t2").key).toBe(
      "brain-chat:s1:brain-thread:t2",
    );
    expect(nextBrainChatMount(bound, "s2", "brain-thread:t1").key).toBe(
      "brain-chat:s2:brain-thread:t1",
    );
    const waking = nextBrainChatMount(null, "s1", undefined);
    expect(nextBrainChatMount(waking, "s2", undefined).key).toBe("brain-chat:s2:");
  });
});
