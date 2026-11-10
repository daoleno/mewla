/** The chat activity turn from the Expo app's services/screenshotDemoFixtures.ts (test support). */
import type { CodexConversationEvent } from "../conversation/conversation";

const DEMO_TIMESTAMP = "2026-06-18T09:30:00.000Z";

/**
 * A Worker turn with every tool row state: read, search, passed test, patch,
 * a failed command with its output, and a command still running.
 */
export const SCREENSHOT_CHAT_ACTIVITY_EVENTS: CodexConversationEvent[] = [
  {
    id: "activity-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Tidy the settings copy. Keep the word “sync”, then post the notes to Notion.",
  },
  {
    id: "activity-read",
    seq: 2,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Read",
    input: '{"file_path":"app/settings/strings.ts"}',
    status: "completed",
  },
  {
    id: "activity-grep",
    seq: 3,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: 'rg -n "sync" app/settings',
    output: "app/settings/strings.ts:12: syncPaused\napp/settings/strings.ts:31: syncConflict",
    status: "completed",
    exit_code: 0,
  },
  {
    id: "activity-patch",
    seq: 4,
    kind: "patch",
    timestamp: DEMO_TIMESTAMP,
    status: "completed",
    file_changes: [
      { path: "app/settings/strings.ts", operation: "update", additions: 7, deletions: 7 },
    ],
  },
  {
    id: "activity-test",
    seq: 5,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "bun test settings",
    output: "12 pass\n0 fail",
    status: "completed",
    exit_code: 0,
  },
  {
    id: "activity-failed",
    seq: 6,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "bun run notes:publish",
    output: "Error: Notion API returned 401 Unauthorized\n    at publish (scripts/notion.ts:42:11)",
    body: "Error: Notion API returned 401 Unauthorized\n    at publish (scripts/notion.ts:42:11)",
    status: "failed",
    exit_code: 1,
  },
  {
    id: "activity-assistant",
    seq: 7,
    kind: "assistant_message",
    timestamp: DEMO_TIMESTAMP,
    body: "The copy is tidy and the tests pass; `syncPaused` now reads:\n\n```ts\nexport const strings = {\n  syncPaused: \"Sync paused\", // was 33 chars\n  retries: 3,\n};\n```\n\nPublishing failed: Notion rejected the token, so nothing was posted. I’m running the typecheck while you look.",
  },
  {
    id: "activity-running",
    seq: 8,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "bunx tsc --noEmit",
    output: "",
    status: "running",
  },
];
