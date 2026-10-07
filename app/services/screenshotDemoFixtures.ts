import type { CodexConversationEvent } from "./codexConversation";
import type { Worker } from "../store/workers";
import type { PendingUserMessage } from "../components/terminal/InterfaceChatSession";
import type { ProvidersSnapshot } from "./providers/types";

export const SCREENSHOT_DEMO_SERVER_ID = "demo-server";
export const SCREENSHOT_DEMO_SERVER_NAME = "Studio Mac";
export const SCREENSHOT_DEMO_SERVER_URL = "https://demo.invalid";

export const SCREENSHOT_CHAT_PENDING_FIXTURES = [
  "none",
  "pending",
  "failed",
  "long",
] as const;

export type ScreenshotChatPendingFixture =
  (typeof SCREENSHOT_CHAT_PENDING_FIXTURES)[number];

const DEMO_TIMESTAMP = "2026-06-18T09:30:00.000Z";
const DEMO_PENDING_CREATED_AT = "2026-06-18T09:30:12.000Z";

/** Deterministic Pending rows for screenshot-demo; never used by live chat. */
export function screenshotChatPendingUserMessages(
  fixture: ScreenshotChatPendingFixture,
): PendingUserMessage[] {
  if (fixture === "none") {
    return [];
  }
  if (fixture === "failed") {
    return [
      {
        id: "demo-pending-failed",
        body: "Retry this short send after the transport failure.",
        sentText: "Retry this short send after the transport failure.",
        attachments: [],
        createdAt: DEMO_PENDING_CREATED_AT,
        lifecycle: "failed",
        dispatchRequestId: "demo-request-failed",
        dispatchAttemptOrder: 1,
        failureCode: "send_input_failed",
        failureMessage: "Provider unavailable",
        createdAfterMaxSeq: 3,
        createdAfterEventIds: ["chat-assistant"],
      },
    ];
  }
  if (fixture === "long") {
    return [
      {
        id: "demo-pending-long",
        body: [
          "Sending a longer pending bubble so wrapping, grouped spacing,",
          "and the external clock mark stay stable across light and dark.",
          "The bubble geometry must not change when acknowledgement arrives.",
        ].join(" "),
        sentText: [
          "Sending a longer pending bubble so wrapping, grouped spacing,",
          "and the external clock mark stay stable across light and dark.",
          "The bubble geometry must not change when acknowledgement arrives.",
        ].join(" "),
        attachments: [],
        createdAt: DEMO_PENDING_CREATED_AT,
        lifecycle: "pending",
        dispatchRequestId: "demo-request-long",
        dispatchAttemptOrder: 1,
        createdAfterMaxSeq: 3,
        createdAfterEventIds: ["chat-assistant"],
      },
    ];
  }
  return [
    {
      id: "demo-pending-short",
      body: "On my way.",
      sentText: "On my way.",
      attachments: [],
      createdAt: DEMO_PENDING_CREATED_AT,
      lifecycle: "pending",
      dispatchRequestId: "demo-request-short",
      dispatchAttemptOrder: 1,
      createdAfterMaxSeq: 3,
      createdAfterEventIds: ["chat-assistant"],
    },
  ];
}

export const SCREENSHOT_CHAT_EVENTS: CodexConversationEvent[] = [
  {
    id: "chat-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Polish the mobile handoff and verify the smallest layout.",
  },
  {
    id: "chat-plan",
    seq: 2,
    kind: "plan",
    timestamp: DEMO_TIMESTAMP,
    explanation:
      "I’ll keep the agent running here while you continue from your phone.",
    plan: [
      { step: "Tighten the handoff layout", status: "completed" },
      { step: "Run focused UI checks", status: "completed" },
      { step: "Review the 320 px viewport", status: "in_progress" },
    ],
  },
  {
    id: "chat-assistant",
    seq: 3,
    kind: "assistant_message",
    timestamp: DEMO_TIMESTAMP,
    body: "The handoff is ready. The agent is still running on your computer, and the focused checks pass. Open **Terminal** anytime for the live process.",
  },
];

/**
 * Collapsed Tool activity headers for screenshot/demo and geometry coverage:
 * Run + short command, Search + query, Run + long ellipsized path.
 */
