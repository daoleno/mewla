import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { ToolDeveloperDetails } from "./toolCallDetails";
import type { TimelineItem } from "./types";
import type {
  ActivityChild,
  ActivityTimelineItem,
} from "./activityTypes";
import {
  activityChildrenEqual,
  attachmentsEqual,
  brainWorkResultEventsEqual,
  stringRecordsEqual,
  timelineItemsSemanticEqual,
  toolDeveloperDetailsEqual,
} from "./timelineItemsSemanticEqual";
import type { DisplayAttachment } from "./types";
import type { PlanTimelineItem } from "./types";
import type { CodexPlanStep } from "../conversation/conversation";

function makeActivity(
  overrides: Partial<ActivityTimelineItem> = {},
): ActivityTimelineItem {
  return {
    type: "activity",
    id: "activity-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    title: "run command",
    tone: "running",
    icon: "terminal",
    developerDetails: makeDeveloperDetails(),
    children: makeChildren(),
    ...overrides,
  };
}

function makeDeveloperDetails(
  overrides: Partial<ToolDeveloperDetails> = {},
): ToolDeveloperDetails {
  return {
    providerToolId: "tool-42",
    rawInput: '{"command":"ls","session_id":"s1"}',
    transport: {
      session_id: "s1",
      call_id: "c1",
      wall_time: "12ms",
    },
    ...overrides,
  };
}

function makeChildren(): ActivityChild[] {
  return [
    {
      id: "child-a",
      title: "spawn",
      tone: "success",
      providerToolId: "tool-a",
    },
    {
      id: "child-b",
      title: "await",
      tone: "running",
      providerToolId: "tool-b",
    },
  ];
}

function makeBrainEvent(
  overrides: Partial<BrainWorkResultEvent> = {},
): BrainWorkResultEvent {
  return {
    event_id: "evt-1",
    kind: "session.done",
    work_id: "work-1",
    work_title: "Ship slice",
    summary: "Done",
    session_id: "sess-1",
    session_name: "main",
    occurred_at: "2026-08-06T12:00:00.000Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    ...overrides,
  };
}

function makeBrainItem(
  overrides: Partial<Extract<TimelineItem, { type: "brain-work-event" }>> = {},
): Extract<TimelineItem, { type: "brain-work-event" }> {
  const event = makeBrainEvent();
  return {
    type: "brain-work-event",
    id: "brain-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    event,
    events: [event],
    onPress: () => {},
    ...overrides,
  };
}

function makeAttachment(
  overrides: Partial<DisplayAttachment> = {},
): DisplayAttachment {
  return {
    name: "photo.png",
    path: "/remote/photo.png",
    localUri: "file:///local/photo.png",
    mimeType: "image/png",
    ...overrides,
  };
}

function makeMessage(
  overrides: Partial<Extract<TimelineItem, { type: "message" }>> = {},
): Extract<TimelineItem, { type: "message" }> {
  return {
    type: "message",
    id: "msg-1",
    role: "user",
    timestamp: "2026-08-06T12:00:00.000Z",
    body: "hello",
    attachments: [makeAttachment()],
    ...overrides,
  };
}

function makePlanStep(
  overrides: Partial<CodexPlanStep> = {},
): CodexPlanStep {
  return {
    step: "Ship equality",
    status: "in_progress",
    ...overrides,
  };
}

function makePlan(
  overrides: Partial<PlanTimelineItem> = {},
): PlanTimelineItem {
  return {
    type: "plan",
    id: "plan-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    explanation: "Do the work",
    steps: [makePlanStep(), makePlanStep({ step: "Verify", status: "pending" })],
    ...overrides,
  };
}

