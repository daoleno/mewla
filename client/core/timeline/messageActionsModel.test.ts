import { describe, expect, test } from "bun:test";
import { normalizeWorkerSessionCapabilities } from "./sessionCapabilities";
import {
  isEditablePrompt,
  editIsSendable,
  messageEditMode,
  type MessageActionEnvironment,
  type MessageActionSubject,
} from "./messageActionsModel";

const user: MessageActionSubject = { role: "user", body: "ship it", hasAttachments: false, pending: false };
const assistant: MessageActionSubject = { ...user, role: "assistant", body: "**done**" };
const env: MessageActionEnvironment = { running: false, connected: true };

describe("a tap on a sent message edits it, where it can be", () => {
  test("the person's own sent text, in a connected, idle chat", () => {
    expect(isEditablePrompt(user, env)).toBe(true);
    expect(isEditablePrompt(assistant, env)).toBe(false);
  });

  test("not while the agent works or the computer is away", () => {
    expect(isEditablePrompt(user, { ...env, running: true })).toBe(false);
    expect(isEditablePrompt(user, { ...env, connected: false })).toBe(false);
  });

  test("not a message still being sent, an empty one, or one with attachments (it cannot be faithfully edited)", () => {
    expect(isEditablePrompt({ ...user, pending: true }, env)).toBe(false);
    expect(isEditablePrompt({ ...user, body: "  " }, env)).toBe(false);
    expect(isEditablePrompt({ ...user, hasAttachments: true }, env)).toBe(false);
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
      expect(isEditablePrompt(user, environment)).toBe(true);
    });
  }

  test("an older daemon that does not advertise rewind never gets a rewind request", () => {
    expect(normalizeWorkerSessionCapabilities({ structured_events: true })?.message_rewind).toBeUndefined();
    expect(messageEditMode({ messageRewind: false, brain: false })).toBe("copy");
  });
});