export const SCREENSHOT_ACTIVITY_HEADER_EVENTS: CodexConversationEvent[] = [
  {
    id: "activity-run-short",
    seq: 10,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "sleep 45",
    output: "",
    status: "completed",
    exit_code: 0,
  },
  {
    id: "activity-search",
    seq: 11,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Grep",
    input: '{"pattern":"daemonSocketPath","path":"daemon"}',
    status: "completed",
  },
  {
    id: "activity-run-long",
    seq: 12,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "/home/daoleno/workspace/zen/daemon/brain/timeline_test.go",
    output: "",
    status: "completed",
    exit_code: 0,
  },
];

export const SCREENSHOT_BRAIN_EVENTS: CodexConversationEvent[] = [
  {
    id: "brain-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Prepare the next release while I’m away from my desk.",
  },
  {
    id: "brain-plan",
    seq: 2,
    kind: "plan",
    timestamp: DEMO_TIMESTAMP,
    explanation:
      "Brain is carrying the release context and coordinating focused work.",
    plan: [
      { step: "Audit the onboarding flow", status: "completed" },
      { step: "Delegate mobile regression checks", status: "in_progress" },
      { step: "Summarize release readiness", status: "pending" },
    ],
  },
  {
    id: "brain-tool",
    seq: 3,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "worker progress",
    title: "Mobile QA agent",
    input: "Review compact layouts in the sample workspace",
    output: "Running accessibility and viewport checks",
    status: "running",
  },
  ...SCREENSHOT_ACTIVITY_HEADER_EVENTS.map((event, index) => ({
    ...event,
    id: `brain-${event.id}`,
    seq: 4 + index,
  })),
  {
    id: "brain-calendar-result",
    seq: 7,
    kind: "status",
    timestamp: "2026-06-18T09:31:00.000Z",
    title: "Daily Hacker News failed",
    body: "Linked Work is no longer observable after restart.",
    status: "failed",
    source: "calendar_result",
  },
  {
    id: "brain-assistant",
    seq: 8,
    kind: "assistant_message",
    timestamp: "2026-06-18T09:32:00.000Z",
    body: "I’ll keep the workspace and delegated results together here. You can leave this chat and return without rebuilding the context.",
  },
  {
    id: "brain-work-lifecycle-only",
    seq: 9,
    kind: "status",
    timestamp: "2026-06-18T09:33:00.000Z",
    title: "data-platform-dashboard-production-release",
    body: "Delegated provider reported done; awaiting exact control completion",
    status: "session.done",
    source: "work_result",
    work_id: "demo-lifecycle-only",
    work_session_id: "demo-release-session",
    unread: false,
    work_review_state: "resolved",
    work_session_state: "finalized",
    work_result_current: true,
    work_phase: "reporting",
    work_attention: "done",
    work_event_kind: "done",
    work_next_action: "Review the delegated Session result.",
    work_wait_for: "Session control completion",
  },
  {
    id: "brain-work-context-rich",
    seq: 10,
    kind: "status",
    timestamp: "2026-06-18T09:34:00.000Z",
    title: "zen-next-beta-release",
    body: "Publishing image",
    status: "session.done",
    source: "work_result",
    work_id: "demo-context-rich",
    work_session_id: "demo-build-session",
    unread: true,
    work_review_state: "queued",
    work_session_state: "open",
    work_result_current: true,
    work_phase: "working",
    work_attention: "none",
    work_event_kind: "artifact",
    work_details_json: '{"ci_run":32645890201,"digest":"sha256:91aa"}',
    work_next_action: "Promote the verified build",
    work_wait_for: "Preview iOS archive",
  },
];

function demoWork(
  id: string,
  seq: number,
  minute: number,
  title: string,
  body: string,
  status: string,
  extra: Partial<CodexConversationEvent> = {},
): CodexConversationEvent {
  return {
    id: `desk-${id}`,
    seq,
    kind: "status",
    timestamp: `2026-10-07T09:${String(minute).padStart(2, "0")}:00.000Z`,
    title,
    body,
    status,
    source: "work_result",
    work_id: `desk-${id}`,
    work_session_id: `desk-${id}-session`,
    unread: false,
    work_review_state: "queued",
    work_session_state: "open",
    work_result_current: true,
    work_phase: "working",
    work_attention: "none",
    work_event_kind: "progress",
    ...extra,
  };
}

