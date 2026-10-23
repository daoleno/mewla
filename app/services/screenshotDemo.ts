import {
  SCREENSHOT_CHAT_PENDING_FIXTURES,
  SCREENSHOT_PROVIDERS_EMPTY_FIXTURE,
  SCREENSHOT_PROVIDERS_FIXTURE,
  type ScreenshotChatPendingFixture,
} from "./screenshotDemoFixtures";
import type { ProvidersEditorState } from "../components/providers/providersPresentationModel";
import type { ProvidersSnapshot } from "./providers/types";

export const SCREENSHOT_DEMO_STATES = [
  "chat",
  "sessions",
  "brain",
  "cat",
  "stats",
  "calendar",
  "profile",
  "reading",
  "providers",
  "plugins",
  "skills",
  "resources",
  "work",
  "composer",
  "onboarding",
  "empty",
  "mermaid",
  "computers",
] as const;

export type ScreenshotDemoState = (typeof SCREENSHOT_DEMO_STATES)[number];

export type { ScreenshotChatPendingFixture };
export { SCREENSHOT_CHAT_PENDING_FIXTURES };

export function resolveScreenshotDemoState(
  value: string | string[] | undefined,
): ScreenshotDemoState {
  const candidate = Array.isArray(value) ? value[0] : value;
  return SCREENSHOT_DEMO_STATES.includes(candidate as ScreenshotDemoState)
    ? (candidate as ScreenshotDemoState)
    : "chat";
}

/** Opt-in chat fixture for inspecting real Pending send chrome without live IO. */
export function resolveScreenshotChatPendingFixture(
  value: string | string[] | undefined,
): ScreenshotChatPendingFixture {
  const candidate = Array.isArray(value) ? value[0] : value;
  if (candidate === "1") {
    return "pending";
  }
  return SCREENSHOT_CHAT_PENDING_FIXTURES.includes(
    candidate as ScreenshotChatPendingFixture,
  )
    ? (candidate as ScreenshotChatPendingFixture)
    : "none";
}

function firstParam(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

/**
 * Providers demo state: `fixture=empty` shows the empty surface, otherwise the
 * connected fixture renders. `editor=create` binds the Add Provider form,
 * `editor=retry` rebinds the Anthropic connection for a failed-save retry,
 * and any other value is treated as a curated preset id. `keyboard=1`
 * autofocuses the API key field to capture the keyboard state.
 */
export function resolveScreenshotProvidersDemo(input: {
  fixture?: string | string[] | undefined;
  editor?: string | string[] | undefined;
  keyboard?: string | string[] | undefined;
}): {
  catalog: ProvidersSnapshot;
  editor: ProvidersEditorState;
  apiKeyAutoFocus: boolean;
} {
  const fixtureParam = firstParam(input.fixture);
  const catalog =
    fixtureParam === "empty"
      ? SCREENSHOT_PROVIDERS_EMPTY_FIXTURE
      : SCREENSHOT_PROVIDERS_FIXTURE;
  return {
    catalog,
    editor: resolveScreenshotProvidersEditor(firstParam(input.editor), catalog),
    apiKeyAutoFocus: firstParam(input.keyboard) === "1",
  };
}

export function resolveScreenshotProvidersEditor(
  value: string | undefined,
  catalog: ProvidersSnapshot,
): ProvidersEditorState {
  if (value === "custom" || value === "create" || value === "codex") {
    return { kind: "create", client: "codex" };
  }
  if (value === "claude") return { kind: "create", client: "claude" };
  if (value === "retry" || value === "needs-key") {
    const connection =
      catalog.connections.find(
        (candidate) => candidate.preset_id === "anthropic",
      ) ?? catalog.connections[0];
    if (!connection) return null;
    return { kind: "edit", connection, retry: value === "retry" };
  }
  return null;
}
