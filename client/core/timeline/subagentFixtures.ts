/** The subagent session from the Expo app's services/screenshotDemoFixtures.ts (test support). */
import type { CodexConversationEvent } from "../conversation/conversation";

const DEMO_TIMESTAMP = "2026-06-18T09:30:00.000Z";

/** A turn that delegates: finished reads fold, subagents stay on their own rows. */
export const SCREENSHOT_SUBAGENT_EVENTS: CodexConversationEvent[] = [
  {
    id: "subagent-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Audit the sync module and the settings copy in parallel.",
  },
  {
    id: "subagent-read",
    seq: 2,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Read",
    input: '{"file_path":"app/sync/index.ts"}',
    status: "completed",
  },
  {
    id: "subagent-read-2",
    seq: 3,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Read",
    input: '{"file_path":"app/settings/strings.ts"}',
    status: "completed",
  },
  {
    id: "subagent-task",
    seq: 4,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Task",
    input: '{"description":"Audit the sync module","subagent_type":"Explore","prompt":"Look for conflicts."}',
    output: "Found 3 conflict paths; two lose the newest edit.",
    status: "completed",
  },
  {
    id: "subagent-agent",
    seq: 5,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Agent",
    input: '{"description":"Tidy the settings copy","subagent_type":"general-purpose","prompt":"Keep the word sync."}',
    status: "running",
  },
];

const subagentStep = (
  id: string,
  seq: number,
  tool_name: string,
  input: string,
  extra: Partial<CodexConversationEvent> = {},
): CodexConversationEvent => ({
  id,
  seq,
  kind: "tool",
  timestamp: DEMO_TIMESTAMP,
  tool_name,
  input,
  status: "completed",
  ...extra,
});

/**
 * The same session once the daemon attaches each subagent's own calls to the
 * call that started it: the finished one carries its steps and how long it took,
 * the running one is still at work.
 */
export const SCREENSHOT_SUBAGENT_WORK_EVENTS: CodexConversationEvent[] =
  SCREENSHOT_SUBAGENT_EVENTS.map((event) => {
    if (event.id === "subagent-task") {
      return {
        ...event,
        subagent: {
          agent_id: "a1",
          type: "Explore",
          description: "Audit the sync module",
          finished: true,
          started_at: "2026-10-17T10:00:00.000Z",
          ended_at: "2026-10-17T10:02:14.000Z",
          steps: [
            subagentStep("sub-1-read", 1, "Read", '{"file_path":"app/sync/index.ts"}'),
            subagentStep("sub-1-read-2", 2, "Read", '{"file_path":"app/sync/merge.ts"}'),
            subagentStep("sub-1-grep", 3, "Grep", '{"pattern":"lastWriteWins","path":"app/sync"}'),
            subagentStep("sub-1-test", 4, "Bash", '{"command":"bun test app/sync"}', {
              status: "failed",
              exit_code: 1,
              output: "2 fail",
            }),
            subagentStep("sub-1-read-3", 5, "Read", '{"file_path":"app/sync/queue.ts"}'),
            subagentStep("sub-1-read-4", 6, "Read", '{"file_path":"app/sync/clock.ts"}'),
          ],
        },
      };
    }
    if (event.id === "subagent-agent") {
      return {
        ...event,
        subagent: {
          agent_id: "a2",
          type: "general-purpose",
          description: "Tidy the settings copy",
          finished: false,
          started_at: "2026-10-17T10:01:00.000Z",
          ended_at: "2026-10-17T10:01:40.000Z",
          steps: [
            subagentStep("sub-2-read", 1, "Read", '{"file_path":"app/settings/strings.ts"}'),
            subagentStep("sub-2-edit", 2, "Edit", '{"file_path":"app/settings/strings.ts"}', {
              status: "running",
            }),
          ],
        },
      };
    }
    return event;
  });
