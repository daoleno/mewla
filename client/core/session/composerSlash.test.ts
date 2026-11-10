import { describe, expect, test } from "bun:test";
import { buildChatuiSlashCommands, type CodexSlashCommand } from "./codexSlashCommands";
import { groupSlashCommands, insertSkillMention, routeComposerPick, routeComposerSend, slashMenuState } from "./composerSlash";

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
  daemon("status"),
  daemon("skills", { category: "tools" }),
  daemon("rename"),
  daemon("model", { category: "settings", execution: "native" }),
  daemon("fork", { category: "navigation" }),
  daemon("init", { category: "tools" }),
  daemon("mention", { category: "weird", execution: "insert-only" }),
  daemon("logout", { category: "management", execution: "unsupported" }),
]);
const codex = { agentKind: "codex" as const, slashCommands: commands };

describe("routeComposerSend", () => {
  test("for any agent but Codex the text goes as typed", () => {
    expect(routeComposerSend("/compact", { agentKind: "claude", slashCommands: commands })).toEqual({ kind: "send", text: "/compact" });
    expect(routeComposerSend("/status", { agentKind: "terminal", slashCommands: commands })).toEqual({ kind: "send", text: "/status" });
  });

  test("a plain message and an insert-only command are sent as typed", () => {
    expect(routeComposerSend("hello", codex)).toEqual({ kind: "send", text: "hello" });
    expect(routeComposerSend("/mention @a", codex)).toEqual({ kind: "send", text: "/mention @a" });
  });

  test("/status and /skills open their sheets", () => {
    expect(routeComposerSend("/status", codex)).toEqual({ kind: "status" });
    expect(routeComposerSend("/skills", codex)).toEqual({ kind: "skills" });
  });

  test("/new and a terminal command go to the agent as its input", () => {
    expect(routeComposerSend("/new", codex)).toMatchObject({ kind: "send", text: "/new", command: { name: "new" } });
    expect(routeComposerSend("/rename Release", codex)).toMatchObject({ kind: "send", text: "/rename Release", command: { name: "rename" } });
  });

  test("a command missing its arguments, or another native one, is prefilled", () => {
    expect(routeComposerSend("/rename", codex)).toEqual({ kind: "prefill", text: "/rename " });
    expect(routeComposerSend("/model", codex)).toEqual({ kind: "prefill", text: "/model " });
  });

  test("an unknown or chat-unsupported command is prefilled with the unavailable alert", () => {
    const unknown = routeComposerSend("/nope", { ...codex, canOpenTerminal: true });
    expect(unknown).toMatchObject({ kind: "prefill", text: "/nope ", alert: { title: "/nope is not available" } });
    expect(unknown.kind === "prefill" && unknown.alert?.buttons.map((b) => b.action)).toEqual(["cancel", "open_terminal"]);
    const init = routeComposerSend("/init", codex);
    expect(init).toMatchObject({ kind: "prefill", text: "/init" });
    expect(init.kind === "prefill" && init.alert?.buttons.map((b) => b.action)).toEqual(["cancel"]);
  });

  test("attachments with a non-native command ask whether to send it as a message", () => {
    const route = routeComposerSend("/rename x", { ...codex, hasAttachments: true });
    expect(route).toMatchObject({ kind: "alert", title: "/rename cannot use attachments here" });
    expect(route.kind === "alert" && route.buttons.map((b) => b.action)).toEqual(["cancel", "send_as_message"]);
  });
});

describe("routeComposerPick", () => {
  const pick = (name: string, ctx = {}) => routeComposerPick(commands.find((c) => c.name === name)!, ctx);
  test("a command without arguments is sent alone; one that takes them is prefilled", () => {
    expect(pick("fork")).toMatchObject({ kind: "send", text: "/fork", command: { name: "fork" } });
    expect(pick("rename")).toEqual({ kind: "prefill", text: "/rename " });
    expect(pick("mention")).toEqual({ kind: "prefill", text: "/mention " });
    expect(pick("status")).toEqual({ kind: "status" });
    expect(pick("new")).toMatchObject({ kind: "send", text: "/new" });
  });

  test("with attachments it is only prefilled", () => {
    expect(pick("fork", { hasAttachments: true })).toEqual({ kind: "prefill", text: "/fork " });
  });

  test("an unsupported one alerts", () => {
    expect(pick("logout")).toMatchObject({ kind: "alert", title: "/logout is not available" });
  });
});

describe("slashMenuState", () => {
  const state = (text: string, patch: Partial<Parameters<typeof slashMenuState>[0]> = {}) =>
    slashMenuState({ text, agentKind: "codex", commands, connected: true, pinned: false, ...patch });

  test("typing /x opens the filtered list; a space closes it", () => {
    expect(state("").open).toBe(false);
    const typing = state("/re");
    expect(typing).toMatchObject({ slashQueryActive: true, showCommandList: true, showActions: false, open: true });
    expect(typing.visibleCommands.map((c) => c.name)).toEqual(["rename"]);
    expect(state("/rename x").open).toBe(false);
    expect(state("/re\nx").open).toBe(false);
  });

  test("a /query that matches nothing still shows the list, to say so", () => {
    const none = state("/zzz");
    expect(none.showCommandList).toBe(true);
    expect(none.visibleCommands).toEqual([]);
  });

  test("the + button shows the actions and every visible command; a normal draft hides the commands", () => {
    const pinned = state("", { pinned: true });
    expect(pinned).toMatchObject({ showActions: true, showCommandList: true });
    expect(pinned.visibleCommands.map((c) => c.name)).not.toContain("logout");
    expect(state("hello", { pinned: true })).toMatchObject({ showActions: true, showCommandList: false });
  });

  test("not for other agents, and nothing while disconnected", () => {
    expect(state("/re", { agentKind: "claude" })).toMatchObject({ open: false, slashQueryActive: false });
    expect(state("", { agentKind: "claude", pinned: true })).toMatchObject({ showActions: true, showCommandList: false });
    expect(state("/re", { connected: false }).open).toBe(false);
  });
});

describe("groupSlashCommands", () => {
  test("in the Expo order, the rest under Other, empty groups left out", () => {
    const groups = groupSlashCommands(commands);
    expect(groups.map((g) => g.label)).toEqual(["Session", "Navigation", "Project Tools", "Settings", "Management", "Other"]);
    expect(groups.at(-1)!.commands.map((c) => c.name)).toEqual(["mention"]);
    expect(groupSlashCommands([daemon("x", { category: "session" })]).map((g) => g.key)).toEqual(["session"]);
  });
});

describe("insertSkillMention", () => {
  test("adds to a message, replaces an empty draft or a command", () => {
    expect(insertSkillMention("", "deploy")).toBe("$deploy ");
    expect(insertSkillMention("/skills", "deploy")).toBe("$deploy ");
    expect(insertSkillMention("ship it", "deploy")).toBe("ship it $deploy ");
    expect(insertSkillMention("ship it ", "deploy")).toBe("ship it $deploy ");
  });
});
