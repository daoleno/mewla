import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

/**
 * Owner 11-17: AWS expiring daily, with nothing to do about it from Chat. A notice that an Extensions account's
 * sign-in ended carries Sign in, which opens Extensions on that account's own sign-in: for AWS Identity Center,
 * a code to approve on any device.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const at = (i: number) => new Date(Date.UTC(2026, 10, 18, 2, 0, i)).toISOString();
const account = { id: "aws-cli-1", integration: "aws", name: "freeride-prod", profile: "freeride-prod", profile_kind: "SSO", profile_status: "expired", identity: "", enabled: true, status: "authorization_required", history: [] };
const flow = { id: "f1", integration: "aws", status: "waiting", user_code: "QRST-WXYZ", authorization_url: "https://us-east-1.signin.aws/platform/authorize?request_uri=x", expires: "2099-01-01T00:00:00Z" };

describe("an Extensions sign-in notice in Chat", () => {
  test("Sign in opens Extensions on that account's own sign-in, a code to approve", async () => {
    const events = [
      { id: "m1", seq: 1, kind: "user_message", timestamp: at(1), body: "Row 1" },
      { id: "plugin-signin-lost:aws-cli-1:1", seq: 2, kind: "status", role: "assistant", timestamp: at(2), title: "AWS sign-in ended", body: "AWS ended the sign-in for “freeride-prod”, so agents can't use it. Tap Sign in and approve the code on any device.", status: "warning", source: "plugin_notice", plugin_account_id: "aws-cli-1" },
    ];
    // Every connections request gets the account list and, for a sign-in, the code it started.
    const replies = { connections: { type: "connections_result", connections: { catalog: [{ id: "aws", name: "AWS", available: true, setup_url: "", description: "Cloud accounts" }], accounts: [account], flow } } };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": events }, replies }) });
    await app.waitFor("id:plugin-notice", { timeout: 10_000 });
    expect(app.texts()).toContain("AWS sign-in ended");
    await app.press("id:plugin-notice-sign-in");
    await app.waitUntil(() => framesOf(app.host.posts(), "connections").some((f) => (f.connection_request as { action?: string })?.action === "connect_start"), { timeout: 10_000 });
    const start = framesOf(app.host.posts(), "connections").map((f) => f.connection_request as { action: string; input?: unknown }).find((r) => r.action === "connect_start");
    expect(start?.input).toEqual({ integration: "aws", account_id: "aws-cli-1" });
    await app.waitFor("id:device-code", { timeout: 10_000 });
    expect(app.texts()).toContain("QRST-WXYZ");
  }, 30_000);
});
