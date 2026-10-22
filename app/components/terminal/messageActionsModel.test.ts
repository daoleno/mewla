import { describe, expect, test } from "bun:test";
import { normalizeWorkerSessionCapabilities } from "../../services/providers/sessionCapabilities";
import {
  isEditablePrompt,
  editIsSendable,
  messageActionKinds,
  messageCopyText,
  messageEditMode,
  messageMenuEntries,
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

describe("message menu rows", () => {
  test("list Copy, Select Text and Edit in that order", () => {
    const entries = messageMenuEntries(["copy", "select", "edit"]);
    expect(entries.map((entry) => entry.label)).toEqual(["Copy", "Select Text", "Edit"]);
    expect(entries.map((entry) => entry.kind)).toEqual(["copy", "select", "edit"]);
  });
});

describe("edit mode per executor", () => {
  test("rewinds only where the daemon says the CLI can, and never in Brain", () => {
    expect(messageEditMode({ messageRewind: true, brain: false })).toBe("rewind");
    expect(messageEditMode({ messageRewind: false, brain: false })).toBe("copy");
    expect(messageEditMode({ messageRewind: true, brain: true })).toBe("copy");
  });

  test("an edit is sent whenever it says something", () => {
    const edit = { id: "u1", original: "ship it", mode: "rewind" as const };
    expect(editIsSendable(edit, "ship it")).toBe(true);
    expect(editIsSendable(edit, "ship it today")).toBe(true);
    expect(editIsSendable(edit, "  ")).toBe(false);
    expect(editIsSendable(edit, "")).toBe(false);
  });

  test("Edit applies to a sent text prompt of an idle, connected Session", () => {
    expect(isEditablePrompt(user, env)).toBe(true);
    expect(isEditablePrompt(user, { ...env, running: true })).toBe(false);
    expect(isEditablePrompt(user, { ...env, connected: false })).toBe(false);
    expect(isEditablePrompt({ ...user, hasAttachments: true }, env)).toBe(false);
    expect(isEditablePrompt({ ...user, pending: true }, env)).toBe(false);
    expect(isEditablePrompt(assistant, env)).toBe(false);
  });
});

describe("what Edit does per executor", () => {
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
    test(`${row.executor}: edit ${row.edit === "rewind" ? "rewinds the conversation" : "sends an edited copy as a new message"}`, () => {
      const messageRewind = wire(row.rewind)?.message_rewind === true;
      const environment = { ...env, messageRewind, brain: row.brain };
      expect(messageEditMode(environment)).toBe(row.edit);
      expect(messageActionKinds(user, environment)).toContain("edit");
      expect(isEditablePrompt(user, environment)).toBe(true);
    });
  }

  test("an older daemon that does not advertise rewind never gets a rewind request", () => {
    expect(normalizeWorkerSessionCapabilities({ structured_events: true })?.message_rewind).toBeUndefined();
    expect(messageEditMode({ messageRewind: false, brain: false })).toBe("copy");
  });
});
