import { describe, expect, test } from "bun:test";
import {
  buildChatuiSlashCommands,
  filterSlashCommands,
  slashCommandEmptyText,
  slashCommandRequestFromDraft,
  type CodexSlashCommand,
} from "./codexSlashCommands";
import {
  routePickedSlashCommand,
  routeSlashCommand,
  slashCommandActionHandled,
  slashCommandAlert,
} from "./slashCommandRouter";

// The Expo app had no tests for the router hooks; these pin each branch they took.
function daemon(name: string, overrides: Partial<CodexSlashCommand> = {}): CodexSlashCommand {
  return {
    value: `/${name}`,
    name,
    title: "",
    description: "",
    category: "session",
    execution: "terminal-required",
    input: { kind: "none" },
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
    ...overrides,
  };
}

const commands = buildChatuiSlashCommands([
  daemon("new"),
  daemon("clear"),
  daemon("status"),
  daemon("skills"),
  daemon("model", { execution: "native" }),
  daemon("review"),
  daemon("rename"),
  daemon("compact"),
  daemon("mention", { execution: "insert-only" }),
  daemon("permissions", { chat_supported: false }),
  daemon("quit", { execution: "unsupported" }),
  daemon("fork", { input: { kind: "inline-args" } }),
  daemon("fork"),
  daemon("diff-tool"),
]);
const route = (draft: string, hasAttachments = false) =>
  routeSlashCommand({ draft, slashCommands: commands, hasAttachments });
const pick = (name: string, hasAttachments = false) =>
  routePickedSlashCommand({ command: commands.find((c) => c.name === name)!, hasAttachments });

describe("the daemon's command list", () => {
  test("applies ChatUI's overrides, drops hidden commands and keeps the first of a name", () => {
    const names = commands.map((c) => c.name);
    expect(names).not.toContain("compact");
    expect(names.filter((n) => n === "fork")).toHaveLength(1);
    expect(commands.find((c) => c.name === "status")).toMatchObject({ execution: "native", chat_supported: true, title: "Status" });
    expect(commands.find((c) => c.name === "clear")?.chat_supported).toBe(false);
    expect(commands.find((c) => c.name === "rename")?.input).toMatchObject({ kind: "freeform", required: true });
    expect(commands.find((c) => c.name === "diff-tool")?.title).toBe("Diff Tool");
  });

  test("filtering ranks exact, prefix, then contains, and hides what chat cannot run", () => {
    expect(filterSlashCommands(commands, "/re").map((c) => c.name)).toEqual(["review", "rename"]);
    expect(filterSlashCommands(commands, "/").map((c) => c.name)).not.toContain("permissions");
    expect(filterSlashCommands(commands, "re")).toEqual([]);
  });

  test("a draft names a command on its first line; an unknown one is unavailable", () => {
    expect(slashCommandRequestFromDraft("  /review the diff\nmore", commands)).toMatchObject({ known: true, rawText: "/review the diff\nmore" });
    expect(slashCommandRequestFromDraft("/nope", commands)?.command).toMatchObject({ execution: "unsupported", chat_supported: false });
    expect(slashCommandRequestFromDraft("hello /review", commands)).toBeNull();
  });

  test("an unloaded list reads as unavailable, not as no match", () => {
    expect(slashCommandEmptyText(true)).toBe("Codex commands unavailable");
    expect(slashCommandEmptyText(false)).toBe("No matching command");
  });
});

describe("routeSlashCommand (a sent draft)", () => {
  test("plain text and insert-only commands go out as messages", () => {
    expect(route("hello")).toEqual({ kind: "none" });
    expect(route("/mention @file")).toMatchObject({ kind: "insert" });
    expect(slashCommandActionHandled(route("hello"))).toBe(false);
    expect(slashCommandActionHandled(route("/mention x"))).toBe(false);
    expect(slashCommandActionHandled(route("/status"))).toBe(true);
  });

  test("attachments with a non-native command ask first; native commands ignore them", () => {
    expect(route("/review x", true)).toMatchObject({ kind: "alert", alert: "attachments" });
    expect(route("/status", true)).toMatchObject({ kind: "status" });
  });

  test("a command missing its required words is prefilled", () => {
    expect(route("/rename")).toMatchObject({ kind: "prefill", draft: "/rename " });
    expect(route("/rename Sync fix")).toMatchObject({ kind: "send", text: "/rename Sync fix" });
  });

  test("unknown and chat-unsupported commands prefill and say they are unavailable", () => {
    expect(route("/nope now")).toMatchObject({ kind: "prefill", draft: "/nope ", alert: "unavailable" });
    expect(route("/permissions now")).toMatchObject({ kind: "prefill", draft: "/permissions now", alert: "unavailable" });
  });

  test("an unsupported command that chat lists says so", () => {
    const quit = buildChatuiSlashCommands([daemon("quit", { execution: "unsupported" })]);
    expect(routeSlashCommand({ draft: "/quit", slashCommands: quit, hasAttachments: false })).toMatchObject({ kind: "alert", alert: "unsupported" });
  });

  test("status and skills open their sheets; new starts a new chat; other native commands prefill", () => {
    expect(route("/status")).toMatchObject({ kind: "status" });
    expect(route("/skills")).toMatchObject({ kind: "skills" });
    expect(route("/new")).toMatchObject({ kind: "new_chat", text: "/new" });
    expect(route("/model")).toMatchObject({ kind: "prefill", draft: "/model " });
  });

  test("a terminal command is sent as typed", () => {
    expect(route("/review the sync module")).toMatchObject({ kind: "send", text: "/review the sync module" });
  });
});

describe("routePickedSlashCommand (picked from the list)", () => {
  test("with attachments it only prefills", () => {
    expect(pick("status", true)).toMatchObject({ kind: "prefill", draft: "/status " });
  });

  test("unsupported alerts, chat-unsupported prefills with an alert, insert-only prefills", () => {
    expect(pick("quit")).toMatchObject({ kind: "alert", alert: "unsupported" });
    expect(pick("permissions")).toMatchObject({ kind: "prefill", draft: "/permissions ", alert: "unavailable" });
    expect(pick("mention")).toMatchObject({ kind: "prefill", draft: "/mention " });
  });

  test("sheets, a new chat, commands that take words, and the rest", () => {
    expect(pick("status")).toMatchObject({ kind: "status" });
    expect(pick("skills")).toMatchObject({ kind: "skills" });
    expect(pick("new")).toMatchObject({ kind: "new_chat", text: "/new" });
    expect(pick("model")).toMatchObject({ kind: "prefill", draft: "/model " });
    expect(pick("review")).toMatchObject({ kind: "prefill", draft: "/review " });
    expect(pick("fork")).toMatchObject({ kind: "prefill", draft: "/fork " });
    expect(pick("diff-tool")).toMatchObject({ kind: "send", text: "/diff-tool" });
  });
});

describe("slashCommandAlert", () => {
  const review = commands.find((c) => c.name === "review")!;
  test("offers the terminal only when there is one", () => {
    expect(slashCommandAlert("unavailable", review, true).buttons.map((b) => b.action)).toEqual(["cancel", "open_terminal"]);
    expect(slashCommandAlert("unavailable", review, false).buttons.map((b) => b.label)).toEqual(["OK"]);
  });

  test("attachments can still go out as a message", () => {
    expect(slashCommandAlert("attachments", review, false)).toEqual({
      title: "/review cannot use attachments here",
      message: "Run the slash command without attachments, or send this as a normal message.",
      buttons: [
        { label: "Cancel", action: "cancel" },
        { label: "Send as Message", action: "send_as_message" },
      ],
    });
  });
});