describe("timelineItemsSemanticEqual", () => {
  test("identity fast path returns true without field walks", () => {
    const activity = makeActivity();
    const brain = makeBrainItem();
    expect(timelineItemsSemanticEqual(activity, activity)).toBe(true);
    expect(timelineItemsSemanticEqual(brain, brain)).toBe(true);
  });

  test("equal ToolDeveloperDetails and Activity children reuse", () => {
    const left = makeActivity();
    const right = makeActivity({
      developerDetails: makeDeveloperDetails(),
      children: makeChildren(),
    });
    expect(timelineItemsSemanticEqual(left, right)).toBe(true);
    expect(
      toolDeveloperDetailsEqual(left.developerDetails, right.developerDetails),
    ).toBe(true);
    expect(activityChildrenEqual(left.children, right.children)).toBe(true);
  });

  test("transport records with same pairs in different insertion order are equal", () => {
    const left = makeDeveloperDetails({
      transport: {
        session_id: "s1",
        call_id: "c1",
        wall_time: "12ms",
      },
    });
    const right = makeDeveloperDetails({
      transport: {
        wall_time: "12ms",
        session_id: "s1",
        call_id: "c1",
      },
    });
    expect(stringRecordsEqual(left.transport, right.transport)).toBe(true);
    expect(toolDeveloperDetailsEqual(left, right)).toBe(true);
    expect(
      timelineItemsSemanticEqual(
        makeActivity({ developerDetails: left }),
        makeActivity({ developerDetails: right }),
      ),
    ).toBe(true);
    // Legacy JSON.stringify treats insertion order as unequal — intentional fix.
    expect(JSON.stringify(left.transport) === JSON.stringify(right.transport)).toBe(
      false,
    );
  });

  test("each ToolDeveloperDetails field mutation independently prevents reuse", () => {
    const base = makeDeveloperDetails();
    expect(
      toolDeveloperDetailsEqual(base, makeDeveloperDetails({ providerToolId: "other" })),
    ).toBe(false);
    expect(
      toolDeveloperDetailsEqual(base, makeDeveloperDetails({ rawInput: '{"x":1}' })),
    ).toBe(false);
    expect(
      toolDeveloperDetailsEqual(
        base,
        makeDeveloperDetails({
          transport: { ...base.transport!, call_id: "changed" },
        }),
      ),
    ).toBe(false);
    expect(toolDeveloperDetailsEqual(base, undefined)).toBe(false);
    expect(toolDeveloperDetailsEqual(undefined, base)).toBe(false);
    expect(toolDeveloperDetailsEqual(undefined, undefined)).toBe(true);
    expect(
      toolDeveloperDetailsEqual(
        makeDeveloperDetails({ transport: undefined }),
        makeDeveloperDetails({ transport: { session_id: "s1" } }),
      ),
    ).toBe(false);
    expect(
      stringRecordsEqual({ a: "1" }, { a: "1", b: "2" }),
    ).toBe(false);
  });

  test("each Activity child field mutation and order change prevents reuse", () => {
    const base = makeChildren();
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, id: "other" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, title: "changed" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, tone: "failed" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, providerToolId: "other-tool" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(activityChildrenEqual(base, [base[1]!, base[0]!])).toBe(false);
    expect(activityChildrenEqual(base, undefined)).toBe(false);
    expect(activityChildrenEqual(undefined, base)).toBe(false);
    expect(activityChildrenEqual(undefined, undefined)).toBe(true);
    expect(
      activityChildrenEqual(
        [{ id: "a", title: "t" }],
        [{ id: "a", title: "t", tone: "running" }],
      ),
    ).toBe(false);
    expect(
      activityChildrenEqual(
        [{ id: "a", title: "t", providerToolId: "p" }],
        [{ id: "a", title: "t" }],
      ),
    ).toBe(false);
  });

  test("each Brain Work event field mutation prevents reuse", () => {
    const base = makeBrainEvent();
    const fields: Array<Partial<BrainWorkResultEvent>> = [
      { event_id: "other" },
      { kind: "session.failed" },
      { work_id: "other-work" },
      { work_title: "Other title" },
      { summary: "Other summary" },
      { session_id: "other-sess" },
      { session_name: "other-name" },
      { occurred_at: "2026-08-06T13:00:00.000Z" },
      { unread: false },
      { review_state: "reviewing" },
      { session_state: "closing" },
      { current_result: false },
      { phase: "verifying" },
      { attention: "user_input" },
      { event_kind: "verification" },
      { details_json: '{"ci_run":3}' },
      { actions: [{ kind: "pull_request", url: "https://github.com/o/r/pull/1" }] },
      { wait_for: "Preview archive" },
    ];
    for (const override of fields) {
      expect(
        brainWorkResultEventsEqual(base, makeBrainEvent(override)),
      ).toBe(false);
    }
    expect(
      brainWorkResultEventsEqual(
        makeBrainEvent({ session_id: undefined }),
        makeBrainEvent({ session_id: "present" }),
      ),
    ).toBe(false);
    expect(
      brainWorkResultEventsEqual(
        makeBrainEvent({ session_name: undefined }),
        makeBrainEvent({ session_name: "present" }),
      ),
    ).toBe(false);
  });

  test("a Brain Work card is the same with a new press closure (each projection makes one), not with a press it lacked or another destination", () => {
    const event = makeBrainEvent();
    const onPressA = () => {};
    const onPressB = () => {};
    const left = makeBrainItem({ event, onPress: onPressA, canOpenSession: true });
    const sameCallback = makeBrainItem({ event: { ...event }, onPress: onPressA, canOpenSession: true });
    const differentCallback = makeBrainItem({
      event: { ...event },
      onPress: onPressB,
      canOpenSession: true,
    });
    expect(timelineItemsSemanticEqual(left, sameCallback)).toBe(true);
    expect(timelineItemsSemanticEqual(left, differentCallback)).toBe(true);
    expect(timelineItemsSemanticEqual(left, makeBrainItem({ event, onPress: onPressB, canOpenSession: false }))).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeBrainItem({ event, onPress: undefined }),
        makeBrainItem({ event, onPress: onPressA }),
      ),
    ).toBe(false);
  });

  test("Brain Work grouped revisions and source counts invalidate only that stable row", () => {
    const event = makeBrainEvent();
    const onPress = () => {};
    const base = makeBrainItem({ event, events: [event], onPress });
    const reviewing = makeBrainEvent({
      event_id: "evt-reviewing",
      review_state: "reviewing",
    });

    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({
          event,
          events: [event, reviewing],
          onPress,
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({ event, events: [event], sourceCount: 2, onPress }),
      ),
    ).toBe(false);
  });

  test("Brain Work current projection status invalidates its stable row", () => {
    const event = makeBrainEvent();
    const onPress = () => {};
    const currentWork = {
      work_id: event.work_id,
      revision: 1,
      title: "Current Work",
      status: "waiting" as const,
      unread_result: false,
    };
    const base = makeBrainItem({
      event,
      events: [event],
      currentWork,
      onPress,
    });

    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({
          event,
          events: [event],
          currentWork: { ...currentWork, status: "done" },
          onPress,
        }),
      ),
    ).toBe(false);
  });

  test("each DisplayAttachment field mutation independently prevents reuse", () => {
    const base = makeAttachment();
    expect(attachmentsEqual([base], [makeAttachment()])).toBe(true);
    expect(
      attachmentsEqual([base], [makeAttachment({ name: "other.png" })]),
    ).toBe(false);
    expect(
      attachmentsEqual([base], [makeAttachment({ path: "/other/path.png" })]),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [base],
        [makeAttachment({ localUri: "file:///other/photo.png" })],
      ),
    ).toBe(false);
    expect(
      attachmentsEqual([base], [makeAttachment({ mimeType: "image/jpeg" })]),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [makeAttachment({ localUri: undefined })],
        [makeAttachment({ localUri: "file:///present.png" })],
      ),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [makeAttachment({ mimeType: undefined })],
        [makeAttachment({ mimeType: "image/png" })],
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeMessage({ attachments: [base] }),
        makeMessage({
          attachments: [makeAttachment({ localUri: "file:///stale.png" })],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeMessage({ attachments: [base] }),
        makeMessage({
          attachments: [makeAttachment({ mimeType: "application/pdf" })],
        }),
      ),
    ).toBe(false);
  });

  test("plan steps use indexed equality without Array.every", () => {
    const left = makePlan();
    const right = makePlan({
      steps: [
        makePlanStep(),
        makePlanStep({ step: "Verify", status: "pending" }),
      ],
    });
    expect(timelineItemsSemanticEqual(left, right)).toBe(true);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ step: "changed" }),
            makePlanStep({ step: "Verify", status: "pending" }),
          ],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ status: "completed" }),
            makePlanStep({ step: "Verify", status: "pending" }),
          ],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ step: "Verify", status: "pending" }),
            makePlanStep(),
          ],
        }),
      ),
    ).toBe(false);
  });
});