const DESK_ASK: CodexConversationEvent = {
  id: "desk-user",
  seq: 1,
  kind: "user_message",
  timestamp: "2026-10-07T09:00:00.000Z",
  body: "Ship atlas-notes v1.4 this week: fix the sync bug, tidy the settings copy, write release notes, post them to Notion.",
};

const DESK_REPLY: CodexConversationEvent = {
  id: "desk-reply",
  seq: 2,
  kind: "assistant_message",
  timestamp: "2026-10-07T09:01:00.000Z",
  body: "On it. Five parts, each tracked as Work. I’ll only call you when it matters.",
};

const DESK_NEEDS = demoWork("sync", 20, 22, "Sync fix needs your call", "Keep both copies, or the newest edit?", "session.needs_input", {
  session_name: "Codex · atlas-notes",
  work_attention: "user_input",
  work_event_kind: "question",
  unread: true,
});

const DESK_READY = demoWork("wording", 10, 12, "Conflict wording", "Five apps compared. “Conflicted copy” wins 3 of 5.", "session.done", {
  session_name: "Grok · research",
  work_event_kind: "artifact",
  unread: true,
});

/** The atlas-notes goal from the landing: one slip for each Work state. */
export const SCREENSHOT_BRAIN_WORK_DESK: CodexConversationEvent[] = [
  DESK_ASK,
  DESK_REPLY,
  DESK_READY,
  demoWork("tests", 11, 16, "Sync tests", "41 pass, but 2 were flaky and needed a retry.", "session.uncertain", {
    session_name: "Codex · atlas-notes",
    work_event_kind: "verification",
  }),
  demoWork("notion", 12, 20, "Post notes to Notion", "Notion said 401: the token expired. Nothing was posted.", "session.failed", {
    session_name: "Pi · release-notes",
    work_event_kind: "failed",
  }),
  demoWork("notes", 13, 21, "Draft the release notes", "Waiting on the sync fix.", "session.stale", {
    session_name: "Claude Code · release-notes",
    work_attention: "blocked",
    work_event_kind: "blocked",
  }),
  demoWork("copy", 14, 21, "Tidy the settings copy", "Rewriting strings · 7 of 12", "session.stale", {
    session_name: "Claude Code · settings",
  }),
  DESK_NEEDS,
];

/** Brain waiting on you: the result that came back, then the question. */
export const SCREENSHOT_BRAIN_NEEDS: CodexConversationEvent[] = [
  DESK_ASK,
  DESK_REPLY,
  DESK_READY,
  {
    id: "desk-reply-2",
    seq: 15,
    kind: "assistant_message",
    timestamp: "2026-10-07T09:13:00.000Z",
    body: "Conflict wording came back first. “Conflicted copy” is what 3 of 5 apps say, so I’ve handed that to the sync fix.",
  },
  DESK_NEEDS,
];

