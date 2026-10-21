import { describe, expect, test } from "bun:test";
import { normalizeWorkerSessionCapabilities } from "../../services/providers/sessionCapabilities";
import {
  RESEND_HOLD_MS,
  isRerunnablePrompt,
  createResendGuard,
  editIsSendable,
  messageActionKinds,
  messageCopyText,
  messageEditMode,
  messageMenuItems,
  type MessageActionEnvironment,
  type MessageActionSubject,
} from "./messageActionsModel";

const user: MessageActionSubject = {
  id: "u1",
  role: "user",
  body: "ship it",
  hasAttachments: false,
  pending: false,
  streaming: false,
};
const assistant: MessageActionSubject = { ...user, id: "a1", role: "assistant", body: "**done**" };
const env: MessageActionEnvironment = {
  messageRewind: true,
  brain: false,
  running: false,
  connected: true,
  canSelectText: true,
};

describe("message menu", () => {
  test("a user message offers copy, select text and edit", () => {
    expect(messageActionKinds(user, env)).toEqual(["copy", "select", "edit"]);
  });

  test("an assistant message offers only copy and select", () => {
    expect(messageActionKinds(assistant, env)).toEqual(["copy", "select"]);
  });

  test("desktop has native selection, so no select entry", () => {
    expect(messageActionKinds(user, { ...env, canSelectText: false })).toEqual(["copy", "edit"]);
  });

  test("edit waits for the agent to stop and for a connection", () => {
    expect(messageActionKinds(user, { ...env, running: true })).toEqual(["copy", "select"]);
    expect(messageActionKinds(user, { ...env, connected: false })).toEqual(["copy", "select"]);
  });

  test("a message still being sent, streaming or empty has nothing to act on", () => {
    expect(messageActionKinds({ ...user, pending: true }, env)).toEqual(["copy", "select"]);
    expect(messageActionKinds({ ...assistant, streaming: true }, env)).toEqual([]);
    expect(messageActionKinds({ ...user, body: "  " }, env)).toEqual([]);
  });

  test("a message with attachments cannot be faithfully edited", () => {
    expect(messageActionKinds({ ...user, hasAttachments: true }, env)).toEqual(["copy", "select"]);
  });

  test("copy keeps Markdown and drops only trailing whitespace", () => {
    expect(messageCopyText({ body: "```ts\nlet a = 1;\n```\n\n- one\n" })).toBe("```ts\nlet a = 1;\n```\n\n- one");
  });
});

describe("message sheet rows", () => {
  test("list Copy, Select Text and Edit in that order, each running its own kind", () => {
    const ran: string[] = [];
    const items = messageMenuItems(["copy", "select", "edit"], (kind) => ran.push(kind));
    expect(items.map((item) => item.label)).toEqual(["Copy", "Select Text", "Edit"]);
    items.forEach((item) => item.onPress());
    expect(ran).toEqual(["copy", "select", "edit"]);
  });
});

describe("edit mode per executor", () => {
  test("rewinds only where the daemon says the CLI can, and never in Brain", () => {
    expect(messageEditMode({ messageRewind: true, brain: false })).toBe("rewind");
    expect(messageEditMode({ messageRewind: false, brain: false })).toBe("copy");
    expect(messageEditMode({ messageRewind: true, brain: true })).toBe("copy");
  });

  test("an edit is sent whenever it says something; unchanged it is a retry", () => {
    const edit = { id: "u1", original: "ship it", mode: "rewind" as const };
    expect(editIsSendable(edit, "ship it")).toBe(true);
    expect(editIsSendable(edit, "ship it today")).toBe(true);
    expect(editIsSendable(edit, "  ")).toBe(false);
    expect(editIsSendable(edit, "")).toBe(false);
  });

  test("Retry runs a sent text prompt of an idle, connected Session again", () => {
    expect(isRerunnablePrompt(user, env)).toBe(true);
    expect(isRerunnablePrompt(user, { ...env, running: true })).toBe(false);
    expect(isRerunnablePrompt(user, { ...env, connected: false })).toBe(false);
    expect(isRerunnablePrompt({ ...user, hasAttachments: true }, env)).toBe(false);
    expect(isRerunnablePrompt({ ...user, pending: true }, env)).toBe(false);
    expect(isRerunnablePrompt(assistant, env)).toBe(false);
  });
});

describe("retry guard", () => {
  test("a second tap while the first is held does nothing", () => {
    let now = 1000;
    const guard = createResendGuard(() => now);
    expect(guard.begin("u1")).toBe(true);
    expect(guard.begin("u1")).toBe(false);
    now += RESEND_HOLD_MS - 1;
    expect(guard.begin("u1")).toBe(false);
  });

  test("other messages are independent and the hold expires", () => {
    let now = 1000;
    const guard = createResendGuard(() => now);
    expect(guard.begin("u1")).toBe(true);
    expect(guard.begin("u2")).toBe(true);
    now += RESEND_HOLD_MS;
    expect(guard.begin("u1")).toBe(true);
  });

  test("a settled send releases the hold early", () => {
    const guard = createResendGuard(() => 5);
    guard.begin("u1");
    guard.release("u1");
    expect(guard.begin("u1")).toBe(true);
  });
});

describe("what Edit and Retry do per executor", () => {
  // The daemon advertises message_rewind per Session; the app never guesses it
  // from the executor's name.
  const wire = (messageRewind?: boolean) =>
    normalizeWorkerSessionCapabilities({
      structured_events: true,
      model_profile_managed: false,
      model_profile_active_switch: false,
      ...(messageRewind ? { message_rewind: true } : {}),
    });
  const table: Array<{ executor: string; rewind: boolean; brain: boolean; edit: "rewind" | "copy" }> = [
    { executor: "Claude Code", rewind: true, brain: false, edit: "rewind" },
    { executor: "Codex", rewind: true, brain: false, edit: "rewind" },
    { executor: "pi", rewind: true, brain: false, edit: "rewind" },
    { executor: "DSH", rewind: false, brain: false, edit: "copy" },
    { executor: "Cursor", rewind: false, brain: false, edit: "copy" },
    { executor: "OpenCode", rewind: false, brain: false, edit: "copy" },
    { executor: "Brain on Claude Code", rewind: true, brain: true, edit: "copy" },
    { executor: "Brain on Codex", rewind: true, brain: true, edit: "copy" },
  ];

  for (const row of table) {
    test(`${row.executor}: edit ${row.edit === "rewind" ? "rewinds the conversation" : "sends an edited copy as a new message"}; retry follows the same mode`, () => {
      const messageRewind = wire(row.rewind)?.message_rewind === true;
      const environment = { ...env, messageRewind, brain: row.brain };
      expect(messageEditMode(environment)).toBe(row.edit);
      expect(messageActionKinds(user, environment)).toContain("edit");
      expect(isRerunnablePrompt(user, environment)).toBe(true);
    });
  }

  test("an older daemon that does not advertise rewind never gets a rewind request", () => {
    expect(normalizeWorkerSessionCapabilities({ structured_events: true })?.message_rewind).toBeUndefined();
    expect(messageEditMode({ messageRewind: false, brain: false })).toBe("copy");
  });
});