export const SCREENSHOT_SESSION_AGENTS: Worker[] = [
  {
    key: "demo-server:atlas-mobile",
    id: "atlas-mobile",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Mobile handoff",
    project: "atlas-notes",
    cwd: "/Users/demo/Projects/atlas-notes",
    command: "codex",
    summary: "Reviewing the compact onboarding layout",
    status: "running",
    last_output_lines: ["Reviewing the compact onboarding layout"],
    updated_at: Date.parse(DEMO_TIMESTAMP),
  },
  {
    key: "demo-server:release-brain",
    id: "release-brain",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Release coordinator",
    project: "sample-app",
    cwd: "/Users/demo/Projects/sample-app",
    command: "codex",
    summary: "Delegated mobile regression checks",
    status: "running",
    delegated: true,
    last_output_lines: ["Delegated mobile regression checks"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 60_000,
  },
  {
    key: "demo-server:api-review",
    id: "api-review",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "API review",
    project: "weather-kit",
    cwd: "/Users/demo/Projects/weather-kit",
    command: "claude",
    summary: "Waiting for a product decision",
    status: "blocked",
    last_output_lines: ["Waiting for a product decision"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 18 * 60_000,
  },
  {
    key: "demo-server:docs-pass",
    id: "docs-pass",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Docs refresh",
    project: "garden-journal",
    cwd: "/Users/demo/Projects/garden-journal",
    command: "cursor-agent",
    summary: "README links and examples verified",
    status: "done",
    last_output_lines: ["README links and examples verified"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 45 * 60_000,
  },
];

export const SCREENSHOT_STATS_FIXTURE = {
  ranges: {
    all: {
      cost: 19.39,
      costKnown: true,
      totalTokens: 346_660_000,
      totalTokensKnown: true,
      inputTokens: 4_853_000,
      outputTokens: 1_168_000,
      reasoningTokens: 827_000,
      cacheRead: 339_782_000,
      cacheCreate: 30_000,
      tokenBreakdownKnown: true,
      sessions: 2559,
      models: [
        {
          // Synthetic layout-coverage rows; every value is fictional.
          name: "codex-large",
          totalTokens: 1_640_000,
          totalTokensKnown: true,
          inputTokens: 1_100_000,
          outputTokens: 320_000,
          reasoningTokens: 90_000,
          cacheRead: 120_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 11.62,
          costKnown: true,
          sessions: 26,
        },
        {
          name: "claude-sonnet",
          totalTokens: 840_000,
          totalTokensKnown: true,
          inputTokens: 520_000,
          outputTokens: 190_000,
          reasoningTokens: 60_000,
          cacheRead: 60_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 6.8,
          costKnown: true,
          sessions: 16,
        },
        {
          // Synthetic OpenCode model row: model names kept for layout
          // coverage only; the numbers are representative, not account data.
          name: "deepseek-v4-flash",
          totalTokens: 340_000_000,
          totalTokensKnown: true,
          inputTokens: 3_000_000,
          outputTokens: 640_000,
          reasoningTokens: 670_000,
          cacheRead: 335_680_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 0.97,
          costKnown: true,
          sessions: 2400,
        },
        {
          name: "kimi-k2.5-free",
          totalTokens: 4_180_000,
          totalTokensKnown: true,
          inputTokens: 233_000,
          outputTokens: 18_000,
          reasoningTokens: 7_000,
          cacheRead: 3_922_000,
          cacheCreate: 0,
          tokenBreakdownKnown: true,
          cost: 0,
          costKnown: true,
          sessions: 117,
        },
      ],
      projects: [
        { name: "atlas-notes", totalTokens: 980_000, sessions: 17, cost: 7.24 },
        { name: "weather-kit", totalTokens: 760_000, sessions: 13, cost: 5.83 },
        {
          name: "garden-journal",
          totalTokens: 740_000,
          sessions: 12,
          cost: 5.35,
        },
      ],
      skills: [
        { name: "mobile-qa", calls: 18, projects: ["atlas-notes"] },
        { name: "release-review", calls: 11, projects: ["weather-kit"] },
      ],
      tools: [
        { name: "exec", calls: 96 },
        { name: "apply_patch", calls: 38 },
      ],
      days: [
        { date: "2026-06-14", totalTokens: 320_000, sessions: 6, cost: 2.1 },
        { date: "2026-06-15", totalTokens: 510_000, sessions: 9, cost: 3.8 },
        { date: "2026-06-16", totalTokens: 420_000, sessions: 8, cost: 3.2 },
        { date: "2026-06-17", totalTokens: 610_000, sessions: 11, cost: 4.9 },
        { date: "2026-06-18", totalTokens: 620_000, sessions: 8, cost: 4.42 },
      ],
    },
  },
  codexSubscriptions: [
    {
      authKind: "official",
      state: "available",
      plan: "plus",
      windows: [
        { name: "primary", usedPercent: 34, windowMinutes: 300 },
        { name: "secondary", usedPercent: 18, windowMinutes: 10080 },
      ],
      serverLabel: SCREENSHOT_DEMO_SERVER_NAME,
    },
  ],
} as const;

export const SCREENSHOT_PROVIDERS_FIXTURE: ProvidersSnapshot = {
  revision: 41,
  connections: [
    {
      id: "conn-openai",
      name: "OpenAI",
      preset_id: "openai",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
    },
    {
      id: "conn-deepseek",
      name: "DeepSeek",
      preset_id: "deepseek",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
    },
    {
      id: "conn-anthropic",
      name: "Anthropic",
      preset_id: "anthropic",
      clients: ["claude"],
      credential_ready: false,
      advanced: false,
    },
    {
      id: "conn-gateway",
      name: "Studio Gateway",
      preset_id: "custom",
      clients: ["claude"],
      credential_ready: true,
      advanced: true,
      base_url: "https://gateway.studio.example/v1",
    },
  ],
  defaults: {
    codex: { connection_id: "conn-deepseek" },
    claude: { connection_id: "conn-gateway" },
  },
  presets: [
    {
      id: "openai",
      label: "OpenAI",
      clients: ["codex"],
      advanced: false,
    },
    {
      id: "openrouter",
      label: "OpenRouter",
      clients: ["codex"],
      advanced: false,
    },
    {
      id: "anthropic",
      label: "Anthropic",
      clients: ["claude"],
      advanced: false,
    },
    {
      id: "deepseek",
      label: "DeepSeek",
      clients: ["codex", "claude"],
      advanced: false,
    },
    {
      id: "custom",
      label: "Custom Gateway",
      clients: ["codex", "claude"],
      advanced: true,
    },
  ],
  models: {
    "conn-openai": [{ id: "gpt-5", available: true, source: "discovered" }],
    "conn-deepseek": [
      { id: "deepseek-v4-flash", available: true, source: "discovered" },
      { id: "deepseek-v4-pro", available: true, source: "discovered" },
    ],
    "conn-anthropic": [],
    "conn-gateway": [
      { id: "claude-sonnet-4-6", available: true, source: "discovered" },
      { id: "claude-opus-4-1", available: true, source: "discovered" },
    ],
  },
};

/**
 * Empty Providers surface: no connections, no models, curated presets still
 * offered by the daemon so the direct-connect rows remain visible.
 */
export const SCREENSHOT_PROVIDERS_EMPTY_FIXTURE: ProvidersSnapshot = {
  revision: 1,
  connections: [],
  defaults: {},
  presets: SCREENSHOT_PROVIDERS_FIXTURE.presets,
  models: {},
};

const DEMO_GITHUB_TOOLS = [
  { name: "search_issues", description: "Search issues and pull requests", allowed: true, group: "read" as const },
  { name: "get_pull_request", description: "Read a pull request", allowed: true, group: "read" as const },
  { name: "create_issue", description: "Open an issue", allowed: false, group: "write" as const },
  { name: "add_comment", description: "Comment on an issue", allowed: false, group: "write" as const },
];

/** Plugins on the demo server: two connected services, one needing a reconnect. */
export const SCREENSHOT_PLUGINS = {
  github: { id: "github", name: "GitHub", available: true, setup_url: "", description: "Repositories, issues and pull requests." },
  notion: { id: "notion", name: "Notion", available: true, setup_url: "", description: "Pages and databases in your workspace." },
  linear: { id: "linear", name: "Linear", available: true, setup_url: "", description: "Issues, projects and team activity." },
  google: { id: "google", name: "Google", available: true, setup_url: "", description: "Gmail, Drive and Calendar." },
  slack: { id: "slack", name: "Slack", available: false, unavailable_reason: "Sign-in is not set up on this server yet.", setup_url: "", description: "Channels and messages." },
  mcp: { id: "mcp", name: "MCP server", available: true, setup_url: "", description: "Any Model Context Protocol server." },
  openapi: { id: "openapi", name: "OpenAPI", available: true, setup_url: "", description: "Any HTTP API with an OpenAPI spec." },
};

export const SCREENSHOT_PLUGIN_ACCOUNTS = {
  github: {
    id: "acct-github", integration: "github", name: "daoleno", identity: "daoleno", enabled: true,
    status: "connected" as const, tools: DEMO_GITHUB_TOOLS, history: [],
  },
  notion: {
    id: "acct-notion", integration: "notion", name: "Atlas workspace", identity: "atlas", enabled: true,
    status: "authorization_required" as const, tools: [], history: [],
  },
  linear: {
    id: "acct-linear", integration: "linear", name: "Atlas team", identity: "atlas", enabled: true,
    status: "connected" as const, tools: [], history: [],
  },
};
